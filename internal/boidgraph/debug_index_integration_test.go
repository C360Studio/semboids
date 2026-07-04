//go:build integration

package boidgraph_test

import (
	"context"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"log/slog"
)

// TestDebugDumpOutgoingIndex is a diagnostic (not a spec assertion): publish
// the two disjoint clusters and dump what graph-index actually writes.
func TestDebugDumpOutgoingIndex(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithE2EDefaults(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	payloadReg := payloadregistry.New()
	_ = payloadbuiltins.Register(payloadReg)
	registry := component.NewRegistry()
	_ = graphingest.Register(registry)
	_ = graphindex.Register(registry)
	_ = graphclustering.Register(registry)
	deps := component.Dependencies{
		NATSClient: tc.Client, Logger: slog.Default(),
		Platform:        component.PlatformMeta{Org: "c360", Platform: "semboids"},
		PayloadRegistry: payloadReg,
	}

	startComponent(t, ctx, registry, deps, "gi", "graph-ingest", map[string]any{
		"ports": map[string]any{
			"inputs":  []map[string]any{{"name": "entity_stream", "subject": "entity.>", "type": "jetstream", "stream_name": "ENTITY"}},
			"outputs": []map[string]any{{"name": "entity_states", "type": "kv-write", "subject": "ENTITY_STATES"}},
		},
	})
	startComponent(t, ctx, registry, deps, "gx", "graph-index", map[string]any{
		"ports": map[string]any{
			"inputs": []map[string]any{{"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}},
			"outputs": []map[string]any{
				{"name": "outgoing_index", "type": "kv-write", "subject": "OUTGOING_INDEX"},
				{"name": "incoming_index", "type": "kv-write", "subject": "INCOMING_INDEX"},
				{"name": "alias_index", "type": "kv-write", "subject": "ALIAS_INDEX"},
				{"name": "predicate_index", "type": "kv-write", "subject": "PREDICATE_INDEX"},
			},
		},
	})

	startComponent(t, ctx, registry, deps, "gc", "graph-clustering", map[string]any{
		"detection_interval": "2s",
		"batch_size":         1,
		"min_community_size": 3,
		"enable_llm":         false,
		"ports": map[string]any{
			"inputs":  []map[string]any{{"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"}},
			"outputs": []map[string]any{{"name": "communities", "type": "kv-write", "subject": "COMMUNITY_INDEX"}},
		},
	})

	publishTwoClusters(t, ctx, tc)

	bucket, err := tc.Client.WaitForBucket(ctx, "OUTGOING_INDEX", 30*time.Second)
	if err != nil {
		t.Fatalf("OUTGOING_INDEX: %v", err)
	}
	time.Sleep(8 * time.Second) // let indexing + two detection runs settle
	keys, _ := bucket.Keys(ctx)
	t.Logf("OUTGOING_INDEX keys (%d):", len(keys))
	for _, k := range keys {
		entry, err := bucket.Get(ctx, k)
		if err != nil {
			continue
		}
		t.Logf("  %s = %s", k, truncate(string(entry.Value()), 400))
	}

	// Is ENTITY_STATES polluted with community entities after detection?
	es, err := tc.Client.WaitForBucket(ctx, "ENTITY_STATES", 10*time.Second)
	if err == nil {
		esKeys, _ := es.Keys(ctx)
		t.Logf("ENTITY_STATES keys (%d): %v", len(esKeys), esKeys)
		for _, k := range []string{boidID(0), boidID(4)} {
			if entry, err := es.Get(ctx, k); err == nil {
				t.Logf("ENTITY %s = %s", k, truncate(string(entry.Value()), 900))
			}
		}
	}

	// Incoming index — the other half of LPA adjacency.
	if inc, err := tc.Client.WaitForBucket(ctx, "INCOMING_INDEX", 10*time.Second); err == nil {
		incKeys, _ := inc.Keys(ctx)
		t.Logf("INCOMING_INDEX keys (%d):", len(incKeys))
		for _, k := range incKeys {
			if entry, err := inc.Get(ctx, k); err == nil {
				t.Logf("  %s = %s", k, truncate(string(entry.Value()), 400))
			}
		}
	}

	// Raw COMMUNITY_INDEX entries: keys + full values. Poll until populated.
	ci, err := tc.Client.WaitForBucket(ctx, "COMMUNITY_INDEX", 20*time.Second)
	if err != nil {
		t.Fatalf("COMMUNITY_INDEX: %v", err)
	}
	deadline := time.After(45 * time.Second)
	for {
		ciKeys, _ := ci.Keys(ctx)
		if len(ciKeys) > 0 {
			t.Logf("COMMUNITY_INDEX keys (%d):", len(ciKeys))
			for _, k := range ciKeys {
				entry, err := ci.Get(ctx, k)
				if err != nil {
					continue
				}
				t.Logf("  %s = %s", k, truncate(string(entry.Value()), 600))
			}
			return
		}
		select {
		case <-deadline:
			t.Log("COMMUNITY_INDEX never populated")
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

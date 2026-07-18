//go:build integration

// Package boidgraph_test holds the spike-1.2 integration wiring: real
// graph-ingest + graph-index + graph-clustering over hand-published boid
// entities, asserting LPA separates two disjoint neighbor clusters into
// distinct communities in COMMUNITY_INDEX. Reused as the substrate half of
// the full-chain test (task 3.4).
package boidgraph_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/types"
)

func boidID(n int) string {
	return fmt.Sprintf("c360.semboids.sim.flock.boid.%d", n)
}

// startComponent creates and starts a lifecycle component, failing the test
// on any error and registering cleanup.
func startComponent(
	t *testing.T, ctx context.Context, registry *component.Registry,
	deps component.Dependencies, instance, factory string, cfg map[string]any,
) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal %s config: %v", instance, err)
	}
	inst, err := registry.CreateComponent(instance, types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: factory, Enabled: true, Config: raw,
	}, deps)
	if err != nil {
		t.Fatalf("create %s: %v", instance, err)
	}
	lc, ok := component.AsLifecycleComponent(inst)
	if !ok {
		t.Fatalf("%s is not a lifecycle component", instance)
	}
	if err := lc.Initialize(); err != nil {
		t.Fatalf("initialize %s: %v", instance, err)
	}
	if err := lc.Start(ctx); err != nil {
		t.Fatalf("start %s: %v", instance, err)
	}
	t.Cleanup(func() { _ = lc.Stop(5 * time.Second) })
}

// publishTwoClusters creates two fully-connected 4-boid clusters with no
// cross edges (boids 0-3 and 4-7) via the mutation API.
func publishTwoClusters(t *testing.T, ctx context.Context, tc *natsclient.TestClient) {
	t.Helper()
	publishCluster := func(members []int) {
		for _, m := range members {
			var triples []map[string]any
			triples = append(triples,
				map[string]any{"subject": boidID(m), "predicate": "flock.position.x",
					"object": float64(100 * m), "source": "spike", "confidence": 1.0},
			)
			for _, other := range members {
				if other == m {
					continue
				}
				triples = append(triples, map[string]any{
					"subject": boidID(m), "predicate": "flock.neighbor.of",
					"object": boidID(other), "source": "spike", "confidence": 1.0,
				})
			}
			req := map[string]any{
				"entity": map[string]any{
					"id":           boidID(m),
					"message_type": map[string]any{"domain": "boids", "category": "boid", "version": "v1"},
				},
				"triples": triples,
			}
			data, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal create request: %v", err)
			}
			resp, err := tc.Client.Request(ctx, "graph.mutation.entity.create_with_triples", data, 10*time.Second)
			if err != nil {
				t.Fatalf("create boid %d: %v", m, err)
			}
			var result struct {
				Success bool   `json:"success"`
				Error   string `json:"error"`
			}
			if err := json.Unmarshal(resp, &result); err == nil && !result.Success && result.Error != "" {
				t.Fatalf("create boid %d rejected: %s", m, result.Error)
			}
		}
	}
	publishCluster([]int{0, 1, 2, 3})
	publishCluster([]int{4, 5, 6, 7})
}

// TestLPADistinguishesDisjointFlocks is the spike-1.2 assertion: two
// fully-connected 4-boid clusters with no cross edges must land in
// different communities.
func TestLPADistinguishesDisjointFlocks(t *testing.T) {
	// semstreams#461 (fixed in beta.136): virtual sibling/system-peer edges
	// must be disabled via entity_id_edges for explicit-topology detection —
	// with them on, all same-type entities merge into one community.
	tc := natsclient.NewTestClient(t,
		natsclient.WithE2EDefaults(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	payloadReg := payloadregistry.New()
	if err := payloadbuiltins.Register(payloadReg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}

	registry := component.NewRegistry()
	for name, register := range map[string]func(*component.Registry) error{
		"graph-ingest":     graphingest.Register,
		"graph-index":      graphindex.Register,
		"graph-clustering": graphclustering.Register,
	} {
		if err := register(registry); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	deps := component.Dependencies{
		NATSClient:      tc.Client,
		Logger:          slog.Default(),
		Platform:        component.PlatformMeta{Org: "c360", Platform: "semboids"},
		PayloadRegistry: payloadReg,
	}

	startComponent(t, ctx, registry, deps, "graph-ingest-t", "graph-ingest", map[string]any{
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "entity_stream", "subject": "entity.>", "type": "jetstream", "stream_name": "ENTITY"},
			},
			"outputs": []map[string]any{
				{"name": "entity_states", "type": "kv-write", "subject": "ENTITY_STATES"},
			},
		},
	})
	startComponent(t, ctx, registry, deps, "graph-index-t", "graph-index", map[string]any{
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"},
			},
			"outputs": []map[string]any{
				{"name": "outgoing_index", "type": "kv-write", "subject": "OUTGOING_INDEX"},
				{"name": "incoming_index", "type": "kv-write", "subject": "INCOMING_INDEX"},
				{"name": "alias_index", "type": "kv-write", "subject": "ALIAS_INDEX"},
				{"name": "predicate_index", "type": "kv-write", "subject": "PREDICATE_INDEX"},
			},
		},
	})
	startComponent(t, ctx, registry, deps, "graph-clustering-t", "graph-clustering", map[string]any{
		"detection_interval": "2s",
		"batch_size":         1,
		"min_community_size": 3,
		"enable_llm":         false,
		"entity_id_edges": map[string]any{
			"include_siblings":     false,
			"include_system_peers": false,
		},
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "entity_watch", "type": "kv-watch", "subject": "ENTITY_STATES"},
			},
			"outputs": []map[string]any{
				{"name": "communities", "type": "kv-write", "subject": "COMMUNITY_INDEX"},
			},
		},
	})

	publishTwoClusters(t, ctx, tc)

	// Poll COMMUNITY_INDEX for two disjoint communities.
	bucket, err := tc.Client.WaitForBucket(ctx, "COMMUNITY_INDEX", 60*time.Second)
	if err != nil {
		t.Fatalf("COMMUNITY_INDEX bucket: %v", err)
	}

	deadline := time.After(60 * time.Second)
	for {
		// Communities are hierarchical (Level 0 = bottom); higher levels
		// legitimately contain everything. Flock membership is level 0.
		communityOf := map[string]string{}
		keys, _ := bucket.Keys(ctx)
		for _, key := range keys {
			entry, err := bucket.Get(ctx, key)
			if err != nil {
				continue
			}
			var c struct {
				ID      string   `json:"id"`
				Level   int      `json:"level"`
				Members []string `json:"members"`
			}
			if err := json.Unmarshal(entry.Value(), &c); err != nil {
				continue
			}
			if c.Level != 0 {
				continue
			}
			for _, m := range c.Members {
				communityOf[m] = c.ID
			}
		}

		if len(communityOf) >= 8 {
			a := communityOf[boidID(0)]
			b := communityOf[boidID(4)]
			sameA := communityOf[boidID(1)] == a && communityOf[boidID(2)] == a && communityOf[boidID(3)] == a
			sameB := communityOf[boidID(5)] == b && communityOf[boidID(6)] == b && communityOf[boidID(7)] == b
			if a != "" && b != "" && a != b && sameA && sameB {
				t.Logf("spike confirmed: cluster A → %s, cluster B → %s", a, b)
				return
			}
			t.Logf("assignments not yet separated: %v", communityOf)
		}

		select {
		case <-deadline:
			t.Fatalf("LPA never separated the clusters; last assignments: %v", communityOf)
		case <-time.After(1 * time.Second):
		}
	}
}

//go:build integration

package boidgraph_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"

	"github.com/c360studio/semboids/internal/boidgraph"
)

// TestNeighborEmptyGate is a durable substrate-contract guard for the
// neighbor-empty workaround (prevHadNeighbors + removeNeighborTriples in
// publisher.go).
//
// It pins the merge-vs-replace semantics that make that workaround necessary:
// when a boid snapshot with an EMPTY neighbor set is published through the
// ordinary stream-upsert path (entity.boid.upsert -> graph-ingest MergeEntity),
// the boid's stale flock.neighbor.of edges DO NOT clear — neither in
// ENTITY_STATES (what the graph pane reads via api/graphstream.go) nor in the
// derived INCOMING index — because MergeTriples preserves predicates the
// arrival does not carry (correct multi-writer behavior). Only the explicit
// graph.mutation.triple.remove clears them.
//
// Verified on beta.152 (2026-07-19); the retire question is tracked upstream as
// C360Studio/semstreams#578 (opt-in source-authoritative predicate replacement).
// If a future substrate bump ever clears empties on the stream path, the final
// assertion here fails loudly and re-opens the decision to retire the workaround.
//
// The publish here is deliberately the RAW stream path (PublishToStreamWithAck),
// bypassing Publisher.removeNeighborTriples, so it measures the substrate's
// merge semantics alone.
func TestNeighborEmptyGate(t *testing.T) {
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
	if err := boidgraph.RegisterPayloads(payloadReg); err != nil {
		t.Fatalf("register boid payloads: %v", err)
	}

	registry := component.NewRegistry()
	if err := graphingest.Register(registry); err != nil {
		t.Fatalf("register graph-ingest: %v", err)
	}
	if err := graphindex.Register(registry); err != nil {
		t.Fatalf("register graph-index: %v", err)
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

	const org, platform = "c360", "semboids"
	boid0 := boidgraph.BoidEntityID(org, platform, 0)

	// publishBoid sends one boid snapshot through the stream-upsert path with
	// the given neighbor IDs (nil => empty neighbor set), waiting for the ack.
	publishBoid := func(id uint32, neighbors []uint32) {
		t.Helper()
		e := &boidgraph.Entity{
			Boid: boidgraph.BoidState{
				ID: id, X: float64(id) * 10, Y: float64(id) * 5,
				VX: 1, VY: -1, Neighbors: neighbors,
			},
			OrgID: org, Platform: platform, Tick: 1, ObservedAt: time.Now(),
		}
		baseMsg := message.NewBaseMessage(e.Schema(), e, "semboids-sim")
		data, err := json.Marshal(baseMsg)
		if err != nil {
			t.Fatalf("marshal boid %d: %v", id, err)
		}
		if _, err := tc.Client.PublishToStreamWithAck(ctx, boidgraph.IngestSubject, data); err != nil {
			t.Fatalf("publish boid %d: %v", id, err)
		}
	}

	esBucket, err := tc.Client.WaitForBucket(ctx, "ENTITY_STATES", 30*time.Second)
	if err != nil {
		t.Fatalf("ENTITY_STATES: %v", err)
	}

	type entityState struct {
		Version uint64 `json:"version"`
		Triples []struct {
			Predicate string `json:"predicate"`
			Object    any    `json:"object"`
		} `json:"triples"`
	}
	// neighborOfCount returns (version, count of flock.neighbor.of triples, ok).
	neighborOfCount := func(id string) (uint64, int, bool) {
		entry, err := esBucket.Get(ctx, id)
		if err != nil {
			return 0, 0, false
		}
		var es entityState
		if err := json.Unmarshal(entry.Value(), &es); err != nil {
			return 0, 0, false
		}
		n := 0
		for _, tr := range es.Triples {
			if tr.Predicate == "flock.neighbor.of" {
				n++
			}
		}
		return es.Version, n, true
	}

	// incomingRowsForSource counts INCOMING index rows asserted BY boid 0.
	// ADR-077 key shape is target6.source6.hex(predicate): 6 + 6 + 1 = 13
	// dot-separated tokens, with the source ID in positions [6:12]. Match on
	// that exact run so a boid appearing in the TARGET position is not counted.
	incomingRowsForSource := func(sourceID string) int {
		inc, err := tc.Client.WaitForBucket(ctx, "INCOMING_INDEX", 10*time.Second)
		if err != nil {
			return -1
		}
		keys, err := inc.Keys(ctx)
		if err != nil {
			return -1
		}
		n := 0
		for _, k := range keys {
			tokens := strings.Split(k, ".")
			if len(tokens) < 12 {
				continue
			}
			if strings.Join(tokens[6:12], ".") == sourceID {
				n++
			}
		}
		return n
	}

	waitVersionAtLeast := func(id string, want uint64) (uint64, int) {
		t.Helper()
		deadline := time.After(30 * time.Second)
		for {
			if v, n, ok := neighborOfCount(id); ok && v >= want {
				return v, n
			}
			select {
			case <-deadline:
				t.Fatalf("boid %s never reached version %d", id, want)
			case <-time.After(200 * time.Millisecond):
			}
		}
	}

	// --- Step 1: publish boid 0 WITH neighbors {1,2,3}; confirm they land. ---
	publishBoid(0, []uint32{1, 2, 3})
	v1, n1 := waitVersionAtLeast(boid0, 1)
	if n1 != 3 {
		t.Fatalf("after non-empty publish: flock.neighbor.of = %d in ENTITY_STATES, want 3", n1)
	}
	time.Sleep(2 * time.Second) // let graph-index project
	incBefore := incomingRowsForSource(boid0)
	t.Logf("STEP 1 (neighbors {1,2,3}): version=%d  ENTITY_STATES flock.neighbor.of=%d  INCOMING rows for source=%d",
		v1, n1, incBefore)

	// --- Step 2: republish boid 0 with an EMPTY neighbor set via the stream. ---
	publishBoid(0, nil)
	v2, n2 := waitVersionAtLeast(boid0, v1+1)
	time.Sleep(2 * time.Second) // let graph-index re-project
	incAfterStream := incomingRowsForSource(boid0)
	t.Logf("STEP 2 (empty via stream MergeEntity): version=%d  ENTITY_STATES flock.neighbor.of=%d  INCOMING rows for source=%d",
		v2, n2, incAfterStream)

	streamCleared := n2 == 0
	if streamCleared {
		t.Logf("GATE RESULT: stream MergeEntity CLEARS an emptied neighbor set on beta.152 — workaround CAN be retired.")
	} else {
		t.Logf("GATE RESULT: stream MergeEntity does NOT clear an emptied neighbor set (%d stale flock.neighbor.of remain in ENTITY_STATES) — the app-side removal is still load-bearing.", n2)
	}

	// --- Step 3: confirm the workaround's remedy (triple.remove) DOES clear,
	// end to end (ENTITY_STATES + INCOMING) on beta.152. ---
	rmReq, _ := json.Marshal(map[string]any{
		"subject":   boid0,
		"predicate": "flock.neighbor.of",
	})
	if _, err := tc.Client.Request(ctx, "graph.mutation.triple.remove", rmReq, 5*time.Second); err != nil {
		t.Fatalf("triple.remove: %v", err)
	}
	v3, n3 := waitVersionAtLeast(boid0, v2+1)
	time.Sleep(2 * time.Second)
	incAfterRemove := incomingRowsForSource(boid0)
	t.Logf("STEP 3 (graph.mutation.triple.remove): version=%d  ENTITY_STATES flock.neighbor.of=%d  INCOMING rows for source=%d",
		v3, n3, incAfterRemove)
	if n3 != 0 {
		t.Fatalf("after triple.remove: flock.neighbor.of = %d in ENTITY_STATES, want 0 (the workaround remedy must clear)", n3)
	}

	// Assert the expected (source-derived) finding so a substrate change that
	// flips it fails loudly and re-opens the gate.
	if streamCleared {
		t.Errorf("UNEXPECTED: stream MergeEntity cleared the empty neighbor set — re-run the gate decision; the workaround may be retireable via the stream path.")
	}
}

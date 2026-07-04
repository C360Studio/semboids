//go:build integration

package boidgraph_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/types"
	natspkg "github.com/nats-io/nats.go"

	"github.com/c360studio/semboids/internal/boidgraph"
	simpkg "github.com/c360studio/semboids/internal/sim"
)

// TestSnapshotsLandAndReplace runs the real sim (dial on) against real
// graph-ingest: boid entities land in ENTITY_STATES with neighbor state,
// subsequent snapshots update in place (version advances, no triple
// accumulation), and frames keep flowing at tick rate while the graph
// pipeline works — the ADR-001 isolation, observed end to end.
func TestSnapshotsLandAndReplace(t *testing.T) {
	// BLOCKED upstream: graph-ingest's MergeEntity raw-appends triples
	// (component.go:1802) instead of predicate-level replacement, so
	// repeated snapshots grow entities unboundedly. Acceptance criterion
	// for the load dial — unskip when
	// https://github.com/C360Studio/semstreams/issues/466 lands.
	t.Skip("blocked on semstreams#466 (MergeEntity appends without predicate replacement)")

	tc := natsclient.NewTestClient(t,
		natsclient.WithE2EDefaults(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
	if err := simpkg.Register(registry); err != nil {
		t.Fatalf("register sim: %v", err)
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

	// Frames observer: proves physics cadence while the graph pipeline runs.
	frames := 0
	frameSub, err := tc.Client.Subscribe(ctx, "boids.frames", func(_ context.Context, _ *natspkg.Msg) {
		frames++
	})
	if err != nil {
		t.Fatalf("subscribe frames: %v", err)
	}
	defer func() { _ = frameSub.Unsubscribe() }()

	simCfgJSON, _ := json.Marshal(map[string]any{
		"boids": 20, "tick_hz": 30, "seed": 7,
		"graph_hz": 5,
	})
	simInst, err := registry.CreateComponent("sim-t", types.ComponentConfig{
		Type: types.ComponentTypeInput, Name: "sim", Enabled: true, Config: simCfgJSON,
	}, deps)
	if err != nil {
		t.Fatalf("create sim: %v", err)
	}
	simLC, _ := component.AsLifecycleComponent(simInst)
	if err := simLC.Initialize(); err != nil {
		t.Fatalf("sim initialize: %v", err)
	}
	if err := simLC.Start(ctx); err != nil {
		t.Fatalf("sim start: %v", err)
	}
	t.Cleanup(func() { _ = simLC.Stop(5 * time.Second) })

	bucket, err := tc.Client.WaitForBucket(ctx, "ENTITY_STATES", 30*time.Second)
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
	getBoid := func(id string) (entityState, bool) {
		entry, err := bucket.Get(ctx, id)
		if err != nil {
			return entityState{}, false
		}
		var es entityState
		if err := json.Unmarshal(entry.Value(), &es); err != nil {
			return entityState{}, false
		}
		return es, true
	}

	// Wait for the first landing.
	wantID := "c360.semboids.sim.flock.boid.0"
	deadline := time.After(30 * time.Second)
	var first entityState
	for {
		if es, ok := getBoid(wantID); ok {
			first = es
			break
		}
		select {
		case <-deadline:
			t.Fatal("boid entity never landed in ENTITY_STATES")
		case <-time.After(200 * time.Millisecond):
		}
	}

	// Wait for an update (version advances at ~5Hz snapshots).
	deadline = time.After(30 * time.Second)
	var second entityState
	for {
		if es, ok := getBoid(wantID); ok && es.Version > first.Version {
			second = es
			break
		}
		select {
		case <-deadline:
			t.Fatal("boid entity never updated (second snapshot missing)")
		case <-time.After(200 * time.Millisecond):
		}
	}

	// Triple hygiene: exactly one of each position/velocity/count property,
	// and neighbor relationships bounded by the population (no union growth).
	counts := map[string]int{}
	for _, tr := range second.Triples {
		counts[tr.Predicate]++
	}
	for _, p := range []string{"flock.position.x", "flock.position.y",
		"flock.velocity.x", "flock.velocity.y", "flock.neighbor.count"} {
		if counts[p] != 1 {
			t.Fatalf("predicate %s count = %d, want exactly 1 (merge must replace)", p, counts[p])
		}
	}
	if counts["flock.neighbor"] >= 20 {
		t.Fatalf("flock.neighbor triples = %d — accumulating instead of replacing", counts["flock.neighbor"])
	}

	// Physics cadence held while the graph pipeline ran: ≥ 20 frames/s
	// observed over the test window (30Hz nominal, generous CI margin).
	if frames < 40 {
		t.Fatalf("only %d frames observed — physics cadence suffered", frames)
	}
}

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
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/projection"
	graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/types"
	"github.com/c360studio/semstreams/vocabulary"

	"github.com/c360studio/semboids/internal/boidgraph"
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

// spikeContract declares the fixture's write intent: strict-create boids
// carrying position + neighbor facts. Test-local on purpose — the production
// semboids-neighbors contract covers only the reconcile group, and the spike's
// birth shape should not warp it.
func spikeContract() projection.Contract {
	vocabulary.Register("flock.position.x",
		vocabulary.WithDescription("Boid x position at snapshot time (spike fixture)"),
		vocabulary.WithDataType("number"))
	return projection.Contract{
		Name:            "spike-boids",
		EntityPattern:   boidgraph.BoidEntityIDPattern,
		BirthPredicates: []string{"flock.position.x", boidgraph.NeighborPredicate},
	}
}

// publishTwoClusters creates two fully-connected 4-boid clusters with no
// cross edges (boids 0-3 and 4-7) via the typed mutation client (beta.160:
// the create_with_triples wire op is retired; entity.create is strict).
func publishTwoClusters(t *testing.T, ctx context.Context, tc *natsclient.TestClient) {
	t.Helper()
	mutations, err := projection.NewMutationClient(projection.MutationClientConfig{
		NATS:      tc.Client,
		Contracts: []projection.Contract{spikeContract()},
	})
	if err != nil {
		t.Fatalf("build mutation client: %v", err)
	}
	publishCluster := func(members []int) {
		for _, m := range members {
			now := time.Now()
			mk := func(predicate string, object any) message.Triple {
				return message.Triple{
					Subject: boidID(m), Predicate: predicate, Object: object,
					Source: "spike", Timestamp: now, Confidence: 1.0,
				}
			}
			triples := []message.Triple{mk("flock.position.x", float64(100*m))}
			for _, other := range members {
				if other == m {
					continue
				}
				triples = append(triples, mk(boidgraph.NeighborPredicate, boidID(other)))
			}
			_, err := mutations.Create(ctx, projection.CreateMutation{
				Contract: "spike-boids",
				Entity: &graph.EntityState{
					ID:          boidID(m),
					MessageType: message.Type{Domain: "boids", Category: "boid", Version: "v1"},
				},
				Triples: triples,
				Metadata: projection.MutationMetadata{
					RequestID: fmt.Sprintf("spike-create-%d", m),
					Source:    "spike",
					Timestamp: now,
				},
			})
			if err != nil {
				t.Fatalf("create boid %d: %v", m, err)
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
				{"name": "entity_stream", "config": map[string]any{"kind": "jetstream", "stream_name": "ENTITY", "subjects": []any{"entity.>"}}},
				{"name": "graph_mutations", "required": true, "config": map[string]any{"kind": "nats-request", "subject": "graph.mutation.>", "interface": map[string]any{"type": "semstreams.graph.mutation", "version": "v1"}}},
			},
			"outputs": []map[string]any{
				{"name": "entity_states", "config": map[string]any{"kind": "kv-write", "bucket": "ENTITY_STATES"}},
			},
		},
	})
	startComponent(t, ctx, registry, deps, "graph-index-t", "graph-index", map[string]any{
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "entity_watch", "config": map[string]any{"kind": "kv-watch", "bucket": "ENTITY_STATES"}},
			},
			"outputs": []map[string]any{
				{"name": "outgoing_index", "config": map[string]any{"kind": "kv-write", "bucket": "OUTGOING_INDEX"}},
				{"name": "incoming_index", "config": map[string]any{"kind": "kv-write", "bucket": "INCOMING_INDEX"}},
				{"name": "alias_index", "config": map[string]any{"kind": "kv-write", "bucket": "ALIAS_INDEX"}},
				{"name": "predicate_index", "config": map[string]any{"kind": "kv-write", "bucket": "PREDICATE_INDEX"}},
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
				{"name": "entity_watch", "config": map[string]any{"kind": "kv-watch", "bucket": "ENTITY_STATES"}},
				{"name": "entity_states", "config": map[string]any{"kind": "kv-read", "bucket": "ENTITY_STATES"}},
				{"name": "outgoing_index", "config": map[string]any{"kind": "kv-read", "bucket": "OUTGOING_INDEX"}},
				{"name": "incoming_index", "config": map[string]any{"kind": "kv-read", "bucket": "INCOMING_INDEX"}},
			},
			"outputs": []map[string]any{
				{"name": "graph_mutations", "required": true, "config": map[string]any{"kind": "nats-request", "subject": "graph.mutation.>", "interface": map[string]any{"type": "semstreams.graph.mutation", "version": "v1"}}},
				{"name": "communities", "config": map[string]any{"kind": "kv-write", "bucket": "COMMUNITY_INDEX"}},
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

//go:build integration

package boidgraph_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/projection"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semboids/internal/boidgraph"
)

type migrationGraph struct {
	client    *natsclient.Client
	mutations *projection.MutationClient
	manager   *lifecycle.Manager
	publisher *boidgraph.Publisher
}

func newMigrationGraph(t *testing.T, ctx context.Context) migrationGraph {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithE2EDefaults(), natsclient.WithStreams(natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}}))
	reg := payloadregistry.New()
	if err := payloadbuiltins.Register(reg); err != nil {
		t.Fatal(err)
	}
	if err := boidgraph.RegisterPayloads(reg); err != nil {
		t.Fatal(err)
	}
	registry := component.NewRegistry()
	if err := graphingest.Register(registry); err != nil {
		t.Fatal(err)
	}
	if err := graphindex.Register(registry); err != nil {
		t.Fatal(err)
	}
	deps := component.Dependencies{NATSClient: tc.Client, PayloadRegistry: reg, Logger: slog.Default(), Platform: component.PlatformMeta{Org: "c360", Platform: "semboids"}}
	startComponent(t, ctx, registry, deps, "ingest", "graph-ingest", map[string]any{"ports": map[string]any{
		"inputs": []map[string]any{
			{"name": "entity_stream", "config": map[string]any{"kind": "jetstream", "stream_name": "ENTITY", "subjects": []string{"entity.>"}}},
			{"name": "graph_mutations", "required": true, "config": map[string]any{"kind": "nats-request", "subject": "graph.mutation.>", "interface": map[string]any{"type": "semstreams.graph.mutation", "version": "v1"}}},
		}, "outputs": []map[string]any{{"name": "entity_states", "config": map[string]any{"kind": "kv-write", "bucket": "ENTITY_STATES"}}},
	}})
	startComponent(t, ctx, registry, deps, "index", "graph-index", map[string]any{})
	mutations, err := projection.NewMutationClient(projection.MutationClientConfig{NATS: tc.Client, Contracts: []projection.Contract{boidgraph.NeighborContract()}})
	if err != nil {
		t.Fatal(err)
	}
	mgr := lifecycle.NewManager(tc.Client, slog.Default())
	if err := mgr.Register(boidgraph.BoidWorkflow()); err != nil {
		t.Fatal(err)
	}
	pub := boidgraph.NewPublisher(tc.Client, mutations, "c360", "semboids", nil, slog.Default())
	pubctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); pub.Run(pubctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("publisher did not join")
		}
	})
	return migrationGraph{client: tc.Client, mutations: mutations, manager: mgr, publisher: pub}
}

func eventuallyGraph(t *testing.T, ctx context.Context, reason string, ready func() bool) {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v", reason, ctx.Err())
		case <-ticker.C:
		}
	}
}

func entityFacts(t *testing.T, ctx context.Context, g migrationGraph, id uint32) (map[string][]any, uint64) {
	t.Helper()
	exact, err := g.mutations.ReadAuthoritative(ctx, boidgraph.BoidEntityID("c360", "semboids", id))
	if err != nil {
		return nil, 0
	}
	facts := map[string][]any{}
	for _, tr := range exact.Entity.Triples {
		facts[tr.Predicate] = append(facts[tr.Predicate], tr.Object)
	}
	return facts, exact.KVRevision
}

func lifecycleFacts(facts map[string][]any) map[string][]any {
	result := make(map[string][]any)
	for predicate, values := range facts {
		if predicate == boidgraph.BoidPhasePredicate || strings.HasPrefix(predicate, "lifecycle.") {
			result[predicate] = values
		}
	}
	return result
}

// Independent writers share one entity. A snapshot and neighbor-group clear
// must preserve lifecycle phase, including a terminal phase, and replace edges.
func TestSnapshotsPreserveLifecycleFactsAndRetractDeletedNeighbors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g := newMigrationGraph(t, ctx)
	id0, id1 := boidgraph.BoidEntityID("c360", "semboids", 0), boidgraph.BoidEntityID("c360", "semboids", 1)
	if err := g.manager.Create(ctx, boidgraph.NewBoidLifecycle(id0)); err != nil {
		t.Fatal(err)
	}
	activeBefore, _ := entityFacts(t, ctx, g, 0)
	publish := func(tick uint64, ns []uint32, includeTarget bool) {
		t.Helper()
		boids := []boidgraph.BoidState{{ID: 0, X: float64(tick), Neighbors: ns}, {ID: 2, X: float64(tick)}}
		if includeTarget {
			boids = append(boids, boidgraph.BoidState{ID: 1, X: float64(tick), Neighbors: []uint32{0}})
		}
		if !g.publisher.Offer(boidgraph.Snapshot{Tick: tick, At: time.Now(), Boids: boids}) {
			t.Fatal("unexpected snapshot drop")
		}
		eventuallyGraph(t, ctx, "snapshot applied", func() bool {
			f, _ := entityFacts(t, ctx, g, 0)
			return reflect.DeepEqual(f["flock.position.x"], []any{float64(tick)})
		})
	}
	publish(1, []uint32{1}, true)
	for _, bucket := range []string{"INCOMING_INDEX", "OUTGOING_INDEX"} {
		kv, err := g.client.WaitForBucket(ctx, bucket, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		eventuallyGraph(t, ctx, bucket+" observes fixture before removal", func() bool {
			keys, err := kv.Keys(ctx)
			if err != nil {
				return false
			}
			for _, key := range keys {
				entry, err := kv.Get(ctx, key)
				if err == nil && (strings.Contains(key, id1) || strings.Contains(string(entry.Value()), id1)) {
					return true
				}
			}
			return false
		})
	}
	facts, _ := entityFacts(t, ctx, g, 0)
	if !reflect.DeepEqual(lifecycleFacts(facts), lifecycleFacts(activeBefore)) {
		t.Fatalf("snapshot lost active lifecycle facts: %v", facts)
	}
	if err := g.manager.Transition(ctx, boidgraph.BoidWorkflowName, id0, boidgraph.PhaseExpired, lifecycle.TransitionSourceComponent, "migration fixture"); err != nil {
		t.Fatal(err)
	}
	terminalBefore, _ := entityFacts(t, ctx, g, 0)
	publish(2, []uint32{1}, true)
	facts, _ = entityFacts(t, ctx, g, 0)
	if !reflect.DeepEqual(lifecycleFacts(facts), lifecycleFacts(terminalBefore)) {
		t.Fatalf("snapshot lost terminal lifecycle facts: %v", facts)
	}
	exact, err := g.mutations.ReadAuthoritative(ctx, id1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.mutations.Delete(ctx, projection.DeleteMutation{EntityID: id1, ExpectedRevision: exact.KVRevision}); err != nil {
		t.Fatal(err)
	}
	publish(3, []uint32{2}, false)
	facts, _ = entityFacts(t, ctx, g, 0)
	if !reflect.DeepEqual(facts[boidgraph.NeighborPredicate], []any{boidgraph.BoidEntityID("c360", "semboids", 2)}) {
		t.Fatalf("replacement retained old neighbors: %v", facts)
	}
	publish(4, nil, false)
	eventuallyGraph(t, ctx, "empty neighbors preserve terminal phase", func() bool {
		f, _ := entityFacts(t, ctx, g, 0)
		return len(f[boidgraph.NeighborPredicate]) == 0 && reflect.DeepEqual(lifecycleFacts(f), lifecycleFacts(terminalBefore))
	})
	// Each incoming row belongs to its source. After the final survivor snapshot,
	// neither side may retain an assertion involving the deleted boid.
	for _, bucket := range []string{"INCOMING_INDEX", "OUTGOING_INDEX"} {
		kv, err := g.client.WaitForBucket(ctx, bucket, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		eventuallyGraph(t, ctx, bucket+" retracts deleted boid", func() bool {
			keys, err := kv.Keys(ctx)
			if errors.Is(err, jetstream.ErrNoKeysFound) {
				return true
			}
			if err != nil {
				return false
			}
			for _, key := range keys {
				entry, err := kv.Get(ctx, key)
				if err != nil {
					return false
				}
				if strings.Contains(key, id1) || strings.Contains(string(entry.Value()), id1) {
					return false
				}
			}
			return true
		})
	}
	if _, err := g.mutations.ReadAuthoritative(ctx, id1); err == nil {
		t.Fatal("deleted boid remains after final snapshots")
	}
}

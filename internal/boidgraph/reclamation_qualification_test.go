//go:build integration && qualification

package boidgraph_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/projection"

	"github.com/c360studio/semboids/internal/boidgraph"
)

// This required qualification stays separate from ordinary migration gates.
// The frozen substrate currently permits a late Graphable to recreate a deleted
// entity. Keep the stronger consumer assertion red until that contract is admitted
// upstream; do not hide it behind a consumer tombstone or a skipped test.
func TestReclaimedBoidRejectsDelayedPreCullSnapshot(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	g := newMigrationGraph(t, ctx)
	id := boidgraph.BoidEntityID("c360", "semboids", 1)
	e := &boidgraph.Entity{Boid: boidgraph.BoidState{ID: 1, X: 3, Neighbors: []uint32{0}}, OrgID: "c360", Platform: "semboids", Tick: 1, ObservedAt: time.Now()}
	held, err := json.Marshal(message.NewBaseMessage(e.Schema(), e, "semboids-sim"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.client.PublishToStreamWithAck(ctx, boidgraph.IngestSubject, held); err != nil {
		t.Fatal(err)
	}
	eventuallyGraph(t, ctx, "initial snapshot applied", func() bool { _, rev := entityFacts(t, ctx, g, 1); return rev != 0 })
	exact, err := g.mutations.ReadAuthoritative(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := g.mutations.Delete(ctx, projection.DeleteMutation{EntityID: id, ExpectedRevision: exact.KVRevision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.mutations.ReadAuthoritative(ctx, id); err == nil {
		t.Fatal("delete did not remove entity")
	}
	// Release bytes prepared before reclamation: a queued/in-flight snapshot can
	// outlive the watcher's independent exact-read/delete pair.
	if _, err := g.client.PublishToStreamWithAck(ctx, boidgraph.IngestSubject, held); err != nil {
		t.Fatal(err)
	}
	js, err := g.client.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := js.Consumer(ctx, "ENTITY", "graph-ingest-entity-wildcard")
	if err != nil {
		t.Fatal(err)
	}
	eventuallyGraph(t, ctx, "late snapshot processed", func() bool {
		info, err := consumer.Info(ctx)
		return err == nil && info.NumPending == 0 && info.NumAckPending == 0
	})
	after, err := g.mutations.ReadAuthoritative(ctx, id)
	if err == nil {
		t.Fatalf("reclaimed boid resurrected: deletion receipt revision=%d, late-snapshot revision=%d; held snapshot tick=%d observed_at=%s", receipt.KVRevision, after.KVRevision, e.Tick, e.ObservedAt.Format(time.RFC3339Nano))
	}
	var mutationErr *projection.MutationError
	if !errors.As(err, &mutationErr) || mutationErr.Kind != projection.MutationNotFound {
		t.Fatalf("read outcome unknown: %v", err)
	}
}

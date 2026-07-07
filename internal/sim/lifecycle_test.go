package sim

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semstreams/pkg/lifecycle"

	"github.com/c360studio/semboids/internal/boidgraph"
)

type fakeSpawner struct{ created []lifecycle.Participant }

func (f *fakeSpawner) Create(_ context.Context, p lifecycle.Participant) error {
	f.created = append(f.created, p)
	return nil
}

func TestCreatePendingCreatesActivePerBoid(t *testing.T) {
	fake := &fakeSpawner{}
	c := &Component{
		org:        "c360",
		platform:   "semboids",
		logger:     slog.Default(),
		population: newPopulationState(),
		spawner:    fake,
	}
	c.population.stageCreate([]uint32{1, 2, 3})
	c.createPending(context.Background())

	if len(fake.created) != 3 {
		t.Fatalf("created %d participants, want 3", len(fake.created))
	}
	for _, p := range fake.created {
		if p.Phase() != boidgraph.PhaseActive {
			t.Fatalf("spawned boid phase = %q, want active", p.Phase())
		}
	}
	if fake.created[0].EntityID() != "c360.semboids.sim.flock.boid.1" {
		t.Fatalf("entity id = %q", fake.created[0].EntityID())
	}
	// Drained → a second pass creates nothing.
	c.createPending(context.Background())
	if len(fake.created) != 3 {
		t.Fatal("second pass re-created boids")
	}
}

func entityWithPhase(t *testing.T, phase string) []byte {
	t.Helper()
	es := map[string]any{
		"triples": []map[string]any{
			{"predicate": boidgraph.BoidPhasePredicate, "object": phase},
			{"predicate": "flock.position.x", "object": 1.0},
		},
	}
	data, err := json.Marshal(es)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestCulledBoidID(t *testing.T) {
	const boidKey = "c360.semboids.sim.flock.boid.42"

	if id, ok := culledBoidID(boidKey, entityWithPhase(t, boidgraph.PhaseCulled)); !ok || id != 42 {
		t.Fatalf("culled boid = %d/%v, want 42/true", id, ok)
	}
	if _, ok := culledBoidID(boidKey, entityWithPhase(t, boidgraph.PhaseActive)); ok {
		t.Fatal("active boid must not read as culled")
	}
	if _, ok := culledBoidID("c360.semboids.sim.zones.zone.pred", entityWithPhase(t, boidgraph.PhaseCulled)); ok {
		t.Fatal("non-boid key must be skipped")
	}
	if _, ok := culledBoidID(boidKey, []byte("{malformed")); ok {
		t.Fatal("malformed value must be skipped")
	}
}

func TestBoidIDFromKey(t *testing.T) {
	if id, ok := boidIDFromKey("c360.semboids.sim.flock.boid.7"); !ok || id != 7 {
		t.Fatalf("got %d/%v, want 7/true", id, ok)
	}
	if _, ok := boidIDFromKey("no-dots"); ok {
		t.Fatal("keyless should fail")
	}
	if _, ok := boidIDFromKey("a.b.c.d.e.notanumber"); ok {
		t.Fatal("non-numeric instance should fail")
	}
}

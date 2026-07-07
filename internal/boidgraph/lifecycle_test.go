package boidgraph

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// TestBoidWorkflowRegisters drives the workflow declaration through the real
// Manager.Register path (which validates tags, schema, transitions, and
// predicate disjointness) — Register touches no NATS, so a nil client is fine.
func TestBoidWorkflowRegisters(t *testing.T) {
	mgr := lifecycle.NewManager(nil, nil)
	if err := mgr.Register(BoidWorkflow()); err != nil {
		t.Fatalf("register boid workflow: %v", err)
	}
}

func TestBoidLifecycleParticipant(t *testing.T) {
	var _ lifecycle.Participant = (*BoidLifecycle)(nil)

	b := NewBoidLifecycle("c360.semboids.sim.flock.boid.7")
	if b.EntityID() != "c360.semboids.sim.flock.boid.7" {
		t.Fatalf("entity id = %q", b.EntityID())
	}
	if b.Workflow() != BoidWorkflowName {
		t.Fatalf("workflow = %q, want %q", b.Workflow(), BoidWorkflowName)
	}
	if b.Phase() != PhaseActive || b.IsTerminal() {
		t.Fatalf("new boid = %q terminal=%v, want active non-terminal", b.Phase(), b.IsTerminal())
	}

	b.PhaseField = PhaseCulled
	if !b.IsTerminal() {
		t.Fatal("culled must be terminal")
	}
}

func TestBoidTransitionsValid(t *testing.T) {
	if err := boidTransitions.Validate(); err != nil {
		t.Fatalf("boid transitions invalid: %v", err)
	}
	if !boidTransitions.IsValidTransition(PhaseActive, PhaseCulled) {
		t.Fatal("active→culled should be valid")
	}
	if boidTransitions.IsValidTransition(PhaseCulled, PhaseActive) {
		t.Fatal("culled→active must be rejected (terminal)")
	}
}

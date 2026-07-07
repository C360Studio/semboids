package boidgraph

import (
	"reflect"

	"github.com/c360studio/semstreams/pkg/lifecycle"
)

// Boid lifecycle workflow (add-lifecycle-population): each boid is a per-boid
// lifecycle Participant under one flock.boid workflow. Per ADR-049 lifecycle
// owns no bucket — the phase triple lands in the SAME ENTITY_STATES entity as
// the boid's snapshot triples, so a boid's graph node just gains a phase.
const (
	// BoidWorkflowName is the lifecycle workflow type id (== Participant.Workflow()).
	BoidWorkflowName = "flock.boid"
	// BoidPhasePredicate carries the current phase as a triple. Keep in sync
	// with the `lifecycle:"phase,predicate=…"` struct tag below (tags must be
	// literals).
	BoidPhasePredicate = "flock.lifecycle.phase"
	// BoidEntityIDPattern is the 6-part federated-ID glob for boids —
	// org.platform.sim.flock.boid.instance (org/platform/instance wildcarded).
	BoidEntityIDPattern = "*.*.sim.flock.boid.*"

	// PhaseActive is a live boid; PhaseCulled/PhaseExpired are terminal.
	PhaseActive  = "active"
	PhaseCulled  = "culled"
	PhaseExpired = "expired"
)

// boidTransitions is the boid phase graph: an active boid can be culled (by the
// predator rule) or expire; both terminal.
var boidTransitions = lifecycle.Transitions{
	PhaseActive:  {PhaseCulled, PhaseExpired},
	PhaseCulled:  {},
	PhaseExpired: {},
}

// BoidLifecycle is the boid's lifecycle Participant — id + phase only, since a
// cull is a pure phase move with no operator-writable payload. Follows the
// framework field-vs-method convention (IDField/PhaseField back
// EntityID()/Phase()).
type BoidLifecycle struct {
	IDField    string `json:"entity_id" lifecycle:"id"`
	PhaseField string `json:"phase" lifecycle:"phase,predicate=flock.lifecycle.phase"`
}

// EntityID returns the 6-part federated entity ID.
func (b *BoidLifecycle) EntityID() string { return b.IDField }

// Workflow returns the workflow type id; MUST equal BoidWorkflowName.
func (b *BoidLifecycle) Workflow() string { return BoidWorkflowName }

// Phase returns the current phase.
func (b *BoidLifecycle) Phase() string { return b.PhaseField }

// IsTerminal reports whether the current phase has no out-edges.
func (b *BoidLifecycle) IsTerminal() bool { return boidTransitions.IsTerminal(b.PhaseField) }

// ParentEntityID returns "" — boids are root workflows.
func (b *BoidLifecycle) ParentEntityID() string { return "" }

// NewBoidLifecycle builds an active participant for the boid with the given
// 6-part entity ID (from BoidEntityID).
func NewBoidLifecycle(entityID string) *BoidLifecycle {
	return &BoidLifecycle{IDField: entityID, PhaseField: PhaseActive}
}

// BoidWorkflow returns the lifecycle.Workflow to pass to Manager.Register.
func BoidWorkflow() lifecycle.Workflow {
	return lifecycle.Workflow{
		Name:            BoidWorkflowName,
		EntityIDPattern: BoidEntityIDPattern,
		Phases:          []string{PhaseActive, PhaseCulled, PhaseExpired},
		Transitions:     boidTransitions,
		PhasePredicate:  BoidPhasePredicate,
		Schema:          reflect.TypeOf(BoidLifecycle{}),
	}
}

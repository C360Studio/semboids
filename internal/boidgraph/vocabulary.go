package boidgraph

import "github.com/c360studio/semstreams/vocabulary"

// SemStreams' v1 first-party authoring policy (ADR-036) requires any predicate
// that appears on a *declaration surface* — a lifecycle workflow's
// PhasePredicate, a `lifecycle:"phase,predicate=…"` struct tag, a rule
// condition, or (since beta.160) a projection contract's predicate group — to
// be declared in the vocabulary registry before it can be used
// (`vocabulary.RequireDeclaredPredicate`). Runtime graph persistence (our
// snapshot triples through graph-ingest) uses the syntax-only final-candidate
// seam instead, so the position/velocity/zone predicates need no declaration —
// only the boid lifecycle phase predicate and the neighbor predicate (named by
// the semboids-neighbors reconcile contract) do.
//
// init registers them so every consumer of this package (the composition root
// and the tests that drive `Manager.Register` / `NewMutationClient`) sees the
// declarations without extra wiring; vocabulary's registry is initialized
// before this package's init runs.
func init() { RegisterVocabulary() }

// RegisterVocabulary declares semboids' first-party declaration-surface
// predicates. Idempotent (options amend an existing registration), so a second
// call from an explicit composition root is harmless.
func RegisterVocabulary() {
	vocabulary.Register(BoidPhasePredicate,
		vocabulary.WithDescription("Current lifecycle phase of a boid (active, culled, or expired)"),
		vocabulary.WithDataType("string"))
	vocabulary.Register(NeighborPredicate,
		vocabulary.WithDescription("Directed neighbor edge between two boids at snapshot time"),
		vocabulary.WithDataType("string"))
}

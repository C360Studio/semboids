package boidgraph

import "github.com/c360studio/semstreams/pkg/projection"

const (
	// NeighborContractName is the projection contract declaring the
	// publisher's write intent for the neighbor predicate group.
	NeighborContractName = "semboids-neighbors"
	// NeighborGroup is the contract's reconcile-mode predicate group.
	NeighborGroup = "neighbors"
	// NeighborPredicate is the boid↔boid edge predicate the group owns.
	NeighborPredicate = "flock.neighbor.of"
)

// NeighborContract declares the reconcile-mode group for `flock.neighbor.of`.
// Reconcile is what lets an empty desired set express "now zero neighbors" —
// the beta.160 typed replacement for the removed
// `graph.mutation.triple.remove` operation (semstreams#578, resolved
// upstream). The contract validates caller intent only; bulk snapshot
// publishing stays on the batch stream lane (ADR-001 / design D1).
func NeighborContract() projection.Contract {
	return projection.Contract{
		Name:          NeighborContractName,
		EntityPattern: BoidEntityIDPattern,
		Groups: []projection.PredicateGroup{{
			Name:       NeighborGroup,
			Mode:       projection.ModeReconcile,
			Predicates: []string{NeighborPredicate},
		}},
	}
}

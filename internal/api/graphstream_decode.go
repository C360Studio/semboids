package api

import (
	"encoding/json"
	"strings"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/graphview"
)

// decodeBoidEntity is the graphview decode seam for ENTITY_STATES. It runs
// exactly once per delivered write on the view's watcher goroutine, regardless
// of how many SSE clients are attached — that amortization is the point of the
// shared view.
//
// Non-boid keys return keep=false, which maps them to absence in the
// projection: the pane can only render boids, so nothing else is worth holding
// in memory. This replaces the old isBoidKey filter that sat downstream of a
// per-client watcher.
//
// The validating UnmarshalEntityState is deliberate. ADR-081 forbids
// UnmarshalEntityStateTrusted inside a view — that fast path belongs to
// graph-ingest as the sole writer of ENTITY_STATES; every other reader keeps
// the validating decode. A decode failure is returned as an error so graphview
// raises it as a typed per-key poison signal rather than silently skipping the
// key, which is what the previous hand-rolled unmarshal did.
func decodeBoidEntity(key string, value []byte, _ graphview.EntryMeta) (graphEntity, bool, error) {
	if !isBoidKey(key) {
		return graphEntity{}, false, nil
	}
	var es graph.EntityState
	if err := graph.UnmarshalEntityState(value, &es); err != nil {
		return graphEntity{}, false, err
	}
	e := graphEntity{ID: key}
	// Later triples win within one state (upstream #466 appends duplicates;
	// newest values sit at the tail, so last-write-wins reads correctly
	// either way).
	for _, tr := range es.Triples {
		switch tr.Predicate {
		case "flock.position.x":
			if v, ok := tr.Object.(float64); ok {
				e.X = v
			}
		case "flock.neighbor.count":
			// Marks the start of a fresh neighbor set in append order.
			e.Neighbors = e.Neighbors[:0]
		case "flock.position.y":
			if v, ok := tr.Object.(float64); ok {
				e.Y = v
			}
		case "flock.neighbor.of":
			if v, ok := tr.Object.(string); ok {
				e.Neighbors = append(e.Neighbors, v)
			}
		}
	}
	return e, true, nil
}

// decodeCommunity is the graphview decode seam for COMMUNITY_INDEX, yielding a
// community's level-0 member list.
//
// Only level-0 entries are kept; the hierarchy's upper levels legitimately
// contain everything and would flatten the pane to one color. Both the key
// prefix check and the decoded Level field are enforced — the prefix alone is a
// convention, the field is authoritative.
//
// COMMUNITY_INDEX is not ENTITY_STATES, so the graph entity-state contract does
// not apply here and a plain unmarshal is correct. A malformed value still
// returns an error so it surfaces as poison rather than a silent skip.
func decodeCommunity(key string, value []byte, _ graphview.EntryMeta) ([]string, bool, error) {
	if !strings.HasPrefix(key, "0.") {
		return nil, false, nil
	}
	var c struct {
		Level   int      `json:"level"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal(value, &c); err != nil {
		return nil, false, err
	}
	if c.Level != 0 {
		return nil, false, nil
	}
	return c.Members, true, nil
}

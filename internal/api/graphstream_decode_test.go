package api

import (
	"testing"

	"github.com/c360studio/semstreams/pkg/graphview"
)

const testBoidID = "c360.semboids-001.sim.flock.boid.7"

// boidStateJSON builds a contract-valid ENTITY_STATES payload for a boid.
// The 6-part entity ID and 3-part predicates are required by the graph
// entity-state contract that decodeBoidEntity validates against.
func boidStateJSON(triples string) []byte {
	return []byte(`{"id":"` + testBoidID + `","triples":[` + triples + `]}`)
}

func triple(predicate, object string) string {
	return `{"subject":"` + testBoidID + `","predicate":"` + predicate + `","object":` + object + `}`
}

func TestDecodeBoidEntity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		key           string
		value         []byte
		wantKeep      bool
		wantErr       bool
		wantX, wantY  float64
		wantNeighbors []string
	}{
		{
			name: "boid key decodes position and neighbors",
			key:  testBoidID,
			value: boidStateJSON(
				triple("flock.position.x", "1.5") + "," +
					triple("flock.position.y", "2.5") + "," +
					triple("flock.neighbor.of", `"c360.semboids-001.sim.flock.boid.8"`),
			),
			wantKeep:      true,
			wantX:         1.5,
			wantY:         2.5,
			wantNeighbors: []string{"c360.semboids-001.sim.flock.boid.8"},
		},
		{
			name: "neighbor count resets the set so a shrinking flock does not accumulate",
			key:  testBoidID,
			value: boidStateJSON(
				triple("flock.neighbor.of", `"c360.semboids-001.sim.flock.boid.8"`) + "," +
					triple("flock.neighbor.count", "1") + "," +
					triple("flock.neighbor.of", `"c360.semboids-001.sim.flock.boid.9"`),
			),
			wantKeep:      true,
			wantNeighbors: []string{"c360.semboids-001.sim.flock.boid.9"},
		},
		{
			name:     "non-boid key maps to absence",
			key:      "c360.semboids-001.sim.flock.zone.1",
			value:    boidStateJSON(triple("flock.position.x", "1")),
			wantKeep: false,
		},
		{
			name:     "malformed value poisons the key rather than skipping silently",
			key:      testBoidID,
			value:    []byte(`{"id":`),
			wantKeep: false,
			wantErr:  true,
		},
		{
			// Proves the decode is the *validating* one, not a plain unmarshal:
			// a 2-part predicate violates the canonical 3-part contract and must
			// be rejected. This is the exact shape that broke us in the beta.149
			// migration (flock.neighbor -> flock.neighbor.of).
			name:     "non-canonical predicate is rejected by the contract",
			key:      testBoidID,
			value:    boidStateJSON(triple("flock.neighbor", `"c360.semboids-001.sim.flock.boid.8"`)),
			wantKeep: false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, keep, err := decodeBoidEntity(tt.key, tt.value, graphview.EntryMeta{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if keep != tt.wantKeep {
				t.Fatalf("keep = %v, want %v", keep, tt.wantKeep)
			}
			if !tt.wantKeep {
				return
			}
			if got.X != tt.wantX || got.Y != tt.wantY {
				t.Errorf("position = (%v,%v), want (%v,%v)", got.X, got.Y, tt.wantX, tt.wantY)
			}
			if len(got.Neighbors) != len(tt.wantNeighbors) {
				t.Fatalf("neighbors = %v, want %v", got.Neighbors, tt.wantNeighbors)
			}
			for i, n := range tt.wantNeighbors {
				if got.Neighbors[i] != n {
					t.Errorf("neighbor[%d] = %q, want %q", i, got.Neighbors[i], n)
				}
			}
		})
	}
}

func TestDecodeCommunity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		key         string
		value       []byte
		wantKeep    bool
		wantErr     bool
		wantMembers []string
	}{
		{
			name:        "level-0 entry keeps its members",
			key:         "0.community-a",
			value:       []byte(`{"level":0,"members":["boid.1","boid.2"]}`),
			wantKeep:    true,
			wantMembers: []string{"boid.1", "boid.2"},
		},
		{
			name:     "non level-0 key maps to absence",
			key:      "1.community-a",
			value:    []byte(`{"level":1,"members":["boid.1"]}`),
			wantKeep: false,
		},
		{
			name: "level field is authoritative even when the key prefix says otherwise",
			key:  "0.community-a",
			// Upper hierarchy levels contain everything; keeping one would
			// flatten the whole pane to a single color.
			value:    []byte(`{"level":2,"members":["boid.1"]}`),
			wantKeep: false,
		},
		{
			name:     "malformed value poisons the key rather than skipping silently",
			key:      "0.community-a",
			value:    []byte(`{"level":`),
			wantKeep: false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, keep, err := decodeCommunity(tt.key, tt.value, graphview.EntryMeta{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if keep != tt.wantKeep {
				t.Fatalf("keep = %v, want %v", keep, tt.wantKeep)
			}
			if !tt.wantKeep {
				return
			}
			if len(got) != len(tt.wantMembers) {
				t.Fatalf("members = %v, want %v", got, tt.wantMembers)
			}
			for i, m := range tt.wantMembers {
				if got[i] != m {
					t.Errorf("member[%d] = %q, want %q", i, got[i], m)
				}
			}
		})
	}
}

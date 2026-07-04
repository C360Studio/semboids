package sim

import (
	"github.com/c360studio/semboids/internal/flock"
	"github.com/c360studio/semboids/internal/zone"
)

// transition is one zone containment edge for one boid.
type transition struct {
	boidID  uint32
	zone    zone.Zone
	entered bool
}

// zoneTracker detects containment edges against static zone geometry.
// Membership state is indexed [boid][zone]; the events slice is reused
// across calls so steady-state ticks do not allocate.
type zoneTracker struct {
	zones  []zone.Zone
	inside map[uint32][]bool
	events []transition
}

func newZoneTracker(zones []zone.Zone) *zoneTracker {
	return &zoneTracker{
		zones:  zones,
		inside: make(map[uint32][]bool),
	}
}

// transitions returns the containment edges since the previous call. The
// returned slice is valid until the next call.
func (t *zoneTracker) transitions(boids []flock.Boid) []transition {
	t.events = t.events[:0]
	for _, b := range boids {
		state, ok := t.inside[b.ID]
		if !ok {
			state = make([]bool, len(t.zones))
			t.inside[b.ID] = state
		}
		for zi, z := range t.zones {
			now := z.Contains(b.Pos.X, b.Pos.Y)
			if now != state[zi] {
				t.events = append(t.events, transition{boidID: b.ID, zone: z, entered: now})
				state[zi] = now
			}
		}
	}
	return t.events
}

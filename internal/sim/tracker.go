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

// linger is a "boid overstayed a predator zone" edge — the sim publishes it as
// a fact for the predator-cull rule to act on (add-lifecycle-population). The
// rule owns the existence decision; the tracker only reports the dwell.
type linger struct {
	boidID uint32
	zone   zone.Zone
}

// zoneTracker detects containment edges and predator-zone dwell against static
// zone geometry. Membership and dwell are indexed [boid][zone]; the event
// slices are reused across calls so steady-state ticks do not allocate.
type zoneTracker struct {
	zones      []zone.Zone
	graceTicks int // predator dwell ticks before a lingered edge; 0 disables
	inside     map[uint32][]bool
	dwell      map[uint32][]int
	fired      map[uint32][]bool // lingered already emitted this crossing
	events     []transition
	lingers    []linger
}

func newZoneTracker(zones []zone.Zone, graceTicks int) *zoneTracker {
	return &zoneTracker{
		zones:      zones,
		graceTicks: graceTicks,
		inside:     make(map[uint32][]bool),
		dwell:      make(map[uint32][]int),
		fired:      make(map[uint32][]bool),
	}
}

// transitions returns the containment edges since the previous call and, as a
// side effect, advances predator dwell — read lingered() for the resulting
// overstay edges. Both returned slices are valid until the next call.
func (t *zoneTracker) transitions(boids []flock.Boid) []transition {
	t.events = t.events[:0]
	t.lingers = t.lingers[:0]
	for _, b := range boids {
		state, ok := t.inside[b.ID]
		if !ok {
			state = make([]bool, len(t.zones))
			t.inside[b.ID] = state
			t.dwell[b.ID] = make([]int, len(t.zones))
			t.fired[b.ID] = make([]bool, len(t.zones))
		}
		dwell := t.dwell[b.ID]
		fired := t.fired[b.ID]
		for zi, z := range t.zones {
			now := z.Contains(b.Pos.X, b.Pos.Y)
			if now != state[zi] {
				t.events = append(t.events, transition{boidID: b.ID, zone: z, entered: now})
				state[zi] = now
			}
			if now {
				dwell[zi]++
				if t.graceTicks > 0 && z.Type == zone.TypePredator &&
					dwell[zi] >= t.graceTicks && !fired[zi] {
					t.lingers = append(t.lingers, linger{boidID: b.ID, zone: z})
					fired[zi] = true
				}
			} else {
				dwell[zi] = 0
				fired[zi] = false
			}
		}
	}
	return t.events
}

// lingered returns the predator-zone overstay edges from the last transitions
// call — valid until the next call.
func (t *zoneTracker) lingered() []linger { return t.lingers }

// forget drops per-boid membership/dwell state for despawned boids, so a
// high-churn population does not leak the tracker maps. The run loop calls it
// with the IDs it removes from the engine.
func (t *zoneTracker) forget(ids []uint32) {
	for _, id := range ids {
		delete(t.inside, id)
		delete(t.dwell, id)
		delete(t.fired, id)
	}
}

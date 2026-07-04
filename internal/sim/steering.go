package sim

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/c360studio/semboids/internal/flock"
	"github.com/c360studio/semboids/internal/zone"
)

// modKind is the frame-encoded modifier kind (the boid tuple's 6th element).
type modKind uint8

// Modifier kinds. Wire names are the lowercase strings in kindNames.
const (
	modNone    modKind = 0
	modFlee    modKind = 1
	modAttract modKind = 2
	modWind    modKind = 3
	// modCancel is a control message, never stored in the table.
	modCancel modKind = 255
)

var kindNames = map[string]modKind{
	"flee":    modFlee,
	"attract": modAttract,
	"wind":    modWind,
	"cancel":  modCancel,
}

var kindStrings = map[modKind]string{
	modFlee:    "flee",
	modAttract: "attract",
	modWind:    "wind",
}

// modifier is one steering instruction from the rule engine.
type modifier struct {
	boidID uint32
	zoneID string
	kind   modKind
	ttl    int
}

// parseModifier decodes the rule engine publish-action envelope
// ({entity_id, subject, ..., properties: {boid_id, zone_id, kind, ttl_ticks}}).
// boid_id and ttl_ticks are coerced leniently: `$message.*` substitution can
// render numbers as strings.
func parseModifier(data []byte) (modifier, error) {
	var envelope struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return modifier{}, fmt.Errorf("unmarshal steering envelope: %w", err)
	}
	props := envelope.Properties
	if props == nil {
		return modifier{}, fmt.Errorf("steering envelope has no properties")
	}

	kindStr, _ := props["kind"].(string)
	kind, ok := kindNames[kindStr]
	if !ok {
		return modifier{}, fmt.Errorf("unknown modifier kind %q", kindStr)
	}

	boidID, err := coerceUint32(props["boid_id"])
	if err != nil {
		return modifier{}, fmt.Errorf("boid_id: %w", err)
	}
	zoneID, _ := props["zone_id"].(string)
	if zoneID == "" {
		return modifier{}, fmt.Errorf("zone_id is required")
	}

	m := modifier{boidID: boidID, zoneID: zoneID, kind: kind}
	if kind != modCancel {
		ttl, err := coerceInt(props["ttl_ticks"])
		if err != nil || ttl <= 0 {
			return modifier{}, fmt.Errorf("ttl_ticks must be a positive number, got %v", props["ttl_ticks"])
		}
		m.ttl = ttl
	}
	return m, nil
}

func coerceUint32(v any) (uint32, error) {
	switch n := v.(type) {
	case float64:
		return uint32(n), nil
	case string:
		parsed, err := strconv.ParseUint(n, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", n, err)
		}
		return uint32(parsed), nil
	default:
		return 0, fmt.Errorf("missing or non-numeric value %v", v)
	}
}

func coerceInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", n, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("missing or non-numeric value %v", v)
	}
}

type modTableKey struct {
	boidID uint32
	zoneID string
}

type modEntry struct {
	kind modKind
	ttl  int
}

// steeringState holds active modifiers and the per-kind demo gate. The
// subscription goroutine writes to the staged slice under the mutex; the
// tick loop drains it once per tick via advance() — the hot per-boid loop
// only ever sees the plain table map.
type steeringState struct {
	logger *slog.Logger

	mu     sync.Mutex
	staged []modifier
	gated  map[modKind]bool // true = disabled

	// table is owned by the tick loop after advance(); no lock needed there.
	table map[modTableKey]modEntry
}

func newSteeringState(logger *slog.Logger) *steeringState {
	return &steeringState{
		logger: logger,
		gated:  make(map[modKind]bool),
		table:  make(map[modTableKey]modEntry),
	}
}

// stage queues a modifier from the subscription goroutine.
func (s *steeringState) stage(m modifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.staged = append(s.staged, m)
}

// setKindEnabled flips the demo gate for a modifier kind ("flee", "attract",
// "wind"). Disabling clears active entries of that kind immediately.
func (s *steeringState) setKindEnabled(kind string, enabled bool) error {
	k, ok := kindNames[kind]
	if !ok || k == modCancel {
		return fmt.Errorf("unknown modifier kind %q", kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gated[k] = !enabled
	if !enabled {
		for key, e := range s.table {
			if e.kind == k {
				delete(s.table, key)
			}
		}
	}
	return nil
}

// kindStates reports the gate state per kind (true = enabled).
func (s *steeringState) kindStates() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]bool, len(kindStrings))
	for k, name := range kindStrings {
		out[name] = !s.gated[k]
	}
	return out
}

// advance drains staged modifiers into the table (applying cancels and the
// gate) and decrements TTLs, expiring dead entries — the once-per-tick step.
func (s *steeringState) advance() {
	s.mu.Lock()
	staged := s.staged
	s.staged = nil
	gated := make(map[modKind]bool, len(s.gated))
	for k, v := range s.gated {
		gated[k] = v
	}
	s.mu.Unlock()

	// Age existing entries first so a fresh modifier gets its full TTL of
	// influence (arrival tick counts as tick one).
	for key, e := range s.table {
		e.ttl--
		if e.ttl <= 0 {
			delete(s.table, key) // TTL expiry self-heals missed exits
			continue
		}
		s.table[key] = e
	}

	for _, m := range staged {
		key := modTableKey{boidID: m.boidID, zoneID: m.zoneID}
		if m.kind == modCancel {
			delete(s.table, key)
			continue
		}
		if gated[m.kind] {
			continue
		}
		s.table[key] = modEntry{kind: m.kind, ttl: m.ttl}
	}
}

// activeCount reports live table entries (test observability).
func (s *steeringState) activeCount() int { return len(s.table) }

// external derives per-boid steering vectors from the active table and zone
// geometry: flee pushes away from the zone center, attract pulls toward it,
// wind pushes along the zone's direction — each scaled by zone strength
// against the force budget. Called from the tick loop before Engine.Tick.
func (s *steeringState) external(
	boids []flock.Boid, zones []zone.Zone, p flock.Params,
) map[uint32]flock.Vec2 {
	if len(s.table) == 0 {
		return nil
	}
	zoneByID := make(map[string]zone.Zone, len(zones))
	for _, z := range zones {
		zoneByID[z.ID] = z
	}
	posByID := make(map[uint32]flock.Vec2, len(boids))
	for _, b := range boids {
		posByID[b.ID] = b.Pos
	}

	ext := make(map[uint32]flock.Vec2, len(s.table))
	for key, e := range s.table {
		z, ok := zoneByID[key.zoneID]
		if !ok {
			continue
		}
		pos, ok := posByID[key.boidID]
		if !ok {
			continue
		}
		var dir flock.Vec2
		switch e.kind {
		case modFlee:
			dir = pos.Sub(flock.Vec2{X: z.X, Y: z.Y})
		case modAttract:
			dir = flock.Vec2{X: z.X, Y: z.Y}.Sub(pos)
		case modWind:
			dir = flock.Vec2{X: z.DX, Y: z.DY}
		}
		l := dir.Len()
		if l == 0 {
			continue
		}
		force := dir.Scale(p.MaxForce * z.Strength / l)
		ext[key.boidID] = ext[key.boidID].Add(force)
	}
	return ext
}

// modFlags returns the frame tint flag per boid (aligned with boids order):
// the kind of one active modifier, or 0.
func (s *steeringState) modFlags(boids []flock.Boid) []uint8 {
	flags := make([]uint8, len(boids))
	if len(s.table) == 0 {
		return flags
	}
	kindByBoid := make(map[uint32]modKind, len(s.table))
	for key, e := range s.table {
		if existing, ok := kindByBoid[key.boidID]; !ok || e.kind < existing {
			kindByBoid[key.boidID] = e.kind
		}
	}
	for i, b := range boids {
		flags[i] = uint8(kindByBoid[b.ID])
	}
	return flags
}

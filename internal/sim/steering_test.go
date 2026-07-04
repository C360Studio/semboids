package sim

import (
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/c360studio/semboids/internal/flock"
	"github.com/c360studio/semboids/internal/zone"
)

// ruleEnvelope builds the rule engine's publish-action wire shape.
func ruleEnvelope(t *testing.T, props map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"entity_id":  "c360.semboids.sim.flock.boid.7",
		"subject":    "boids.steering",
		"timestamp":  "2026-07-04T12:00:00Z",
		"source":     "rule_engine",
		"properties": props,
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return data
}

func TestParseModifier(t *testing.T) {
	tests := []struct {
		name    string
		props   map[string]any
		wantErr bool
		want    modifier
	}{
		{
			"numeric boid_id",
			map[string]any{"boid_id": 7, "zone_id": "pred-1", "kind": "flee", "ttl_ticks": 60},
			false,
			modifier{boidID: 7, zoneID: "pred-1", kind: modFlee, ttl: 60},
		},
		{
			"string boid_id (substitution renders strings)",
			map[string]any{"boid_id": "7", "zone_id": "pred-1", "kind": "flee", "ttl_ticks": "60"},
			false,
			modifier{boidID: 7, zoneID: "pred-1", kind: modFlee, ttl: 60},
		},
		{
			"cancel",
			map[string]any{"boid_id": 3, "zone_id": "wind-1", "kind": "cancel"},
			false,
			modifier{boidID: 3, zoneID: "wind-1", kind: modCancel},
		},
		{
			"unknown kind",
			map[string]any{"boid_id": 1, "zone_id": "z", "kind": "teleport"},
			true,
			modifier{},
		},
		{
			"missing boid_id",
			map[string]any{"zone_id": "z", "kind": "flee"},
			true,
			modifier{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseModifier(ruleEnvelope(t, tt.props))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("want error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseModifier: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseModifierMalformedJSON(t *testing.T) {
	if _, err := parseModifier([]byte("not json")); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

func steeringZones() []zone.Zone {
	return []zone.Zone{
		{ID: "pred-1", Type: zone.TypePredator, X: 100, Y: 100, R: 50, Strength: 1.0},
		{ID: "wind-1", Type: zone.TypeWind, X: 500, Y: 500, R: 100, Strength: 0.5, DX: 1, DY: 0},
	}
}

func TestSteeringTableLifecycle(t *testing.T) {
	st := newSteeringState(slog.Default())
	st.stage(modifier{boidID: 0, zoneID: "pred-1", kind: modFlee, ttl: 2})
	st.advance()
	if n := st.activeCount(); n != 1 {
		t.Fatalf("active = %d, want 1 after stage+advance", n)
	}

	// TTL decrements each advance; entry expires after 2 more.
	st.advance()
	if n := st.activeCount(); n != 1 {
		t.Fatalf("active = %d, want 1 (ttl 1 remaining)", n)
	}
	st.advance()
	if n := st.activeCount(); n != 0 {
		t.Fatalf("active = %d, want 0 after TTL expiry (self-heal)", n)
	}
}

func TestSteeringCancelRemoves(t *testing.T) {
	st := newSteeringState(slog.Default())
	st.stage(modifier{boidID: 4, zoneID: "wind-1", kind: modWind, ttl: 1000})
	st.advance()
	if st.activeCount() != 1 {
		t.Fatal("wind modifier not active")
	}
	st.stage(modifier{boidID: 4, zoneID: "wind-1", kind: modCancel})
	st.advance()
	if st.activeCount() != 0 {
		t.Fatal("cancel did not remove modifier")
	}
}

func TestSteeringGateDropsAndClears(t *testing.T) {
	st := newSteeringState(slog.Default())
	st.stage(modifier{boidID: 0, zoneID: "pred-1", kind: modFlee, ttl: 100})
	st.advance()
	if st.activeCount() != 1 {
		t.Fatal("flee modifier not active")
	}

	// Disabling a kind clears existing entries and drops new arrivals.
	st.setKindEnabled("flee", false)
	if st.activeCount() != 0 {
		t.Fatal("gate did not clear existing flee entries")
	}
	st.stage(modifier{boidID: 1, zoneID: "pred-1", kind: modFlee, ttl: 100})
	st.advance()
	if st.activeCount() != 0 {
		t.Fatal("gate did not drop staged flee modifier")
	}

	st.setKindEnabled("flee", true)
	st.stage(modifier{boidID: 1, zoneID: "pred-1", kind: modFlee, ttl: 100})
	st.advance()
	if st.activeCount() != 1 {
		t.Fatal("re-enabled kind not accepted")
	}
}

func TestExternalVectors(t *testing.T) {
	zones := steeringZones()
	p := flock.DefaultParams()
	st := newSteeringState(slog.Default())

	boids := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 120, Y: 100}}, // right of predator center
		{ID: 1, Pos: flock.Vec2{X: 500, Y: 550}}, // inside wind zone
		{ID: 2, Pos: flock.Vec2{X: 800, Y: 450}}, // unaffected
	}
	st.stage(modifier{boidID: 0, zoneID: "pred-1", kind: modFlee, ttl: 10})
	st.stage(modifier{boidID: 1, zoneID: "wind-1", kind: modWind, ttl: 10})
	st.advance()

	ext := st.external(boids, zones, p)

	// Flee: away from predator center — boid 0 is +X of center, so push +X.
	if v := ext[0]; v.X <= 0 || v.Y != 0 {
		t.Fatalf("flee vector = %+v, want +X push", v)
	}
	// Wind: along (DX, DY) = (1, 0).
	if v := ext[1]; v.X <= 0 || v.Y != 0 {
		t.Fatalf("wind vector = %+v, want +X push", v)
	}
	if _, ok := ext[2]; ok {
		t.Fatal("unaffected boid has external vector")
	}

	// Modifier flags for frame tinting.
	flags := st.modFlags(boids)
	if flags[0] != uint8(modFlee) || flags[1] != uint8(modWind) || flags[2] != 0 {
		t.Fatalf("mod flags = %v", flags)
	}
}

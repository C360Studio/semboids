// Package zone defines the static steering zones (predator/food/wind) the
// flock reacts to: config model, validation, and the Graphable payload that
// lands each zone in the graph via graph-ingest at startup.
package zone

import "fmt"

// Zone type constants.
const (
	// TypePredator repels boids that enter it.
	TypePredator = "predator"
	// TypeFood attracts boids that enter it.
	TypeFood = "food"
	// TypeWind applies a constant directional bias inside it.
	TypeWind = "wind"
)

// Zone is one static circular steering zone in world coordinates.
type Zone struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	R        float64 `json:"r"`
	Strength float64 `json:"strength"`
	// DX, DY give the wind direction; required (non-zero) for wind zones.
	DX float64 `json:"dx,omitempty"`
	DY float64 `json:"dy,omitempty"`
}

// Contains reports whether the point lies within the zone (boundary
// inclusive). World toroidality is intentionally ignored: zones are expected
// to sit away from edges, and edge behavior is not a demo requirement.
func (z Zone) Contains(x, y float64) bool {
	dx := x - z.X
	dy := y - z.Y
	return dx*dx+dy*dy <= z.R*z.R
}

// validate checks a single zone.
func (z Zone) validate() error {
	if z.ID == "" {
		return fmt.Errorf("zone id is required")
	}
	switch z.Type {
	case TypePredator, TypeFood, TypeWind:
	default:
		return fmt.Errorf("zone %q: unknown type %q", z.ID, z.Type)
	}
	if z.R <= 0 {
		return fmt.Errorf("zone %q: radius must be positive, got %v", z.ID, z.R)
	}
	if z.Type == TypeWind && z.DX == 0 && z.DY == 0 {
		return fmt.Errorf("zone %q: wind zones require a non-zero direction (dx, dy)", z.ID)
	}
	return nil
}

// Validate checks a zone set: each zone individually plus set-level
// constraints (unique ids).
func Validate(zones []Zone) error {
	seen := make(map[string]struct{}, len(zones))
	for _, z := range zones {
		if err := z.validate(); err != nil {
			return err
		}
		if _, dup := seen[z.ID]; dup {
			return fmt.Errorf("duplicate zone id %q", z.ID)
		}
		seen[z.ID] = struct{}{}
	}
	return nil
}

package zone

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// Entity is the Graphable payload that lands a zone in the graph. It wraps
// the config Zone with the organizational identity needed for the federated
// 6-part entity ID.
type Entity struct {
	Zone       Zone      `json:"zone"`
	OrgID      string    `json:"org_id"`
	Platform   string    `json:"platform"`
	ObservedAt time.Time `json:"observed_at"`
}

// EntityID returns the deterministic 6-part ID:
// org.platform.domain.system.type.instance.
func (e *Entity) EntityID() string {
	return fmt.Sprintf("%s.%s.sim.flock.zone.%s", e.OrgID, e.Platform, e.Zone.ID)
}

// Triples returns the zone's facts: classification, geometry, behavior, and
// (for wind) direction.
func (e *Entity) Triples() []message.Triple {
	entityID := e.EntityID()
	mk := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     "semboids",
			Timestamp:  e.ObservedAt,
			Confidence: 1.0,
		}
	}
	triples := []message.Triple{
		mk("zone.classification.type", e.Zone.Type),
		mk("zone.geometry.x", e.Zone.X),
		mk("zone.geometry.y", e.Zone.Y),
		mk("zone.geometry.radius", e.Zone.R),
		mk("zone.behavior.strength", e.Zone.Strength),
	}
	if e.Zone.Type == TypeWind {
		triples = append(triples,
			mk("zone.wind.dx", e.Zone.DX),
			mk("zone.wind.dy", e.Zone.DY),
		)
	}
	return triples
}

// Schema returns the payload type identifier (boids.zone.v1).
func (e *Entity) Schema() message.Type {
	return message.Type{Domain: "boids", Category: "zone", Version: "v1"}
}

// Validate checks the payload is complete enough to ingest.
func (e *Entity) Validate() error {
	if e.OrgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if e.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	return e.Zone.validate()
}

// MarshalJSON serializes the payload (alias avoids marshal recursion).
func (e *Entity) MarshalJSON() ([]byte, error) {
	type alias Entity
	return json.Marshal((*alias)(e))
}

// UnmarshalJSON deserializes the payload (alias avoids unmarshal recursion).
func (e *Entity) UnmarshalJSON(data []byte) error {
	type alias Entity
	return json.Unmarshal(data, (*alias)(e))
}

// buildEntity reconstructs an Entity from decoded wire fields.
func buildEntity(fields map[string]any) (any, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("re-marshal zone entity fields: %w", err)
	}
	e := &Entity{}
	if err := json.Unmarshal(raw, e); err != nil {
		return nil, fmt.Errorf("unmarshal zone entity: %w", err)
	}
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("validate zone entity: %w", err)
	}
	return e, nil
}

// RegisterPayloads registers the zone entity payload type (boids.zone.v1)
// with the supplied registry.
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "boids",
		Category:    "zone",
		Version:     "v1",
		Description: "Static steering zone (predator/food/wind) as a graph entity",
		Factory:     func() any { return &Entity{} },
		Builder:     buildEntity,
		Example: map[string]any{
			"zone": map[string]any{
				"id": "pred-1", "type": "predator",
				"x": 400.0, "y": 300.0, "r": 80.0, "strength": 1.0,
			},
			"org_id":   "c360",
			"platform": "semboids",
		},
	})
}

// Package boidgraph publishes flock state into the SemStreams graph: boid
// Graphable payloads (position/velocity properties + flock.neighbor.of
// relationships), snapshot derivation types, and the decoupled publisher
// that keeps JetStream pressure off the physics loop (ADR-001; design D1/D2
// of add-graph-pane).
package boidgraph

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// IngestSubject is the JetStream subject boid snapshots publish to, under
// graph-ingest's `entity.>` input (ENTITY stream).
const IngestSubject = "entity.boid.upsert"

// BoidState is one boid's snapshot state.
type BoidState struct {
	ID        uint32   `json:"id"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	VX        float64  `json:"vx"`
	VY        float64  `json:"vy"`
	Neighbors []uint32 `json:"neighbors"`
}

// Entity is the Graphable payload for one boid at snapshot time.
type Entity struct {
	Boid       BoidState `json:"boid"`
	OrgID      string    `json:"org_id"`
	Platform   string    `json:"platform"`
	Tick       uint64    `json:"tick"`
	ObservedAt time.Time `json:"observed_at"`
}

// BoidEntityID renders the deterministic 6-part boid entity ID.
func BoidEntityID(orgID, platform string, id uint32) string {
	return fmt.Sprintf("%s.%s.sim.flock.boid.%d", orgID, platform, id)
}

// EntityID returns the deterministic 6-part ID.
func (e *Entity) EntityID() string {
	return BoidEntityID(e.OrgID, e.Platform, e.Boid.ID)
}

// Triples returns the boid's facts: position/velocity properties, the
// always-present neighbor count (a real degree property and the graph pane's
// neighbor-set reset sentinel — api/graphstream.go), and flock.neighbor.of
// relationships. The neighbor set is cleared on emptying via an explicit
// triple.remove, not the count: the stream merge cannot express now-zero (see
// publisher.go / TestNeighborEmptyGate, verified beta.152; semstreams#578).
func (e *Entity) Triples() []message.Triple {
	entityID := e.EntityID()
	mk := func(predicate string, object any) message.Triple {
		return message.Triple{
			Subject:    entityID,
			Predicate:  predicate,
			Object:     object,
			Source:     "semboids-sim",
			Timestamp:  e.ObservedAt,
			Confidence: 1.0,
		}
	}
	triples := []message.Triple{
		mk("flock.position.x", e.Boid.X),
		mk("flock.position.y", e.Boid.Y),
		mk("flock.velocity.x", e.Boid.VX),
		mk("flock.velocity.y", e.Boid.VY),
		mk("flock.neighbor.count", float64(len(e.Boid.Neighbors))),
	}
	for _, n := range e.Boid.Neighbors {
		triples = append(triples, mk("flock.neighbor.of", BoidEntityID(e.OrgID, e.Platform, n)))
	}
	return triples
}

// Schema returns the payload type identifier (boids.boid.v1).
func (e *Entity) Schema() message.Type {
	return message.Type{Domain: "boids", Category: "boid", Version: "v1"}
}

// Validate checks the payload is complete enough to ingest.
func (e *Entity) Validate() error {
	if e.OrgID == "" {
		return fmt.Errorf("org_id is required")
	}
	if e.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	return nil
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
		return nil, fmt.Errorf("re-marshal boid entity fields: %w", err)
	}
	e := &Entity{}
	if err := json.Unmarshal(raw, e); err != nil {
		return nil, fmt.Errorf("unmarshal boid entity: %w", err)
	}
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("validate boid entity: %w", err)
	}
	return e, nil
}

// RegisterPayloads registers the boid entity payload type (boids.boid.v1).
func RegisterPayloads(reg *payloadregistry.Registry) error {
	return reg.Register(&payloadregistry.Registration{
		Domain:      "boids",
		Category:    "boid",
		Version:     "v1",
		Description: "One boid's graph snapshot: position/velocity + neighbor relationships",
		Factory:     func() any { return &Entity{} },
		Builder:     buildEntity,
		Example: map[string]any{
			"boid": map[string]any{
				"id": 7, "x": 812.4, "y": 301.2, "vx": 21.8, "vy": -37.1,
				"neighbors": []any{12.0, 31.0},
			},
			"org_id":   "c360",
			"platform": "semboids",
			"tick":     1234,
		},
	})
}

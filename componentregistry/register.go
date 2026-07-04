// Package componentregistry provides registration for all semboids
// components: the SemStreams components the flow consumes plus the
// semboids-owned sim input. Mirrors the sibling pattern in semdragons.
package componentregistry

import (
	"github.com/c360studio/semstreams/component"
	wsoutput "github.com/c360studio/semstreams/output/websocket"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	rule "github.com/c360studio/semstreams/processor/rule"

	"github.com/c360studio/semboids/internal/sim"
)

// RegisterAll registers all components semboids uses with the given registry.
func RegisterAll(registry *component.Registry) error {
	// SemStreams components consumed by the flock flow.
	semstreamsComponents := []func(*component.Registry) error{
		wsoutput.Register,    // frames → browser
		graphingest.Register, // zone entities → ENTITY_STATES
		rule.Register,        // zone transitions → steering modifiers
	}
	for _, register := range semstreamsComponents {
		if err := register(registry); err != nil {
			return err
		}
	}

	// SemBoids domain components.
	return sim.Register(registry)
}

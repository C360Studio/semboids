// Package componentregistry provides registration for all semboids
// components: the SemStreams components the flow consumes plus the
// semboids-owned sim input. Mirrors the sibling pattern in semdragons.
package componentregistry

import (
	"github.com/c360studio/semstreams/component"
	wsoutput "github.com/c360studio/semstreams/output/websocket"

	"github.com/c360studio/semboids/internal/sim"
)

// RegisterAll registers all components semboids uses with the given registry.
func RegisterAll(registry *component.Registry) error {
	// SemStreams components consumed by the flock flow.
	if err := wsoutput.Register(registry); err != nil {
		return err
	}

	// SemBoids domain components.
	return sim.Register(registry)
}

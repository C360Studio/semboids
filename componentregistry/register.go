// Package componentregistry provides registration for all semboids
// components: the SemStreams components the flow consumes plus the
// semboids-owned sim input. Mirrors the sibling pattern in semdragons.
package componentregistry

import (
	"encoding/json"
	"fmt"

	"github.com/c360studio/semstreams/component"
	wsoutput "github.com/c360studio/semstreams/output/websocket"
	graphclustering "github.com/c360studio/semstreams/processor/graph-clustering"
	graphindex "github.com/c360studio/semstreams/processor/graph-index"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	rule "github.com/c360studio/semstreams/processor/rule"

	"github.com/c360studio/semboids/internal/api"
	"github.com/c360studio/semboids/internal/sim"
)

// RegisterAll registers all components semboids uses with the given registry.
func RegisterAll(registry *component.Registry) error {
	return RegisterWithControls(registry, nil)
}

// RegisterWithControls preserves each upstream registration and static port
// declaration while capturing only named application controls at construction.
func RegisterWithControls(registry *component.Registry, controls *api.Controls) error {
	source := component.NewRegistry()
	// SemStreams components consumed by the flock flow.
	semstreamsComponents := []func(*component.Registry) error{
		wsoutput.Register,        // frames → browser
		graphingest.Register,     // zone + boid entities → ENTITY_STATES
		rule.Register,            // zone transitions → steering modifiers
		graphindex.Register,      // relationship indexes (LPA adjacency)
		graphclustering.Register, // flock communities (COMMUNITY_INDEX)
	}
	for _, register := range semstreamsComponents {
		if err := register(source); err != nil {
			return err
		}
	}

	// SemBoids domain components.
	if err := sim.Register(source); err != nil {
		return err
	}
	for name, registration := range source.ListFactories() {
		factory, _ := source.GetFactory(name)
		registration.Factory = factory
		if controls != nil && (name == "rule-processor" || name == "sim") {
			registration.Factory = func(raw json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
				comp, err := factory(raw, deps)
				if err != nil {
					return nil, err
				}
				switch name {
				case "rule-processor":
					control, ok := comp.(api.RuleControl)
					if !ok {
						return nil, fmt.Errorf("rule-processor does not implement application rule control")
					}
					controls.BindRules(control)
				case "sim":
					control, ok := comp.(api.SimControl)
					if !ok {
						return nil, fmt.Errorf("sim does not implement application sim control")
					}
					controls.BindSim(control)
				}
				return comp, nil
			}
		}
		if err := registry.RegisterFactory(name, registration); err != nil {
			return err
		}
	}
	return nil
}

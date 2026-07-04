package sim

import (
	"github.com/c360studio/semstreams/component"
)

// Register registers the sim input component factory with the registry.
func Register(registry *component.Registry) error {
	comp := &Component{config: DefaultConfig()}
	registration := &component.Registration{
		Name:        "sim",
		Type:        "input",
		Protocol:    "nats",
		Domain:      "simulation",
		Description: "Reynolds boids physics loop publishing one frame per tick",
		Version:     "1.0.0",
		Schema:      comp.ConfigSchema(),
		Factory:     NewComponent,
	}
	return registry.RegisterFactory("sim", registration)
}

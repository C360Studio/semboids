package sim

import (
	"encoding/json"
	"github.com/c360studio/semstreams/component"
)

// Register registers the sim input component factory with the registry.
func Register(registry *component.Registry) error {
	return RegisterWithFactory(registry, NewComponent)
}

// RegisterWithFactory lets the composition root capture its narrow live controls.
func RegisterWithFactory(registry *component.Registry, factory component.Factory) error {
	comp := &Component{config: DefaultConfig()}
	registration := &component.Registration{
		Name:        "sim",
		Type:        "input",
		Protocol:    "nats",
		Domain:      "simulation",
		Description: "Reynolds boids physics loop publishing one frame per tick",
		Version:     "1.0.0",
		Schema:      comp.ConfigSchema(),
		Factory:     factory,
		Ports:       DeclarePorts,
	}
	return registry.RegisterFactory("sim", registration)
}

// DeclarePorts describes the actual configured transport endpoints without I/O.
func DeclarePorts(raw json.RawMessage, _ string) (component.PortConfig, error) {
	cfg := DefaultConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return component.PortConfig{}, err
		}
	}
	inputs, outputs, err := resolveSimPorts(cfg.Ports)
	if err != nil {
		return component.PortConfig{}, err
	}
	return component.PortConfigFrom(inputs, outputs), nil
}

func resolveSimPorts(ports *component.PortConfig) ([]component.Port, []component.Port, error) {
	if ports == nil || len(ports.Outputs) == 0 {
		ports = DefaultConfig().Ports
	}
	var inputs, outputs []component.Port
	for _, def := range ports.Inputs {
		p, err := def.Resolve(component.DirectionInput)
		if err != nil {
			return nil, nil, err
		}
		inputs = append(inputs, p)
	}
	for _, def := range ports.Outputs {
		p, err := def.Resolve(component.DirectionOutput)
		if err != nil {
			return nil, nil, err
		}
		outputs = append(outputs, p)
	}
	return inputs, outputs, nil
}

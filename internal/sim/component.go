// Package sim wraps the flock physics engine in a SemStreams Input component:
// the "external system" is the simulation itself. Per ADR-001 the tick loop
// publishes exactly one aggregated frame per tick to the egress subject as a
// fire-and-forget core-NATS publish — no per-boid substrate traffic.
package sim

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/component"

	"github.com/c360studio/semboids/internal/flock"
)

// DefaultSubject is the core-NATS subject frames publish to.
const DefaultSubject = "boids.frames"

// Config holds the sim component configuration.
type Config struct {
	// Boids is the population size (default 200).
	Boids int `json:"boids"`
	// TickHz is the simulation tick rate (default 30).
	TickHz float64 `json:"tick_hz"`
	// Seed makes runs reproducible (default 1).
	Seed uint64 `json:"seed"`
	// Ports optionally overrides the default frames output port.
	Ports *component.PortConfig `json:"ports,omitempty"`
}

// DefaultConfig returns the ADR-001 defaults: 200 boids at 30Hz.
func DefaultConfig() Config {
	return Config{
		Boids:  200,
		TickHz: 30,
		Seed:   1,
		Ports: &component.PortConfig{
			Outputs: []component.PortDefinition{{
				Name:        "frames",
				Type:        "nats",
				Subject:     DefaultSubject,
				Required:    true,
				Description: "Aggregated flock frame per tick (compact JSON)",
			}},
		},
	}
}

// publishFunc abstracts the NATS publish so unit tests run without a broker.
type publishFunc func(ctx context.Context, subject string, data []byte) error

// Component drives the flock engine on a ticker and publishes frames.
type Component struct {
	config  Config
	engine  *flock.Engine
	logger  *slog.Logger
	subject string
	publish publishFunc

	outputPorts []component.Port

	mu        sync.RWMutex
	started   bool
	startTime time.Time
	cancel    context.CancelFunc
	done      chan struct{}
	frames    uint64
}

// NewComponent creates the sim component from raw JSON config.
func NewComponent(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	config := DefaultConfig()
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, fmt.Errorf("parse sim config: %w", err)
		}
	}
	defaults := DefaultConfig()
	if config.Boids <= 0 {
		config.Boids = defaults.Boids
	}
	if config.TickHz <= 0 {
		config.TickHz = defaults.TickHz
	}
	if config.Ports == nil || len(config.Ports.Outputs) == 0 {
		config.Ports = defaults.Ports
	}

	params := flock.DefaultParams()
	params.DT = 1 / config.TickHz

	subject := config.Ports.Outputs[0].Subject
	if subject == "" {
		subject = DefaultSubject
	}

	c := &Component{
		config:  config,
		engine:  flock.NewEngine(config.Boids, config.Seed, params),
		logger:  deps.GetLogger(),
		subject: subject,
		outputPorts: []component.Port{
			component.BuildPortFromDefinition(config.Ports.Outputs[0], component.DirectionOutput),
		},
	}
	if deps.NATSClient != nil {
		c.publish = deps.NATSClient.Publish
	}
	return c, nil
}

// Meta returns component metadata.
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "sim",
		Type:        "input",
		Description: "Reynolds boids physics loop publishing one frame per tick",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions (none — the sim is a source).
func (c *Component) InputPorts() []component.Port { return nil }

// OutputPorts returns the frames output port.
func (c *Component) OutputPorts() []component.Port { return c.outputPorts }

// ConfigSchema returns the configuration schema.
func (c *Component) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{
		Properties: map[string]component.PropertySchema{
			"boids": {
				Type:        "integer",
				Description: "Population size",
				Default:     200,
			},
			"tick_hz": {
				Type:        "number",
				Description: "Simulation tick rate in Hz",
				Default:     30,
			},
			"seed": {
				Type:        "integer",
				Description: "Deterministic seed for initial placement",
				Default:     1,
			},
		},
		Required: []string{},
	}
}

// Health reports whether the tick loop is running.
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := "stopped"
	uptime := time.Duration(0)
	if c.started {
		status = "running"
		uptime = time.Since(c.startTime)
	}
	return component.HealthStatus{
		Healthy:   c.started,
		LastCheck: time.Now(),
		Uptime:    uptime,
		Status:    status,
	}
}

// DataFlow returns frame publication metrics.
func (c *Component) DataFlow() component.FlowMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	mps := float64(0)
	if c.started {
		mps = c.config.TickHz
	}
	_ = c.frames
	return component.FlowMetrics{
		MessagesPerSecond: mps,
		LastActivity:      time.Now(),
	}
}

// Initialize prepares the component (Pattern A: setup only, no context).
func (c *Component) Initialize() error {
	if c.publish == nil {
		return fmt.Errorf("sim: no publisher available (NATS client missing)")
	}
	return nil
}

// Start launches the tick loop goroutine.
func (c *Component) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("sim: context cannot be nil")
	}
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return fmt.Errorf("sim already started")
	}
	c.started = true
	c.startTime = time.Now()
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	c.mu.Unlock()

	c.logger.Info("Starting sim component",
		slog.Int("boids", c.config.Boids),
		slog.Float64("tick_hz", c.config.TickHz),
		slog.Uint64("seed", c.config.Seed),
		slog.String("subject", c.subject))

	go c.run(ctx)
	return nil
}

// Stop cancels the tick loop and waits for it to exit (bounded by timeout).
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = false
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
	case <-time.After(timeout):
		return fmt.Errorf("sim: tick loop did not stop within %s", timeout)
	}
	c.logger.Info("Sim component stopped")
	return nil
}

// run is the tick loop: advance physics, publish one frame, repeat.
func (c *Component) run(ctx context.Context) {
	defer close(c.done)

	interval := time.Duration(float64(time.Second) / c.config.TickHz)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	params := c.engine.Params()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			c.engine.Tick()
			frame := NewFrame(c.engine.TickCount(), now, params, c.engine.Boids())
			data, err := json.Marshal(frame)
			if err != nil {
				c.logger.Error("marshal frame", slog.String("error", err.Error()))
				continue
			}
			// Re-check cancellation before publishing so Stop guarantees
			// no frames after the loop observes ctx.Done().
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := c.publish(ctx, c.subject, data); err != nil {
				c.logger.Warn("publish frame",
					slog.Uint64("tick", frame.Tick),
					slog.String("error", err.Error()))
				continue
			}
			c.mu.Lock()
			c.frames++
			c.mu.Unlock()
		}
	}
}

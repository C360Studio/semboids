package sim

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
)

// newTestComponent builds a sim component with a fast tick and an injected
// publisher that forwards published frames to the returned channel. No NATS.
func newTestComponent(t *testing.T, boids int, tickHz float64) (*Component, <-chan []byte) {
	t.Helper()
	cfg, err := json.Marshal(map[string]any{
		"boids":   boids,
		"tick_hz": tickHz,
		"seed":    42,
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	disc, err := NewComponent(cfg, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp := disc.(*Component)
	frames := make(chan []byte, 256)
	comp.publish = func(_ context.Context, _ string, data []byte) error {
		frames <- data
		return nil
	}
	return comp, frames
}

func decodeFrame(t *testing.T, data []byte) (tick uint64, boids [][]float64) {
	t.Helper()
	var f struct {
		Tick  uint64      `json:"tick"`
		Boids [][]float64 `json:"boids"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("decode frame: %v", err)
	}
	return f.Tick, f.Boids
}

func TestComponentPublishesOneFramePerTick(t *testing.T) {
	comp, frames := newTestComponent(t, 20, 200) // fast ticks keep the test quick
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = comp.Stop(time.Second) }()

	var prevTick uint64
	for i := range 3 {
		select {
		case data := <-frames:
			tick, boids := decodeFrame(t, data)
			if len(boids) != 20 {
				t.Fatalf("frame %d: population %d, want 20", i, len(boids))
			}
			if i > 0 && tick != prevTick+1 {
				t.Fatalf("frame %d: tick %d, want %d (one frame per tick)", i, tick, prevTick+1)
			}
			prevTick = tick
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for frame %d", i)
		}
	}
}

func TestComponentStopsCleanlyOnContextCancel(t *testing.T) {
	comp, frames := newTestComponent(t, 10, 200)
	ctx, cancel := context.WithCancel(context.Background())

	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Receive at least one frame so we know the loop is live.
	select {
	case <-frames:
	case <-time.After(2 * time.Second):
		t.Fatal("no frame before cancel")
	}

	cancel()
	// The tick goroutine must exit: Stop waits for it (explicit sync, no sleeps).
	if err := comp.Stop(2 * time.Second); err != nil {
		t.Fatalf("Stop after cancel: %v", err)
	}

	// Drain anything published before the loop observed cancellation, then
	// verify silence: no further frames may arrive.
	for {
		select {
		case <-frames:
			continue
		default:
		}
		break
	}
	select {
	case data := <-frames:
		t.Fatalf("frame published after Stop returned: %s", data)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestComponentStopIsIdempotent(t *testing.T) {
	comp, _ := newTestComponent(t, 5, 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := comp.Stop(time.Second); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := comp.Stop(time.Second); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestComponentRejectsDoubleStart(t *testing.T) {
	comp, _ := newTestComponent(t, 5, 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = comp.Stop(time.Second) }()

	if err := comp.Start(ctx); err == nil {
		t.Fatal("second Start succeeded, want error")
	}
}

func TestComponentDefaults(t *testing.T) {
	disc, err := NewComponent(json.RawMessage(`{}`), component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent with empty config: %v", err)
	}
	comp := disc.(*Component)
	if comp.config.Boids != 200 || comp.config.TickHz != 30 {
		t.Fatalf("defaults = %d boids @ %vHz, want 200 @ 30Hz", comp.config.Boids, comp.config.TickHz)
	}
	outs := comp.OutputPorts()
	if len(outs) != 1 {
		t.Fatalf("output ports = %d, want 1", len(outs))
	}
}

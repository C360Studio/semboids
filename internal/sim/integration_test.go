//go:build integration

package sim

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	natspkg "github.com/nats-io/nats.go"
)

// TestFramesFlowThroughNATS exercises the real publish path: a sim component
// with a live NATS client, a core-NATS subscriber on boids.frames, and the
// spec scenario "Frame cadence matches tick rate" (approximately, to stay
// robust on loaded CI runners).
func TestFramesFlowThroughNATS(t *testing.T) {
	tc := natsclient.NewTestClient(t)

	cfg, err := json.Marshal(map[string]any{"boids": 50, "tick_hz": 30, "seed": 7})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	disc, err := NewComponent(cfg, component.Dependencies{NATSClient: tc.Client})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	comp := disc.(*Component)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames := make(chan []byte, 128)
	sub, err := tc.Client.Subscribe(ctx, DefaultSubject, func(_ context.Context, msg *natspkg.Msg) {
		frames <- msg.Data
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := comp.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = comp.Stop(2 * time.Second) }()

	// Collect frames for one second of wall clock; expect roughly 30
	// (loose bounds: half to double, catching gross cadence failures only).
	deadline := time.After(time.Second)
	count := 0
	var last []byte
collect:
	for {
		select {
		case data := <-frames:
			count++
			last = data
		case <-deadline:
			break collect
		}
	}
	if count < 15 || count > 60 {
		t.Fatalf("received %d frames in 1s at 30Hz, want ~30 (15..60)", count)
	}

	var f struct {
		Boids [][]float64 `json:"boids"`
	}
	if err := json.Unmarshal(last, &f); err != nil {
		t.Fatalf("decode last frame: %v", err)
	}
	if len(f.Boids) != 50 {
		t.Fatalf("last frame population %d, want 50", len(f.Boids))
	}
}

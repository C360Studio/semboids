//go:build integration

package boidgraph_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/types"

	"github.com/c360studio/semboids/internal/boidgraph"
	simpkg "github.com/c360studio/semboids/internal/sim"
)

// graphDialer is the sim's runtime dial surface (satisfied by *sim.Component).
type graphDialer interface {
	SetGraphHz(hz float64) error
}

// e2eHistStats gathers the probe histogram's cumulative (count, sum) from the
// host metrics registry — the same surface scraped on :9090.
func e2eHistStats(t *testing.T, reg *metric.MetricsRegistry) (count uint64, sum float64) {
	t.Helper()
	families, err := reg.PrometheusRegistry().Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, fam := range families {
		if fam.GetName() != "boids_graph_e2e_latency_seconds" {
			continue
		}
		for _, m := range fam.GetMetric() {
			if h := m.GetHistogram(); h != nil {
				return h.GetSampleCount(), h.GetSampleSum()
			}
		}
	}
	return 0, 0
}

// waitForCount blocks until the probe histogram reaches at least want samples.
func waitForCount(t *testing.T, reg *metric.MetricsRegistry, want uint64, timeout time.Duration) uint64 {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if c, _ := e2eHistStats(t, reg); c >= want {
			return c
		}
		select {
		case <-deadline:
			c, _ := e2eHistStats(t, reg)
			t.Fatalf("e2e histogram reached %d samples, want ≥ %d within %s", c, want, timeout)
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// TestE2ELatencyProbeTracksBacklog runs the real sim + graph-ingest with the
// probe live and ZERO SSE clients: the histogram populates from the substrate
// alone, and when the dial is cranked far past ingest capacity (inducing a
// backlog), the flood window's mean e2e latency rises above the baseline
// window's — upper quantiles tracking the backlog (spec: "Latency reflects
// backlog", D4).
func TestE2ELatencyProbeTracksBacklog(t *testing.T) {
	tc := natsclient.NewTestClient(t,
		natsclient.WithE2EDefaults(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}))
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	payloadReg := payloadregistry.New()
	if err := payloadbuiltins.Register(payloadReg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}
	if err := boidgraph.RegisterPayloads(payloadReg); err != nil {
		t.Fatalf("register boid payloads: %v", err)
	}

	registry := component.NewRegistry()
	if err := graphingest.Register(registry); err != nil {
		t.Fatalf("register graph-ingest: %v", err)
	}
	if err := simpkg.Register(registry); err != nil {
		t.Fatalf("register sim: %v", err)
	}

	metricsReg := metric.NewMetricsRegistry()
	deps := component.Dependencies{
		NATSClient:      tc.Client,
		MetricsRegistry: metricsReg,
		Logger:          slog.Default(),
		Platform:        component.PlatformMeta{Org: "c360", Platform: "semboids"},
		PayloadRegistry: payloadReg,
	}

	startComponent(t, ctx, registry, deps, "graph-ingest-t", "graph-ingest", map[string]any{
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "entity_stream", "subject": "entity.>", "type": "jetstream", "stream_name": "ENTITY"},
			},
			"outputs": []map[string]any{
				{"name": "entity_states", "type": "kv-write", "subject": "ENTITY_STATES"},
			},
		},
	})

	// Start the sim at a gentle dial: 400 boids at 1Hz = 400 entity/s, well
	// within ingest capacity → low, flat e2e latency. sample_n=1 so the probe
	// records every update and the windows fill fast. No SSE client is ever
	// connected — the probe runs off the sim's own watcher.
	simCfgJSON, _ := json.Marshal(map[string]any{
		"boids": 400, "tick_hz": 30, "seed": 7,
		"graph_hz": 1, "graph_probe_sample_n": 1,
	})
	simInst, err := registry.CreateComponent("sim-t", types.ComponentConfig{
		Type: types.ComponentTypeInput, Name: "sim", Enabled: true, Config: simCfgJSON,
	}, deps)
	if err != nil {
		t.Fatalf("create sim: %v", err)
	}
	simLC, _ := component.AsLifecycleComponent(simInst)
	if err := simLC.Initialize(); err != nil {
		t.Fatalf("sim initialize: %v", err)
	}
	if err := simLC.Start(ctx); err != nil {
		t.Fatalf("sim start: %v", err)
	}
	t.Cleanup(func() { _ = simLC.Stop(5 * time.Second) })

	dialer, ok := simInst.(graphDialer)
	if !ok {
		t.Fatal("sim component does not expose SetGraphHz")
	}

	// Baseline window: gather enough low-dial samples with zero SSE clients.
	countA := waitForCount(t, metricsReg, 60, 40*time.Second)
	_, sumA := e2eHistStats(t, metricsReg)
	baselineMean := sumA / float64(countA)
	t.Logf("baseline: %d samples, mean e2e %.4fs", countA, baselineMean)

	// Crank the dial to 30Hz: 400 boids × 30Hz = 12k entity/s, far past
	// graph-ingest's ~4.3k/s → the backlog grows and e2e latency climbs.
	if err := dialer.SetGraphHz(30); err != nil {
		t.Fatalf("bump dial: %v", err)
	}

	// Let the backlog build, then measure the flood window's own mean.
	time.Sleep(12 * time.Second)
	countB := waitForCount(t, metricsReg, countA+200, 40*time.Second)
	_, sumB := e2eHistStats(t, metricsReg)
	floodMean := (sumB - sumA) / float64(countB-countA)
	t.Logf("flood: %d new samples, window mean e2e %.4fs", countB-countA, floodMean)

	if countB == 0 {
		t.Fatal("e2e histogram never populated — probe not recording")
	}
	if floodMean <= baselineMean {
		t.Fatalf("flood mean e2e %.4fs did not exceed baseline %.4fs — latency did not track backlog",
			floodMean, baselineMean)
	}
}

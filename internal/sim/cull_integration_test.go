//go:build integration

package sim

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	rule "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
	natspkg "github.com/nats-io/nats.go"

	"github.com/c360studio/semboids/internal/boidgraph"
)

// cullRuleFile renders the predator-cull rule (mirroring the predator-cull
// entry in configs/rules/zone-steering/predator.json) to a temp file with a
// controllable enabled flag. The action is a lifecycle_transition to culled —
// no publish, so the rule needs no output port.
func cullRuleFile(t *testing.T, enabled bool) string {
	t.Helper()
	rules := []map[string]any{{
		"id":      "predator-cull",
		"type":    "expression",
		"enabled": enabled,
		"conditions": []map[string]any{
			{"field": "zone_type", "operator": "eq", "value": "predator"},
			{"field": "event", "operator": "eq", "value": "lingered"},
		},
		"logic": "and",
		"on_enter": []map[string]any{{
			"type":     "lifecycle_transition",
			"workflow": boidgraph.BoidWorkflowName,
			"phase":    boidgraph.PhaseCulled,
			"reason":   "lingered in predator zone",
		}},
	}}
	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal cull rule: %v", err)
	}
	path := filepath.Join(t.TempDir(), "predator-cull.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write rule file: %v", err)
	}
	return path
}

// startLifecycleComponent creates, initializes, and starts one lifecycle
// component against the shared deps, registering cleanup.
func startLifecycleComponent(
	t *testing.T, ctx context.Context, registry *component.Registry,
	deps component.Dependencies, instance, factory string, cfg map[string]any,
) component.Discoverable {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal %s config: %v", instance, err)
	}
	inst, err := registry.CreateComponent(instance, types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: factory, Enabled: true, Config: raw,
	}, deps)
	if err != nil {
		t.Fatalf("create %s: %v", instance, err)
	}
	lc, ok := component.AsLifecycleComponent(inst)
	if !ok {
		t.Fatalf("%s is not a lifecycle component", instance)
	}
	if err := lc.Initialize(); err != nil {
		t.Fatalf("initialize %s: %v", instance, err)
	}
	if err := lc.Start(ctx); err != nil {
		t.Fatalf("start %s: %v", instance, err)
	}
	t.Cleanup(func() { _ = lc.Stop(5 * time.Second) })
	return inst
}

// startCullChain boots the full predator-cull causal chain against a
// testcontainer NATS: graph-ingest (mutation API + ENTITY_STATES), a rule
// processor with the predator-cull rule and the boid lifecycle Manager wired,
// and the sim with a world-covering predator zone so every boid lingers. It
// returns the frame channel and the ENTITY_STATES bucket for reclaim checks.
func startCullChain(
	t *testing.T, ctx context.Context, ruleEnabled bool,
) (<-chan [][]float64, func() int) {
	t.Helper()
	// E2E: JetStream + KV (rule stateful evaluator + ENTITY_STATES). The
	// ENTITY stream carries entity mutations from graph-ingest's handlers to
	// its own consumer (the create/update path).
	tc := natsclient.NewTestClient(t,
		natsclient.WithE2EDefaults(),
		natsclient.WithStreams(natsclient.TestStreamConfig{
			Name: "ENTITY", Subjects: []string{"entity.>"},
		}))

	payloadReg := payloadregistry.New()
	if err := payloadbuiltins.Register(payloadReg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}

	registry := component.NewRegistry()
	for name, register := range map[string]func(*component.Registry) error{
		"graph-ingest": graphingest.Register,
		"rule":         rule.Register,
		"sim":          Register,
	} {
		if err := register(registry); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}

	// One Manager owns the flock.boid workflow — the framework fans it to the
	// rule processor (lifecycle_transition) and the sim (spawn Create) via deps.
	mgr := lifecycle.NewManager(tc.Client, slog.Default())
	if err := mgr.Register(boidgraph.BoidWorkflow()); err != nil {
		t.Fatalf("register boid workflow: %v", err)
	}

	deps := component.Dependencies{
		NATSClient:       tc.Client,
		Logger:           slog.Default(),
		Platform:         component.PlatformMeta{Org: "c360", Platform: "semboids"},
		PayloadRegistry:  payloadReg,
		LifecycleManager: mgr,
	}

	// graph-ingest first: it serves graph.mutation.entity.* (create/update/
	// delete) and writes ENTITY_STATES — the substrate the Manager and the
	// cull watcher both depend on.
	startLifecycleComponent(t, ctx, registry, deps, "graph-ingest-t", "graph-ingest", map[string]any{
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "entity_stream", "subject": "entity.>", "type": "jetstream", "stream_name": "ENTITY"},
			},
			"outputs": []map[string]any{
				{"name": "entity_states", "type": "kv-write", "subject": "ENTITY_STATES"},
			},
		},
	})

	// Rule processor with the predator-cull rule; the Manager is auto-wired
	// from deps.LifecycleManager (factory.go). lifecycle_transition publishes
	// nothing, so only the zone-events input port is needed.
	startLifecycleComponent(t, ctx, registry, deps, "rule-processor-t", "rule-processor", map[string]any{
		"rules_files":              []string{cullRuleFile(t, ruleEnabled)},
		"enable_graph_integration": false,
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "zone_events", "subject": EventsSubject, "type": "nats", "required": true},
			},
		},
	})

	// Sim: a world-covering predator zone means every boid is inside from tick
	// 1 and lingers past the grace window (no flee rule loaded to push them
	// out) — so the whole seed flock funnels through the cull chain. A modest
	// grace gives the seed Creates time to land before the first lingered fires.
	simCfg := map[string]any{
		"boids": 12, "tick_hz": 30, "seed": 7, "cull_grace_ticks": 45,
		"zones": []map[string]any{
			{"id": "pred-1", "type": "predator", "x": 800, "y": 450, "r": 5000, "strength": 1.0},
		},
	}
	simCfgJSON, err := json.Marshal(simCfg)
	if err != nil {
		t.Fatalf("marshal sim config: %v", err)
	}
	simInst, err := registry.CreateComponent("sim-t", types.ComponentConfig{
		Type: types.ComponentTypeInput, Name: "sim", Enabled: true, Config: simCfgJSON,
	}, deps)
	if err != nil {
		t.Fatalf("create sim: %v", err)
	}
	simLC, _ := component.AsLifecycleComponent(simInst)

	frames := make(chan [][]float64, 256)
	if _, err := tc.Client.Subscribe(ctx, DefaultSubject, func(_ context.Context, msg *natspkg.Msg) {
		var f struct {
			Boids [][]float64 `json:"boids"`
		}
		if err := json.Unmarshal(msg.Data, &f); err == nil {
			select {
			case frames <- f.Boids:
			default:
			}
		}
	}); err != nil {
		t.Fatalf("subscribe frames: %v", err)
	}

	if err := simLC.Initialize(); err != nil {
		t.Fatalf("sim initialize: %v", err)
	}
	if err := simLC.Start(ctx); err != nil {
		t.Fatalf("sim start: %v", err)
	}
	t.Cleanup(func() { _ = simLC.Stop(5 * time.Second) })

	// boidEntityCount reports how many boid entities remain in ENTITY_STATES —
	// the reclaim (graph.mutation.entity.delete) side of the chain.
	boidEntityCount := func() int {
		kv, err := tc.Client.WaitForBucket(ctx, "ENTITY_STATES", 30*time.Second)
		if err != nil {
			return -1
		}
		keys, err := kv.Keys(ctx)
		if err != nil {
			return 0 // empty bucket surfaces as an error on some KV versions
		}
		n := 0
		for _, k := range keys {
			if strings.Contains(k, ".flock.boid.") {
				n++
			}
		}
		return n
	}

	return frames, boidEntityCount
}

// TestPredatorCullReclaimsBoid is the 4.2 full-chain assertion: a boid lingers
// in a predator zone → the rule processor fires lifecycle_transition → culled
// → phase=culled lands in ENTITY_STATES → the sim removes it from physics AND
// reclaims the entity (graph.mutation.entity.delete). The flock shrinks through
// the graph, not by a private sim signal.
func TestPredatorCullReclaimsBoid(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	frames, boidEntityCount := startCullChain(t, ctx, true)

	const initial = 12
	// The whole flock lingers, so physics should collapse well below the seed.
	// A margin (≤ 4 survivors) proves many boids were culled through the graph
	// while tolerating the odd create-race straggler at the seed edge.
	const wantAtMost = 4

	deadline := time.After(90 * time.Second)
	for {
		select {
		case boids := <-frames:
			if len(boids) <= wantAtMost {
				// Physics shrank through the graph. Now confirm the reclaim
				// side: culled entities are deleted from ENTITY_STATES.
				remaining := boidEntityCount()
				if remaining >= 0 && remaining < initial {
					t.Logf("cull chain complete: physics %d→%d boids, ENTITY_STATES boid entities down to %d",
						initial, len(boids), remaining)
					return
				}
				t.Logf("physics shrank to %d but ENTITY_STATES still has %d boid entities; waiting for reclaim",
					len(boids), remaining)
			}
		case <-deadline:
			t.Fatalf("predator cull chain never shrank the flock to <=%d (last frame seen above); chain broken", wantAtMost)
		}
	}
}

// TestCullRuleDisabledKeepsFlock is the control: identical wiring with the
// predator-cull rule disabled. No boid may be culled — the flock holds at its
// seed size through a steady window (the sim skips disabled rules at load, and
// with no lingered→transition the cull watcher never fires).
func TestCullRuleDisabledKeepsFlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	frames, _ := startCullChain(t, ctx, false)

	const seed = 12
	// Watch a steady window: population must never dip below the seed.
	quiet := time.After(8 * time.Second)
	for {
		select {
		case boids := <-frames:
			if len(boids) < seed {
				t.Fatalf("disabled cull rule still removed boids: population %d < seed %d", len(boids), seed)
			}
		case <-quiet:
			return // 8s of frames, flock intact
		}
	}
}

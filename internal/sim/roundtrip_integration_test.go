//go:build integration

package sim

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	rule "github.com/c360studio/semstreams/processor/rule"
	"github.com/c360studio/semstreams/types"
	natspkg "github.com/nats-io/nats.go"
)

// predatorRule renders the predator-flee rule (mirroring
// configs/rules/zone-steering/predator.json) with a controllable enabled
// flag, written to a temp file for the rule processor to load.
func predatorRuleFile(t *testing.T, enabled bool) string {
	t.Helper()
	rules := []map[string]any{{
		"id":      "predator-flee",
		"type":    "expression",
		"enabled": enabled,
		"conditions": []map[string]any{
			{"field": "zone_type", "operator": "eq", "value": "predator"},
			{"field": "event", "operator": "eq", "value": "entered"},
		},
		"logic": "and",
		"on_enter": []map[string]any{{
			"type":    "publish",
			"subject": SteeringSubject,
			"properties": map[string]any{
				"boid_id":   "$message.boid_id",
				"zone_id":   "$message.zone_id",
				"kind":      "flee",
				"ttl_ticks": 60,
			},
		}},
	}}
	data, err := json.Marshal(rules)
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}
	path := filepath.Join(t.TempDir(), "predator.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write rule file: %v", err)
	}
	return path
}

// startRoundTrip boots a real rule processor + sim against a testcontainer
// NATS and returns a channel of decoded frames.
func startRoundTrip(t *testing.T, ctx context.Context, ruleEnabled bool) <-chan [][]float64 {
	t.Helper()
	// E2E defaults: JetStream + KV, which the rule processor's stateful
	// evaluator (RULE_STATE bucket) needs to execute on_enter actions.
	tc := natsclient.NewTestClient(t, natsclient.WithE2EDefaults())

	payloadReg := payloadregistry.New()
	if err := payloadbuiltins.Register(payloadReg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}

	registry := component.NewRegistry()
	if err := rule.Register(registry); err != nil {
		t.Fatalf("register rule processor: %v", err)
	}
	if err := Register(registry); err != nil {
		t.Fatalf("register sim: %v", err)
	}

	deps := component.Dependencies{
		NATSClient:      tc.Client,
		Logger:          slog.Default(),
		Platform:        component.PlatformMeta{Org: "c360", Platform: "semboids"},
		PayloadRegistry: payloadReg,
	}

	ruleCfg := map[string]any{
		"rules_files":              []string{predatorRuleFile(t, ruleEnabled)},
		"enable_graph_integration": false,
		"ports": map[string]any{
			"inputs": []map[string]any{
				{"name": "zone_events", "subject": EventsSubject, "type": "nats", "required": true},
			},
			"outputs": []map[string]any{
				{"name": "steering", "subject": SteeringSubject, "type": "nats"},
			},
		},
	}
	ruleCfgJSON, _ := json.Marshal(ruleCfg)
	ruleInst, err := registry.CreateComponent("rule-processor-test", types.ComponentConfig{
		Type: types.ComponentTypeProcessor, Name: "rule-processor", Enabled: true, Config: ruleCfgJSON,
	}, deps)
	if err != nil {
		t.Fatalf("create rule processor: %v", err)
	}
	ruleLC, _ := component.AsLifecycleComponent(ruleInst)
	if err := ruleLC.Initialize(); err != nil {
		t.Fatalf("rule initialize: %v", err)
	}
	if err := ruleLC.Start(ctx); err != nil {
		t.Fatalf("rule start: %v", err)
	}
	t.Cleanup(func() { _ = ruleLC.Stop(5 * time.Second) })

	// Sim with a world-covering predator zone: every boid enters on tick 1.
	simCfg := map[string]any{
		"boids": 10, "tick_hz": 30, "seed": 7,
		"zones": []map[string]any{
			{"id": "pred-1", "type": "predator", "x": 800, "y": 450, "r": 5000, "strength": 1.0},
		},
	}
	simCfgJSON, _ := json.Marshal(simCfg)
	simInst, err := registry.CreateComponent("sim-test", types.ComponentConfig{
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

	return frames
}

// TestRuleRoundTripAppliesModifiers is the full-loop assertion: transition
// event → real rule processor (decode, conditions, $message.* substitution)
// → steering modifier → sim table → m flag visible in frames.
func TestRuleRoundTripAppliesModifiers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	frames := startRoundTrip(t, ctx, true)

	deadline := time.After(20 * time.Second)
	for {
		select {
		case boids := <-frames:
			flagged := 0
			for _, b := range boids {
				if len(b) >= 6 && b[5] == 1 { // modFlee
					flagged++
				}
			}
			if flagged > 0 {
				t.Logf("round trip complete: %d/%d boids under flee modifier", flagged, len(boids))
				return
			}
		case <-deadline:
			t.Fatal("no boid ever showed a flee modifier: rule round trip broken")
		}
	}
}

// TestDisabledRuleProducesNoModifiers is the control: same wiring, rule
// disabled — no modifier may ever appear.
func TestDisabledRuleProducesNoModifiers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	frames := startRoundTrip(t, ctx, false)

	quiet := time.After(5 * time.Second)
	for {
		select {
		case boids := <-frames:
			for _, b := range boids {
				if len(b) >= 6 && b[5] != 0 {
					t.Fatalf("disabled rule produced modifier flag %v", b[5])
				}
			}
		case <-quiet:
			return // 5s of frames, all clean
		}
	}
}

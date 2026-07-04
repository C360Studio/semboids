package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
)

// fakeRules is a stub rule processor: Discoverable + the RuntimeConfigurable
// trio, tracking validate/apply calls.
type fakeRules struct {
	rules       map[string]any
	validated   int
	applied     int
	failApply   bool
	lastChanges map[string]any
}

func newFakeRules() *fakeRules {
	rules := map[string]any{}
	for _, ids := range kindRules {
		for _, id := range ids {
			rules[id] = map[string]any{"id": id, "type": "expression", "enabled": true}
		}
	}
	return &fakeRules{rules: rules}
}

func (f *fakeRules) Meta() component.Metadata {
	return component.Metadata{Name: "rule-processor", Type: "processor"}
}
func (f *fakeRules) InputPorts() []component.Port  { return nil }
func (f *fakeRules) OutputPorts() []component.Port { return nil }
func (f *fakeRules) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (f *fakeRules) Health() component.HealthStatus  { return component.HealthStatus{Healthy: true} }
func (f *fakeRules) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

func (f *fakeRules) GetRuntimeConfig() map[string]any {
	return map[string]any{"rules": f.rules}
}

func (f *fakeRules) ValidateConfigUpdate(changes map[string]any) error {
	f.validated++
	f.lastChanges = changes
	return nil
}

func (f *fakeRules) ApplyConfigUpdate(changes map[string]any) error {
	if f.failApply {
		return http.ErrAbortHandler
	}
	f.applied++
	if rules, ok := changes["rules"].(map[string]any); ok {
		f.rules = rules
	}
	return nil
}

// fakeSim is a stub sim exposing only ClearModifierKind.
type fakeSim struct {
	cleared []string
}

func (f *fakeSim) Meta() component.Metadata {
	return component.Metadata{Name: "sim", Type: "input"}
}
func (f *fakeSim) InputPorts() []component.Port  { return nil }
func (f *fakeSim) OutputPorts() []component.Port { return nil }
func (f *fakeSim) ConfigSchema() component.ConfigSchema {
	return component.ConfigSchema{}
}
func (f *fakeSim) Health() component.HealthStatus  { return component.HealthStatus{Healthy: true} }
func (f *fakeSim) DataFlow() component.FlowMetrics { return component.FlowMetrics{} }

func (f *fakeSim) ClearModifierKind(kind string) error {
	f.cleared = append(f.cleared, kind)
	return nil
}

// newTestService wires the API service against a registry holding the fakes.
func newTestService(t *testing.T, withRules, withSim bool) (*Service, *http.ServeMux, *fakeRules, *fakeSim) {
	t.Helper()
	registry := component.NewRegistry()
	nc, err := natsclient.NewClient("nats://localhost:4222") // unconnected: satisfies dep validation
	if err != nil {
		t.Fatalf("new nats client: %v", err)
	}
	deps := component.Dependencies{NATSClient: nc}

	rules := newFakeRules()
	sim := &fakeSim{}
	register := func(name string, disc component.Discoverable) {
		if err := registry.RegisterFactory(name, &component.Registration{
			Name: name, Type: "processor", Protocol: "test", Domain: "test",
			Description: "test stub", Version: "0",
			Factory: func(_ json.RawMessage, _ component.Dependencies) (component.Discoverable, error) {
				return disc, nil
			},
		}); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
		if _, err := registry.CreateComponent(name, types.ComponentConfig{
			Type: types.ComponentTypeProcessor, Name: name, Enabled: true,
			Config: json.RawMessage(`{}`),
		}, deps); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}
	if withRules {
		register("rule-processor", rules)
	}
	if withSim {
		register("sim", sim)
	}

	svc, err := New(nil, &service.Dependencies{ComponentRegistry: registry})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := svc.(*Service)
	mux := http.NewServeMux()
	s.RegisterHTTPHandlers("/boids", mux)
	return s, mux, rules, sim
}

func TestGetRulesDerivesFromRuleConfig(t *testing.T) {
	_, mux, fake, _ := newTestService(t, true, true)
	fake.rules["predator-flee"].(map[string]any)["enabled"] = false

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boids/rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /boids/rules = %d: %s", rec.Code, rec.Body)
	}
	var states map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &states); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if states["flee"] || !states["attract"] || !states["wind"] {
		t.Fatalf("states = %v, want flee=false others=true", states)
	}
}

func TestToggleFlipsRulePairAndClears(t *testing.T) {
	_, mux, fake, sim := newTestService(t, true, true)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/boids/rules/flee",
		strings.NewReader(`{"enabled": false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT toggle = %d: %s", rec.Code, rec.Body)
	}

	if fake.validated != 1 || fake.applied != 1 {
		t.Fatalf("validate/apply calls = %d/%d, want 1/1", fake.validated, fake.applied)
	}
	// Both rules of the pair flipped, full rule set round-tripped.
	rules := fake.lastChanges["rules"].(map[string]any)
	if len(rules) != 6 {
		t.Fatalf("changes carried %d rules, want the complete set of 6 (absent rules get deleted)", len(rules))
	}
	for _, id := range kindRules["flee"] {
		if rules[id].(map[string]any)["enabled"] != false {
			t.Fatalf("rule %s not disabled in changes", id)
		}
	}
	if rules["food-attract"].(map[string]any)["enabled"] != true {
		t.Fatal("unrelated rule was modified")
	}
	// Active modifiers cleared on disable.
	if len(sim.cleared) != 1 || sim.cleared[0] != "flee" {
		t.Fatalf("cleared = %v, want [flee]", sim.cleared)
	}

	// Re-enable: no clear call.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/boids/rules/flee",
		strings.NewReader(`{"enabled": true}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("re-enable = %d: %s", rec.Code, rec.Body)
	}
	if len(sim.cleared) != 1 {
		t.Fatalf("cleared on enable: %v", sim.cleared)
	}
}

func TestToggleRejections(t *testing.T) {
	_, mux, _, _ := newTestService(t, true, true)
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"unknown kind", http.MethodPut, "/boids/rules/teleport", `{"enabled": false}`, http.StatusNotFound},
		{"bad body", http.MethodPut, "/boids/rules/flee", `{}`, http.StatusBadRequest},
		{"wrong method on toggle", http.MethodGet, "/boids/rules/flee", "", http.StatusMethodNotAllowed},
		{"wrong method on list", http.MethodPut, "/boids/rules", `{}`, http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body)))
			if rec.Code != tt.want {
				t.Fatalf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
			}
		})
	}
}

func TestUnavailableWithoutRuleProcessor(t *testing.T) {
	_, mux, _, _ := newTestService(t, false, false)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boids/rules", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET without rule processor = %d, want 503", rec.Code)
	}
}

func TestToggleWorksWithoutSim(t *testing.T) {
	_, mux, _, _ := newTestService(t, true, false)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/boids/rules/wind",
		strings.NewReader(`{"enabled": false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle without sim = %d: %s", rec.Code, rec.Body)
	}
}

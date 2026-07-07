// Package api provides the SemBoids domain REST service: live rule toggles
// for the demo UI. It implements service.Service and the service manager's
// HTTPHandler so it mounts at /boids on the :8080 API server.
//
// Toggles flip the actual zone-steering rules through the rule processor's
// runtime-reconfiguration interface (real hot-reload — semstreams#455,
// fixed in beta.135) and clear the kind's active modifiers in the sim so
// the visual effect stops instantly instead of draining through TTLs.
package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/c360studio/semstreams/service"
)

// ServiceName is the service-manager registration key; the HTTP prefix
// derives from it ("boids" → /boids).
const ServiceName = "boids"

// kindRules maps a modifier kind to the zone-steering rule IDs that emit it
// (the entered→modifier rule and its exited→cancel twin toggle together).
var kindRules = map[string][]string{
	"flee":    {"predator-flee", "predator-clear"},
	"attract": {"food-attract", "food-clear"},
	"wind":    {"wind-bias", "wind-clear"},
	// cull is a lifecycle rule, not a steering pair: toggling it on/off
	// enables the predator-cull transition (add-lifecycle-population). It has
	// no TTL'd modifier to clear on disable.
	"cull": {"predator-cull"},
}

// cullKind is the lifecycle rule kind — excluded from modifier clearing.
const cullKind = "cull"

// ruleReconfigurer is the slice of the rule processor the API needs — the
// service.RuntimeConfigurable trio, resolved structurally so this package
// needs no processor/rule import.
type ruleReconfigurer interface {
	GetRuntimeConfig() map[string]any
	ValidateConfigUpdate(changes map[string]any) error
	ApplyConfigUpdate(changes map[string]any) error
}

// modifierClearer is the slice of the sim component the API needs.
type modifierClearer interface {
	ClearModifierKind(kind string) error
}

// graphDialer is the sim's runtime control surface — the snapshot load dial
// plus the population controls (add-lifecycle-population).
type graphDialer interface {
	SetGraphHz(hz float64) error
	GraphHz() float64
	GraphCounts() (snapshots, entities, dropped uint64)
	SpawnBoids(n int)
	ChurnHz() float64
	SetChurnHz(hz float64) error
}

// Service exposes the rule-toggle endpoints.
type Service struct {
	*service.BaseService
	deps   *service.Dependencies
	logger *slog.Logger
}

// New creates the boids API service (service.Constructor compatible).
func New(_ json.RawMessage, deps *service.Dependencies) (service.Service, error) {
	if deps == nil || deps.ComponentRegistry == nil {
		return nil, fmt.Errorf("boids service requires a component registry")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With("service", ServiceName)

	base := service.NewBaseServiceWithOptions(
		ServiceName,
		nil,
		service.WithLogger(logger),
		service.WithMetrics(deps.MetricsRegistry),
		service.WithNATS(deps.NATSClient),
	)
	return &Service{BaseService: base, deps: deps, logger: logger}, nil
}

// rules resolves the rule processor lazily: the component manager creates
// components after services construct, so resolution happens per request.
func (s *Service) rules() (ruleReconfigurer, error) {
	for _, comp := range s.deps.ComponentRegistry.ListComponents() {
		if r, ok := comp.(ruleReconfigurer); ok {
			return r, nil
		}
	}
	return nil, fmt.Errorf("rule processor not available")
}

// clearer resolves the sim component lazily (optional — clearing is a
// visual nicety; toggling still works if the sim is absent).
func (s *Service) clearer() modifierClearer {
	for _, comp := range s.deps.ComponentRegistry.ListComponents() {
		if c, ok := comp.(modifierClearer); ok {
			return c
		}
	}
	return nil
}

// kindStates derives per-kind enabled state from the live rule config: a
// kind is enabled when its primary (entered→modifier) rule is enabled.
func kindStates(cfg map[string]any) map[string]bool {
	rules, _ := cfg["rules"].(map[string]any)
	out := make(map[string]bool, len(kindRules))
	for kind, ids := range kindRules {
		enabled := false
		if rule, ok := rules[ids[0]].(map[string]any); ok {
			enabled, _ = rule["enabled"].(bool)
		}
		out[kind] = enabled
	}
	return out
}

// OpenAPISpec documents the rule-toggle endpoints (required half of the
// service manager's HTTPHandler interface — without it the handlers are
// never mounted).
func (s *Service) OpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Boids", Description: "Flock zone-rule toggles"},
		},
		Paths: map[string]service.PathSpec{
			"/rules": {
				GET: &service.OperationSpec{
					Summary:     "List rule states",
					Description: "Returns enabled state per modifier kind (flee, attract, wind), derived from the live rule engine config",
					Tags:        []string{"Boids"},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Enabled state per kind", ContentType: "application/json"},
					},
				},
			},
			"/rules/{kind}": {
				PUT: &service.OperationSpec{
					Summary:     "Toggle a zone rule pair",
					Description: "Enables or disables the rules emitting one modifier kind; body {\"enabled\": bool}",
					Tags:        []string{"Boids"},
					Parameters: []service.ParameterSpec{
						{Name: "kind", In: "path", Required: true,
							Description: "Modifier kind (flee, attract, wind)",
							Schema:      service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "New state", ContentType: "application/json"},
						"404": {Description: "Unknown kind"},
					},
				},
			},
		},
	}
}

// RegisterHTTPHandlers mounts the rule-toggle endpoints on the service
// manager's HTTP server.
func (s *Service) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc(prefix+"rules", s.handleRules)
	mux.HandleFunc(prefix+"rules/", s.handleRuleToggle)
	mux.HandleFunc(prefix+"graph", s.handleGraph)
	mux.HandleFunc(prefix+"graph/hz", s.handleGraphHz)
	mux.HandleFunc(prefix+"graph/stream", s.handleGraphStream)
	mux.HandleFunc(prefix+"population/spawn", s.handleSpawn)
	mux.HandleFunc(prefix+"population/churn-hz", s.handleChurnHz)
	s.logger.Info("Boids API handlers registered", "prefix", prefix)
}

// dialer resolves the sim's dial surface lazily.
func (s *Service) dialer() (graphDialer, error) {
	for _, comp := range s.deps.ComponentRegistry.ListComponents() {
		if d, ok := comp.(graphDialer); ok {
			return d, nil
		}
	}
	return nil, fmt.Errorf("sim component not available")
}

// handleGraph serves GET <prefix>/graph: dial state + pipeline counters.
func (s *Service) handleGraph(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d, err := s.dialer()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	snapshots, entities, dropped := d.GraphCounts()
	writeJSON(w, http.StatusOK, map[string]any{
		"hz":        d.GraphHz(),
		"snapshots": snapshots,
		"entities":  entities,
		"dropped":   dropped,
	})
}

// handleGraphHz serves PUT <prefix>/graph/hz with body {"hz": N} — the
// runtime load dial.
func (s *Service) handleGraphHz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Hz *float64 `json:"hz"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Hz == nil {
		http.Error(w, `body must be {"hz": <number>}`, http.StatusBadRequest)
		return
	}
	d, err := s.dialer()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := d.SetGraphHz(*req.Hz); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.logger.Info("Graph dial set", "hz", d.GraphHz())
	writeJSON(w, http.StatusOK, map[string]any{"hz": d.GraphHz()})
}

// handleSpawn serves POST <prefix>/population/spawn with body {"n": N}: stages
// a spawn wave of N boids (add-lifecycle-population).
func (s *Service) handleSpawn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		N *int `json:"n"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.N == nil || *req.N <= 0 {
		http.Error(w, `body must be {"n": <positive int>}`, http.StatusBadRequest)
		return
	}
	d, err := s.dialer()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	d.SpawnBoids(*req.N)
	s.logger.Info("Spawn wave requested", "n", *req.N)
	writeJSON(w, http.StatusOK, map[string]any{"spawned": *req.N})
}

// handleChurnHz serves PUT <prefix>/population/churn-hz with body {"hz": N}:
// the runtime spawn-churn dial (the create/delete load axis).
func (s *Service) handleChurnHz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Hz *float64 `json:"hz"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Hz == nil {
		http.Error(w, `body must be {"hz": <number>}`, http.StatusBadRequest)
		return
	}
	d, err := s.dialer()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := d.SetChurnHz(*req.Hz); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.logger.Info("Churn dial set", "hz", d.ChurnHz())
	writeJSON(w, http.StatusOK, map[string]any{"churn_hz": d.ChurnHz()})
}

// handleRules serves GET <prefix>/rules: enabled state per modifier kind.
func (s *Service) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rp, err := s.rules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, kindStates(rp.GetRuntimeConfig()))
}

// handleRuleToggle serves PUT <prefix>/rules/{kind} with body
// {"enabled": bool}: flips the kind's rule pair through the rule engine's
// validate+apply runtime path, then clears active modifiers on disable.
func (s *Service) handleRuleToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kind := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	ruleIDs, known := kindRules[kind]
	if !known {
		http.Error(w, fmt.Sprintf("unknown modifier kind %q", kind), http.StatusNotFound)
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		http.Error(w, `body must be {"enabled": true|false}`, http.StatusBadRequest)
		return
	}

	rp, err := s.rules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// applyRuleChanges treats the rules map as the complete set (absent
	// rules are deleted), so round-trip the full live config with just the
	// kind's pair flipped.
	cfg := rp.GetRuntimeConfig()
	rules, ok := cfg["rules"].(map[string]any)
	if !ok || len(rules) == 0 {
		http.Error(w, "rule engine has no rules loaded", http.StatusServiceUnavailable)
		return
	}
	found := 0
	for _, id := range ruleIDs {
		if rule, ok := rules[id].(map[string]any); ok {
			rule["enabled"] = *req.Enabled
			found++
		}
	}
	if found == 0 {
		http.Error(w, fmt.Sprintf("rules for kind %q not loaded", kind), http.StatusNotFound)
		return
	}

	changes := map[string]any{"rules": rules}
	if err := rp.ValidateConfigUpdate(changes); err != nil {
		http.Error(w, fmt.Sprintf("validate rule update: %v", err), http.StatusBadRequest)
		return
	}
	if err := rp.ApplyConfigUpdate(changes); err != nil {
		http.Error(w, fmt.Sprintf("apply rule update: %v", err), http.StatusInternalServerError)
		return
	}

	// Disabling stops new modifiers via the rules; clearing makes the
	// existing ones stop influencing boids right now. Cull has no lingering
	// modifier (a terminal lifecycle transition), so skip the clearer for it.
	if !*req.Enabled && kind != cullKind {
		if c := s.clearer(); c != nil {
			if err := c.ClearModifierKind(kind); err != nil {
				s.logger.Warn("clear modifiers after disable", "kind", kind, "error", err)
			}
		}
	}

	s.logger.Info("Zone rules toggled", "kind", kind, "rules", ruleIDs, "enabled", *req.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "enabled": *req.Enabled, "rules": ruleIDs})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

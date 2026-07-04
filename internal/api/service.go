// Package api provides the SemBoids domain REST service: live rule-gate
// toggles for the demo UI. It implements service.Service and the service
// manager's HTTPHandler so it mounts at /boids on the :8080 API server.
//
// The gate flips modifier kinds in the sim component (D5 fallback while the
// rule engine's runtime reconfiguration is unreachable over HTTP —
// https://github.com/C360Studio/semstreams/issues/455). When #455 lands,
// these endpoints swap to real rule toggling without UI changes.
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

// modifierGate is the slice of the sim component the API needs.
type modifierGate interface {
	SetModifierKindEnabled(kind string, enabled bool) error
	ModifierKindStates() map[string]bool
}

// Service exposes the rule-gate endpoints.
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

// gate resolves the sim component lazily: the component manager creates it
// after services construct, so resolution must happen per request.
func (s *Service) gate() (modifierGate, error) {
	for _, comp := range s.deps.ComponentRegistry.ListComponents() {
		if g, ok := comp.(modifierGate); ok {
			return g, nil
		}
	}
	return nil, fmt.Errorf("sim component not available")
}

// OpenAPISpec documents the rule-gate endpoints (required half of the
// service manager's HTTPHandler interface — without it the handlers are
// never mounted).
func (s *Service) OpenAPISpec() *service.OpenAPISpec {
	return &service.OpenAPISpec{
		Tags: []service.TagSpec{
			{Name: "Boids", Description: "Flock rule-gate toggles"},
		},
		Paths: map[string]service.PathSpec{
			"/rules": {
				GET: &service.OperationSpec{
					Summary:     "List rule gates",
					Description: "Returns enabled state per modifier kind (flee, attract, wind)",
					Tags:        []string{"Boids"},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "Gate state per kind", ContentType: "application/json"},
					},
				},
			},
			"/rules/{kind}": {
				PUT: &service.OperationSpec{
					Summary:     "Toggle a rule gate",
					Description: "Enables or disables one modifier kind; body {\"enabled\": bool}",
					Tags:        []string{"Boids"},
					Parameters: []service.ParameterSpec{
						{Name: "kind", In: "path", Required: true,
							Description: "Modifier kind (flee, attract, wind)",
							Schema:      service.Schema{Type: "string"}},
					},
					Responses: map[string]service.ResponseSpec{
						"200": {Description: "New gate state", ContentType: "application/json"},
						"404": {Description: "Unknown kind"},
					},
				},
			},
		},
	}
}

// RegisterHTTPHandlers mounts the rule-gate endpoints on the service
// manager's HTTP server.
func (s *Service) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	mux.HandleFunc(prefix+"rules", s.handleRules)
	mux.HandleFunc(prefix+"rules/", s.handleRuleToggle)
	s.logger.Info("Boids API handlers registered", "prefix", prefix)
}

// handleRules serves GET <prefix>/rules: the gate state per modifier kind.
func (s *Service) handleRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	g, err := s.gate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, g.ModifierKindStates())
}

// handleRuleToggle serves PUT <prefix>/rules/{kind} with body
// {"enabled": bool}.
func (s *Service) handleRuleToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kind := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if kind == "" {
		http.Error(w, "kind is required", http.StatusBadRequest)
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		http.Error(w, `body must be {"enabled": true|false}`, http.StatusBadRequest)
		return
	}

	g, err := s.gate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	if err := g.SetModifierKindEnabled(kind, *req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.logger.Info("Rule gate toggled", "kind", kind, "enabled", *req.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "enabled": *req.Enabled})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

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

	"github.com/c360studio/semboids/internal/sim"
)

// newTestService wires the API service against a registry holding a real
// sim component instance.
func newTestService(t *testing.T) (*Service, *http.ServeMux) {
	t.Helper()
	registry := component.NewRegistry()
	if err := sim.Register(registry); err != nil {
		t.Fatalf("register sim: %v", err)
	}
	// An unconnected client satisfies dependency validation; the component
	// is never started in these tests.
	nc, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatalf("new nats client: %v", err)
	}
	if _, err := registry.CreateComponent("sim", types.ComponentConfig{
		Type: types.ComponentTypeInput, Name: "sim", Enabled: true,
		Config: json.RawMessage(`{"boids": 5}`),
	}, component.Dependencies{NATSClient: nc}); err != nil {
		t.Fatalf("create sim: %v", err)
	}

	svc, err := New(nil, &service.Dependencies{ComponentRegistry: registry})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := svc.(*Service)
	mux := http.NewServeMux()
	s.RegisterHTTPHandlers("/boids", mux)
	return s, mux
}

func TestGetRules(t *testing.T) {
	_, mux := newTestService(t)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boids/rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /boids/rules = %d: %s", rec.Code, rec.Body)
	}
	var states map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &states); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !states["flee"] || !states["attract"] || !states["wind"] {
		t.Fatalf("kinds not all enabled by default: %v", states)
	}
}

func TestToggleRule(t *testing.T) {
	_, mux := newTestService(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/boids/rules/flee",
		strings.NewReader(`{"enabled": false}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT toggle = %d: %s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boids/rules", nil))
	var states map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &states); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if states["flee"] {
		t.Fatal("flee still enabled after toggle off")
	}
}

func TestToggleRejections(t *testing.T) {
	_, mux := newTestService(t)
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

func TestGateUnavailableWithoutSim(t *testing.T) {
	svc, err := New(nil, &service.Dependencies{ComponentRegistry: component.NewRegistry()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mux := http.NewServeMux()
	svc.(*Service).RegisterHTTPHandlers("/boids", mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boids/rules", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET without sim = %d, want 503", rec.Code)
	}
}

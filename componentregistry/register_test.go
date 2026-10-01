package componentregistry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/c360studio/semboids/internal/api"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
)

func TestFactoryBindsControlsAfterServiceConstruction(t *testing.T) {
	controls := &api.Controls{}
	registry := component.NewRegistry()
	if err := RegisterWithControls(registry, controls); err != nil {
		t.Fatal(err)
	}
	svc, err := api.NewWithControls(nil, &service.Dependencies{}, controls)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc.(*api.Service).RegisterHTTPHandlers("/boids", mux)
	check := func(want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boids/graph", nil))
		if rec.Code != want {
			t.Fatalf("GET graph=%d: %s", rec.Code, rec.Body)
		}
	}
	check(http.StatusServiceUnavailable)
	nc, err := natsclient.NewClient("nats://localhost:4222")
	if err != nil {
		t.Fatal(err)
	}
	factory, ok := registry.GetFactory("sim")
	if !ok {
		t.Fatal("sim factory missing")
	}
	_, err = factory(json.RawMessage(`{"graph_hz":0}`), component.Dependencies{NATSClient: nc, Platform: types.PlatformMeta{Org: "c360", Platform: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	check(http.StatusOK)
	for name, registration := range registry.ListFactories() {
		if registration.Ports == nil {
			t.Errorf("%s lost its static ports", name)
		}
	}
}

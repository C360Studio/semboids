//go:build integration

package zone

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	graphingest "github.com/c360studio/semstreams/processor/graph-ingest"
	"github.com/c360studio/semstreams/types"
)

// TestZonesLandInEntityStates runs the real graph-ingest component against a
// NATS testcontainer and asserts ingested zones appear in ENTITY_STATES with
// their triples — the host never touches the bucket directly.
func TestZonesLandInEntityStates(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithStreams(natsclient.TestStreamConfig{
		Name:     "ENTITY",
		Subjects: []string{"entity.>"},
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Payload registry with builtins + our zone payload, as the host wires it.
	payloadReg := payloadregistry.New()
	if err := payloadbuiltins.Register(payloadReg); err != nil {
		t.Fatalf("register builtins: %v", err)
	}
	if err := RegisterPayloads(payloadReg); err != nil {
		t.Fatalf("register zone payloads: %v", err)
	}

	// Real graph-ingest with default ports (jetstream entity.>).
	registry := component.NewRegistry()
	if err := graphingest.Register(registry); err != nil {
		t.Fatalf("register graph-ingest: %v", err)
	}
	deps := component.Dependencies{
		NATSClient:      tc.Client,
		Logger:          slog.Default(),
		Platform:        component.PlatformMeta{Org: "c360", Platform: "semboids"},
		PayloadRegistry: payloadReg,
	}
	inst, err := registry.CreateComponent("graph-ingest-test", types.ComponentConfig{
		Type:    types.ComponentTypeProcessor,
		Name:    "graph-ingest",
		Enabled: true,
		Config: json.RawMessage(`{
			"ports": {
				"inputs": [
					{"name": "entity_stream", "subject": "entity.>", "type": "jetstream", "stream_name": "ENTITY"}
				],
				"outputs": [
					{"name": "entity_states", "type": "kv-write", "subject": "ENTITY_STATES"}
				]
			}
		}`),
	}, deps)
	if err != nil {
		t.Fatalf("create graph-ingest: %v", err)
	}
	lc, ok := component.AsLifecycleComponent(inst)
	if !ok {
		t.Fatal("graph-ingest is not a lifecycle component")
	}
	if err := lc.Initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if err := lc.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = lc.Stop(5 * time.Second) }()

	// Ingest the zones through the real pipeline.
	zones := validZones()
	if err := Ingest(ctx, tc.Client, zones, "c360", "semboids"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Poll ENTITY_STATES for the predator zone entity.
	wantID := "c360.semboids.sim.flock.zone.pred-1"
	bucket, err := tc.Client.WaitForBucket(ctx, "ENTITY_STATES", 30*time.Second)
	if err != nil {
		t.Fatalf("ENTITY_STATES bucket: %v", err)
	}

	deadline := time.After(30 * time.Second)
	for {
		entry, err := bucket.Get(ctx, wantID)
		if err == nil {
			var state struct {
				Triples []struct {
					Predicate string `json:"predicate"`
					Object    any    `json:"object"`
				} `json:"triples"`
			}
			if err := json.Unmarshal(entry.Value(), &state); err != nil {
				t.Fatalf("unmarshal entity state: %v", err)
			}
			preds := map[string]any{}
			for _, tr := range state.Triples {
				preds[tr.Predicate] = tr.Object
			}
			if preds["zone.classification.type"] != "predator" {
				t.Fatalf("zone type triple missing/wrong: %v", preds)
			}
			if preds["zone.geometry.radius"] != 80.0 {
				t.Fatalf("zone radius triple missing/wrong: %v", preds)
			}
			return // success
		}
		select {
		case <-deadline:
			t.Fatalf("zone entity %q never appeared in ENTITY_STATES (last err: %v)", wantID, err)
		case <-time.After(250 * time.Millisecond):
		}
	}
}

package zone

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/payloadregistry"
)

func testEntity() *Entity {
	return &Entity{
		Zone:       Zone{ID: "pred-1", Type: TypePredator, X: 400, Y: 300, R: 80, Strength: 1.0},
		OrgID:      "c360",
		Platform:   "semboids",
		ObservedAt: time.UnixMilli(1719936000123),
	}
}

func TestEntityID6Part(t *testing.T) {
	got := testEntity().EntityID()
	want := "c360.semboids.sim.flock.zone.pred-1"
	if got != want {
		t.Fatalf("EntityID = %q, want %q", got, want)
	}
}

func TestEntityTriples(t *testing.T) {
	e := testEntity()
	triples := e.Triples()
	if len(triples) == 0 {
		t.Fatal("no triples produced")
	}

	found := map[string]any{}
	for _, tr := range triples {
		if tr.Subject != e.EntityID() {
			t.Fatalf("triple %q has subject %q, want %q", tr.Predicate, tr.Subject, e.EntityID())
		}
		found[tr.Predicate] = tr.Object
	}

	wantPredicates := map[string]any{
		"zone.classification.type": "predator",
		"zone.geometry.x":          400.0,
		"zone.geometry.y":          300.0,
		"zone.geometry.radius":     80.0,
		"zone.behavior.strength":   1.0,
	}
	for pred, want := range wantPredicates {
		got, ok := found[pred]
		if !ok {
			t.Fatalf("missing predicate %q (got %v)", pred, found)
		}
		if got != want {
			t.Fatalf("predicate %q = %v, want %v", pred, got, want)
		}
	}
}

func TestWindEntityHasDirectionTriples(t *testing.T) {
	e := testEntity()
	e.Zone = Zone{ID: "wind-1", Type: TypeWind, X: 800, Y: 450, R: 200, Strength: 0.4, DX: 1, DY: -0.5}
	found := map[string]any{}
	for _, tr := range e.Triples() {
		found[tr.Predicate] = tr.Object
	}
	if found["zone.wind.dx"] != 1.0 || found["zone.wind.dy"] != -0.5 {
		t.Fatalf("wind direction triples missing or wrong: %v", found)
	}
}

func TestPayloadSchemaAndValidate(t *testing.T) {
	e := testEntity()
	if got := e.Schema().String(); got != "boids.zone.v1" {
		t.Fatalf("Schema = %q, want boids.zone.v1", got)
	}
	if err := e.Validate(); err != nil {
		t.Fatalf("valid entity rejected: %v", err)
	}
	bad := testEntity()
	bad.OrgID = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("entity without org accepted")
	}
}

func TestRegisterPayloads(t *testing.T) {
	reg := payloadregistry.New()
	if err := RegisterPayloads(reg); err != nil {
		t.Fatalf("RegisterPayloads: %v", err)
	}
	if err := RegisterPayloads(reg); err == nil {
		t.Fatal("double registration accepted, want error")
	}
}

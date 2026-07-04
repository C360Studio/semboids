package sim

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semboids/internal/flock"
	"github.com/c360studio/semboids/internal/zone"
)

func TestFrameMarshalsCompactWireFormat(t *testing.T) {
	p := flock.DefaultParams()
	boids := []flock.Boid{
		{ID: 0, Pos: flock.Vec2{X: 100, Y: 200}, Vel: flock.Vec2{X: 1.5, Y: -2}},
		{ID: 1, Pos: flock.Vec2{X: 0.25, Y: 900}, Vel: flock.Vec2{X: 0, Y: 3}},
	}
	zones := []zone.Zone{
		{ID: "pred-1", Type: zone.TypePredator, X: 400, Y: 300, R: 80, Strength: 1},
	}
	mods := []uint8{0, 1}
	f := NewFrame(42, time.UnixMilli(1719936000123), p, boids, zones, mods)
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"tick":42,"t":1719936000123,"w":1600,"h":900,` +
		`"zones":[["predator",400,300,80]],` +
		`"boids":[[0,100,200,1.5,-2,0],[1,0.25,900,0,3,1]]}`
	if string(data) != want {
		t.Fatalf("wire format mismatch:\ngot  %s\nwant %s", data, want)
	}
}

func TestFrameWithoutZonesOrMods(t *testing.T) {
	p := flock.DefaultParams()
	boids := []flock.Boid{{ID: 0, Pos: flock.Vec2{X: 1, Y: 2}, Vel: flock.Vec2{X: 3, Y: 4}}}
	f := NewFrame(1, time.UnixMilli(1000), p, boids, nil, nil)
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"tick":1,"t":1000,"w":1600,"h":900,"zones":[],"boids":[[0,1,2,3,4,0]]}`
	if string(data) != want {
		t.Fatalf("wire format mismatch:\ngot  %s\nwant %s", data, want)
	}
}

func TestFrameRoundTrips(t *testing.T) {
	p := flock.DefaultParams()
	boids := []flock.Boid{{ID: 7, Pos: flock.Vec2{X: 5, Y: 6}, Vel: flock.Vec2{X: -1, Y: 0.5}}}
	data, err := json.Marshal(NewFrame(9, time.UnixMilli(1000), p, boids, nil, []uint8{2}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded struct {
		Tick  uint64      `json:"tick"`
		T     int64       `json:"t"`
		W     float64     `json:"w"`
		H     float64     `json:"h"`
		Zones [][]any     `json:"zones"`
		Boids [][]float64 `json:"boids"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Tick != 9 || decoded.T != 1000 || decoded.W != p.Width || decoded.H != p.Height {
		t.Fatalf("header mismatch: %+v", decoded)
	}
	if len(decoded.Boids) != 1 || len(decoded.Boids[0]) != 6 {
		t.Fatalf("boids shape mismatch: %v", decoded.Boids)
	}
	got := decoded.Boids[0]
	if got[0] != 7 || got[1] != 5 || got[2] != 6 || got[3] != -1 || got[4] != 0.5 || got[5] != 2 {
		t.Fatalf("boid values mismatch: %v", got)
	}
}

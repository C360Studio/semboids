package zone

import (
	"strings"
	"testing"
)

func validZones() []Zone {
	return []Zone{
		{ID: "pred-1", Type: TypePredator, X: 400, Y: 300, R: 80, Strength: 1.0},
		{ID: "food-1", Type: TypeFood, X: 1200, Y: 600, R: 100, Strength: 0.6},
		{ID: "wind-1", Type: TypeWind, X: 800, Y: 450, R: 200, Strength: 0.4, DX: 1, DY: 0},
	}
}

func TestValidateAcceptsValidSet(t *testing.T) {
	if err := Validate(validZones()); err != nil {
		t.Fatalf("valid set rejected: %v", err)
	}
}

func TestValidateRejections(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(zs []Zone) []Zone
		wantSub string
	}{
		{
			"unknown type",
			func(zs []Zone) []Zone { zs[0].Type = "blackhole"; return zs },
			"blackhole",
		},
		{
			"zero radius",
			func(zs []Zone) []Zone { zs[1].R = 0; return zs },
			"radius",
		},
		{
			"negative radius",
			func(zs []Zone) []Zone { zs[1].R = -5; return zs },
			"radius",
		},
		{
			"duplicate ids",
			func(zs []Zone) []Zone { zs[1].ID = zs[0].ID; return zs },
			"duplicate",
		},
		{
			"empty id",
			func(zs []Zone) []Zone { zs[2].ID = ""; return zs },
			"id",
		},
		{
			"wind without direction",
			func(zs []Zone) []Zone { zs[2].DX, zs[2].DY = 0, 0; return zs },
			"direction",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.mutate(validZones()))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tt.wantSub)
			}
		})
	}
}

func TestContains(t *testing.T) {
	z := Zone{ID: "z", Type: TypeFood, X: 100, Y: 100, R: 50, Strength: 1}
	tests := []struct {
		name string
		x, y float64
		want bool
	}{
		{"center", 100, 100, true},
		{"inside", 130, 100, true},
		{"boundary", 150, 100, true},
		{"outside", 151, 100, false},
		{"diagonal inside", 130, 130, true},
		{"diagonal outside", 140, 140, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := z.Contains(tt.x, tt.y); got != tt.want {
				t.Fatalf("Contains(%v,%v) = %v, want %v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}

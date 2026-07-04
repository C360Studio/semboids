package flock

import (
	"math"
	"testing"
)

const eps = 1e-9

func TestVec2Ops(t *testing.T) {
	tests := []struct {
		name string
		got  Vec2
		want Vec2
	}{
		{"add", Vec2{1, 2}.Add(Vec2{3, -1}), Vec2{4, 1}},
		{"sub", Vec2{1, 2}.Sub(Vec2{3, -1}), Vec2{-2, 3}},
		{"scale", Vec2{1, -2}.Scale(2.5), Vec2{2.5, -5}},
		{"limit under", Vec2{3, 4}.Limit(10), Vec2{3, 4}},
		{"limit over", Vec2{3, 4}.Limit(2.5), Vec2{1.5, 2}},
		{"limit zero vec", Vec2{}.Limit(5), Vec2{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if math.Abs(tt.got.X-tt.want.X) > eps || math.Abs(tt.got.Y-tt.want.Y) > eps {
				t.Fatalf("got %+v, want %+v", tt.got, tt.want)
			}
		})
	}
}

func TestVec2Len(t *testing.T) {
	if got := (Vec2{3, 4}).Len(); math.Abs(got-5) > eps {
		t.Fatalf("Len() = %v, want 5", got)
	}
	if got := (Vec2{}).Len(); got != 0 {
		t.Fatalf("Len() of zero vec = %v, want 0", got)
	}
}

func TestTorusDelta(t *testing.T) {
	const w, h = 100.0, 80.0
	tests := []struct {
		name string
		a, b Vec2
		want Vec2
	}{
		{"direct", Vec2{10, 10}, Vec2{20, 30}, Vec2{10, 20}},
		{"wrap x", Vec2{95, 10}, Vec2{5, 10}, Vec2{10, 0}},
		{"wrap x negative", Vec2{5, 10}, Vec2{95, 10}, Vec2{-10, 0}},
		{"wrap y", Vec2{10, 75}, Vec2{10, 5}, Vec2{0, 10}},
		{"wrap both", Vec2{95, 75}, Vec2{5, 5}, Vec2{10, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := torusDelta(tt.a, tt.b, w, h)
			if math.Abs(got.X-tt.want.X) > eps || math.Abs(got.Y-tt.want.Y) > eps {
				t.Fatalf("torusDelta(%+v, %+v) = %+v, want %+v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	const w, h = 100.0, 80.0
	tests := []struct {
		name string
		in   Vec2
		want Vec2
	}{
		{"inside", Vec2{50, 40}, Vec2{50, 40}},
		{"over x", Vec2{105, 40}, Vec2{5, 40}},
		{"under x", Vec2{-5, 40}, Vec2{95, 40}},
		{"over y", Vec2{50, 85}, Vec2{50, 5}},
		{"under y", Vec2{50, -5}, Vec2{50, 75}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wrap(tt.in, w, h)
			if math.Abs(got.X-tt.want.X) > eps || math.Abs(got.Y-tt.want.Y) > eps {
				t.Fatalf("wrap(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

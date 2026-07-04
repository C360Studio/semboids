package flock

import "math"

// Vec2 is a 2D vector.
type Vec2 struct {
	X, Y float64
}

// Add returns v + o.
func (v Vec2) Add(o Vec2) Vec2 { return Vec2{v.X + o.X, v.Y + o.Y} }

// Sub returns v - o.
func (v Vec2) Sub(o Vec2) Vec2 { return Vec2{v.X - o.X, v.Y - o.Y} }

// Scale returns v scaled by s.
func (v Vec2) Scale(s float64) Vec2 { return Vec2{v.X * s, v.Y * s} }

// Len returns the vector's magnitude.
func (v Vec2) Len() float64 { return math.Hypot(v.X, v.Y) }

// Limit returns v clamped to at most maxLen magnitude, preserving direction.
func (v Vec2) Limit(maxLen float64) Vec2 {
	l := v.Len()
	if l <= maxLen || l == 0 {
		return v
	}
	return v.Scale(maxLen / l)
}

// torusDelta returns the shortest vector from a to b on a w×h torus.
func torusDelta(a, b Vec2, w, h float64) Vec2 {
	dx := b.X - a.X
	switch {
	case dx > w/2:
		dx -= w
	case dx < -w/2:
		dx += w
	}
	dy := b.Y - a.Y
	switch {
	case dy > h/2:
		dy -= h
	case dy < -h/2:
		dy += h
	}
	return Vec2{dx, dy}
}

// wrap maps v into [0,w) × [0,h) with toroidal wrapping.
func wrap(v Vec2, w, h float64) Vec2 {
	x := math.Mod(v.X, w)
	if x < 0 {
		x += w
	}
	y := math.Mod(v.Y, h)
	if y < 0 {
		y += h
	}
	return Vec2{x, y}
}

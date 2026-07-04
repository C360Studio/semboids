package flock

// Params holds all tunables for the simulation. Radii and positions are in
// world units; speeds are units/second, forces units/second²; DT is the fixed
// timestep in seconds. Identical Params + seed yield identical trajectories.
type Params struct {
	// World dimensions (toroidal).
	Width, Height float64

	// SeparationRadius is the repulsion range; must be <= NeighborRadius.
	SeparationRadius float64
	// NeighborRadius is the cohesion/alignment range and the spatial-hash
	// cell size.
	NeighborRadius float64

	// Rule weights; zero disables a rule.
	SeparationWeight, CohesionWeight, AlignmentWeight float64

	// MaxSpeed clamps velocity magnitude (units/second).
	MaxSpeed float64
	// MaxForce clamps per-rule and total steering (units/second²).
	MaxForce float64

	// DT is the fixed timestep (seconds per tick).
	DT float64
}

// DefaultParams returns the standard 1600×900 world at 30Hz with classic
// Reynolds weighting (separation biased over cohesion/alignment).
func DefaultParams() Params {
	return Params{
		Width:            1600,
		Height:           900,
		SeparationRadius: 25,
		NeighborRadius:   50,
		SeparationWeight: 1.5,
		CohesionWeight:   1.0,
		AlignmentWeight:  1.0,
		MaxSpeed:         120,
		MaxForce:         240,
		DT:               1.0 / 30,
	}
}

package flock

import "testing"

func benchmarkTick(b *testing.B, n int) {
	e := NewEngine(n, 42, DefaultParams())
	// Warm up so scratch and buckets reach steady-state capacity.
	for range 10 {
		e.Tick()
	}
	b.ResetTimer()
	for b.Loop() {
		e.Tick()
	}
}

func BenchmarkTick200(b *testing.B) { benchmarkTick(b, 200) }
func BenchmarkTick500(b *testing.B) { benchmarkTick(b, 500) }

// TestTickSteadyStateAllocs pins the ~zero-allocation goal from the design.
// Bucket capacities grow amortized as flocks form and migrate (a bucket can
// always meet a new max occupancy), so the assertion is <0.5 allocs/tick on
// average: rare growth passes, any constant per-tick allocation (≥1.0) fails.
func TestTickSteadyStateAllocs(t *testing.T) {
	e := NewEngine(200, 42, DefaultParams())
	for range 300 {
		e.Tick()
	}
	allocs := testing.AllocsPerRun(100, func() { e.Tick() })
	if allocs >= 0.5 {
		t.Fatalf("steady-state Tick allocates %.2f allocs/op, want ~0", allocs)
	}
}

// TestTickBudgetHeadroom asserts the spec scenario "Tick budget holds at
// target scale": a 500-boid tick stays far below the 33ms tick period.
// The bound is deliberately loose (5ms) to stay robust on slow CI runners.
func TestTickBudgetHeadroom(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	e := NewEngine(500, 42, DefaultParams())
	for range 10 {
		e.Tick()
	}
	res := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			e.Tick()
		}
	})
	perTick := res.NsPerOp()
	if perTick > 5_000_000 {
		t.Fatalf("tick at 500 boids took %dns, want < 5ms (33ms budget)", perTick)
	}
	t.Logf("tick at 500 boids: %dns (budget 33ms)", perTick)
}

package sim

import (
	"context"
	"sync"
)

// drainPool bounds the sim's off-loop lifecycle IO (Manager.Create for spawns,
// graph.mutation.entity.delete for reclaims) to a fixed number of in-flight
// operations. Distinct boids have distinct entity IDs → distinct graph-ingest
// lanes (beta.142 ADR-072), so their calls run concurrently; the pool caps the
// concurrency at the lane count so a spawn burst never fans out into unbounded
// goroutines. submit blocks its caller when the pool is full — backpressure
// onto the spawn-create/cull-watch goroutines, never onto the physics tick loop
// (ADR-001).
type drainPool struct {
	sem chan struct{}
	wg  sync.WaitGroup
}

// newDrainPool builds a pool with n concurrent slots. n < 1 clamps to 1 — the
// serial path (parity with today, and the A/B baseline for the churn campaign).
func newDrainPool(n int) *drainPool {
	if n < 1 {
		n = 1
	}
	return &drainPool{sem: make(chan struct{}, n)}
}

// submit runs fn in a bounded goroutine. It blocks until a slot frees or ctx is
// done; on ctx-done it drops fn (the component is shutting down) and returns
// without running it.
func (p *drainPool) submit(ctx context.Context, fn func()) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() { <-p.sem }()
		fn()
	}()
}

// wait blocks until all in-flight closures finish — called on Stop so a reclaim
// or create in flight completes (or its cancelled-ctx Request errors out)
// before the component tears down.
func (p *drainPool) wait() { p.wg.Wait() }

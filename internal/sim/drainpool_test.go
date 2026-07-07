package sim

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestDrainPoolBoundsConcurrency asserts the pool never runs more than n
// closures at once and does reach n (concurrency, not just serialization).
func TestDrainPoolBoundsConcurrency(t *testing.T) {
	const n = 4
	const total = 40
	pool := newDrainPool(n)

	var inFlight, maxSeen atomic.Int32
	gate := make(chan struct{})           // holds the first cohort concurrent
	entered := make(chan struct{}, total) // one signal per closure entry
	submitted := make(chan struct{})      // closes when the submit loop is done

	go func() {
		for i := 0; i < total; i++ {
			pool.submit(context.Background(), func() {
				cur := inFlight.Add(1)
				for { // maxSeen = max(maxSeen, cur)
					m := maxSeen.Load()
					if cur <= m || maxSeen.CompareAndSwap(m, cur) {
						break
					}
				}
				entered <- struct{}{}
				<-gate
				inFlight.Add(-1)
			})
		}
		close(submitted)
	}()

	// Exactly n closures acquire the n slots and block on the gate; the submit
	// loop is now parked on the (n+1)th, so in-flight is pinned at n.
	for i := 0; i < n; i++ {
		<-entered
	}
	if got := inFlight.Load(); got != int32(n) {
		t.Fatalf("in-flight while full = %d, want %d", got, n)
	}

	close(gate) // release everyone; remaining closures drain n-at-a-time
	<-submitted // let the submit loop finish before wait() (no concurrent Add)
	pool.wait()
	if got := maxSeen.Load(); got != int32(n) {
		t.Fatalf("max concurrent = %d, want exactly %d", got, n)
	}
}

// TestDrainPoolSubmitBlocksWhenFull asserts submit backpressures the caller
// instead of spawning an unbounded goroutine when every slot is busy.
func TestDrainPoolSubmitBlocksWhenFull(t *testing.T) {
	pool := newDrainPool(1)
	gate := make(chan struct{})
	entered := make(chan struct{})
	pool.submit(context.Background(), func() { close(entered); <-gate })
	<-entered // the one slot is now busy

	blocked := make(chan struct{})
	go func() {
		pool.submit(context.Background(), func() {})
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("submit returned while the pool was full; expected backpressure")
	case <-time.After(50 * time.Millisecond):
		// still blocked — correct
	}
	close(gate) // free the busy slot
	<-blocked   // the parked submit now proceeds
	pool.wait()
}

// TestDrainPoolWaitJoinsInFlight asserts wait() does not return until every
// submitted closure has run.
func TestDrainPoolWaitJoinsInFlight(t *testing.T) {
	pool := newDrainPool(4)
	var done atomic.Int32
	const total = 20
	for i := 0; i < total; i++ {
		pool.submit(context.Background(), func() { done.Add(1) })
	}
	pool.wait()
	if got := done.Load(); got != total {
		t.Fatalf("completed %d closures, want %d", got, total)
	}
}

// TestDrainPoolCancelledCtxDropsWork asserts submit drops fn (never runs it)
// when the pool is full and ctx is already done — the shutdown path.
func TestDrainPoolCancelledCtxDropsWork(t *testing.T) {
	pool := newDrainPool(1)
	gate := make(chan struct{})
	entered := make(chan struct{})
	pool.submit(context.Background(), func() { close(entered); <-gate })
	<-entered // fill the only slot so submit can't acquire

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var ran atomic.Bool
	pool.submit(ctx, func() { ran.Store(true) }) // full pool + done ctx → drop
	if ran.Load() {
		t.Fatal("submit ran fn despite a full pool and a cancelled context")
	}

	close(gate)
	pool.wait()
}

// TestDrainPoolClampsToSerial asserts n < 1 clamps to 1 (the serial path).
func TestDrainPoolClampsToSerial(t *testing.T) {
	pool := newDrainPool(0)
	gate := make(chan struct{})
	entered := make(chan struct{})
	pool.submit(context.Background(), func() { close(entered); <-gate })
	<-entered

	blocked := make(chan struct{})
	go func() {
		pool.submit(context.Background(), func() {})
		close(blocked)
	}()
	select {
	case <-blocked:
		t.Fatal("clamped pool allowed a second concurrent closure; want serial")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	<-blocked
	pool.wait()
}

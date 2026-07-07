package sim

import "sync"

// populationState stages boid spawn/despawn deltas off the tick loop. The
// spawn API, the churn ticker, and the cull watcher all stage under the mutex;
// the tick loop drains once per tick and applies the deltas to the engine
// between ticks — the hot per-tick path never touches locks, KV, or lifecycle
// (ADR-001). Mirrors steeringState's stage/drain discipline.
type populationState struct {
	mu       sync.Mutex
	spawns   int
	removals []uint32
	// pendingCreate holds engine-allocated IDs awaiting Manager.Create. The
	// tick loop stages them (fast, lock-only) after AddBoids; the off-loop
	// spawn-create goroutine drains and Creates at NATS pace — so a
	// create-melt backs up here, never onto the tick loop.
	pendingCreate []uint32
}

func newPopulationState() *populationState {
	return &populationState{}
}

// stageSpawn queues n boids to add on the next tick.
func (p *populationState) stageSpawn(n int) {
	if n <= 0 {
		return
	}
	p.mu.Lock()
	p.spawns += n
	p.mu.Unlock()
}

// stageRemoval queues boid IDs to remove on the next tick.
func (p *populationState) stageRemoval(ids ...uint32) {
	if len(ids) == 0 {
		return
	}
	p.mu.Lock()
	p.removals = append(p.removals, ids...)
	p.mu.Unlock()
}

// drain takes and clears the staged deltas — called once per tick by the loop.
// Returns (0, nil) when nothing is staged, so the tick loop skips the engine
// mutation entirely and a fixed-population run stays deterministic.
func (p *populationState) drain() (spawns int, removals []uint32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	spawns, removals = p.spawns, p.removals
	p.spawns, p.removals = 0, nil
	return spawns, removals
}

// stageCreate queues engine-allocated IDs for the off-loop Manager.Create.
func (p *populationState) stageCreate(ids []uint32) {
	if len(ids) == 0 {
		return
	}
	p.mu.Lock()
	p.pendingCreate = append(p.pendingCreate, ids...)
	p.mu.Unlock()
}

// drainCreates takes and clears the pending-create IDs — the spawn-create
// goroutine's queue, independent of the tick loop's drain().
func (p *populationState) drainCreates() []uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := p.pendingCreate
	p.pendingCreate = nil
	return ids
}

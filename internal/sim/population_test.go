package sim

import (
	"sync"
	"testing"
)

func TestPopulationStateStagesAndDrains(t *testing.T) {
	p := newPopulationState()
	p.stageSpawn(3)
	p.stageSpawn(2)
	p.stageRemoval(1, 4)
	p.stageRemoval(7)

	spawns, removals := p.drain()
	if spawns != 5 {
		t.Fatalf("spawns = %d, want 5", spawns)
	}
	if len(removals) != 3 || removals[0] != 1 || removals[1] != 4 || removals[2] != 7 {
		t.Fatalf("removals = %v, want [1 4 7]", removals)
	}

	// Drained → empty, so the tick loop skips the engine mutation.
	spawns, removals = p.drain()
	if spawns != 0 || removals != nil {
		t.Fatalf("after drain: %d/%v, want 0/nil", spawns, removals)
	}
}

// TestPopulationStateConcurrentStaging exercises the mutex under -race:
// concurrent stagers plus a drainer must not race.
func TestPopulationStateConcurrentStaging(t *testing.T) {
	p := newPopulationState()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(id uint32) {
			defer wg.Done()
			for range 100 {
				p.stageSpawn(1)
				p.stageRemoval(id)
			}
		}(uint32(i))
	}
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				p.drain()
			}
		}
	}()
	wg.Wait()
	close(done)

	// Whatever's left drains cleanly.
	spawns, _ := p.drain()
	if spawns < 0 {
		t.Fatal("negative spawns")
	}
}

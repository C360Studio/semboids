package boidgraph_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/graph/clustering"
)

// unitBoidID mirrors the integration helper (that one is behind the
// integration build tag).
func unitBoidID(n int) string {
	return fmt.Sprintf("c360.semboids.sim.flock.boid.%d", n)
}

// fakeProvider serves two fully-connected 4-cliques with no cross edges.
type fakeProvider struct {
	adj map[string][]string
}

func (f *fakeProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	ids := make([]string, 0, len(f.adj))
	for id := range f.adj {
		ids = append(ids, id)
	}
	return ids, nil
}

func (f *fakeProvider) GetNeighbors(_ context.Context, entityID, _ string) ([]string, error) {
	return f.adj[entityID], nil
}

func (f *fakeProvider) GetEdgeWeight(_ context.Context, fromID, toID string) (float64, error) {
	for _, n := range f.adj[fromID] {
		if n == toID {
			return 1.0, nil
		}
	}
	return 0.0, nil
}

// memStorage is an in-memory CommunityStorage.
type memStorage struct {
	mu    sync.Mutex
	comms map[string]*clustering.Community
}

func newMemStorage() *memStorage {
	return &memStorage{comms: map[string]*clustering.Community{}}
}

func (m *memStorage) SaveCommunity(_ context.Context, c *clustering.Community) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comms[fmt.Sprintf("%d.%s", c.Level, c.ID)] = c
	return nil
}

func (m *memStorage) GetCommunity(_ context.Context, id string) (*clustering.Community, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.comms[id]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *memStorage) GetCommunitiesByLevel(_ context.Context, level int) ([]*clustering.Community, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*clustering.Community
	for _, c := range m.comms {
		if c.Level == level {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memStorage) GetEntityCommunity(_ context.Context, entityID string, level int) (*clustering.Community, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.comms {
		if c.Level != level {
			continue
		}
		for _, member := range c.Members {
			if member == entityID {
				return c, nil
			}
		}
	}
	return nil, fmt.Errorf("not found")
}

func (m *memStorage) DeleteCommunity(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.comms, id)
	return nil
}

func (m *memStorage) Clear(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.comms = map[string]*clustering.Community{}
	return nil
}

// Prune keeps only the supplied partition, dropping everything a previous
// detection run left behind (ADR-085 write-then-prune). Keyed like
// SaveCommunity so the retained set matches what was written.
func (m *memStorage) Prune(_ context.Context, keep []*clustering.Community) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	retained := make(map[string]*clustering.Community, len(keep))
	for _, c := range keep {
		retained[fmt.Sprintf("%d.%s", c.Level, c.ID)] = c
	}
	m.comms = retained
	return nil
}

func (m *memStorage) GetAllCommunities(_ context.Context) ([]*clustering.Community, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*clustering.Community, 0, len(m.comms))
	for _, c := range m.comms {
		out = append(out, c)
	}
	return out, nil
}

// TestLPALibraryOnDisjointCliques isolates the LPA library from all
// substrate wiring: two 4-cliques, no cross edges. If this merges them,
// the library itself is the problem.
func TestLPALibraryOnDisjointCliques(t *testing.T) {
	clique := func(base int) map[string][]string {
		out := map[string][]string{}
		for i := base; i < base+4; i++ {
			var ns []string
			for j := base; j < base+4; j++ {
				if i != j {
					ns = append(ns, unitBoidID(j))
				}
			}
			out[unitBoidID(i)] = ns
		}
		return out
	}
	adj := clique(0)
	for k, v := range clique(4) {
		adj[k] = v
	}

	detector := clustering.NewLPADetector(&fakeProvider{adj: adj}, newMemStorage())
	result, err := detector.DetectCommunities(context.Background())
	if err != nil {
		t.Fatalf("DetectCommunities: %v", err)
	}

	level0 := result[0]
	t.Logf("level 0: %d communities", len(level0))
	for _, c := range level0 {
		t.Logf("  %s (level %d): %v", c.ID, c.Level, c.Members)
	}
	if len(level0) != 2 {
		t.Fatalf("LPA merged disjoint cliques: got %d level-0 communities, want 2", len(level0))
	}
}

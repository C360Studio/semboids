package sim

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/projection"

	"github.com/c360studio/semboids/internal/boidgraph"
)

// fakeSpawner records created participants (thread-safe, since createPending now
// dispatches concurrently). If gate is non-nil, Create blocks on it and tracks
// concurrency — used to assert the drain runs boids in parallel.
type fakeSpawner struct {
	mu       sync.Mutex
	created  []lifecycle.Participant
	gate     chan struct{}
	entered  chan struct{}
	inFlight atomic.Int32
	maxSeen  atomic.Int32
}

func (f *fakeSpawner) Create(_ context.Context, p lifecycle.Participant) error {
	if f.gate != nil {
		cur := f.inFlight.Add(1)
		for {
			m := f.maxSeen.Load()
			if cur <= m || f.maxSeen.CompareAndSwap(m, cur) {
				break
			}
		}
		f.entered <- struct{}{}
		<-f.gate
		f.inFlight.Add(-1)
	}
	f.mu.Lock()
	f.created = append(f.created, p)
	f.mu.Unlock()
	return nil
}

func (f *fakeSpawner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

func TestCreatePendingCreatesActivePerBoid(t *testing.T) {
	fake := &fakeSpawner{}
	c := &Component{
		org:        "c360",
		platform:   "semboids",
		logger:     slog.Default(),
		population: newPopulationState(),
		spawner:    fake,
		drainPool:  newDrainPool(8),
	}
	c.population.stageCreate([]uint32{1, 2, 3})
	c.createPending(context.Background())
	c.drainPool.wait()

	if fake.count() != 3 {
		t.Fatalf("created %d participants, want 3", fake.count())
	}
	// Concurrent dispatch → order is not fixed; assert the set instead.
	ids := map[string]bool{}
	for _, p := range fake.created {
		if p.Phase() != boidgraph.PhaseActive {
			t.Fatalf("spawned boid phase = %q, want active", p.Phase())
		}
		ids[p.EntityID()] = true
	}
	for _, want := range []string{
		"c360.semboids.sim.flock.boid.1",
		"c360.semboids.sim.flock.boid.2",
		"c360.semboids.sim.flock.boid.3",
	} {
		if !ids[want] {
			t.Fatalf("missing created entity %q (got %v)", want, ids)
		}
	}
	// Drained → a second pass creates nothing.
	c.createPending(context.Background())
	c.drainPool.wait()
	if fake.count() != 3 {
		t.Fatal("second pass re-created boids")
	}
}

// TestCreatePendingRunsConcurrently asserts the drain runs boids across the
// pool's slots (not one at a time) and still creates every boid exactly once.
func TestCreatePendingRunsConcurrently(t *testing.T) {
	const concurrency = 5
	const total = 20
	fake := &fakeSpawner{gate: make(chan struct{}), entered: make(chan struct{}, total)}
	c := &Component{
		org:        "c360",
		platform:   "semboids",
		logger:     slog.Default(),
		population: newPopulationState(),
		spawner:    fake,
		drainPool:  newDrainPool(concurrency),
	}
	ids := make([]uint32, total)
	for i := range ids {
		ids[i] = uint32(i + 1)
	}
	c.population.stageCreate(ids)

	done := make(chan struct{})
	go func() { c.createPending(context.Background()); close(done) }()

	// The pool fills to `concurrency`; createPending then parks in submit.
	for i := 0; i < concurrency; i++ {
		<-fake.entered
	}
	if got := fake.inFlight.Load(); got != concurrency {
		t.Fatalf("in-flight creates = %d, want %d", got, concurrency)
	}
	close(fake.gate) // release; the rest drain concurrency-at-a-time
	<-done           // all submitted
	c.drainPool.wait()

	if got := fake.maxSeen.Load(); got != int32(concurrency) {
		t.Fatalf("max concurrent creates = %d, want %d", got, concurrency)
	}
	if fake.count() != total {
		t.Fatalf("created %d boids, want %d", fake.count(), total)
	}
}

func entityWithPhase(t *testing.T, phase string) []byte {
	t.Helper()
	es := map[string]any{
		"triples": []map[string]any{
			{"predicate": boidgraph.BoidPhasePredicate, "object": phase},
			{"predicate": "flock.position.x", "object": 1.0},
		},
	}
	data, err := json.Marshal(es)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestCulledBoidID(t *testing.T) {
	const boidKey = "c360.semboids.sim.flock.boid.42"

	if id, ok := culledBoidID(boidKey, entityWithPhase(t, boidgraph.PhaseCulled)); !ok || id != 42 {
		t.Fatalf("culled boid = %d/%v, want 42/true", id, ok)
	}
	if _, ok := culledBoidID(boidKey, entityWithPhase(t, boidgraph.PhaseActive)); ok {
		t.Fatal("active boid must not read as culled")
	}
	if _, ok := culledBoidID("c360.semboids.sim.zones.zone.pred", entityWithPhase(t, boidgraph.PhaseCulled)); ok {
		t.Fatal("non-boid key must be skipped")
	}
	if _, ok := culledBoidID(boidKey, []byte("{malformed")); ok {
		t.Fatal("malformed value must be skipped")
	}
}

func TestBoidIDFromKey(t *testing.T) {
	if id, ok := boidIDFromKey("c360.semboids.sim.flock.boid.7"); !ok || id != 7 {
		t.Fatalf("got %d/%v, want 7/true", id, ok)
	}
	if _, ok := boidIDFromKey("no-dots"); ok {
		t.Fatal("keyless should fail")
	}
	if _, ok := boidIDFromKey("a.b.c.d.e.notanumber"); ok {
		t.Fatal("non-numeric instance should fail")
	}
}

// fakeReclaimer scripts the fenced read/delete pairing per call (nil beyond
// the script = success), standing in for the projection mutation client.
type fakeReclaimer struct {
	mu           sync.Mutex
	reads        int
	deletes      int
	readScript   []error
	deleteScript []error
	revision     uint64
	deleted      []projection.DeleteMutation
}

func (f *fakeReclaimer) ReadAuthoritative(_ context.Context, _ string) (*graph.ExactEntity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := f.reads
	f.reads++
	if call < len(f.readScript) && f.readScript[call] != nil {
		return nil, f.readScript[call]
	}
	return &graph.ExactEntity{KVRevision: f.revision}, nil
}

func (f *fakeReclaimer) Delete(_ context.Context, req projection.DeleteMutation) (projection.MutationReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	call := f.deletes
	f.deletes++
	f.deleted = append(f.deleted, req)
	if call < len(f.deleteScript) && f.deleteScript[call] != nil {
		return projection.MutationReceipt{}, f.deleteScript[call]
	}
	return projection.MutationReceipt{}, nil
}

func simMutationErr(kind projection.MutationErrorKind) error {
	return &projection.MutationError{Kind: kind, Err: errors.New(string(kind))}
}

// TestDeleteEntityFencedPairing pins the boid-lifecycle delta's reclaim
// contract: read the authoritative revision, delete at exactly that revision.
func TestDeleteEntityFencedPairing(t *testing.T) {
	fake := &fakeReclaimer{revision: 42}
	c := &Component{logger: slog.Default(), reclaimer: fake}
	c.deleteEntity(context.Background(), "c360.semboids.sim.flock.boid.7")
	if fake.reads != 1 || fake.deletes != 1 {
		t.Fatalf("reads=%d deletes=%d, want 1/1", fake.reads, fake.deletes)
	}
	if got := fake.deleted[0]; got.EntityID != "c360.semboids.sim.flock.boid.7" || got.ExpectedRevision != 42 {
		t.Fatalf("delete request = %+v", got)
	}
}

// TestDeleteEntityRetryClassification pins design D3: revision conflicts
// re-read and retry (bounded), a missing entity is already-reclaimed, and an
// ambiguous outcome is never blind-retried.
func TestDeleteEntityRetryClassification(t *testing.T) {
	cases := []struct {
		name         string
		readScript   []error
		deleteScript []error
		wantReads    int
		wantDeletes  int
	}{
		{"conflict re-reads then succeeds", nil,
			[]error{simMutationErr(projection.MutationRevisionConflict), nil}, 2, 2},
		{"read not-found is already reclaimed",
			[]error{simMutationErr(projection.MutationNotFound)}, nil, 1, 0},
		{"delete not-found is already reclaimed", nil,
			[]error{simMutationErr(projection.MutationNotFound)}, 1, 1},
		{"ambiguous is not retried", nil,
			[]error{simMutationErr(projection.MutationCommitUnknown)}, 1, 1},
		{"conflict exhausts bound", nil, []error{
			simMutationErr(projection.MutationRevisionConflict),
			simMutationErr(projection.MutationRevisionConflict),
			simMutationErr(projection.MutationRevisionConflict),
			simMutationErr(projection.MutationRevisionConflict),
			simMutationErr(projection.MutationRevisionConflict),
			simMutationErr(projection.MutationRevisionConflict), // must never be reached
		}, reclaimRetries, reclaimRetries},
		{"invalid is not retried", nil,
			[]error{simMutationErr(projection.MutationInvalid)}, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeReclaimer{readScript: tc.readScript, deleteScript: tc.deleteScript}
			c := &Component{logger: slog.Default(), reclaimer: fake}
			c.deleteEntity(context.Background(), "c360.semboids.sim.flock.boid.9")
			if fake.reads != tc.wantReads || fake.deletes != tc.wantDeletes {
				t.Fatalf("reads=%d deletes=%d, want %d/%d",
					fake.reads, fake.deletes, tc.wantReads, tc.wantDeletes)
			}
		})
	}
}

// failNTimesSpawner fails its first n Creates — the seed-flock startup race,
// where Manager.Create's exact read races the mutation responder coming up.
type failNTimesSpawner struct {
	fakeSpawner
	failures atomic.Int32
}

func (f *failNTimesSpawner) Create(ctx context.Context, p lifecycle.Participant) error {
	if f.failures.Add(-1) >= 0 {
		return errors.New("nats: no responders available for request")
	}
	return f.fakeSpawner.Create(ctx, p)
}

// TestCreateWithRetrySurvivesSeedRace pins task 3.5: a transiently absent
// mutation responder delays the create instead of leaving the boid
// lifecycle-less.
func TestCreateWithRetrySurvivesSeedRace(t *testing.T) {
	fake := &failNTimesSpawner{}
	fake.failures.Store(2)
	c := &Component{logger: slog.Default(), spawner: fake}
	if err := c.createWithRetry(context.Background(), "c360.semboids.sim.flock.boid.1"); err != nil {
		t.Fatalf("createWithRetry = %v, want success after transient failures", err)
	}
	if fake.count() != 1 {
		t.Fatalf("created %d participants, want 1", fake.count())
	}
}

// TestCreateWithRetryExhaustsBound pins the failure surface: a persistent
// error is returned exactly once, after the bounded attempts.
func TestCreateWithRetryExhaustsBound(t *testing.T) {
	fake := &failNTimesSpawner{}
	fake.failures.Store(int32(createRetries) + 3)
	c := &Component{logger: slog.Default(), spawner: fake}
	if err := c.createWithRetry(context.Background(), "c360.semboids.sim.flock.boid.2"); err == nil {
		t.Fatal("createWithRetry = nil, want error at exhaustion")
	}
	if fake.count() != 0 {
		t.Fatalf("created %d participants, want 0", fake.count())
	}
}

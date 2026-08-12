package sim

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semboids/internal/boidgraph"
)

const (
	entityStatesBucket = "ENTITY_STATES"
	// spawnCreatePoll is how often the off-loop creator drains pending
	// Manager.Create work; adds ≤ this to spawn→graph latency, keeps the tick
	// loop free of NATS.
	spawnCreatePoll = 20 * time.Millisecond
	churnBase       = 100 * time.Millisecond
	// churnWaveSize is the boids per churn-driven spawn wave.
	churnWaveSize = 5
	// defaultDrainConcurrency is the default in-flight lifecycle IO cap when
	// lifecycle_drain_concurrency is unset — matches graph-ingest's ingest_lanes
	// default (beta.142 ADR-072) so the drain uses every lane the substrate has.
	defaultDrainConcurrency = 8
	// reclaimRetries bounds the fenced-delete loop. Conflicts come from the
	// culled boid's own in-flight snapshot writes; the boid left physics at
	// observation, so its writes stop and the retry converges (design D3).
	reclaimRetries = 5
	// createRetries bounds Manager.Create attempts. Create now round-trips
	// through the graph mutation responder, which can lag the sim at process
	// start (component-start races the responder subscription) — a bounded
	// backoff turns the seed-flock race into a short wait instead of a
	// silently lifecycle-less boid.
	createRetries = 5
	// createRetryBase is the first retry backoff (doubled per attempt).
	createRetryBase = 100 * time.Millisecond
)

// boidSpawner is the slice of lifecycle.Manager the sim uses to record a
// spawned boid as an active participant (*lifecycle.Manager satisfies it).
type boidSpawner interface {
	Create(ctx context.Context, p lifecycle.Participant) error
}

// bucketWaiter is the slice of the NATS client the cull watcher needs to
// attach its raw ENTITY_STATES watch.
type bucketWaiter interface {
	WaitForBucket(ctx context.Context, name string, timeout time.Duration) (jetstream.KeyValue, error)
}

// entityReclaimer is the slice of the projection mutation client the cull
// watcher needs: lifecycle exposes no despawn, so the sim reclaims a culled
// boid itself via the beta.160 revision-fenced pairing — read the
// authoritative revision, then delete at exactly that revision. The client
// never retries for the caller (design D3).
type entityReclaimer interface {
	ReadAuthoritative(ctx context.Context, entityID string) (*graph.ExactEntity, error)
	Delete(ctx context.Context, request projection.DeleteMutation) (projection.MutationReceipt, error)
}

// culledBoidID returns the boid's numeric ID and true when an ENTITY_STATES
// value carries phase=culled for a boid key. Non-boid keys, other phases, and
// malformed values return false — the pure core of the cull watcher.
func culledBoidID(key string, value []byte) (uint32, bool) {
	if !strings.Contains(key, ".flock.boid.") {
		return 0, false
	}
	var es struct {
		Triples []struct {
			Predicate string `json:"predicate"`
			Object    any    `json:"object"`
		} `json:"triples"`
	}
	if err := json.Unmarshal(value, &es); err != nil {
		return 0, false
	}
	culled := false
	for _, tr := range es.Triples {
		if tr.Predicate == boidgraph.BoidPhasePredicate {
			if s, ok := tr.Object.(string); ok && s == boidgraph.PhaseCulled {
				culled = true
			}
		}
	}
	if !culled {
		return 0, false
	}
	return boidIDFromKey(key)
}

// boidIDFromKey parses the instance segment of a 6-part boid entity ID.
func boidIDFromKey(key string) (uint32, bool) {
	i := strings.LastIndexByte(key, '.')
	if i < 0 {
		return 0, false
	}
	n, err := strconv.ParseUint(key[i+1:], 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(n), true
}

// runCullWatcher watches ENTITY_STATES for boids reaching phase=culled, stages
// their physics removal, and deletes the entity to reclaim it (D3/D6). It uses
// a raw KV watch, not Manager.Watch, which ignores the delete op we then
// issue. Runs independently of any UI client.
func (c *Component) runCullWatcher(ctx context.Context) {
	kv, err := c.buckets.WaitForBucket(ctx, entityStatesBucket, 60*time.Second)
	if err != nil {
		c.logger.Warn("cull watcher: ENTITY_STATES unavailable", slog.String("error", err.Error()))
		return
	}
	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		c.logger.Warn("cull watcher: watch failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = watcher.Stop() }()

	seen := make(map[uint32]struct{}) // culled boids already reclaimed
	for {
		select {
		case <-ctx.Done():
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				return
			}
			if entry == nil {
				continue // initial-sync marker
			}
			if entry.Operation() == jetstream.KeyValueDelete ||
				entry.Operation() == jetstream.KeyValuePurge {
				continue // our own reclaim delete — not a cull signal
			}
			id, culled := culledBoidID(entry.Key(), entry.Value())
			if !culled {
				continue
			}
			if _, done := seen[id]; done {
				continue
			}
			seen[id] = struct{}{}
			// The boid leaves physics and the cull is counted the moment we
			// observe it — synchronous, so `active` tracks live population,
			// not reclaim completion. Only the delete IO is offloaded, so a
			// slow reclaim never stalls observing the next cull.
			c.population.stageRemoval(id)
			c.metrics.observeCull()
			key := entry.Key()
			c.drainPool.submit(ctx, func() { c.deleteEntity(ctx, key) })
		}
	}
}

// deleteEntity reclaims a culled boid's ENTITY_STATES entry through the
// revision-fenced pairing (graph-ingest is the mutation provider; lifecycle
// has no despawn). Retry policy per design D3: a revision conflict or missing
// responder is a definite non-commit and retries with a fresh read; a
// missing entity means someone already reclaimed it; an ambiguous
// (commit-unknown) outcome is never blind-retried — it is logged and counted,
// and the culled zombie stays observable in ENTITY_STATES.
func (c *Component) deleteEntity(ctx context.Context, entityID string) {
	for attempt := 1; attempt <= reclaimRetries; attempt++ {
		exact, err := c.reclaimer.ReadAuthoritative(ctx, entityID)
		if err != nil {
			if kind, ok := mutationKind(err); ok {
				switch kind {
				case projection.MutationNotFound:
					return // already reclaimed
				case projection.MutationRevisionConflict, projection.MutationUnavailable:
					if attempt < reclaimRetries {
						continue
					}
				}
			}
			c.reclaimFailed(entityID, attempt, err)
			return
		}
		_, err = c.reclaimer.Delete(ctx, projection.DeleteMutation{
			EntityID:         entityID,
			ExpectedRevision: exact.KVRevision,
		})
		if err == nil {
			return
		}
		if kind, ok := mutationKind(err); ok {
			switch kind {
			case projection.MutationNotFound:
				return // already reclaimed
			case projection.MutationRevisionConflict, projection.MutationUnavailable:
				if attempt < reclaimRetries {
					continue // definite non-commit — re-read and retry
				}
			case projection.MutationCommitUnknown:
				c.logger.Warn("reclaim culled entity: ambiguous outcome, not retried",
					slog.String("entity", entityID), slog.String("error", err.Error()))
				c.metrics.observeReclaimFailure()
				return
			}
		}
		c.reclaimFailed(entityID, attempt, err)
		return
	}
	c.reclaimFailed(entityID, reclaimRetries, errors.New("retries exhausted"))
}

// reclaimFailed records a reclaim that gave up — the boid already left
// physics, so the cost is a lingering culled entity, visible in
// ENTITY_STATES and on the failure counter.
func (c *Component) reclaimFailed(entityID string, attempts int, err error) {
	c.logger.Warn("reclaim culled entity",
		slog.String("entity", entityID), slog.Int("attempts", attempts), slog.String("error", err.Error()))
	c.metrics.observeReclaimFailure()
}

// mutationKind extracts the projection client's stable failure category.
func mutationKind(err error) (projection.MutationErrorKind, bool) {
	var me *projection.MutationError
	if errors.As(err, &me) {
		return me.Kind, true
	}
	return "", false
}

// runSpawnCreator drains engine-allocated spawn IDs and records each as an
// active lifecycle participant via Manager.Create — off the tick loop, so a
// create-melt backs up in the queue, not the physics loop.
func (c *Component) runSpawnCreator(ctx context.Context) {
	ticker := time.NewTicker(spawnCreatePoll)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.createPending(ctx)
		}
	}
}

// createPending drains the pending-create queue and records each boid as an
// active participant through the drain pool — distinct boids run concurrently
// across graph-ingest's lanes, bounded by lifecycle_drain_concurrency. submit
// backpressures this goroutine when the pool is full, so a burst never fans out
// into unbounded goroutines. Separated from the poll loop so it is unit-testable.
func (c *Component) createPending(ctx context.Context) {
	for _, id := range c.population.drainCreates() {
		c.drainPool.submit(ctx, func() {
			start := time.Now()
			entityID := boidgraph.BoidEntityID(c.org, c.platform, id)
			if err := c.createWithRetry(ctx, entityID); err != nil {
				c.logger.Warn("create boid lifecycle",
					slog.Uint64("boid", uint64(id)), slog.Int("attempts", createRetries), slog.String("error", err.Error()))
				return
			}
			c.metrics.observeSpawn(time.Since(start))
		})
	}
}

// createWithRetry records one boid as an active participant, retrying with
// doubling backoff. Manager.Create exact-reads through the graph mutation
// responder since beta.160, so process start can race the responder's
// subscription coming up — observed live as "no responders available" on the
// seed flock. The backoff (100ms doubling, ~3s worst case inside one bounded
// pool slot) turns that race into a short wait; a persistent failure still
// surfaces exactly once, at exhaustion.
//
// ErrAlreadyExists is success, not failure: a restart against retained
// ENTITY_STATES re-seeds the same deterministic boid IDs, and beta.160's
// Create is strict (observed live: 160 already-managed rejections on
// restart). The boid IS a managed participant — counting it keeps the active
// gauge honest; a retained culled-phase boid is swept by the cull watcher's
// initial sync.
func (c *Component) createWithRetry(ctx context.Context, entityID string) error {
	backoff := createRetryBase
	var err error
	for attempt := 1; attempt <= createRetries; attempt++ {
		err = c.spawner.Create(ctx, boidgraph.NewBoidLifecycle(entityID))
		if err == nil || errors.Is(err, lifecycle.ErrAlreadyExists) {
			return nil
		}
		if attempt == createRetries {
			break
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return err
}

// runChurn fires spawn waves at the runtime churn dial (spawns/sec), bounded by
// churnCap so a load run doesn't grow the flock without limit — the delete side
// of churn comes from predator culls.
func (c *Component) runChurn(ctx context.Context) {
	ticker := time.NewTicker(churnBase)
	defer ticker.Stop()
	acc := 0.0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hz := c.ChurnHz()
			if hz <= 0 {
				acc = 0
				continue
			}
			acc += hz * churnBase.Seconds()
			for acc >= 1 {
				c.SpawnBoids(churnWaveSize)
				acc--
			}
		}
	}
}

// lifecycleMetrics holds the churn Prometheus handles. A nil receiver no-ops,
// so unit tests build a Component without a registry.
type lifecycleMetrics struct {
	spawns        prometheus.Counter
	culls         prometheus.Counter
	active        prometheus.Gauge
	spawnDur      prometheus.Observer
	reclaimFailed prometheus.Counter
}

func newLifecycleMetrics(reg *metric.MetricsRegistry) *lifecycleMetrics {
	if reg == nil {
		return nil
	}
	ns, sub, svc := "boids", "lifecycle", "boids"
	spawns := prometheus.NewCounter(prometheus.CounterOpts{Namespace: ns, Subsystem: sub, Name: "spawns_total", Help: "Boids spawned (Manager.Create) total."})
	_ = reg.RegisterCounter(svc, "lifecycle_spawns_total", spawns)
	culls := prometheus.NewCounter(prometheus.CounterOpts{Namespace: ns, Subsystem: sub, Name: "culls_total", Help: "Boids culled and reclaimed total."})
	_ = reg.RegisterCounter(svc, "lifecycle_culls_total", culls)
	active := prometheus.NewGauge(prometheus.GaugeOpts{Namespace: ns, Subsystem: sub, Name: "active", Help: "Live boid population (spawned − culled)."})
	_ = reg.RegisterGauge(svc, "lifecycle_active", active)
	dur := prometheus.NewHistogram(prometheus.HistogramOpts{Namespace: ns, Subsystem: sub, Name: "spawn_create_duration_seconds", Help: "Manager.Create round-trip per spawned boid.", Buckets: prometheus.ExponentialBuckets(0.001, 2, 14)})
	_ = reg.RegisterHistogram(svc, "lifecycle_spawn_create_duration_seconds", dur)
	reclaimFailed := prometheus.NewCounter(prometheus.CounterOpts{Namespace: ns, Subsystem: sub, Name: "reclaim_failures_total", Help: "Culled-boid reclaims that gave up (retry exhaustion or ambiguous outcome); the entity lingers in ENTITY_STATES."})
	_ = reg.RegisterCounter(svc, "lifecycle_reclaim_failures_total", reclaimFailed)
	return &lifecycleMetrics{spawns: spawns, culls: culls, active: active, spawnDur: dur, reclaimFailed: reclaimFailed}
}

func (m *lifecycleMetrics) observeSpawn(d time.Duration) {
	if m == nil {
		return
	}
	m.spawns.Inc()
	m.active.Inc()
	m.spawnDur.Observe(d.Seconds())
}

func (m *lifecycleMetrics) observeCull() {
	if m == nil {
		return
	}
	m.culls.Inc()
	m.active.Dec()
}

func (m *lifecycleMetrics) observeReclaimFailure() {
	if m == nil {
		return
	}
	m.reclaimFailed.Inc()
}

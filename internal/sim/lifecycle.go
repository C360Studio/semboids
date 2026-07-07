package sim

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/pkg/lifecycle"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/c360studio/semboids/internal/boidgraph"
)

const (
	entityStatesBucket = "ENTITY_STATES"
	entityDeleteSubj   = "graph.mutation.entity.delete"
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
)

// boidSpawner is the slice of lifecycle.Manager the sim uses to record a
// spawned boid as an active participant (*lifecycle.Manager satisfies it).
type boidSpawner interface {
	Create(ctx context.Context, p lifecycle.Participant) error
}

// entityReclaimer is the slice of the NATS client the cull watcher needs:
// watch ENTITY_STATES and delete reclaimed boid entities (lifecycle exposes no
// despawn, so the sim issues the delete itself).
type entityReclaimer interface {
	WaitForBucket(ctx context.Context, name string, timeout time.Duration) (jetstream.KeyValue, error)
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
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
	kv, err := c.reclaimer.WaitForBucket(ctx, entityStatesBucket, 60*time.Second)
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

// deleteEntity reclaims a culled boid's ENTITY_STATES entry (graph-ingest
// delete path — lifecycle has no despawn). Idempotent upstream.
func (c *Component) deleteEntity(ctx context.Context, entityID string) {
	req, err := json.Marshal(map[string]string{"entity_id": entityID})
	if err != nil {
		return
	}
	if _, err := c.reclaimer.Request(ctx, entityDeleteSubj, req, 5*time.Second); err != nil {
		c.logger.Warn("reclaim culled entity", slog.String("entity", entityID), slog.String("error", err.Error()))
	}
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
			if err := c.spawner.Create(ctx, boidgraph.NewBoidLifecycle(entityID)); err != nil {
				c.logger.Warn("create boid lifecycle", slog.Uint64("boid", uint64(id)), slog.String("error", err.Error()))
				return
			}
			c.metrics.observeSpawn(time.Since(start))
		})
	}
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
	spawns   prometheus.Counter
	culls    prometheus.Counter
	active   prometheus.Gauge
	spawnDur prometheus.Observer
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
	return &lifecycleMetrics{spawns: spawns, culls: culls, active: active, spawnDur: dur}
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

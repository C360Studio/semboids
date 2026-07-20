package api

import (
	"context"
	"sync"
	"time"

	"github.com/c360studio/semstreams/pkg/graphview"
)

const (
	// entityStatesBucket and communityIndexBucket are the two buckets the graph
	// pane reads.
	entityStatesBucket   = "ENTITY_STATES"
	communityIndexBucket = "COMMUNITY_INDEX"

	// viewTickInterval is the view's fan-out coalescing window. It is kept
	// deliberately BELOW the SSE flush interval (500ms) so the flush stays the
	// binding constraint on browser traffic — the flock-communities requirement
	// is that SSE messages are bounded by the flush interval, and a view tick
	// at or above it would make the view the bottleneck instead.
	viewTickInterval = 250 * time.Millisecond

	// viewRetryInterval is how often a supervisor re-attempts its view while
	// the bucket does not exist.
	viewRetryInterval = 5 * time.Second
)

// graphViews owns the shared read-side subscriptions behind GET
// /boids/graph/stream. One View per bucket for the whole process replaces the
// pre-change shape of two WatchAll consumers per connected SSE client
// (semstreams#579 / ADR-081).
//
// Ownership is explicit and injected — ADR-081 specifies no process-global
// registry — and the views must outlive any single connection, which is what
// actually delivers the decode-once-across-N amortization.
// Both views are supervised rather than constructed eagerly, because neither
// bucket is guaranteed to exist when this service starts:
//
//   - ENTITY_STATES is created by graph-ingest shortly AFTER start (the same
//     reason internal/boidgraph/probe.go has to WaitForBucket). Treating its
//     absence as a start error would make boot race-dependent.
//   - COMMUNITY_INDEX may be absent indefinitely, because clustering can be
//     starved (semstreams#590).
//
// While the entity view is absent the stream endpoint answers 503, which is
// exactly the pre-change behavior. While the community view is absent the pane
// renders neutral-colored nodes, which is the graph-pane "degrades gracefully"
// requirement.
type graphViews struct {
	mu          sync.RWMutex
	entities    *graphview.View[graphEntity]
	communities *graphview.View[[]string]

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// entityView returns the ENTITY_STATES view, or nil if it has not come up yet.
// Callers MUST tolerate nil and answer 503.
func (g *graphViews) entityView() *graphview.View[graphEntity] {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.entities
}

// communityView returns the COMMUNITY_INDEX view, or nil if it has not come up
// yet. Callers MUST tolerate nil.
func (g *graphViews) communityView() *graphview.View[[]string] {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.communities
}

// openFunc opens a KV bucket as a view source.
type openFunc func(ctx context.Context, bucket string) (graphview.WatcherSource, error)

// startGraphViews launches a supervisor per bucket and returns immediately.
//
// The context deliberately does NOT derive from the caller's start context: the
// views must live for the service lifetime, and a start-scoped context would
// cancel the watchers as soon as startup finished. Teardown is driven by stop()
// instead, which is deterministic.
func startGraphViews(open openFunc, log logger, metrics *viewMetrics) *graphViews {
	ctx, cancel := context.WithCancel(context.Background())
	g := &graphViews{cancel: cancel}

	g.wg.Add(2)
	go supervise(ctx, &g.wg, entityStatesBucket, log, func() bool {
		view := attach(ctx, open, entityStatesBucket, decodeBoidEntity, log, metrics)
		if view == nil {
			return false
		}
		g.mu.Lock()
		g.entities = view
		g.mu.Unlock()
		return true
	})
	go supervise(ctx, &g.wg, communityIndexBucket, log, func() bool {
		view := attach(ctx, open, communityIndexBucket, decodeCommunity, log, metrics)
		if view == nil {
			return false
		}
		g.mu.Lock()
		g.communities = view
		g.mu.Unlock()
		return true
	})
	return g
}

// supervise retries try until it succeeds or the context ends. Absence is never
// escalated to an error: a bucket that does not exist yet is a normal state
// here, not a fault.
func supervise(ctx context.Context, wg *sync.WaitGroup, bucket string, log logger, try func() bool) {
	defer wg.Done()
	for {
		if try() {
			log.Info("graph view attached", "bucket", bucket)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(viewRetryInterval):
		}
	}
}

// attach makes one construction attempt, returning nil when the bucket is not
// there yet.
func attach[T any](ctx context.Context, open openFunc, bucket string, decode graphview.DecodeFunc[T], log logger, metrics *viewMetrics) *graphview.View[T] {
	src, err := open(ctx, bucket)
	if err != nil {
		log.Debug("graph view bucket not available yet; will retry", "bucket", bucket, "error", err.Error())
		return nil
	}
	view, err := graphview.New[T](src, decode,
		graphview.WithTickInterval(viewTickInterval),
		graphview.WithHooks(metrics.hooks(bucket)))
	if err != nil {
		log.Debug("graph view construction failed; will retry", "bucket", bucket, "error", err.Error())
		return nil
	}
	if err := view.Start(ctx); err != nil {
		log.Debug("graph view start failed; will retry", "bucket", bucket, "error", err.Error())
		view.Stop()
		return nil
	}
	return view
}

// stop tears the views down. Safe to call more than once.
func (g *graphViews) stop() {
	if g == nil {
		return
	}
	if g.cancel != nil {
		g.cancel()
	}
	g.wg.Wait()
	if view := g.entityView(); view != nil {
		view.Stop()
	}
	if view := g.communityView(); view != nil {
		view.Stop()
	}
}

// logger is the slice of *slog.Logger these helpers need, kept narrow so tests
// can supply a quiet implementation.
type logger interface {
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}

package boidgraph

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"

	"github.com/c360studio/semboids/internal/flock"
)

// Snapshot is one derived flock state handed from the sim's tick loop to
// the publisher goroutine. All data is copied — nothing references engine
// buffers.
type Snapshot struct {
	Tick  uint64
	At    time.Time
	Boids []BoidState
}

// StreamPublisher is the JetStream slice of the NATS client the publisher
// needs: one snapshot's boid entities are published as a single async batch
// (gh#470, semstreams beta.138) — pipelined past the per-ack RTT ceiling and
// joined on all acks before returning, so per-subject order is preserved.
type StreamPublisher interface {
	PublishBatchToStream(ctx context.Context, subject string, msgs [][]byte) error
}

// TripleRemover issues graph.mutation.triple.remove requests — used on a
// boid's non-empty→empty neighbor transition. The stream-upsert path
// (MergeEntity/MergeTriples) is add/merge-only and cannot express now-zero: an
// arrival carrying no flock.neighbor.of triple leaves the resident edges in
// place (correct multi-writer merge). Verified-necessary on beta.152
// (TestNeighborEmptyGate); a substrate-native replacement is tracked upstream
// as semstreams#578.
type TripleRemover interface {
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
}

// Publisher consumes snapshots from a bounded buffer and publishes boid
// Graphables to graph-ingest. The producer side never blocks: Offer drops
// when the buffer is full (drop-oldest-in-spirit: the stale pending
// snapshot is superseded only by physics time, so dropping the new one is
// equivalent one tick later) and counts the drop.
type Publisher struct {
	pub      StreamPublisher
	remover  TripleRemover
	logger   *slog.Logger
	orgID    string
	platform string
	metrics  *publisherMetrics

	ch chan Snapshot

	published atomic.Uint64 // boid entities published
	snapshots atomic.Uint64 // snapshots fully published
	dropped   atomic.Uint64 // snapshots dropped at Offer

	// prevHadNeighbors tracks which boids had neighbors in the previous
	// snapshot. It is read and written only on the coordinator goroutine
	// (Run → publishSnapshot's post-join loop) — so no locking (D1).
	prevHadNeighbors map[uint32]bool
}

// NewPublisher creates a Publisher with a small buffer (capacity 2: one in
// flight, one pending). reg may be nil (tests), which disables the Prometheus
// pipeline metrics.
func NewPublisher(pub StreamPublisher, remover TripleRemover, orgID, platform string, reg *metric.MetricsRegistry, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{
		pub:              pub,
		remover:          remover,
		logger:           logger,
		orgID:            orgID,
		platform:         platform,
		metrics:          newPublisherMetrics(reg),
		ch:               make(chan Snapshot, 2),
		prevHadNeighbors: make(map[uint32]bool),
	}
}

// Offer hands a snapshot to the publisher without blocking. Returns false
// (and counts a drop) when the buffer is full.
func (p *Publisher) Offer(s Snapshot) bool {
	select {
	case p.ch <- s:
		return true
	default:
		p.dropped.Add(1)
		p.metrics.incDropped()
		return false
	}
}

// Counts returns (snapshots published, boid entities published, snapshots
// dropped).
func (p *Publisher) Counts() (snapshots, entities, dropped uint64) {
	return p.snapshots.Load(), p.published.Load(), p.dropped.Load()
}

// Run consumes and publishes snapshots until ctx is cancelled.
//
// INVARIANT (D1/D2): snapshots are consumed strictly one at a time —
// publishSnapshot dispatches a whole snapshot as one async batch and joins on
// every ack before returning, so every publish for snapshot N completes
// before snapshot N+1 begins. Each boid appears at most once per snapshot and
// all publish to one subject (in-order per connection), so no entity's
// updates can reorder across snapshots. A future "pipeline snapshots" change
// that consumed concurrently would break this and the ordering test in
// publisher_test.go pins it.
func (p *Publisher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case s := <-p.ch:
			p.publishSnapshot(ctx, s)
		}
	}
}

// publishSnapshot marshals every boid as a BaseMessage-wrapped Graphable and
// publishes the whole snapshot as one async batch, then issues predicate
// removals for boids whose neighbor sets emptied.
//
// The batch (gh#470's PublishBatchToStream) pipelines all the snapshot's
// publishes past the per-ack RTT ceiling — the instrument sits two orders
// above any plausible substrate melt (D2) — and waits for every ack before
// returning, holding the one-snapshot-at-a-time invariant. Stateful
// bookkeeping (prevHadNeighbors + removals) then runs on this coordinator
// goroutine after the batch joins.
func (p *Publisher) publishSnapshot(ctx context.Context, s Snapshot) {
	start := time.Now()

	msgs := make([][]byte, 0, len(s.Boids))
	for i := range s.Boids {
		b := s.Boids[i]
		entity := &Entity{
			Boid: b, OrgID: p.orgID, Platform: p.platform,
			Tick: s.Tick, ObservedAt: s.At,
		}
		baseMsg := message.NewBaseMessage(entity.Schema(), entity, "semboids-sim")
		data, err := json.Marshal(baseMsg)
		if err != nil {
			p.logger.Error("marshal boid entity", slog.Uint64("boid", uint64(b.ID)), slog.Any("error", err))
			continue
		}
		msgs = append(msgs, data)
	}

	if err := p.pub.PublishBatchToStream(ctx, IngestSubject, msgs); err != nil {
		// A batch error means a connection fault or some acks failed; the
		// counters below only advance on a fully-acked batch so achieved-rate
		// metrics never overcount a partial melt window.
		p.logger.Warn("publish snapshot batch",
			slog.Uint64("tick", s.Tick), slog.Int("boids", len(msgs)), slog.Any("error", err))
	} else {
		p.published.Add(uint64(len(msgs)))
		p.metrics.addEntities(len(msgs))
	}

	// Coordinator-only: neighbor-empty transitions. prevHadNeighbors is read
	// and written on this single goroutine, so no locking (D1).
	for i := range s.Boids {
		b := s.Boids[i]
		had := p.prevHadNeighbors[b.ID]
		has := len(b.Neighbors) > 0
		if had && !has {
			p.removeNeighborTriples(ctx, b.ID)
		}
		p.prevHadNeighbors[b.ID] = has
	}

	p.snapshots.Add(1)
	p.metrics.incSnapshot()
	p.metrics.observeDuration(time.Since(start).Seconds())
}

// removeNeighborTriples issues one idempotent predicate removal for a boid
// whose neighbor set transitioned to empty — the only way to clear an emptied
// predicate on beta.152, since the stream merge preserves absent predicates
// (see TripleRemover; semstreams#578 tracks retiring this).
func (p *Publisher) removeNeighborTriples(ctx context.Context, id uint32) {
	if p.remover == nil {
		return
	}
	req, err := json.Marshal(map[string]any{
		"subject":   BoidEntityID(p.orgID, p.platform, id),
		"predicate": "flock.neighbor.of",
	})
	if err != nil {
		p.logger.Error("marshal triple remove", slog.Any("error", err))
		return
	}
	if _, err := p.remover.Request(ctx, "graph.mutation.triple.remove", req, 5*time.Second); err != nil {
		p.logger.Warn("remove stale neighbor triples",
			slog.Uint64("boid", uint64(id)), slog.Any("error", err))
	}
}

// BuildSnapshot copies engine state into a Snapshot (the engine buffer is
// only valid until the next tick — everything is copied out). neighbors
// maps boid ID to neighbor IDs (from flock.Engine.SnapshotNeighbors, whose
// slices are already fresh allocations).
func BuildSnapshot(tick uint64, at time.Time, boids []flock.Boid, neighbors map[uint32][]uint32) Snapshot {
	states := make([]BoidState, len(boids))
	for i, b := range boids {
		states[i] = BoidState{
			ID: b.ID, X: b.Pos.X, Y: b.Pos.Y, VX: b.Vel.X, VY: b.Vel.Y,
			Neighbors: neighbors[b.ID],
		}
	}
	return Snapshot{Tick: tick, At: at, Boids: states}
}

package boidgraph

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/message"

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
// needs.
type StreamPublisher interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// TripleRemover issues graph.mutation.triple.remove requests — used on a
// boid's non-empty→empty neighbor transition, which predicate-level merge
// cannot express (spike 1.1).
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

	ch chan Snapshot

	published atomic.Uint64 // boid entities published
	snapshots atomic.Uint64 // snapshots fully published
	dropped   atomic.Uint64 // snapshots dropped at Offer

	// prevHadNeighbors tracks which boids had neighbors in the previous
	// snapshot (publisher-goroutine local; no locking).
	prevHadNeighbors map[uint32]bool
}

// NewPublisher creates a Publisher with a small buffer (capacity 2: one in
// flight, one pending).
func NewPublisher(pub StreamPublisher, remover TripleRemover, orgID, platform string, logger *slog.Logger) *Publisher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Publisher{
		pub:              pub,
		remover:          remover,
		logger:           logger,
		orgID:            orgID,
		platform:         platform,
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
		return false
	}
}

// Counts returns (snapshots published, boid entities published, snapshots
// dropped).
func (p *Publisher) Counts() (snapshots, entities, dropped uint64) {
	return p.snapshots.Load(), p.published.Load(), p.dropped.Load()
}

// Run consumes and publishes snapshots until ctx is cancelled.
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

// publishSnapshot publishes each boid as a BaseMessage-wrapped Graphable
// and issues predicate removals for boids whose neighbor sets emptied.
func (p *Publisher) publishSnapshot(ctx context.Context, s Snapshot) {
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
		if err := p.pub.PublishToStream(ctx, IngestSubject, data); err != nil {
			p.logger.Warn("publish boid entity", slog.Uint64("boid", uint64(b.ID)), slog.Any("error", err))
			continue
		}
		p.published.Add(1)

		had := p.prevHadNeighbors[b.ID]
		has := len(b.Neighbors) > 0
		if had && !has {
			p.removeNeighborTriples(ctx, b.ID)
		}
		p.prevHadNeighbors[b.ID] = has
	}
	p.snapshots.Add(1)
}

// removeNeighborTriples issues one idempotent predicate removal for a boid
// whose neighbor set transitioned to empty.
func (p *Publisher) removeNeighborTriples(ctx context.Context, id uint32) {
	if p.remover == nil {
		return
	}
	req, err := json.Marshal(map[string]any{
		"subject":   BoidEntityID(p.orgID, p.platform, id),
		"predicate": "flock.neighbor",
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

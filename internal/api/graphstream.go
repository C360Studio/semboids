package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/pkg/graphview"
)

// graphEntity is one boid as streamed to the graph pane.
type graphEntity struct {
	ID        string   `json:"id"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	Neighbors []string `json:"neighbors"`
}

// graphBatch is one SSE message: coalesced entity updates plus (when
// changed) the full per-entity community map — small at demo scale and
// simpler than community deltas.
type graphBatch struct {
	Entities    []graphEntity     `json:"entities,omitempty"`
	Removed     []string          `json:"removed,omitempty"`
	Communities map[string]string `json:"communities,omitempty"`
}

// bridgeState coalesces view deltas between SSE flushes: latest wins per
// entity, communities invert Members[] to entity→community at flush.
//
// The views coalesce once across all subscribers at their tick; this second
// stage builds the per-client wire batch at the flush cadence. Both stages are
// needed: the first amortizes decode across N clients, the second bounds
// browser traffic by the flush interval rather than the dial rate.
type bridgeState struct {
	mu          sync.Mutex
	dirty       map[string]graphEntity // entity ID → latest state
	removed     map[string]struct{}
	communities map[string][]string // community ID → members (level 0)
	commDirty   bool
}

func newBridgeState() *bridgeState {
	return &bridgeState{
		dirty:       map[string]graphEntity{},
		removed:     map[string]struct{}{},
		communities: map[string][]string{},
	}
}

// seedEntities loads a view snapshot as the client's initial full sync. The
// snapshot is consistent at one view sequence and the subscription delivers
// exactly the changes after it, so there is no gap and no duplicate between
// this seed and the deltas that follow.
func (b *bridgeState) seedEntities(snap graphview.Snapshot[graphEntity]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, entry := range snap.Entries {
		b.dirty[key] = entry.Value
	}
}

// seedCommunities loads the community snapshot, marking the map dirty so the
// first flush carries assignments even when none change afterwards.
func (b *bridgeState) seedCommunities(snap graphview.Snapshot[[]string]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, entry := range snap.Entries {
		b.communities[key] = entry.Value
	}
	b.commDirty = true
}

// applyEntityDeltas folds one coalesced delta batch into the pending state.
//
// Poison is deliberately NOT treated as removal: graphview heals a poisoned key
// on the next valid write, so holding last-known-good state beats dropping a
// boid out of the pane because one write failed to decode.
func (b *bridgeState) applyEntityDeltas(deltas []graphview.Delta[graphEntity]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range deltas {
		switch d.Op {
		case graphview.DeltaUpsert:
			delete(b.removed, d.Key)
			b.dirty[d.Key] = d.Value
		case graphview.DeltaDelete:
			delete(b.dirty, d.Key)
			b.removed[d.Key] = struct{}{}
		case graphview.DeltaPoison:
			// Keep the last known good value; heals on a newer valid write.
		}
	}
}

// applyCommunityDeltas folds one coalesced community delta batch in.
func (b *bridgeState) applyCommunityDeltas(deltas []graphview.Delta[[]string]) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, d := range deltas {
		switch d.Op {
		case graphview.DeltaUpsert:
			b.communities[d.Key] = d.Value
			b.commDirty = true
		case graphview.DeltaDelete:
			delete(b.communities, d.Key)
			b.commDirty = true
		case graphview.DeltaPoison:
			// Keep the last known good membership.
		}
	}
}

// flush drains coalesced state into a batch; nil when nothing changed.
func (b *bridgeState) flush() *graphBatch {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.dirty) == 0 && len(b.removed) == 0 && !b.commDirty {
		return nil
	}
	batch := &graphBatch{}
	for _, e := range b.dirty {
		batch.Entities = append(batch.Entities, e)
	}
	for id := range b.removed {
		batch.Removed = append(batch.Removed, id)
	}
	if b.commDirty {
		batch.Communities = map[string]string{}
		for commID, members := range b.communities {
			for _, m := range members {
				batch.Communities[m] = commID
			}
		}
	}
	b.dirty = map[string]graphEntity{}
	b.removed = map[string]struct{}{}
	b.commDirty = false
	return batch
}

// isBoidKey filters ENTITY_STATES keys to boid entities.
func isBoidKey(key string) bool {
	return strings.Contains(key, ".flock.boid.")
}

// handleGraphStream serves GET <prefix>/graph/stream: an SSE stream of graph
// batches fed by the process-shared graph views. Each client attaches as a
// view subscriber rather than opening its own bucket watchers, so the
// JetStream consumer count does not grow with connected clients.
//
// The client receives a snapshot-derived initial sync, then the ~500ms flush
// loop coalesces per entity so browser traffic is bounded by flush rate, not
// dial rate.
func (s *Service) handleGraphStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	entityView := s.views.entityView()
	if entityView == nil {
		http.Error(w, "graph view unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	state := newBridgeState()

	snap, sub, err := entityView.SnapshotAndSubscribe(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("subscribe %s: %v", entityStatesBucket, err), http.StatusServiceUnavailable)
		return
	}
	defer sub.Unsubscribe()
	state.seedEntities(snap)

	// COMMUNITY_INDEX is optional: it may not exist until clustering runs, and
	// clustering can be starved indefinitely. A nil channel blocks forever in
	// select, which is exactly the "no communities" behavior we want.
	//
	// Attachment is retried on the flush tick rather than attempted only once at
	// connect: the bucket can appear long after a client connected, and the spec
	// requires assignments to start flowing on the SAME connection without a
	// reconnect. The retry is a nil check plus a method call, so it costs
	// nothing once attached.
	var (
		communityDeltas <-chan []graphview.Delta[[]string]
		communitySub    *graphview.Subscription[[]string]
	)
	defer func() {
		if communitySub != nil {
			communitySub.Unsubscribe()
		}
	}()
	attachCommunities := func() {
		if communityDeltas != nil {
			return
		}
		cv := s.views.communityView()
		if cv == nil {
			return
		}
		csnap, csub, cerr := cv.SnapshotAndSubscribe(ctx)
		if cerr != nil {
			s.logger.Debug("community view not subscribable yet; retrying",
				"error", cerr.Error())
			return
		}
		communitySub = csub
		state.seedCommunities(csnap)
		communityDeltas = csub.Deltas()
	}
	attachCommunities()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	s.logger.Info("Graph stream client connected")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Graph stream client disconnected")
			return

		case deltas, open := <-sub.Deltas():
			if !open {
				// Fail closed: the view can no longer guarantee the projection
				// is current, so end the response rather than keep emitting the
				// last known state as though it were live. EventSource
				// reconnects and the next attach restarts the view.
				if err := sub.Err(); err != nil && !errors.Is(err, context.Canceled) {
					s.logger.Info("Graph stream ended; view no longer current",
						"error", err.Error())
				}
				return
			}
			state.applyEntityDeltas(deltas)

		case deltas, open := <-communityDeltas:
			if !open {
				// Losing communities is not fatal — the pane falls back to
				// neutral colors. Stop selecting on the dead channel.
				communityDeltas = nil
				continue
			}
			state.applyCommunityDeltas(deltas)

		case <-ticker.C:
			attachCommunities()
			batch := state.flush()
			if batch == nil {
				continue
			}
			data, err := json.Marshal(batch)
			if err != nil {
				s.logger.Error("marshal graph batch", "error", err.Error())
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return // client gone
			}
			flusher.Flush()
		}
	}
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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

// bridgeState coalesces KV events between SSE flushes: latest wins per
// entity, communities invert Members[] to entity→community at flush.
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

// applyEntity ingests an ENTITY_STATES event for a boid key.
func (b *bridgeState) applyEntity(key string, value []byte, deleted bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if deleted {
		delete(b.dirty, key)
		b.removed[key] = struct{}{}
		return
	}
	var es struct {
		Triples []struct {
			Predicate string `json:"predicate"`
			Object    any    `json:"object"`
		} `json:"triples"`
	}
	if err := json.Unmarshal(value, &es); err != nil {
		return
	}
	e := graphEntity{ID: key}
	// Later triples win within one state (upstream #466 appends duplicates;
	// newest values sit at the tail, so last-write-wins reads correctly
	// either way).
	for _, tr := range es.Triples {
		switch tr.Predicate {
		case "flock.position.x":
			if v, ok := tr.Object.(float64); ok {
				e.X = v
			}
		case "flock.neighbor.count":
			// Marks the start of a fresh neighbor set in append order.
			e.Neighbors = e.Neighbors[:0]
		case "flock.position.y":
			if v, ok := tr.Object.(float64); ok {
				e.Y = v
			}
		case "flock.neighbor":
			if v, ok := tr.Object.(string); ok {
				e.Neighbors = append(e.Neighbors, v)
			}
		}
	}
	delete(b.removed, key)
	b.dirty[key] = e
}

// applyCommunity ingests a COMMUNITY_INDEX event (level-0 entries only;
// hierarchy levels legitimately contain everything).
func (b *bridgeState) applyCommunity(key string, value []byte, deleted bool) {
	if !strings.HasPrefix(key, "0.") {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if deleted {
		delete(b.communities, key)
		b.commDirty = true
		return
	}
	var c struct {
		Level   int      `json:"level"`
		Members []string `json:"members"`
	}
	if err := json.Unmarshal(value, &c); err != nil || c.Level != 0 {
		return
	}
	b.communities[key] = c.Members
	b.commDirty = true
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

// handleGraphStream serves GET <prefix>/graph/stream: an SSE stream of
// graph batches from KV watchers on ENTITY_STATES and COMMUNITY_INDEX.
// Watchers deliver current values first (the initial sync), then live
// updates; the ~500ms flush loop coalesces per entity so browser traffic
// is bounded by flush rate, not dial rate.
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
	if s.deps.NATSClient == nil {
		http.Error(w, "NATS unavailable", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	state := newBridgeState()
	if err := s.watchBucket(ctx, "ENTITY_STATES", func(key string, value []byte, deleted bool) {
		if isBoidKey(key) {
			state.applyEntity(key, value, deleted)
		}
	}); err != nil {
		http.Error(w, fmt.Sprintf("watch ENTITY_STATES: %v", err), http.StatusServiceUnavailable)
		return
	}
	// COMMUNITY_INDEX may not exist until clustering runs — degrade
	// gracefully (pane shows neutral colors until assignments arrive).
	if err := s.watchBucket(ctx, "COMMUNITY_INDEX", state.applyCommunity); err != nil {
		s.logger.Info("COMMUNITY_INDEX not watchable yet; streaming without communities",
			"error", err.Error())
	}

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
		case <-ticker.C:
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

// watchBucket attaches a KV watcher and pumps its events into apply until
// ctx is cancelled. Initial values arrive before live updates (jetstream
// WatchAll semantics), giving new clients a full sync.
func (s *Service) watchBucket(ctx context.Context, bucket string, apply func(key string, value []byte, deleted bool)) error {
	kv, err := s.deps.NATSClient.GetKeyValueBucket(ctx, bucket)
	if err != nil {
		return err
	}
	watcher, err := kv.WatchAll(ctx)
	if err != nil {
		return err
	}
	go func() {
		defer func() { _ = watcher.Stop() }()
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					return
				}
				if entry == nil {
					continue // initial-sync completion marker
				}
				deleted := entry.Operation() == jetstream.KeyValueDelete ||
					entry.Operation() == jetstream.KeyValuePurge
				apply(entry.Key(), entry.Value(), deleted)
			}
		}
	}()
	return nil
}

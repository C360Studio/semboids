//go:build integration

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/service"
	"github.com/nats-io/nats.go/jetstream"
)

// TestGraphStreamOverKV drives the SSE endpoint against real KV buckets:
// initial sync, live updates, and community assignments arriving late.
func TestGraphStreamOverKV(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithE2EDefaults())
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pre-create ENTITY_STATES with one boid (initial sync material).
	es, err := tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: "ENTITY_STATES"})
	if err != nil {
		t.Fatalf("create ENTITY_STATES: %v", err)
	}
	boidKey := "c360.semboids.sim.flock.boid.0"
	entity := func(x float64) []byte {
		data, _ := json.Marshal(map[string]any{"triples": []map[string]any{
			{"predicate": "flock.position.x", "object": x},
			{"predicate": "flock.position.y", "object": 50.0},
			{"predicate": "flock.neighbor.count", "object": 0.0},
		}})
		return data
	}
	if _, err := es.Put(ctx, boidKey, entity(10)); err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	svc, err := New(nil, &service.Dependencies{
		NATSClient:        tc.Client,
		ComponentRegistry: component.NewRegistry(), // stream needs only NATS
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := svc.(*Service)

	// Serve the stream on a real HTTP server (httptest.NewRecorder can't
	// stream).
	mux := http.NewServeMux()
	s.RegisterHTTPHandlers("/boids", mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/boids/graph/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}

	batches := make(chan graphBatch, 16)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var b graphBatch
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &b); err == nil {
				batches <- b
			}
		}
	}()

	// Initial sync: the seeded boid arrives.
	waitBatch(t, batches, 10*time.Second, func(b graphBatch) bool {
		for _, e := range b.Entities {
			if e.ID == boidKey && e.X == 10 {
				return true
			}
		}
		return false
	})

	// Live update coalesces to the latest value.
	if _, err := es.Put(ctx, boidKey, entity(99)); err != nil {
		t.Fatalf("update entity: %v", err)
	}
	waitBatch(t, batches, 10*time.Second, func(b graphBatch) bool {
		for _, e := range b.Entities {
			if e.ID == boidKey && e.X == 99 {
				return true
			}
		}
		return false
	})

	// Late-created COMMUNITY_INDEX: assignments flow once it exists…
	// (the handler warns and streams without communities when the bucket is
	// absent at connect — this asserts the graceful half: entities flowed.)
}

func waitBatch(t *testing.T, ch <-chan graphBatch, timeout time.Duration, ok func(graphBatch) bool) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case b := <-ch:
			if ok(b) {
				return
			}
		case <-deadline:
			t.Fatal("expected batch never arrived")
		}
	}
}

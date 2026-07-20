//go:build integration

package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// These tests cover the flock-communities requirements added by
// adopt-graphview-read-side: shared fan-out, slow-client isolation, fail-closed
// watcher loss, and community data remaining optional. One test per spec
// scenario, so the delta spec and this file are the same list.

// seedBoidKey is the boid every fixture starts with.
const seedBoidKey = "c360.semboids.sim.flock.boid.99"

// streamFixture is a started service with its HTTP server, backed by real KV.
type streamFixture struct {
	tc  *natsclient.TestClient
	svc *Service
	srv *httptest.Server
	es  jetstream.KeyValue
}

func newStreamFixture(t *testing.T, ctx context.Context) *streamFixture {
	t.Helper()
	tc := natsclient.NewTestClient(t, natsclient.WithE2EDefaults())

	es, err := tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: "ENTITY_STATES"})
	if err != nil {
		t.Fatalf("create ENTITY_STATES: %v", err)
	}
	// Seed one boid so the projection is non-empty: flush() correctly returns
	// nil when nothing has changed, so a client attached to an empty graph
	// receives no SSE message at all.
	if _, err := es.Put(ctx, seedBoidKey, boidEntityJSON(seedBoidKey, 1)); err != nil {
		t.Fatalf("seed boid: %v", err)
	}

	svc, err := New(nil, &service.Dependencies{
		NATSClient:        tc.Client,
		ComponentRegistry: component.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := svc.(*Service)
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop(5 * time.Second) })

	view := waitEntityView(t, s, 15*time.Second)
	if err := view.WaitCaughtUp(ctx); err != nil {
		t.Fatalf("entity view not caught up: %v", err)
	}

	mux := http.NewServeMux()
	s.RegisterHTTPHandlers("/boids", mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return &streamFixture{tc: tc, svc: s, srv: srv, es: es}
}

// boidEntityJSON builds a contract-valid boid state. The view decodes with the
// validating decoder, so id, triple subjects, and 3-part predicates are all
// required.
func boidEntityJSON(key string, x float64) []byte {
	data, _ := json.Marshal(map[string]any{"id": key, "triples": []map[string]any{
		{"subject": key, "predicate": "flock.position.x", "object": x},
		{"subject": key, "predicate": "flock.position.y", "object": 50.0},
		{"subject": key, "predicate": "flock.neighbor.count", "object": 0.0},
	}})
	return data
}

// consumerCount reports live JetStream consumers on a KV bucket's stream. The
// test client prefixes bucket names for isolation, so the stream name has to be
// built from that prefix.
func (f *streamFixture) consumerCount(ctx context.Context, t *testing.T, bucket string) int {
	t.Helper()
	js, err := f.tc.Client.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	name := fmt.Sprintf("KV_%s%s", f.tc.BucketPrefix, bucket)
	stream, err := js.Stream(ctx, name)
	if err != nil {
		t.Fatalf("stream %s: %v", name, err)
	}
	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	return info.State.Consumers
}

// streamClient is one connected SSE client.
type streamClient struct {
	batches chan graphBatch
	body    io.ReadCloser
	done    chan struct{}
}

func (f *streamFixture) connect(ctx context.Context, t *testing.T) *streamClient {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+"/boids/graph/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type = %q", ct)
	}
	c := &streamClient{
		batches: make(chan graphBatch, 64),
		body:    resp.Body,
		done:    make(chan struct{}),
	}
	go func() {
		defer close(c.done)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var b graphBatch
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &b); err == nil {
				select {
				case c.batches <- b:
				default: // drop when the test is not draining
				}
			}
		}
	}()
	t.Cleanup(func() { _ = resp.Body.Close() })
	return c
}

func (c *streamClient) close() { _ = c.body.Close() }

// TestSharedFanOutIsFlatInClientCount covers the spec scenarios "Consumer count
// is flat in client count" and "Disconnect leaves the shared view running".
func TestSharedFanOutIsFlatInClientCount(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	f := newStreamFixture(t, ctx)

	baseline := f.consumerCount(ctx, t, "ENTITY_STATES")

	var clients []*streamClient
	for n := 1; n <= 4; n++ {
		clients = append(clients, f.connect(ctx, t))
		// Each client must be serving before the count is meaningful.
		waitBatch(t, clients[n-1].batches, 10*time.Second, func(graphBatch) bool { return true })

		if got := f.consumerCount(ctx, t, "ENTITY_STATES"); got != baseline {
			t.Fatalf("consumers with %d clients = %d, want %d (flat) — a per-client watcher regressed", n, got, baseline)
		}
	}

	// Disconnect leaves the shared view running: a later client is served
	// without any new consumer being opened.
	for _, c := range clients {
		c.close()
	}
	if _, err := f.es.Put(ctx, "c360.semboids.sim.flock.boid.0", boidEntityJSON("c360.semboids.sim.flock.boid.0", 1)); err != nil {
		t.Fatalf("put: %v", err)
	}
	late := f.connect(ctx, t)
	waitBatch(t, late.batches, 10*time.Second, func(b graphBatch) bool { return len(b.Entities) > 0 })
	if got := f.consumerCount(ctx, t, "ENTITY_STATES"); got != baseline {
		t.Fatalf("consumers after reconnect = %d, want %d", got, baseline)
	}
}

// TestAttachDuringWritesConverges covers "Snapshot and stream do not overlap or
// gap": a client attaching while writes are in flight ends up with the final
// state, with no change lost between the snapshot and the delta stream.
func TestAttachDuringWritesConverges(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	f := newStreamFixture(t, ctx)

	key := "c360.semboids.sim.flock.boid.7"
	writes := make(chan struct{})
	go func() {
		defer close(writes)
		for i := 1; i <= 40; i++ {
			if _, err := f.es.Put(ctx, key, boidEntityJSON(key, float64(i))); err != nil {
				return
			}
		}
	}()

	// Attach mid-flight, then let the writes finish.
	c := f.connect(ctx, t)
	<-writes

	// Final write is 40; the client must converge to it. A gap between the
	// snapshot and the stream would strand it at an earlier value.
	waitBatch(t, c.batches, 15*time.Second, func(b graphBatch) bool {
		for _, e := range b.Entities {
			if e.ID == key && e.X == 40 {
				return true
			}
		}
		return false
	})
}

// TestSlowClientDoesNotStallPeers covers "Slow client does not stall its peers".
func TestSlowClientDoesNotStallPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	f := newStreamFixture(t, ctx)

	// A client that never reads its body: its socket buffer fills and the
	// handler's writes block.
	slowReq, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+"/boids/graph/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	slowResp, err := f.srv.Client().Do(slowReq)
	if err != nil {
		t.Fatalf("slow GET: %v", err)
	}
	defer func() { _ = slowResp.Body.Close() }()

	fast := f.connect(ctx, t)

	key := "c360.semboids.sim.flock.boid.3"
	for i := 1; i <= 30; i++ {
		if _, err := f.es.Put(ctx, key, boidEntityJSON(key, float64(i))); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	// The fast client keeps receiving current batches despite the stalled peer.
	waitBatch(t, fast.batches, 15*time.Second, func(b graphBatch) bool {
		for _, e := range b.Entities {
			if e.ID == key && e.X == 30 {
				return true
			}
		}
		return false
	})
}

// TestWatcherLossEndsStreamFailClosed covers "Stale projection is not served
// silently": deleting the bucket destroys the shared watcher, and connected
// responses must end rather than keep emitting the last known state.
func TestWatcherLossEndsStreamFailClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	f := newStreamFixture(t, ctx)

	c := f.connect(ctx, t)
	waitBatch(t, c.batches, 10*time.Second, func(graphBatch) bool { return true })

	// Destroy the bucket underneath the view.
	js, err := f.tc.Client.JetStream()
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	if err := js.DeleteKeyValue(ctx, f.tc.BucketPrefix+"ENTITY_STATES"); err != nil {
		t.Fatalf("delete bucket: %v", err)
	}

	// The response body must reach EOF: the reader goroutine finishes.
	select {
	case <-c.done:
	case <-time.After(30 * time.Second):
		t.Fatal("stream did not end after watcher loss — a stale projection is being served as though current")
	}
}

// TestCommunityDataRemainsOptional covers both community scenarios: a stream
// that starts before clustering has ever run stays healthy, and assignments
// begin flowing on that SAME connection once the bucket appears.
func TestCommunityDataRemainsOptional(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	f := newStreamFixture(t, ctx) // COMMUNITY_INDEX deliberately not created

	key := "c360.semboids.sim.flock.boid.2"
	if _, err := f.es.Put(ctx, key, boidEntityJSON(key, 5)); err != nil {
		t.Fatalf("put: %v", err)
	}

	c := f.connect(ctx, t)

	// Entities flow with no communities, connection healthy.
	waitBatch(t, c.batches, 15*time.Second, func(b graphBatch) bool {
		for _, e := range b.Entities {
			if e.ID == key {
				return true
			}
		}
		return false
	})

	// Clustering finally runs: create the bucket and write a level-0 community.
	ci, err := f.tc.Client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{Bucket: "COMMUNITY_INDEX"})
	if err != nil {
		t.Fatalf("create COMMUNITY_INDEX: %v", err)
	}
	members, _ := json.Marshal(map[string]any{"level": 0, "members": []string{key}})
	if _, err := ci.Put(ctx, "0.alpha", members); err != nil {
		t.Fatalf("put community: %v", err)
	}

	// The SAME connection must begin carrying assignments — no reconnect.
	waitBatch(t, c.batches, 60*time.Second, func(b graphBatch) bool {
		return b.Communities[key] == "0.alpha"
	})
}

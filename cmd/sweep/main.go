// Command sweep runs one load-dial sweep point against a running semboids
// stack and prints a classified per-window summary (load-dial §4).
//
// It sets the graph dial via the boids API, subscribes to the frame stream to
// measure real physics fps, warms up, then scrapes :9090 at the window's
// start and end to derive achieved snapshot/entity rates, snapshot drops,
// graph-ingest consumer lag, end-to-end latency quantiles, and graph-index
// write amplification (index KV puts/s per bucket vs entities ingested — the
// gh#474 / semstreams#524 signal). The window is classified per the D5
// attribution matrix (publisher-bound / ingest-bound / index-bound /
// downstream-lag / rejection-loss) with the raw signals printed so the
// campaign doc can quote them.
//
// Usage: go run ./cmd/sweep -hz 30 [-window 90s] [-warmup 30s] [-boids 200]
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sweep: %v\n", err)
		os.Exit(1)
	}
}

// config holds the sweep parameters resolved from flags.
type config struct {
	hz         float64
	window     time.Duration
	warmup     time.Duration
	boids      int
	tickHz     float64
	apiURL     string
	metricsURL string
	natsURL    string
	frames     string
	jsonOut    bool
}

func parseFlags() config {
	var c config
	flag.Float64Var(&c.hz, "hz", 0, "dial cadence to set (Hz); required")
	flag.DurationVar(&c.window, "window", 90*time.Second, "measurement window (60–120s per D5)")
	flag.DurationVar(&c.warmup, "warmup", 30*time.Second, "warm-up hold before measuring")
	flag.IntVar(&c.boids, "boids", 0, "boid count for the row (label only; set via config restart)")
	flag.Float64Var(&c.tickHz, "tick-hz", 30, "physics tick rate; a window is invalid if measured fps drops below 95% of this")
	flag.StringVar(&c.apiURL, "api", "http://localhost:8080/boids", "boids API base URL")
	flag.StringVar(&c.metricsURL, "metrics", "http://localhost:9090/metrics", "Prometheus scrape URL")
	flag.StringVar(&c.natsURL, "nats", "nats://localhost:4222", "NATS URL for the frame stream")
	flag.StringVar(&c.frames, "frames", "boids.frames", "frame subject to count for physics fps")
	flag.BoolVar(&c.jsonOut, "json", false, "also emit a machine-readable JSON summary line")
	flag.Parse()
	return c
}

func run() error {
	cfg := parseFlags()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Count frames off the real egress so physics fps is measured, not
	// inferred — no instrumentation on the physics hot path.
	frames, closeFrames, err := subscribeFrames(ctx, cfg)
	if err != nil {
		return fmt.Errorf("subscribe frames: %w", err)
	}
	defer closeFrames()

	if err := setDial(ctx, cfg); err != nil {
		return fmt.Errorf("set dial: %w", err)
	}
	fmt.Printf("dial → %.4g Hz; warming up %s…\n", cfg.hz, cfg.warmup)
	if err := sleepCtx(ctx, cfg.warmup); err != nil {
		return err
	}

	start, err := scrape(ctx, cfg.metricsURL)
	if err != nil {
		return fmt.Errorf("scrape (window start): %w", err)
	}
	framesStart := frames.Load()
	fmt.Printf("measuring %s window…\n", cfg.window)
	if err := sleepCtx(ctx, cfg.window); err != nil {
		return err
	}
	end, err := scrape(ctx, cfg.metricsURL)
	if err != nil {
		return fmt.Errorf("scrape (window end): %w", err)
	}
	framesDelta := frames.Load() - framesStart

	sum := summarize(cfg, start, end, framesDelta)
	printSummary(sum)
	if cfg.jsonOut {
		printJSON(sum)
	}
	return nil
}

// --- dial + frames -------------------------------------------------------

func setDial(ctx context.Context, cfg config) error {
	body, _ := json.Marshal(map[string]float64{"hz": cfg.hz})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		cfg.apiURL+"/graph/hz", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("dial PUT returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

// subscribeFrames connects to NATS and counts frame publishes into an atomic
// counter until the returned closer runs.
func subscribeFrames(ctx context.Context, cfg config) (*atomic.Int64, func(), error) {
	client, err := natsclient.NewClient(cfg.natsURL)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Connect(ctx); err != nil {
		return nil, nil, err
	}
	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.WaitForConnection(connCtx); err != nil {
		return nil, nil, err
	}
	var count atomic.Int64
	sub, err := client.Subscribe(ctx, cfg.frames, func(_ context.Context, _ *nats.Msg) {
		count.Add(1)
	})
	if err != nil {
		return nil, nil, err
	}
	closer := func() {
		_ = sub.Unsubscribe()
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		client.Close(closeCtx)
	}
	return &count, closer, nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// --- Prometheus scrape ---------------------------------------------------

// sample is one parsed Prometheus series: metric name, its labels, and value.
type sample struct {
	name   string
	labels map[string]string
	value  float64
}

// snapshot is the parsed metrics at one instant plus its wall-clock stamp.
type snapshot struct {
	at      time.Time
	samples []sample
}

func scrape(ctx context.Context, url string) (snapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return snapshot{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return snapshot{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return snapshot{}, fmt.Errorf("scrape returned %d", resp.StatusCode)
	}
	samples, err := parseExposition(resp.Body)
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{at: time.Now(), samples: samples}, nil
}

// parseExposition reads the Prometheus text format into samples, skipping
// comment/HELP/TYPE lines. Timestamps are not emitted by client_golang, so
// the value is the final whitespace-separated token.
func parseExposition(r io.Reader) ([]sample, error) {
	var out []sample
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.LastIndexByte(line, ' ')
		if idx < 0 {
			continue
		}
		v, err := strconv.ParseFloat(line[idx+1:], 64)
		if err != nil {
			continue
		}
		name, labels := parseNameLabels(line[:idx])
		out = append(out, sample{name: name, labels: labels, value: v})
	}
	return out, sc.Err()
}

func parseNameLabels(s string) (string, map[string]string) {
	brace := strings.IndexByte(s, '{')
	if brace < 0 {
		return s, nil
	}
	name := s[:brace]
	labels := map[string]string{}
	body := strings.TrimSuffix(s[brace+1:], "}")
	for _, pair := range splitLabels(body) {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(pair[:eq])
		val := strings.Trim(strings.TrimSpace(pair[eq+1:]), `"`)
		labels[key] = val
	}
	return name, labels
}

// splitLabels splits a label body on commas that are not inside quotes.
func splitLabels(body string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range body {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// sumSeries totals every series named metric (optionally filtered by an exact
// label match). Used for counters/gauges that fan out over labels.
func (s snapshot) sumSeries(metric string, labelKey, labelVal string) float64 {
	total := 0.0
	for _, smp := range s.samples {
		if smp.name != metric {
			continue
		}
		if labelKey != "" && smp.labels[labelKey] != labelVal {
			continue
		}
		total += smp.value
	}
	return total
}

// sumSeriesWhere totals every series named metric whose labels match all of
// the given key=value pairs — the multi-label form of sumSeries (e.g.
// operation=put AND kv_bucket=incoming). An empty match sums the whole family.
func (s snapshot) sumSeriesWhere(metric string, match map[string]string) float64 {
	total := 0.0
	for _, smp := range s.samples {
		if smp.name != metric {
			continue
		}
		ok := true
		for k, v := range match {
			if smp.labels[k] != v {
				ok = false
				break
			}
		}
		if ok {
			total += smp.value
		}
	}
	return total
}

// quantile linearly interpolates q over a histogram's window-delta buckets
// (end minus start), matching Prometheus histogram_quantile semantics.
func quantile(start, end snapshot, metric string, q float64) float64 {
	type bucket struct {
		le    float64
		count float64
	}
	deltas := map[float64]float64{} // le → cumulative-count delta
	bname := metric + "_bucket"
	for _, smp := range end.samples {
		if smp.name != bname {
			continue
		}
		le := parseLe(smp.labels["le"])
		deltas[le] += smp.value
	}
	for _, smp := range start.samples {
		if smp.name != bname {
			continue
		}
		le := parseLe(smp.labels["le"])
		deltas[le] -= smp.value
	}
	if len(deltas) == 0 {
		return 0
	}
	buckets := make([]bucket, 0, len(deltas))
	for le, c := range deltas {
		buckets = append(buckets, bucket{le: le, count: c})
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i].le < buckets[j].le })

	total := buckets[len(buckets)-1].count // +Inf bucket is the running total
	if total <= 0 {
		return 0
	}
	target := q * total
	prevLe, prevCum := 0.0, 0.0
	for _, b := range buckets {
		if b.count >= target {
			// Quantile lands in the overflow (+Inf) bucket: latency exceeds
			// the histogram's top finite bound. Report that bound rather than
			// interpolating toward infinity — the caller reads it as saturated
			// (a severe melt where the backlog drain dwarfs the bucket range).
			if math.IsInf(b.le, 1) {
				return prevLe
			}
			if b.le == prevLe || b.count == prevCum {
				return b.le
			}
			// Interpolate within [prevLe, le].
			frac := (target - prevCum) / (b.count - prevCum)
			return prevLe + frac*(b.le-prevLe)
		}
		prevLe, prevCum = b.le, b.count
	}
	return prevLe // all mass in overflow → the top finite bound
}

func parseLe(s string) float64 {
	if s == "+Inf" || s == "" {
		return math.Inf(1)
	}
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// --- summarize + classify ------------------------------------------------

// windowSummary is the classified result of one sweep point.
type windowSummary struct {
	DialHz        float64 `json:"dial_hz"`
	Boids         int     `json:"boids"`
	WindowSeconds float64 `json:"window_seconds"`
	SnapshotsPerS float64 `json:"snapshots_per_s"`
	EntitiesPerS  float64 `json:"entities_per_s"`
	DropsDelta    float64 `json:"drops_delta"`
	PhysicsFPS    float64 `json:"physics_fps"`
	PendingStart  float64 `json:"consumer_pending_start"`
	PendingEnd    float64 `json:"consumer_pending_end"`
	PendingTrend  float64 `json:"consumer_pending_trend"`
	IngestedPerS  float64 `json:"entities_updated_per_s"`
	RejectionsDlt float64 `json:"mutation_rejections_delta"`
	// Canonical-contract rejections (beta.147+ fail-closed at graph-ingest). A
	// non-zero delta means entities are being dropped for a non-3-part predicate
	// or non-6-part entity ID — the migration's proof-of-clean signal, expected
	// flat at zero on a conforming corpus.
	PredicateRejectDlt   float64 `json:"predicate_contract_rejections_delta"`
	EntityStateRejectDlt float64 `json:"entity_state_contract_rejections_delta"`
	E2EP50Seconds        float64 `json:"e2e_p50_seconds"`
	E2EP99Seconds        float64 `json:"e2e_p99_seconds"`
	// Graph-index maintenance load (semstreams_graph_index_*). IndexPutsPerS
	// is total KV puts/s across all index buckets; IncomingPutsPerS isolates
	// the INCOMING bucket (the gh#474 O(in-degree²) hot path #524 shards).
	// IndexWriteAmp = index puts per entity graph-index actually processed
	// (events_processed, NOT the ingest rate — graph-index reads ENTITY_STATES
	// by KV-watch, a different consumer that lags independently). It's the
	// per-entity fan-out cost (~incoming edges + outgoing + predicates), so it
	// stays roughly constant with load — a cost-structure signal, not an alarm.
	IndexEventsPerS  float64 `json:"index_events_per_s"`
	IndexPutsPerS    float64 `json:"index_puts_per_s"`
	IncomingPutsPerS float64 `json:"incoming_index_puts_per_s"`
	IndexWriteAmp    float64 `json:"index_write_amp"`
	Classification   string  `json:"classification"`
	ValidWindow      bool    `json:"valid_window"`
}

const entityStream = "ENTITY"

func summarize(cfg config, start, end snapshot, framesDelta int64) windowSummary {
	win := end.at.Sub(start.at).Seconds()
	if win <= 0 {
		win = cfg.window.Seconds()
	}
	perS := func(a, b float64) float64 { return (b - a) / win }

	pendStart := start.sumSeries("semstreams_jetstream_consumer_pending_messages", "stream", entityStream)
	pendEnd := end.sumSeries("semstreams_jetstream_consumer_pending_messages", "stream", entityStream)

	// graph-index maintenance load. kv_operations_total fans out over
	// {operation, kv_bucket}; puts are the write pressure #524 targets, and
	// the incoming bucket is the gh#474 hot path specifically.
	const idxKV = "semstreams_graph_index_kv_operations_total"
	putAll := map[string]string{"operation": "put"}
	putIncoming := map[string]string{"operation": "put", "kv_bucket": "incoming"}

	s := windowSummary{
		DialHz:        cfg.hz,
		Boids:         cfg.boids,
		WindowSeconds: win,
		SnapshotsPerS: perS(start.sumSeries("boids_graph_snapshots_published_total", "", ""),
			end.sumSeries("boids_graph_snapshots_published_total", "", "")),
		EntitiesPerS: perS(start.sumSeries("boids_graph_entities_published_total", "", ""),
			end.sumSeries("boids_graph_entities_published_total", "", "")),
		DropsDelta: end.sumSeries("boids_graph_snapshots_dropped_total", "", "") -
			start.sumSeries("boids_graph_snapshots_dropped_total", "", ""),
		PhysicsFPS:   float64(framesDelta) / win,
		PendingStart: pendStart,
		PendingEnd:   pendEnd,
		PendingTrend: pendEnd - pendStart,
		IngestedPerS: perS(start.sumSeries("semstreams_datamanager_entities_updated_total", "", ""),
			end.sumSeries("semstreams_datamanager_entities_updated_total", "", "")),
		RejectionsDlt: end.sumSeries("semstreams_graph_ingest_mutation_rejections_total", "", "") -
			start.sumSeries("semstreams_graph_ingest_mutation_rejections_total", "", ""),
		PredicateRejectDlt: end.sumSeries("semstreams_graph_ingest_predicate_contract_rejections_total", "", "") -
			start.sumSeries("semstreams_graph_ingest_predicate_contract_rejections_total", "", ""),
		EntityStateRejectDlt: end.sumSeries("semstreams_graph_ingest_entity_state_contract_rejections_total", "", "") -
			start.sumSeries("semstreams_graph_ingest_entity_state_contract_rejections_total", "", ""),
		E2EP50Seconds: quantile(start, end, "boids_graph_e2e_latency_seconds", 0.50),
		E2EP99Seconds: quantile(start, end, "boids_graph_e2e_latency_seconds", 0.99),
		IndexEventsPerS: perS(start.sumSeries("semstreams_graph_index_events_processed_total", "", ""),
			end.sumSeries("semstreams_graph_index_events_processed_total", "", "")),
		IndexPutsPerS: perS(start.sumSeriesWhere(idxKV, putAll),
			end.sumSeriesWhere(idxKV, putAll)),
		IncomingPutsPerS: perS(start.sumSeriesWhere(idxKV, putIncoming),
			end.sumSeriesWhere(idxKV, putIncoming)),
	}
	// Index puts per entity graph-index processed — the per-entity fan-out cost.
	// Denominator is the index's own event rate (not ingest): the two consumers
	// sit at different backlog depths, so dividing by ingest understates it
	// badly during an ingest-bound melt. Guarded against a zero-event window.
	if s.IndexEventsPerS > 0 {
		s.IndexWriteAmp = s.IndexPutsPerS / s.IndexEventsPerS
	}
	// Allow 5% jitter: fps is frames counted over a wall-clock window whose
	// boundaries don't align to tick edges, so a healthy 30Hz loop measures a
	// hair under 30. Only a genuine shortfall (physics starved by the sweep or
	// boid density) invalidates the window.
	s.ValidWindow = cfg.tickHz <= 0 || s.PhysicsFPS >= 0.95*cfg.tickHz
	s.Classification = classify(s)
	return s
}

// classify applies the D5 attribution matrix. Thresholds are deliberately
// loose — the raw signals are printed for the doc to quote and a human to
// confirm; this is a first-pass label, not an oracle.
func classify(s windowSummary) string {
	const pendingGrow = 50 // messages of net growth over the window
	switch {
	case !s.ValidWindow:
		return "invalid (physics fps < 30 — raise headroom or lower boids)"
	case s.PredicateRejectDlt > 0 || s.EntityStateRejectDlt > 0:
		return "contract-reject (non-canonical predicate/entity-id dropped fail-closed — check *_contract_rejections_total{reason})"
	case s.DropsDelta > 0 && s.PendingTrend < pendingGrow:
		return "publisher-bound (instrument ceiling — invalid melt window, raise workers)"
	case s.DropsDelta == 0 && s.PendingTrend >= pendingGrow:
		return "ingest-bound (substrate melt — capture pprof, file upstream)"
	case s.RejectionsDlt > 0 && s.EntitiesPerS-s.IngestedPerS > 1:
		return "rejection-loss (check mutation_rejections_total)"
	case s.PendingTrend < pendingGrow && s.E2EP99Seconds > 0.25:
		return "downstream-lag (index/clustering — check kv_operations_total{kv_bucket} + amp)"
	default:
		return "healthy (dial within capacity)"
	}
}

func printSummary(s windowSummary) {
	fmt.Printf(`
── sweep summary ─────────────────────────────────────────────
 dial            %.4g Hz   boids %d   window %.0fs
 achieved        %.2f snapshots/s   %.1f entities/s
 snapshot drops  %.0f (Δ over window)
 physics fps     %.1f  (%s)
 ingest pending  %.0f → %.0f  (Δ %+.0f)
 ingested        %.1f entities/s updated   rejections Δ %.0f
 contract-reject predicate Δ %.0f   entity-id Δ %.0f
 e2e latency     p50 %.1fms   p99 %.1fms
 index writes    %.1f puts/s (incoming %.1f)   %.1f events/s   amp %.1f puts/idx-entity
 ──
 classification  %s
──────────────────────────────────────────────────────────────
`,
		s.DialHz, s.Boids, s.WindowSeconds,
		s.SnapshotsPerS, s.EntitiesPerS,
		s.DropsDelta,
		s.PhysicsFPS, validLabel(s.ValidWindow),
		s.PendingStart, s.PendingEnd, s.PendingTrend,
		s.IngestedPerS, s.RejectionsDlt,
		s.PredicateRejectDlt, s.EntityStateRejectDlt,
		s.E2EP50Seconds*1000, s.E2EP99Seconds*1000,
		s.IndexPutsPerS, s.IncomingPutsPerS, s.IndexEventsPerS, s.IndexWriteAmp,
		s.Classification)
}

func validLabel(valid bool) string {
	if valid {
		return "valid"
	}
	return "INVALID window"
}

func printJSON(s windowSummary) {
	b, _ := json.Marshal(s)
	fmt.Printf("SWEEP_JSON %s\n", b)
}

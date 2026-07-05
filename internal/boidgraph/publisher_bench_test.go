package boidgraph

import (
	"context"
	"testing"
	"time"
)

// batchLatencyStream fakes gh#470's async batch publish: the whole snapshot
// pipelines, so the batch costs ~one drain latency regardless of message
// count — the property that lifts the instrument ceiling off the serial
// 200×ack-RTT ≈ 46ms/snapshot floor documented in graph-dial-first-look.md.
type batchLatencyStream struct {
	drain time.Duration
}

func (s batchLatencyStream) PublishBatchToStream(_ context.Context, _ string, _ [][]byte) error {
	time.Sleep(s.drain)
	return nil
}

// BenchmarkPublishSnapshot measures one snapshot's marshal + async-batch
// publish at 200 boids. With a fixed drain latency the cost is ~constant in
// boid count (the pipelining win), not linear as the old serial path was.
// Run: go test -bench=BenchmarkPublishSnapshot -benchmem ./internal/boidgraph/
func BenchmarkPublishSnapshot(b *testing.B) {
	snap := snapshotN(1, 200)
	stream := batchLatencyStream{drain: 231 * time.Microsecond}
	p := NewPublisher(stream, nil, "c360", "semboids", nil, nil)

	b.ResetTimer()
	for range b.N {
		p.publishSnapshot(context.Background(), snap)
	}
}

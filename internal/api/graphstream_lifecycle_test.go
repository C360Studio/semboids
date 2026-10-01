package api

import (
	"context"
	"errors"
	"github.com/nats-io/nats.go/jetstream"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/pkg/graphview"
	"github.com/c360studio/semstreams/service"
)

func TestGraphViewsCancellationJoinsPendingOpens(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{}, 2)
	exited := make(chan struct{}, 2)
	views := startGraphViews(ctx, func(ctx context.Context, _ string) (graphview.WatcherSource, error) {
		entered <- struct{}{}
		<-ctx.Done()
		exited <- struct{}{}
		return nil, ctx.Err()
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), newViewMetrics(nil))
	<-entered
	<-entered
	cancel()
	if err := views.stop(testStopContext(t)); err != nil {
		t.Fatal(err)
	}
	if len(exited) != 2 {
		t.Fatalf("joined %d bucket opens, want 2", len(exited))
	}
}

func TestGraphViewsStopDeadlineDoesNotClaimJoin(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	views := startGraphViews(context.Background(), func(context.Context, string) (graphview.WatcherSource, error) {
		entered <- struct{}{}
		<-release
		return nil, errors.New("released")
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), newViewMetrics(nil))
	<-entered
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := views.stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop=%v", err)
	}
	select {
	case <-views.stopped:
		t.Fatal("reported a completed join with opens blocked")
	default:
	}
}

func TestGraphViewsNilStopDoesNotCancel(t *testing.T) {
	entered := make(chan struct{}, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	views := startGraphViews(ctx, func(ctx context.Context, _ string) (graphview.WatcherSource, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), newViewMetrics(nil))
	<-entered
	<-entered
	if err := views.stop(nil); err == nil {
		t.Fatal("nil Stop context accepted")
	}
	select {
	case <-views.stopped:
		t.Fatal("nil Stop mutated lifecycle")
	default:
	}
	if err := views.stop(testStopContext(t)); err != nil {
		t.Fatal(err)
	}
}

type blockedWatcher struct {
	updates  chan jetstream.KeyValueEntry
	stopping chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (w *blockedWatcher) Updates() <-chan jetstream.KeyValueEntry { return w.updates }
func (w *blockedWatcher) Stop() error {
	w.once.Do(func() { close(w.stopping) })
	<-w.release
	return nil
}

type watcherSource struct {
	watcher *blockedWatcher
	opened  chan struct{}
}

func (s watcherSource) WatchAll(context.Context, ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	close(s.opened)
	return s.watcher, nil
}

func TestGraphViewsStopBoundsNativeWatcherJoin(t *testing.T) {
	watcher := &blockedWatcher{updates: make(chan jetstream.KeyValueEntry), stopping: make(chan struct{}), release: make(chan struct{})}
	opened := make(chan struct{})
	views := startGraphViews(context.Background(), func(ctx context.Context, bucket string) (graphview.WatcherSource, error) {
		if bucket == entityStatesBucket {
			return watcherSource{watcher: watcher, opened: opened}, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), newViewMetrics(nil))
	<-opened
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := views.stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop=%v", err)
	}
	<-watcher.stopping
	select {
	case <-views.stopped:
		t.Fatal("claimed native watcher joined")
	default:
	}
	close(watcher.release)
	if err := views.stop(testStopContext(t)); err != nil {
		t.Fatal(err)
	}
}

func TestServiceStopCancelsBaseEvenWhenViewsCannotJoin(t *testing.T) {
	svc, err := New(nil, &service.Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	s := svc.(*Service)
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	defer cancelLifetime()
	if err := s.Start(lifetime); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	s.views = startGraphViews(lifetime, func(context.Context, string) (graphview.WatcherSource, error) {
		entered <- struct{}{}
		<-release
		return nil, errors.New("released")
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), newViewMetrics(nil))
	defer close(release)
	<-entered
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop=%v", err)
	}
	if s.Status() == service.StatusRunning {
		t.Error("view join failure left base service running")
	}
}

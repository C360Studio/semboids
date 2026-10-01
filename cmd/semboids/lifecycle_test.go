package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

type lifecycleProbe struct {
	start func(context.Context) error
	stop  func(context.Context) error
}

func (p lifecycleProbe) StartAll(ctx context.Context) error { return p.start(ctx) }
func (p lifecycleProbe) StopAll(ctx context.Context) error  { return p.stop(ctx) }

func TestControlledStopKeepsStartContextLive(t *testing.T) {
	shutdown := make(chan struct{})
	var live context.Context
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&readyWriter{ready: shutdown}, nil)))
	defer slog.SetDefault(original)
	manager := lifecycleProbe{
		start: func(ctx context.Context) error { live = ctx; return nil },
		stop: func(ctx context.Context) error {
			if live.Err() != nil {
				t.Errorf("Start context canceled before Stop joined: %v", live.Err())
			}
			if ctx.Err() != nil {
				t.Errorf("shutdown context already canceled: %v", ctx.Err())
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Error("shutdown has no deadline")
			}
			return nil
		},
	}
	if err := runServices(context.Background(), shutdown, manager, time.Second); err != nil {
		t.Fatal(err)
	}
	if live.Err() == nil {
		t.Error("lifetime context not canceled after Stop")
	}
}
func TestFailedStartStopsWithFreshContext(t *testing.T) {
	failure := errors.New("partial startup")
	stopped := false
	manager := lifecycleProbe{
		start: func(context.Context) error { return failure },
		stop: func(ctx context.Context) error {
			stopped = true
			if ctx.Err() != nil {
				t.Errorf("cleanup canceled: %v", ctx.Err())
			}
			return nil
		},
	}
	if err := runServices(context.Background(), make(chan struct{}), manager, time.Second); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	if !stopped {
		t.Error("partially started services not stopped")
	}
}
func TestAbortStartContextUsesFreshStopContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var live context.Context
	manager := lifecycleProbe{
		start: func(ctx context.Context) error { live = ctx; cancel(); return nil },
		stop: func(ctx context.Context) error {
			if !errors.Is(live.Err(), context.Canceled) {
				t.Error("abort did not cancel lifetime")
			}
			if ctx.Err() != nil {
				t.Errorf("abort reused canceled context: %v", ctx.Err())
			}
			return nil
		},
	}
	if err := runServices(ctx, make(chan struct{}), manager, time.Second); err != nil {
		t.Fatal(err)
	}
}

// readyWriter requests a signal only after the host reports completed startup.
// That distinguishes a controlled shutdown from the startup-abort test.
type readyWriter struct {
	ready chan struct{}
	once  sync.Once
}

func (w *readyWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "All services started") {
		w.once.Do(func() { close(w.ready) })
	}
	return len(p), nil
}

func TestSignalDuringStartupAbortsAndJoinsBeforeCleanup(t *testing.T) {
	shutdown := make(chan struct{})
	entered := make(chan struct{})
	joined := false
	manager := lifecycleProbe{
		start: func(ctx context.Context) error { close(entered); <-ctx.Done(); joined = true; return ctx.Err() },
		stop: func(ctx context.Context) error {
			if !joined {
				t.Error("Stop ran before startup joined")
			}
			if ctx.Err() != nil {
				t.Error("cleanup context canceled")
			}
			return nil
		},
	}
	go func() { <-entered; close(shutdown) }()
	if err := runServices(context.Background(), shutdown, manager, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("startup signal error=%v", err)
	}
}

package sim

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A stopped component is one-shot. Process restart constructs a fresh component
// so old snapshot publishers and lifecycle drains cannot share its mutable state.
func TestComponentStoppedInstanceCannotRestart(t *testing.T) {
	comp, frames := newTestComponent(t, 5, 200)
	if err := comp.Initialize(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := comp.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-frames:
	case <-time.After(time.Second):
		t.Fatal("no initial frame")
	}
	if err := comp.Stop(testStopContext(t)); err != nil {
		t.Fatal(err)
	}
	if err := comp.Start(ctx); err == nil {
		_ = comp.Stop(testStopContext(t))
		t.Fatal("stopped component accepted a second Start")
	}
}

// A blocked owner cannot be reported stopped merely because the caller's budget
// expired. A later join waits for the original work, never starts another loop.
func TestComponentStopReportsIncompleteJoin(t *testing.T) {
	comp, _ := newTestComponent(t, 5, 200)
	entered, release := make(chan struct{}), make(chan struct{})
	comp.publish = func(context.Context, string, []byte) error {
		select {
		case <-entered:
		default:
			close(entered)
		}
		<-release
		return nil
	}
	if err := comp.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := comp.Stop(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop = %v, want canceled incomplete join", err)
	}
	close(release)
	if err := comp.Stop(testStopContext(t)); err != nil {
		t.Fatal(err)
	}
}

func TestComponentRejectsNilContexts(t *testing.T) {
	comp, _ := newTestComponent(t, 5, 200)
	if err := comp.Start(nil); err == nil {
		t.Fatal("Start(nil) succeeded")
	}
	if err := comp.Stop(nil); err == nil {
		t.Fatal("Stop(nil) succeeded")
	}
}

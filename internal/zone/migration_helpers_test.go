package zone

import (
	"context"
	"fmt"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/types"
	"testing"
	"time"
)

func testStopContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func createTestComponent(reg *component.Registry, _ string, cfg types.ComponentConfig, deps component.Dependencies) (component.Discoverable, error) {
	factory, ok := reg.GetFactory(cfg.Name)
	if !ok {
		return nil, fmt.Errorf("factory %q unavailable", cfg.Name)
	}
	return factory(cfg.Config, deps)
}

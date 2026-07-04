package zone

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/c360studio/semstreams/message"
)

// IngestSubject is the JetStream subject zones publish to; it sits under
// graph-ingest's default `entity.>` input filter (ENTITY stream).
const IngestSubject = "entity.zone.upsert"

// StreamPublisher is the slice of the NATS client Ingest needs; narrow so
// tests can fake it.
type StreamPublisher interface {
	PublishToStream(ctx context.Context, subject string, data []byte) error
}

// Ingest publishes each zone as a BaseMessage-wrapped Graphable to the
// graph-ingest input, landing them in ENTITY_STATES. The host never writes
// the bucket directly. Called once at startup, after streams are ensured.
func Ingest(ctx context.Context, pub StreamPublisher, zones []Zone, orgID, platform string) error {
	if err := Validate(zones); err != nil {
		return fmt.Errorf("validate zones: %w", err)
	}
	now := time.Now()
	for _, z := range zones {
		entity := &Entity{Zone: z, OrgID: orgID, Platform: platform, ObservedAt: now}
		baseMsg := message.NewBaseMessage(entity.Schema(), entity, "semboids")
		data, err := json.Marshal(baseMsg)
		if err != nil {
			return fmt.Errorf("marshal zone %q: %w", z.ID, err)
		}
		if err := pub.PublishToStream(ctx, IngestSubject, data); err != nil {
			return fmt.Errorf("publish zone %q: %w", z.ID, err)
		}
	}
	return nil
}

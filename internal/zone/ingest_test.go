package zone

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

type fakeStreamPublisher struct {
	subjects []string
	payloads [][]byte
	fail     bool
}

func (f *fakeStreamPublisher) PublishToStream(_ context.Context, subject string, data []byte) error {
	if f.fail {
		return context.DeadlineExceeded
	}
	f.subjects = append(f.subjects, subject)
	f.payloads = append(f.payloads, data)
	return nil
}

func TestIngestPublishesOneMessagePerZone(t *testing.T) {
	pub := &fakeStreamPublisher{}
	zones := validZones()
	if err := Ingest(context.Background(), pub, zones, "c360", "semboids"); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(pub.payloads) != len(zones) {
		t.Fatalf("published %d messages, want %d", len(pub.payloads), len(zones))
	}
	for _, s := range pub.subjects {
		if s != IngestSubject {
			t.Fatalf("published to %q, want %q", s, IngestSubject)
		}
	}
	// Each payload is a BaseMessage envelope carrying boids.zone.v1.
	var envelope struct {
		Type    map[string]any  `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(pub.payloads[0], &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if !strings.Contains(fmt.Sprint(envelope.Type), "zone") {
		t.Fatalf("envelope type %v does not mention zone", envelope.Type)
	}
	if len(envelope.Payload) == 0 {
		t.Fatal("envelope has no payload")
	}
}

func TestIngestRejectsInvalidZones(t *testing.T) {
	pub := &fakeStreamPublisher{}
	bad := validZones()
	bad[0].R = -1
	if err := Ingest(context.Background(), pub, bad, "c360", "semboids"); err == nil {
		t.Fatal("invalid zones accepted")
	}
	if len(pub.payloads) != 0 {
		t.Fatal("published despite validation failure")
	}
}

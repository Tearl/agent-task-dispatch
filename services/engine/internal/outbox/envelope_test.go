package outbox

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeRoundTripPreservesRawPayloadAndIdentity(t *testing.T) {
	message := Message{ID: "message-1", DedupeKey: "logical-1", Topic: "command.requested", Payload: json.RawMessage(`{"taskId":"task-1"}`), CreatedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.FixedZone("offset", 8*60*60))}
	body, err := EncodeEnvelope(message)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeEnvelope(body, message.Topic)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != message.ID || decoded.DedupeKey != message.DedupeKey || decoded.Topic != message.Topic || string(decoded.Payload) != string(message.Payload) || decoded.CreatedAt.Location() != time.UTC {
		t.Fatalf("unexpected round trip: %#v", decoded)
	}
}

func TestEnvelopeRejectsUnknownVersionRouteTrailingDataAndOversizedMetadata(t *testing.T) {
	valid := `{"version":"outbox-envelope-v1","messageId":"message-1","dedupeKey":"dedupe-1","topic":"command.requested","payload":{},"createdAt":"2026-08-23T12:00:00Z"}`
	values := []struct {
		body  string
		topic string
	}{
		{body: valid, topic: "wrong.requested"},
		{body: valid + `{}`, topic: "command.requested"},
		{body: `{"version":"future","messageId":"message-1","dedupeKey":"dedupe-1","topic":"command.requested","payload":{},"createdAt":"2026-08-23T12:00:00Z"}`, topic: "command.requested"},
		{body: `{"version":"outbox-envelope-v1","messageId":"message-1","dedupeKey":"dedupe-1","topic":"command.requested","payload":{},"createdAt":"2026-08-23T12:00:00Z","extra":true}`, topic: "command.requested"},
	}
	for _, value := range values {
		if _, err := DecodeEnvelope([]byte(value.body), value.topic); err == nil {
			t.Fatalf("invalid envelope was accepted: %s", value.body)
		}
	}
	message := Message{ID: "message-1", DedupeKey: string(make([]byte, maxMetadataBytes)), Topic: "command.requested", Payload: json.RawMessage(`{}`), CreatedAt: time.Now().UTC()}
	if _, err := EncodeEnvelope(message); err == nil {
		t.Fatal("oversized metadata was accepted")
	}
}

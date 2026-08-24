package outbox

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
)

const (
	EnvelopeVersion  = "outbox-envelope-v1"
	MaxEnvelopeBytes = 240 * 1024
	maxMetadataBytes = 8 * 1024

	AttributeMessageID = "message_id"
	AttributeDedupeKey = "dedupe_key"
	AttributeTopic     = "topic"
	AttributeVersion   = "version"
)

type Envelope struct {
	Version   string          `json:"version"`
	MessageID string          `json:"messageId"`
	DedupeKey string          `json:"dedupeKey"`
	Topic     string          `json:"topic"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

func EncodeEnvelope(message Message) ([]byte, error) {
	if err := validateEnvelopeMessage(message, message.Topic); err != nil {
		return nil, err
	}
	body, err := json.Marshal(Envelope{
		Version:   EnvelopeVersion,
		MessageID: message.ID,
		DedupeKey: message.DedupeKey,
		Topic:     message.Topic,
		Payload:   message.Payload,
		CreatedAt: message.CreatedAt.UTC(),
	})
	if err != nil || len(body) > MaxEnvelopeBytes {
		return nil, NewFailure("outbox_envelope_too_large", true)
	}
	return body, nil
}

func DecodeEnvelope(body []byte, expectedTopic string) (Message, error) {
	if len(body) == 0 || len(body) > MaxEnvelopeBytes || strings.TrimSpace(expectedTopic) == "" {
		return Message{}, NewFailure("invalid_outbox_envelope", true)
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	var value Envelope
	if err := decoder.Decode(&value); err != nil {
		return Message{}, NewFailure("invalid_outbox_envelope", true)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF || value.Version != EnvelopeVersion {
		return Message{}, NewFailure("invalid_outbox_envelope", true)
	}
	message := Message{ID: value.MessageID, DedupeKey: value.DedupeKey, Topic: value.Topic, Payload: value.Payload, CreatedAt: value.CreatedAt.UTC()}
	if err := validateEnvelopeMessage(message, expectedTopic); err != nil {
		return Message{}, err
	}
	return message, nil
}

func validateEnvelopeMessage(message Message, expectedTopic string) error {
	metadataBytes := len(message.ID) + len(message.DedupeKey) + len(message.Topic) + len(EnvelopeVersion)
	if strings.TrimSpace(message.ID) == "" || strings.TrimSpace(message.DedupeKey) == "" || message.Topic != expectedTopic || metadataBytes > maxMetadataBytes || message.CreatedAt.IsZero() || len(message.Payload) == 0 || !json.Valid(message.Payload) {
		return NewFailure("invalid_outbox_envelope", true)
	}
	return nil
}

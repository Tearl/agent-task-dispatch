package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrIdempotencyConflict = errors.New("idempotency key reused with different request")

type Request struct {
	Scope       string
	Key         string
	RequestHash string
}

func (r Request) Validate() error {
	if r.Scope == "" || r.Key == "" || r.RequestHash == "" {
		return errors.New("idempotency scope, key, and request hash are required")
	}
	return nil
}

type DomainEvent struct {
	ID               string
	AggregateType    string
	AggregateID      string
	AggregateVersion int64
	EventType        string
	Payload          json.RawMessage
	OccurredAt       time.Time
}

type AuditEvent struct {
	ID           string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Metadata     json.RawMessage
	OccurredAt   time.Time
}

type OutboxMessage struct {
	ID          string
	DedupeKey   string
	Topic       string
	Payload     json.RawMessage
	AvailableAt time.Time
}

type Outcome struct {
	Response     json.RawMessage
	DomainEvents []DomainEvent
	AuditEvents  []AuditEvent
	Outbox       []OutboxMessage
}

func (o Outcome) Validate() error {
	if !json.Valid(o.Response) {
		return errors.New("response must be valid JSON")
	}
	for _, event := range o.DomainEvents {
		if event.ID == "" || event.AggregateType == "" || event.AggregateID == "" || event.AggregateVersion < 1 || event.EventType == "" || event.OccurredAt.IsZero() || !json.Valid(event.Payload) {
			return fmt.Errorf("invalid domain event %q", event.ID)
		}
	}
	for _, event := range o.AuditEvents {
		if event.ID == "" || event.Action == "" || event.ResourceType == "" || event.ResourceID == "" || event.OccurredAt.IsZero() || !json.Valid(event.Metadata) {
			return fmt.Errorf("invalid audit event %q", event.ID)
		}
	}
	for _, message := range o.Outbox {
		if message.ID == "" || message.DedupeKey == "" || message.Topic == "" || message.AvailableAt.IsZero() || !json.Valid(message.Payload) {
			return fmt.Errorf("invalid outbox message %q", message.ID)
		}
	}
	return nil
}

type Work func(context.Context, Transaction) (Outcome, error)

type Transaction interface {
	Exec(context.Context, string, ...any) (affectedRows int64, err error)
}

type Store interface {
	Execute(context.Context, Request, Work) (Outcome, bool, error)
}

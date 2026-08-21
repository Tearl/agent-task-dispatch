package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestMemoryStoreCommitsOutcomeAtomicallyAndReplays(t *testing.T) {
	store := NewMemoryStore()
	calls := 0
	work := func(context.Context, Transaction) (Outcome, error) {
		calls++
		now := time.Unix(1, 0).UTC()
		return Outcome{
			Response:     json.RawMessage(`{"id":"task-1"}`),
			DomainEvents: []DomainEvent{{ID: "event-1", AggregateType: "task", AggregateID: "task-1", AggregateVersion: 1, EventType: "task.created", Payload: json.RawMessage(`{}`), OccurredAt: now}},
			AuditEvents:  []AuditEvent{{ID: "audit-1", ActorID: "user-1", Action: "task.create", ResourceType: "task", ResourceID: "task-1", Metadata: json.RawMessage(`{}`), OccurredAt: now}},
			Outbox:       []OutboxMessage{{ID: "message-1", DedupeKey: "task-1:created", Topic: "task-events", Payload: json.RawMessage(`{}`), AvailableAt: now}},
		}, nil
	}
	request := Request{Scope: "tasks.create", Key: "key-1", RequestHash: "sha256:one"}
	first, replay, err := store.Execute(context.Background(), request, work)
	if err != nil || replay {
		t.Fatalf("first execution: replay=%v err=%v", replay, err)
	}
	second, replay, err := store.Execute(context.Background(), request, work)
	if err != nil || !replay {
		t.Fatalf("replay: replay=%v err=%v", replay, err)
	}
	if calls != 1 || string(first.Response) != string(second.Response) {
		t.Fatalf("work must run once and replay stable response")
	}
	if events, audits, messages := store.Counts(); events != 1 || audits != 1 || messages != 1 {
		t.Fatalf("unexpected counts: %d %d %d", events, audits, messages)
	}
}

func TestMemoryStoreRejectsIdempotencyKeyWithDifferentRequest(t *testing.T) {
	store := NewMemoryStore()
	work := func(context.Context, Transaction) (Outcome, error) {
		return Outcome{Response: json.RawMessage(`{}`)}, nil
	}
	_, _, _ = store.Execute(context.Background(), Request{Scope: "scope", Key: "key", RequestHash: "one"}, work)
	_, _, err := store.Execute(context.Background(), Request{Scope: "scope", Key: "key", RequestHash: "two"}, work)
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestMemoryStoreRollsBackAllFoundationRecordsOnFailure(t *testing.T) {
	store := NewMemoryStore()
	expected := errors.New("domain write failed")
	_, _, err := store.Execute(context.Background(), Request{Scope: "scope", Key: "key", RequestHash: "one"}, func(context.Context, Transaction) (Outcome, error) {
		return Outcome{Response: json.RawMessage(`{}`), DomainEvents: []DomainEvent{{ID: "event"}}}, expected
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected work error, got %v", err)
	}
	if events, audits, messages := store.Counts(); events != 0 || audits != 0 || messages != 0 {
		t.Fatalf("failed work leaked records")
	}
}

func TestOutcomeRejectsZeroTimes(t *testing.T) {
	tests := []Outcome{
		{Response: json.RawMessage(`{}`), DomainEvents: []DomainEvent{{ID: "event", AggregateType: "task", AggregateID: "task-1", AggregateVersion: 1, EventType: "created", Payload: json.RawMessage(`{}`)}}},
		{Response: json.RawMessage(`{}`), AuditEvents: []AuditEvent{{ID: "audit", Action: "create", ResourceType: "task", ResourceID: "task-1", Metadata: json.RawMessage(`{}`)}}},
		{Response: json.RawMessage(`{}`), Outbox: []OutboxMessage{{ID: "message", DedupeKey: "dedupe", Topic: "events", Payload: json.RawMessage(`{}`)}}},
	}
	for index, outcome := range tests {
		if err := outcome.Validate(); err == nil {
			t.Fatalf("case %d accepted zero time", index)
		}
	}
}

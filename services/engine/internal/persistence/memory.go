package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

type MemoryStore struct {
	mu      sync.Mutex
	results map[string]memoryResult
	events  []DomainEvent
	audits  []AuditEvent
	outbox  []OutboxMessage
}

type memoryResult struct {
	requestHash string
	outcome     Outcome
}

type memoryTransaction struct{}

func (*memoryTransaction) Exec(context.Context, string, ...any) (int64, error) {
	return 0, errors.New("memory transaction does not execute SQL")
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{results: make(map[string]memoryResult)}
}

func (s *MemoryStore) Execute(ctx context.Context, request Request, work Work) (Outcome, bool, error) {
	if err := request.Validate(); err != nil {
		return Outcome{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return Outcome{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	identity := request.Scope + "\x00" + request.Key
	if previous, ok := s.results[identity]; ok {
		if previous.requestHash != request.RequestHash {
			return Outcome{}, false, ErrIdempotencyConflict
		}
		return cloneOutcome(previous.outcome), true, nil
	}

	outcome, err := work(ctx, &memoryTransaction{})
	if err != nil {
		return Outcome{}, false, err
	}
	if err := outcome.Validate(); err != nil {
		return Outcome{}, false, err
	}
	stored := cloneOutcome(outcome)
	s.results[identity] = memoryResult{requestHash: request.RequestHash, outcome: stored}
	s.events = append(s.events, stored.DomainEvents...)
	s.audits = append(s.audits, stored.AuditEvents...)
	s.outbox = append(s.outbox, stored.Outbox...)
	return cloneOutcome(stored), false, nil
}

func (s *MemoryStore) Counts() (domainEvents, auditEvents, outboxMessages int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events), len(s.audits), len(s.outbox)
}

func cloneOutcome(value Outcome) Outcome {
	copyJSON := func(value json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), value...) }
	cloned := Outcome{Response: copyJSON(value.Response)}
	cloned.DomainEvents = append([]DomainEvent(nil), value.DomainEvents...)
	for index := range cloned.DomainEvents {
		cloned.DomainEvents[index].Payload = copyJSON(cloned.DomainEvents[index].Payload)
	}
	cloned.AuditEvents = append([]AuditEvent(nil), value.AuditEvents...)
	for index := range cloned.AuditEvents {
		cloned.AuditEvents[index].Metadata = copyJSON(cloned.AuditEvents[index].Metadata)
	}
	cloned.Outbox = append([]OutboxMessage(nil), value.Outbox...)
	for index := range cloned.Outbox {
		cloned.Outbox[index].Payload = copyJSON(cloned.Outbox[index].Payload)
	}
	return cloned
}

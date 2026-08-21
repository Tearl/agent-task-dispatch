package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/example/agent-platform/engine/internal/persistence"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

type transaction struct{ tx *sql.Tx }

func (t transaction) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	result, err := t.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read affected rows: %w", err)
	}
	return affected, nil
}

func (s *Store) Execute(ctx context.Context, request persistence.Request, work persistence.Work) (outcome persistence.Outcome, replay bool, err error) {
	if err = request.Validate(); err != nil {
		return outcome, false, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return outcome, false, fmt.Errorf("begin persistence transaction: %w", err)
	}
	// Rollback is intentionally unconditional. It is harmless after Commit and
	// also releases the connection and transaction-scoped advisory lock when a
	// work callback panics and an outer recovery boundary resumes execution.
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, request.Scope+":"+request.Key); err != nil {
		return outcome, false, fmt.Errorf("lock idempotency key: %w", err)
	}
	var previousHash string
	var previousResponse []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash, response_body FROM idempotency_records WHERE scope = $1 AND idempotency_key = $2`, request.Scope, request.Key).Scan(&previousHash, &previousResponse)
	if err == nil {
		if previousHash != request.RequestHash {
			return outcome, false, persistence.ErrIdempotencyConflict
		}
		outcome.Response = json.RawMessage(previousResponse)
		if err = tx.Commit(); err != nil {
			return outcome, false, fmt.Errorf("commit replay transaction: %w", err)
		}
		return outcome, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return outcome, false, fmt.Errorf("read idempotency result: %w", err)
	}

	outcome, err = work(ctx, transaction{tx: tx})
	if err != nil {
		return outcome, false, err
	}
	if err = outcome.Validate(); err != nil {
		return outcome, false, err
	}
	if err = insertOutcome(ctx, tx, request, outcome); err != nil {
		return outcome, false, err
	}
	if err = tx.Commit(); err != nil {
		return outcome, false, fmt.Errorf("commit persistence transaction: %w", err)
	}
	return outcome, false, nil
}

func insertOutcome(ctx context.Context, tx *sql.Tx, request persistence.Request, outcome persistence.Outcome) error {
	for _, event := range outcome.DomainEvents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO domain_events (event_id, aggregate_type, aggregate_id, aggregate_version, event_type, payload, occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, event.ID, event.AggregateType, event.AggregateID, event.AggregateVersion, event.EventType, string(event.Payload), event.OccurredAt); err != nil {
			return fmt.Errorf("insert domain event: %w", err)
		}
	}
	for _, event := range outcome.AuditEvents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events (event_id, actor_id, action, resource_type, resource_id, metadata, occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, event.ID, nullable(event.ActorID), event.Action, event.ResourceType, event.ResourceID, string(event.Metadata), event.OccurredAt); err != nil {
			return fmt.Errorf("insert audit event: %w", err)
		}
	}
	for _, message := range outcome.Outbox {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox_messages (message_id, dedupe_key, topic, payload, available_at) VALUES ($1,$2,$3,$4,$5)`, message.ID, message.DedupeKey, message.Topic, string(message.Payload), message.AvailableAt); err != nil {
			return fmt.Errorf("insert outbox message: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records (scope, idempotency_key, request_hash, response_body) VALUES ($1,$2,$3,$4)`, request.Scope, request.Key, request.RequestHash, []byte(outcome.Response)); err != nil {
		return fmt.Errorf("insert idempotency result: %w", err)
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

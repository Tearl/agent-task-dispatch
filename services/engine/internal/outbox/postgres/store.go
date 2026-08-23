package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/outbox"
	"github.com/lib/pq"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, outbox.ErrInvalidInput
	}
	return &Store{db: db}, nil
}

func (store *Store) Claim(ctx context.Context, workerID string, topics []string, limit int, lease time.Duration) ([]outbox.Message, error) {
	if workerID == "" || len(topics) == 0 || limit < 1 || limit > 100 || lease < time.Second || lease > 15*time.Minute {
		return nil, outbox.ErrInvalidInput
	}
	rows, err := store.db.QueryContext(ctx, `WITH candidates AS (
    SELECT message_id
    FROM outbox_messages
    WHERE published_at IS NULL
      AND dead_lettered_at IS NULL
      AND topic = ANY($4)
      AND available_at <= clock_timestamp()
      AND (locked_until IS NULL OR locked_until <= clock_timestamp())
    ORDER BY available_at, message_id
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE outbox_messages message
SET locked_by=$2,
    locked_until=clock_timestamp() + ($3 * interval '1 millisecond'),
    attempts=attempts+1
FROM candidates
WHERE message.message_id=candidates.message_id
RETURNING message.message_id,message.dedupe_key,message.topic,message.payload,message.available_at,message.attempts,message.created_at`, limit, workerID, lease.Milliseconds(), pq.Array(topics))
	if err != nil {
		return nil, fmt.Errorf("claim outbox messages: %w", err)
	}
	defer rows.Close()
	messages := make([]outbox.Message, 0, limit)
	for rows.Next() {
		var message outbox.Message
		if err = rows.Scan(&message.ID, &message.DedupeKey, &message.Topic, &message.Payload, &message.AvailableAt, &message.Attempts, &message.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan outbox message: %w", err)
		}
		messages = append(messages, message)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate outbox messages: %w", err)
	}
	return messages, nil
}

func (store *Store) Complete(ctx context.Context, workerID, messageID string) error {
	result, err := store.db.ExecContext(ctx, `UPDATE outbox_messages
SET published_at=clock_timestamp(),locked_by=NULL,locked_until=NULL,last_error=NULL
WHERE message_id=$1 AND locked_by=$2 AND published_at IS NULL AND dead_lettered_at IS NULL`, messageID, workerID)
	return leaseResult(result, err, "complete")
}

func (store *Store) Retry(ctx context.Context, workerID, messageID, code string, retryAt time.Time, dead bool) error {
	if !retryAt.After(time.Time{}) {
		return outbox.ErrInvalidInput
	}
	result, err := store.db.ExecContext(ctx, `UPDATE outbox_messages
SET available_at=$3,
    locked_by=NULL,
    locked_until=NULL,
    last_error=$4,
    dead_lettered_at=CASE WHEN $5 THEN clock_timestamp() ELSE NULL END
WHERE message_id=$1 AND locked_by=$2 AND published_at IS NULL AND dead_lettered_at IS NULL`, messageID, workerID, retryAt.UTC(), code, dead)
	return leaseResult(result, err, "retry")
}

func leaseResult(result sql.Result, err error, operation string) error {
	if err != nil {
		return fmt.Errorf("%s outbox message: %w", operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s outbox message: %w", operation, err)
	}
	if rows != 1 {
		return outbox.ErrLeaseLost
	}
	return nil
}

var _ outbox.Repository = (*Store)(nil)

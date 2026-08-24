package postgres

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"github.com/example/agent-platform/engine/internal/sqsconsumer"
)

var envelopeHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, sqsconsumer.ErrInvalidInput
	}
	return &Store{db: db}, nil
}

func (store *Store) Lookup(ctx context.Context, consumerName, messageID string) (sqsconsumer.Consumption, bool, error) {
	if !validConsumerName(consumerName) || strings.TrimSpace(messageID) == "" || len(messageID) > 8192 {
		return sqsconsumer.Consumption{}, false, sqsconsumer.ErrInvalidInput
	}
	value, err := scanConsumption(store.db.QueryRowContext(ctx, consumptionSelect+` WHERE consumer_name=$1 AND message_id=$2`, consumerName, messageID))
	if errors.Is(err, sql.ErrNoRows) {
		return sqsconsumer.Consumption{}, false, nil
	}
	return value, err == nil, err
}

func (store *Store) Complete(ctx context.Context, value sqsconsumer.Consumption) (bool, error) {
	if !validConsumption(value) {
		return false, sqsconsumer.ErrInvalidInput
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO processed_messages (consumer_name,message_id,topic,dedupe_key,envelope_hash,broker_message_id)
VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (consumer_name,message_id) DO NOTHING`, value.ConsumerName, value.MessageID, value.Topic, value.DedupeKey, value.EnvelopeHash, value.BrokerMessageID)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if inserted == 1 {
		return false, tx.Commit()
	}
	existing, err := scanConsumption(tx.QueryRowContext(ctx, consumptionSelect+` WHERE consumer_name=$1 AND message_id=$2 FOR UPDATE`, value.ConsumerName, value.MessageID))
	if err != nil {
		return false, err
	}
	if !sameIdentity(existing, value) {
		return false, sqsconsumer.ErrLedgerConflict
	}
	return true, tx.Commit()
}

const consumptionSelect = `SELECT consumer_name,message_id,topic,dedupe_key,envelope_hash,broker_message_id,processed_at FROM processed_messages`

type scanner interface{ Scan(...any) error }

func scanConsumption(row scanner) (sqsconsumer.Consumption, error) {
	var value sqsconsumer.Consumption
	err := row.Scan(&value.ConsumerName, &value.MessageID, &value.Topic, &value.DedupeKey, &value.EnvelopeHash, &value.BrokerMessageID, &value.ProcessedAt)
	return value, err
}

func validConsumerName(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || index > 0 && (character == '_' || character == '.' || character == '-') {
			continue
		}
		return false
	}
	return true
}

func validConsumption(value sqsconsumer.Consumption) bool {
	return validConsumerName(value.ConsumerName) && strings.TrimSpace(value.MessageID) != "" && len(value.MessageID) <= 8192 && strings.TrimSpace(value.Topic) != "" && len(value.Topic) <= 512 && strings.TrimSpace(value.DedupeKey) != "" && len(value.DedupeKey) <= 8192 && envelopeHashPattern.MatchString(value.EnvelopeHash) && strings.TrimSpace(value.BrokerMessageID) != "" && len(value.BrokerMessageID) <= 512
}

func sameIdentity(left, right sqsconsumer.Consumption) bool {
	return left.ConsumerName == right.ConsumerName && left.MessageID == right.MessageID && left.Topic == right.Topic && left.DedupeKey == right.DedupeKey && left.EnvelopeHash == right.EnvelopeHash
}

var _ sqsconsumer.Ledger = (*Store)(nil)

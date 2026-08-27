package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/credential"
	"github.com/example/agent-platform/engine/internal/persistence"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (s *Store) CurrentProtocolBundles(ctx context.Context) ([]credential.ProtocolBundleRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT version.agent_id,agent.owner_id,version.credential_type,version.ciphertext,version.nonce,version.wrapped_data_key,version.key_nonce,version.encryption_algorithm,version.key_wrap_algorithm,version.key_reference,version.fingerprint
FROM agents agent JOIN agent_credential_versions version ON version.agent_id=agent.agent_id AND version.version_no=agent.current_credential_version
WHERE version.credential_type='protocol_bundle' ORDER BY version.agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []credential.ProtocolBundleRecord{}
	for rows.Next() {
		var value credential.ProtocolBundleRecord
		if err = rows.Scan(&value.AgentID, &value.OwnerID, &value.CredentialType, &value.Envelope.Ciphertext, &value.Envelope.Nonce, &value.Envelope.WrappedDataKey, &value.Envelope.KeyNonce, &value.Envelope.Algorithm, &value.Envelope.KeyWrapAlgorithm, &value.Envelope.KeyReference, &value.Envelope.Fingerprint); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

func (s *Store) Rotate(ctx context.Context, mutation credential.Mutation, agentID string, input credential.StoreInput, envelope credential.Envelope) (result credential.Metadata, replay bool, err error) {
	body, replay, err := s.execute(ctx, mutation, "agent-credentials.rotate:"+mutation.ActorID+":"+agentID, func(tx *sql.Tx) (any, error) {
		var ownerID, status string
		var aggregateVersion int64
		if loadErr := tx.QueryRowContext(ctx, `SELECT owner_id,status,aggregate_version FROM agents WHERE agent_id=$1 FOR UPDATE`, agentID).Scan(&ownerID, &status, &aggregateVersion); errors.Is(loadErr, sql.ErrNoRows) {
			return nil, credential.ErrNotFound
		} else if loadErr != nil {
			return nil, loadErr
		}
		if ownerID != mutation.ActorID {
			return nil, credential.ErrNotFound
		}
		if aggregateVersion != input.ExpectedVersion {
			return nil, credential.ErrStaleVersion
		}
		if status == agent.StatusRetired {
			return nil, credential.ErrInvalidState
		}
		var version int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version_no),0)+1 FROM agent_credential_versions WHERE agent_id=$1`, agentID).Scan(&version); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO agent_credential_versions (agent_id,version_no,credential_type,label,ciphertext,nonce,wrapped_data_key,key_nonce,encryption_algorithm,key_wrap_algorithm,key_reference,fingerprint,created_by,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, agentID, version, input.CredentialType, input.Label, envelope.Ciphertext, envelope.Nonce, envelope.WrappedDataKey, envelope.KeyNonce, envelope.Algorithm, envelope.KeyWrapAlgorithm, envelope.KeyReference, envelope.Fingerprint, mutation.ActorID, mutation.Now); err != nil {
			return nil, fmt.Errorf("store encrypted agent credential: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE agents SET current_credential_version=$1,aggregate_version=aggregate_version+1,updated_at=$2 WHERE agent_id=$3`, version, mutation.Now, agentID); err != nil {
			return nil, err
		}
		result := credential.Metadata{AgentID: agentID, Version: version, AgentAggregateVersion: aggregateVersion + 1, CredentialType: input.CredentialType, Label: input.Label, Fingerprint: envelope.Fingerprint, CreatedAt: mutation.Now}
		payload, marshalErr := json.Marshal(map[string]any{"credentialType": input.CredentialType, "credentialVersion": version, "aggregateVersion": result.AgentAggregateVersion})
		if marshalErr != nil {
			return nil, marshalErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO domain_events (event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at) VALUES ($1,'agent',$2,$3,'agent.credential_rotated',$4,$5)`, mutation.EventID, agentID, result.AgentAggregateVersion, string(payload), mutation.Now); err != nil {
			return nil, fmt.Errorf("record credential domain event: %w", err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,'agent.credential_rotated','agent',$3,$4,$5)`, mutation.EventID+"_audit", mutation.ActorID, agentID, string(payload), mutation.Now); err != nil {
			return nil, fmt.Errorf("record credential audit event: %w", err)
		}
		return result, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

type work func(*sql.Tx) (any, error)

func (s *Store) execute(ctx context.Context, mutation credential.Mutation, scope string, fn work) (json.RawMessage, bool, error) {
	if mutation.IdempotencyKey == "" || mutation.RequestHash == "" {
		return nil, false, credential.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope+":"+mutation.IdempotencyKey); err != nil {
		return nil, false, err
	}
	var previousHash string
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_body FROM idempotency_records WHERE scope=$1 AND idempotency_key=$2`, scope, mutation.IdempotencyKey).Scan(&previousHash, &previous)
	if err == nil {
		if previousHash != mutation.RequestHash {
			return nil, false, persistence.ErrIdempotencyConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return previous, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	value, err := fn(tx)
	if err != nil {
		return nil, false, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records (scope,idempotency_key,request_hash,response_body) VALUES ($1,$2,$3,$4)`, scope, mutation.IdempotencyKey, mutation.RequestHash, body); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return body, false, nil
}

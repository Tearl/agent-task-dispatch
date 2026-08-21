package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/execution"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) GetOrCreate(ctx context.Context, spec execution.Spec) (execution.Execution, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.Execution{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockIdentities(ctx, tx, "execution-id:"+spec.LogicalExecutionID, "execution-idempotency:"+spec.IdempotencyKey); err != nil {
		return execution.Execution{}, false, err
	}
	var existingHash string
	existing, err := scanExecutionWithHash(tx.QueryRowContext(ctx, executionSelectWithHash+` WHERE logical_execution_id=$1 OR idempotency_key=$2 ORDER BY logical_execution_id=$1 DESC LIMIT 1`, spec.LogicalExecutionID, spec.IdempotencyKey), &existingHash)
	if err == nil {
		expectedHash, hashErr := specHash(spec)
		if hashErr != nil {
			return execution.Execution{}, false, hashErr
		}
		if existing.Spec.LogicalExecutionID != spec.LogicalExecutionID || existing.Spec.IdempotencyKey != spec.IdempotencyKey || existingHash != expectedHash || !reflect.DeepEqual(existing.Spec, spec) {
			return execution.Execution{}, false, execution.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return execution.Execution{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return execution.Execution{}, false, err
	}
	var databaseNow time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return execution.Execution{}, false, err
	}
	if !spec.Deadline.After(databaseNow) {
		return execution.Execution{}, false, execution.ErrInvalidInput
	}
	body, hash, toolPolicy, overview, formal, err := encodeSpec(spec)
	if err != nil {
		return execution.Execution{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO logical_executions (logical_execution_id,idempotency_key,spec_hash,protocol_version,stage,task_id,task_spec_hash,task_spec_version,agent_id,agent_endpoint,responsibility_code,cost_cap,tool_policy,deadline,overview_binding,formal_binding,spec_body,status,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,(SELECT max(version_no) FROM task_spec_versions WHERE task_id=$6 AND content_hash=$7),$8,$9,$10,$11,$12,$13,$14,$15,$16,'pending',$17,$17)`, spec.LogicalExecutionID, spec.IdempotencyKey, hash, execution.ProtocolVersion, spec.Stage, spec.TaskID, spec.TaskSpecHash, spec.AgentID, spec.AgentEndpoint, spec.ResponsibilityCode, spec.CostCap, toolPolicy, spec.Deadline, overview, formal, body, databaseNow)
	if err != nil {
		return execution.Execution{}, false, fmt.Errorf("insert logical execution: %w", err)
	}
	created := execution.Execution{Spec: spec, Status: execution.ExecutionPending, UsedCost: "0", CreatedAt: databaseNow, UpdatedAt: databaseNow}
	if err = tx.Commit(); err != nil {
		return execution.Execution{}, false, err
	}
	return created, false, nil
}

func (store *Store) Get(ctx context.Context, logicalExecutionID string) (execution.Execution, error) {
	value, err := scanExecution(store.db.QueryRowContext(ctx, executionSelect+` WHERE logical_execution_id=$1`, logicalExecutionID))
	if errors.Is(err, sql.ErrNoRows) {
		return value, execution.ErrNotFound
	}
	return value, err
}

func (store *Store) CurrentAttempt(ctx context.Context, logicalExecutionID string) (execution.Attempt, error) {
	value, err := scanAttempt(store.db.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 ORDER BY attempt_no DESC LIMIT 1`, logicalExecutionID))
	if errors.Is(err, sql.ErrNoRows) {
		return value, execution.ErrNotFound
	}
	return value, err
}

func (store *Store) PrepareAttempt(ctx context.Context, logicalExecutionID string, ttl time.Duration) (execution.Execution, execution.Attempt, bool, error) {
	if ttl <= 0 || ttl > time.Hour {
		return execution.Execution{}, execution.Attempt{}, false, execution.ErrInvalidInput
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	value, databaseNow, err := loadExecution(ctx, tx, logicalExecutionID)
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	if value.Status == execution.ExecutionSucceeded || value.Status == execution.ExecutionCancelled || value.Status == execution.ExecutionCancelRequested || value.Status == execution.ExecutionCostStopped {
		return execution.Execution{}, execution.Attempt{}, false, execution.ErrInvalidState
	}
	current, attemptErr := scanAttempt(tx.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 ORDER BY attempt_no DESC LIMIT 1 FOR UPDATE`, logicalExecutionID))
	if attemptErr == nil {
		if current.Status == execution.AttemptPrepared || current.Status == execution.AttemptActive && current.LeaseExpiresAt.After(databaseNow) {
			if err = tx.Commit(); err != nil {
				return execution.Execution{}, execution.Attempt{}, false, err
			}
			return value, current, true, nil
		}
		if current.Status == execution.AttemptActive && !current.LeaseExpiresAt.After(databaseNow) {
			if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET status='expired',terminal_at=$1,updated_at=$1 WHERE logical_execution_id=$2 AND attempt_no=$3`, databaseNow, logicalExecutionID, current.Number); err != nil {
				return execution.Execution{}, execution.Attempt{}, false, err
			}
		}
	} else if !errors.Is(attemptErr, sql.ErrNoRows) {
		return execution.Execution{}, execution.Attempt{}, false, attemptErr
	}
	number := value.CurrentAttempt + 1
	attemptID := fmt.Sprintf("%s:attempt:%d", logicalExecutionID, number)
	created := execution.Attempt{LogicalExecutionID: logicalExecutionID, Number: number, AttemptID: attemptID, ReservationID: attemptID, Status: execution.AttemptPrepared, CreatedAt: databaseNow, UpdatedAt: databaseNow}
	if _, err = tx.ExecContext(ctx, `INSERT INTO execution_attempts (logical_execution_id,attempt_no,attempt_id,reservation_id,status,created_at,updated_at) VALUES ($1,$2,$3,$3,'prepared',$4,$4)`, logicalExecutionID, number, attemptID, databaseNow); err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET status='pending',current_attempt=$1,updated_at=$2 WHERE logical_execution_id=$3`, number, databaseNow, logicalExecutionID); err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	value.Status = execution.ExecutionPending
	value.CurrentAttempt = number
	value.UpdatedAt = databaseNow
	if err = tx.Commit(); err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	return value, created, false, nil
}

func (store *Store) ActivateAttempt(ctx context.Context, logicalExecutionID string, number int, lease agent.CapacityLease, nonceHash, nonceKeyVersion string) (execution.Execution, execution.Attempt, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, err
	}
	defer func() { _ = tx.Rollback() }()
	value, databaseNow, err := loadExecution(ctx, tx, logicalExecutionID)
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, err
	}
	attempt, err := scanAttempt(tx.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 AND attempt_no=$2 FOR UPDATE`, logicalExecutionID, number))
	if errors.Is(err, sql.ErrNoRows) {
		return execution.Execution{}, execution.Attempt{}, execution.ErrNotFound
	}
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, err
	}
	if value.CurrentAttempt != number || lease.AgentID != value.Spec.AgentID || lease.ReservationID != attempt.ReservationID || lease.FencingToken < 1 || !lease.ExpiresAt.After(databaseNow) || !validDigest(nonceHash) || nonceKeyVersion == "" {
		return execution.Execution{}, execution.Attempt{}, execution.ErrInvalidInput
	}
	if attempt.Status == execution.AttemptActive {
		if attempt.FencingToken != lease.FencingToken || attempt.CallbackNonceHash != nonceHash || attempt.NonceKeyVersion != nonceKeyVersion {
			return execution.Execution{}, execution.Attempt{}, execution.ErrStaleFence
		}
		if err = tx.Commit(); err != nil {
			return execution.Execution{}, execution.Attempt{}, err
		}
		return value, attempt, nil
	}
	if attempt.Status != execution.AttemptPrepared {
		return execution.Execution{}, execution.Attempt{}, execution.ErrInvalidState
	}
	if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET status='active',fencing_token=$1,lease_expires_at=$2,callback_nonce_hash=$3,nonce_key_version=$4,updated_at=$5 WHERE logical_execution_id=$6 AND attempt_no=$7`, lease.FencingToken, lease.ExpiresAt, nonceHash, nonceKeyVersion, databaseNow, logicalExecutionID, number); err != nil {
		return execution.Execution{}, execution.Attempt{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET status='running',updated_at=$1 WHERE logical_execution_id=$2`, databaseNow, logicalExecutionID); err != nil {
		return execution.Execution{}, execution.Attempt{}, err
	}
	attempt.Status = execution.AttemptActive
	attempt.FencingToken = lease.FencingToken
	attempt.LeaseExpiresAt = lease.ExpiresAt
	attempt.CallbackNonceHash = nonceHash
	attempt.NonceKeyVersion = nonceKeyVersion
	attempt.UpdatedAt = databaseNow
	value.Status = execution.ExecutionRunning
	value.UpdatedAt = databaseNow
	if err = tx.Commit(); err != nil {
		return execution.Execution{}, execution.Attempt{}, err
	}
	return value, attempt, nil
}

func (store *Store) RecordDispatch(ctx context.Context, logicalExecutionID string, number int) error {
	result, err := store.db.ExecContext(ctx, `UPDATE execution_attempts SET dispatch_count=dispatch_count+1,updated_at=clock_timestamp() WHERE logical_execution_id=$1 AND attempt_no=$2 AND status='active'`, logicalExecutionID, number)
	if err != nil {
		return err
	}
	return requireOne(result, execution.ErrInvalidState)
}

func (store *Store) FailAttempt(ctx context.Context, logicalExecutionID string, number int, fencingToken int64, reason string) error {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	value, now, err := loadExecution(ctx, tx, logicalExecutionID)
	if err != nil {
		return err
	}
	if value.CurrentAttempt != number {
		return execution.ErrStaleFence
	}
	result, err := tx.ExecContext(ctx, `UPDATE execution_attempts SET status='failed',failure_reason=$1,terminal_at=$2,updated_at=$2 WHERE logical_execution_id=$3 AND attempt_no=$4 AND status='active' AND fencing_token=$5`, reason, now, logicalExecutionID, number, fencingToken)
	if err != nil {
		return err
	}
	if err = requireOne(result, execution.ErrStaleFence); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET status='failed',updated_at=$1 WHERE logical_execution_id=$2`, now, logicalExecutionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) RequestCancel(ctx context.Context, logicalExecutionID string) (execution.Execution, execution.Attempt, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	value, now, err := loadExecution(ctx, tx, logicalExecutionID)
	if err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	var attempt execution.Attempt
	if value.CurrentAttempt > 0 {
		attempt, err = scanAttempt(tx.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 AND attempt_no=$2 FOR UPDATE`, logicalExecutionID, value.CurrentAttempt))
		if err != nil {
			return execution.Execution{}, execution.Attempt{}, false, err
		}
	}
	if value.Status == execution.ExecutionSucceeded || value.Status == execution.ExecutionCostStopped {
		return execution.Execution{}, execution.Attempt{}, false, execution.ErrInvalidState
	}
	if value.Status == execution.ExecutionCancelled {
		if err = tx.Commit(); err != nil {
			return execution.Execution{}, execution.Attempt{}, false, err
		}
		return value, attempt, true, nil
	}
	replay := value.Status == execution.ExecutionCancelRequested
	if !replay {
		if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET status='cancel_requested',updated_at=$1 WHERE logical_execution_id=$2`, now, logicalExecutionID); err != nil {
			return execution.Execution{}, execution.Attempt{}, false, err
		}
		value.Status = execution.ExecutionCancelRequested
		value.UpdatedAt = now
	}
	if err = tx.Commit(); err != nil {
		return execution.Execution{}, execution.Attempt{}, false, err
	}
	return value, attempt, replay, nil
}

func (store *Store) CompleteCancel(ctx context.Context, logicalExecutionID string, number int, fencingToken int64) (execution.Execution, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.Execution{}, err
	}
	defer func() { _ = tx.Rollback() }()
	value, now, err := loadExecution(ctx, tx, logicalExecutionID)
	if err != nil {
		return execution.Execution{}, err
	}
	if value.Status == execution.ExecutionCancelled {
		_ = tx.Commit()
		return value, nil
	}
	if value.Status != execution.ExecutionCancelRequested {
		return execution.Execution{}, execution.ErrInvalidState
	}
	if number > 0 {
		attempt, scanErr := scanAttempt(tx.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 AND attempt_no=$2 FOR UPDATE`, logicalExecutionID, number))
		if scanErr != nil {
			return execution.Execution{}, scanErr
		}
		if value.CurrentAttempt != number || attempt.FencingToken != fencingToken {
			return execution.Execution{}, execution.ErrStaleFence
		}
		if attempt.Status != execution.AttemptCancelled {
			if attempt.Status != execution.AttemptActive && attempt.Status != execution.AttemptPrepared {
				return execution.Execution{}, execution.ErrInvalidState
			}
			if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET status='cancelled',terminal_at=$1,updated_at=$1 WHERE logical_execution_id=$2 AND attempt_no=$3`, now, logicalExecutionID, number); err != nil {
				return execution.Execution{}, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET status='cancelled',cancelled_at=$1,updated_at=$1 WHERE logical_execution_id=$2`, now, logicalExecutionID); err != nil {
		return execution.Execution{}, err
	}
	value.Status = execution.ExecutionCancelled
	value.CancelledAt = &now
	value.UpdatedAt = now
	if err = tx.Commit(); err != nil {
		return execution.Execution{}, err
	}
	return value, nil
}

func (store *Store) RecordUsage(ctx context.Context, logicalExecutionID string, number int, fencingToken int64, usedCost string) (execution.Execution, bool, error) {
	if invalidMoney(usedCost) {
		return execution.Execution{}, false, execution.ErrInvalidInput
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.Execution{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	value, now, err := loadExecution(ctx, tx, logicalExecutionID)
	if err != nil {
		return execution.Execution{}, false, err
	}
	attempt, err := scanAttempt(tx.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 AND attempt_no=$2 FOR UPDATE`, logicalExecutionID, number))
	if err != nil {
		return execution.Execution{}, false, err
	}
	if value.CurrentAttempt != number || attempt.Status != execution.AttemptActive || attempt.FencingToken != fencingToken {
		return execution.Execution{}, false, execution.ErrStaleFence
	}
	if compareNumeric(usedCost, value.UsedCost) < 0 {
		return execution.Execution{}, false, execution.ErrInvalidInput
	}
	shouldStop := compareNumeric(usedCost, value.Spec.CostCap) >= 0
	storedCost := usedCost
	if shouldStop {
		storedCost = value.Spec.CostCap
		if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET status='failed',failure_reason='cost_cap_reached',terminal_at=$1,updated_at=$1 WHERE logical_execution_id=$2 AND attempt_no=$3`, now, logicalExecutionID, number); err != nil {
			return execution.Execution{}, false, err
		}
	}
	status := value.Status
	if shouldStop {
		status = execution.ExecutionCostStopped
	}
	if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET used_cost=$1,status=$2,updated_at=$3 WHERE logical_execution_id=$4`, storedCost, status, now, logicalExecutionID); err != nil {
		return execution.Execution{}, false, err
	}
	value.UsedCost = storedCost
	value.Status = status
	value.UpdatedAt = now
	if err = tx.Commit(); err != nil {
		return execution.Execution{}, false, err
	}
	return value, shouldStop, nil
}

func (store *Store) ApplyCallback(ctx context.Context, verified execution.VerifiedCallback) (execution.CallbackResult, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return execution.CallbackResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "execution-callback:"+verified.NonceHash); err != nil {
		return execution.CallbackResult{}, err
	}
	var previousHash string
	var previousBody []byte
	err = tx.QueryRowContext(ctx, `SELECT payload_hash,result_body FROM execution_callback_events WHERE nonce_hash=$1`, verified.NonceHash).Scan(&previousHash, &previousBody)
	if err == nil {
		if previousHash != verified.PayloadHash {
			return execution.CallbackResult{}, execution.ErrContentConflict
		}
		var previous execution.CallbackResult
		if err = json.Unmarshal(previousBody, &previous); err != nil {
			return execution.CallbackResult{}, err
		}
		previous.Replay = true
		if err = tx.Commit(); err != nil {
			return execution.CallbackResult{}, err
		}
		return previous, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return execution.CallbackResult{}, err
	}
	callback := verified.Callback
	value, receivedAt, err := loadExecution(ctx, tx, callback.LogicalExecutionID)
	if err != nil {
		return execution.CallbackResult{}, err
	}
	attempt, err := scanAttempt(tx.QueryRowContext(ctx, attemptSelect+` WHERE logical_execution_id=$1 AND attempt_id=$2 FOR UPDATE`, callback.LogicalExecutionID, callback.AttemptID))
	if errors.Is(err, sql.ErrNoRows) || callback.AgentID != value.Spec.AgentID {
		return execution.CallbackResult{}, execution.ErrInvalidCallback
	}
	if err != nil {
		return execution.CallbackResult{}, err
	}
	if attempt.CallbackNonceHash != verified.NonceHash {
		return execution.CallbackResult{}, execution.ErrCallbackReplay
	}
	result := execution.CallbackResult{Execution: value, Outcome: execution.CallbackAccepted}
	if attempt.FencingToken != callback.FencingToken || value.CurrentAttempt != attempt.Number || !attempt.LeaseExpiresAt.After(receivedAt) {
		result.Outcome = execution.CallbackStaleFence
	} else if value.Status == execution.ExecutionCancelRequested || value.Status == execution.ExecutionCancelled || value.Status == execution.ExecutionCostStopped || attempt.Status != execution.AttemptActive {
		result.Outcome = execution.CallbackLate
	} else if compareNumeric(callback.UsedCost, value.UsedCost) < 0 {
		return execution.CallbackResult{}, execution.ErrInvalidCallback
	} else {
		value.UsedCost = callback.UsedCost
		value.UpdatedAt = receivedAt
		attempt.UpdatedAt = receivedAt
		attempt.TerminalAt = &receivedAt
		if compareNumeric(callback.UsedCost, value.Spec.CostCap) > 0 {
			value.UsedCost = value.Spec.CostCap
			value.Status = execution.ExecutionCostStopped
			attempt.Status = execution.AttemptFailed
			result.Outcome = execution.CallbackCostStop
			result.ShouldCancel = true
		} else if callback.Status == execution.CallbackSucceeded {
			value.Status = execution.ExecutionSucceeded
			value.ContentHash = callback.ContentHash
			value.DeliverableRef = callback.DeliverableRef
			attempt.Status = execution.AttemptCompleted
		} else {
			value.Status = execution.ExecutionFailed
			attempt.Status = execution.AttemptFailed
		}
		if _, err = tx.ExecContext(ctx, `UPDATE execution_attempts SET status=$1,terminal_at=$2,updated_at=$2 WHERE logical_execution_id=$3 AND attempt_no=$4`, attempt.Status, receivedAt, callback.LogicalExecutionID, attempt.Number); err != nil {
			return execution.CallbackResult{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE logical_executions SET status=$1,used_cost=$2,content_hash=$3,deliverable_ref=$4,updated_at=$5 WHERE logical_execution_id=$6`, value.Status, value.UsedCost, nullString(value.ContentHash), nullString(value.DeliverableRef), receivedAt, callback.LogicalExecutionID); err != nil {
			return execution.CallbackResult{}, err
		}
		result.Execution = value
	}
	resultBody, err := json.Marshal(result)
	if err != nil {
		return execution.CallbackResult{}, err
	}
	eventID := digest("callback:" + verified.NonceHash)
	if _, err = tx.ExecContext(ctx, `INSERT INTO execution_callback_events (callback_event_id,logical_execution_id,attempt_id,agent_id,fencing_token,nonce_hash,payload_hash,callback_status,used_cost,content_hash,deliverable_ref,outcome,result_body,callback_timestamp,received_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, eventID, callback.LogicalExecutionID, callback.AttemptID, callback.AgentID, callback.FencingToken, verified.NonceHash, verified.PayloadHash, callback.Status, callback.UsedCost, nullString(callback.ContentHash), nullString(callback.DeliverableRef), result.Outcome, string(resultBody), callback.Timestamp, receivedAt); err != nil {
		return execution.CallbackResult{}, err
	}
	auditMetadata, err := json.Marshal(map[string]any{
		"attemptId":         callback.AttemptID,
		"fencingToken":      callback.FencingToken,
		"callbackStatus":    callback.Status,
		"usedCost":          callback.UsedCost,
		"payloadHash":       verified.PayloadHash,
		"outcome":           result.Outcome,
		"callbackTimestamp": callback.Timestamp,
	})
	if err != nil {
		return execution.CallbackResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,$3,'logical_execution',$4,$5,$6)`, digest("audit:callback:"+verified.NonceHash), callback.AgentID, "execution.callback."+result.Outcome, callback.LogicalExecutionID, string(auditMetadata), receivedAt); err != nil {
		return execution.CallbackResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return execution.CallbackResult{}, err
	}
	return result, nil
}

func loadExecution(ctx context.Context, tx *sql.Tx, logicalExecutionID string) (execution.Execution, time.Time, error) {
	var databaseNow time.Time
	value, err := scanExecution(tx.QueryRowContext(ctx, `SELECT spec_body,status,current_attempt,used_cost::text,content_hash,deliverable_ref,created_at,updated_at,cancelled_at,clock_timestamp()
		FROM logical_executions WHERE logical_execution_id=$1 FOR UPDATE`, logicalExecutionID), &databaseNow)
	if errors.Is(err, sql.ErrNoRows) {
		return value, databaseNow, execution.ErrNotFound
	}
	return value, databaseNow, err
}

type scanner interface{ Scan(...any) error }

func scanExecution(row scanner, additional ...any) (execution.Execution, error) {
	return scanExecutionWithHash(row, nil, additional...)
}

func scanExecutionWithHash(row scanner, specHash *string, additional ...any) (value execution.Execution, err error) {
	var specBody []byte
	var contentHash, deliverable sql.NullString
	var cancelled sql.NullTime
	destinations := []any{&specBody}
	if specHash != nil {
		destinations = append(destinations, specHash)
	}
	destinations = append(destinations, &value.Status, &value.CurrentAttempt, &value.UsedCost, &contentHash, &deliverable, &value.CreatedAt, &value.UpdatedAt, &cancelled)
	destinations = append(destinations, additional...)
	err = row.Scan(destinations...)
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(specBody, &value.Spec); err != nil {
		return value, err
	}
	if contentHash.Valid {
		value.ContentHash = contentHash.String
	}
	if deliverable.Valid {
		value.DeliverableRef = deliverable.String
	}
	if cancelled.Valid {
		value.CancelledAt = &cancelled.Time
	}
	return value, nil
}

func scanAttempt(row scanner) (value execution.Attempt, err error) {
	var fencing sql.NullInt64
	var lease, terminal sql.NullTime
	var nonceHash, nonceVersion sql.NullString
	err = row.Scan(&value.LogicalExecutionID, &value.Number, &value.AttemptID, &value.ReservationID, &value.Status, &fencing, &lease, &nonceHash, &nonceVersion, &value.DispatchCount, &value.CreatedAt, &value.UpdatedAt, &terminal)
	if fencing.Valid {
		value.FencingToken = fencing.Int64
	}
	if lease.Valid {
		value.LeaseExpiresAt = lease.Time
	}
	if nonceHash.Valid {
		value.CallbackNonceHash = nonceHash.String
	}
	if nonceVersion.Valid {
		value.NonceKeyVersion = nonceVersion.String
	}
	if terminal.Valid {
		value.TerminalAt = &terminal.Time
	}
	return value, err
}

func encodeSpec(spec execution.Spec) (body, hash, toolPolicy string, overview, formal any, err error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	tool, err := json.Marshal(spec.ToolPolicy)
	if err != nil {
		return "", "", "", nil, nil, err
	}
	if spec.Overview != nil {
		encodedOverview, marshalErr := json.Marshal(spec.Overview)
		if marshalErr != nil {
			return "", "", "", nil, nil, marshalErr
		}
		overview = string(encodedOverview)
	}
	if spec.Formal != nil {
		encodedFormal, marshalErr := json.Marshal(spec.Formal)
		if marshalErr != nil {
			return "", "", "", nil, nil, marshalErr
		}
		formal = string(encodedFormal)
	}
	digestValue := sha256.Sum256(encoded)
	return string(encoded), "sha256:" + hex.EncodeToString(digestValue[:]), string(tool), overview, formal, nil
}

func specHash(spec execution.Spec) (string, error) {
	_, hash, _, _, _, err := encodeSpec(spec)
	return hash, err
}

func lockIdentities(ctx context.Context, tx *sql.Tx, identities ...string) error {
	sort.Strings(identities)
	for _, identity := range identities {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, identity); err != nil {
			return err
		}
	}
	return nil
}

func requireOne(result sql.Result, fallback error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fallback
	}
	return nil
}

func compareNumeric(left, right string) int {
	// Canonical non-negative decimal integers compare by length then lexical value.
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func invalidMoney(value string) bool {
	if value == "" || len(value) > 78 || len(value) > 1 && value[0] == '0' {
		return true
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return true
		}
	}
	return false
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || len(value) < len("sha256:") || value[:len("sha256:")] != "sha256:" {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const executionSelect = `SELECT spec_body,status,current_attempt,used_cost::text,content_hash,deliverable_ref,created_at,updated_at,cancelled_at FROM logical_executions`
const executionSelectWithHash = `SELECT spec_body,spec_hash,status,current_attempt,used_cost::text,content_hash,deliverable_ref,created_at,updated_at,cancelled_at FROM logical_executions`
const attemptSelect = `SELECT logical_execution_id,attempt_no,attempt_id,reservation_id,status,fencing_token,lease_expires_at,callback_nonce_hash,nonce_key_version,dispatch_count,created_at,updated_at,terminal_at FROM execution_attempts`

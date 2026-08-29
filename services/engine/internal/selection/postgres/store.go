package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/persistence"
	"github.com/example/agent-platform/engine/internal/selection"
	"github.com/lib/pq"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) Replay(ctx context.Context, publisherID, key, requestHash string) (selection.Reservation, bool, error) {
	value, storedHash, err := loadReservation(store.db.QueryRowContext(ctx, reservationSelect+` WHERE publisher_id=$1 AND idempotency_key=$2
AND EXISTS (SELECT 1 FROM tasks task WHERE task.task_id=selection_reservations.task_id AND task.status='awaiting_selection' AND task.deletion_requested_at IS NULL AND task.deleted_at IS NULL)`, publisherID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Reservation{}, false, nil
	}
	if err != nil {
		return selection.Reservation{}, false, err
	}
	if storedHash != requestHash {
		return selection.Reservation{}, false, persistence.ErrIdempotencyConflict
	}
	return value, true, nil
}

func (store *Store) Eligibility(ctx context.Context, publisherID, taskID, batchID, slotID string) (selection.Eligibility, error) {
	value, err := loadEligibility(store.db.QueryRowContext(ctx, eligibilitySelect, publisherID, taskID, batchID, slotID))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Eligibility{}, selection.ErrInvalidState
	}
	return value, err
}

func (store *Store) Prepare(ctx context.Context, mutation selection.Mutation, draft selection.Reservation) (selection.Reservation, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return selection.Reservation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0)),pg_advisory_xact_lock(hashtextextended($2,0))`, "selection-request:"+mutation.PublisherID+":"+mutation.IdempotencyKey, "selection-task:"+draft.TaskID); err != nil {
		return selection.Reservation{}, false, err
	}
	if existing, storedHash, replayErr := loadReservation(tx.QueryRowContext(ctx, reservationSelect+` WHERE publisher_id=$1 AND idempotency_key=$2 FOR UPDATE`, mutation.PublisherID, mutation.IdempotencyKey)); replayErr == nil {
		if storedHash != mutation.RequestHash {
			return selection.Reservation{}, false, persistence.ErrIdempotencyConflict
		}
		if err = requireSelectableTask(ctx, tx, existing.TaskID); err != nil {
			return selection.Reservation{}, false, err
		}
		if err = tx.Commit(); err != nil {
			return selection.Reservation{}, false, err
		}
		return existing, true, nil
	} else if !errors.Is(replayErr, sql.ErrNoRows) {
		return selection.Reservation{}, false, replayErr
	}
	eligible, err := loadEligibility(tx.QueryRowContext(ctx, eligibilitySelect+` FOR UPDATE OF task,agent,batch,slot,allocation`, mutation.PublisherID, draft.TaskID, draft.BatchID, draft.SlotID))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Reservation{}, false, selection.ErrInvalidState
	}
	if err != nil {
		return selection.Reservation{}, false, err
	}
	if !matches(draft, eligible) {
		return selection.Reservation{}, false, selection.ErrContentConflict
	}
	var databaseNow time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return selection.Reservation{}, false, err
	}
	if draft.Proof.Deadline <= uint64(databaseNow.Unix()) || draft.CapacityExpiresAt.Before(time.Unix(int64(draft.Proof.Deadline), 0)) {
		return selection.Reservation{}, false, selection.ErrInvalidState
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO selection_reservations (
reservation_id,publisher_id,publisher_wallet,idempotency_key,request_hash,selection_version,task_id,batch_id,slot_id,snapshot_id,agent_id,provider_id,chain_id,contract_address,
proof_task_id,assignment_id,agent_controller,payout_address,overview_id,allocation_id,proof_allocation_id,quote_hash,task_spec_hash,match_revision,price_version,
overview_price,formal_gross_price,overview_credit,formal_payable,policy_hash,selection_nonce,proof_deadline,proof_payload_hash,proof_digest,capacity_fencing_token,capacity_expires_at,status,created_at,updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,'reserved',$37,$37)`,
		draft.ID, draft.PublisherID, draft.PublisherWallet, mutation.IdempotencyKey, mutation.RequestHash, selection.Version, draft.TaskID, draft.BatchID, draft.SlotID, draft.SnapshotID, draft.AgentID, draft.ProviderID, draft.ChainID, draft.ContractAddress,
		draft.Proof.TaskID, draft.Proof.AssignmentID, draft.Proof.AgentController, draft.Proof.Payout, draft.Proof.OverviewID, eligible.AllocationID, draft.Proof.AllocationID, draft.Proof.QuoteHash, draft.Proof.TaskSpecHash, draft.Proof.MatchRevision, draft.Proof.PriceVersion,
		draft.Proof.OverviewPrice, draft.Proof.FormalGrossPrice, draft.Proof.OverviewCredit, draft.FormalPayable, draft.Proof.PolicyHash, draft.Proof.Nonce, draft.Proof.Deadline, draft.ProofPayloadHash, draft.ProofDigest, draft.CapacityFencingToken, draft.CapacityExpiresAt, databaseNow)
	if err != nil {
		var databaseError *pq.Error
		if errors.As(err, &databaseError) && databaseError.Code == "23505" {
			return selection.Reservation{}, false, selection.ErrInvalidState
		}
		return selection.Reservation{}, false, err
	}
	created, _, err := loadReservation(tx.QueryRowContext(ctx, reservationSelect+` WHERE reservation_id=$1`, draft.ID))
	if err != nil {
		return selection.Reservation{}, false, err
	}
	if err = insertEvent(ctx, tx, draft.ID, "", "reserved", "", map[string]any{"taskId": draft.TaskID, "agentId": draft.AgentID}, databaseNow); err != nil {
		return selection.Reservation{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return selection.Reservation{}, false, err
	}
	return created, false, nil
}

func (store *Store) Get(ctx context.Context, publisherID, reservationID string) (selection.Reservation, error) {
	value, _, err := loadReservation(store.db.QueryRowContext(ctx, reservationSelect+` WHERE reservation_id=$1 AND publisher_id=$2`, reservationID, publisherID))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Reservation{}, selection.ErrNotFound
	}
	return value, err
}

func (store *Store) RecordSubmitted(ctx context.Context, reservationID, transactionHash string) (selection.Reservation, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return selection.Reservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	value, _, err := loadReservation(tx.QueryRowContext(ctx, reservationSelect+` WHERE reservation_id=$1 FOR UPDATE`, reservationID))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Reservation{}, selection.ErrNotFound
	}
	if err != nil {
		return selection.Reservation{}, err
	}
	if err = requireSelectableTask(ctx, tx, value.TaskID); err != nil {
		return selection.Reservation{}, err
	}
	if value.Status == selection.StatusSubmitted && value.TransactionHash == transactionHash {
		_ = tx.Commit()
		return value, nil
	}
	if value.Status != selection.StatusReserved || value.TransactionHash != "" && value.TransactionHash != transactionHash {
		return selection.Reservation{}, selection.ErrInvalidState
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return selection.Reservation{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE selection_reservations SET status='submitted',transaction_hash=$1,updated_at=$2 WHERE reservation_id=$3`, transactionHash, now, reservationID); err != nil {
		return selection.Reservation{}, err
	}
	if err = insertEvent(ctx, tx, reservationID, "", "submitted", transactionHash, map[string]any{}, now); err != nil {
		return selection.Reservation{}, err
	}
	value.Status, value.TransactionHash, value.UpdatedAt = selection.StatusSubmitted, transactionHash, now
	if err = tx.Commit(); err != nil {
		return selection.Reservation{}, err
	}
	return value, nil
}

func (store *Store) Confirm(ctx context.Context, reservationID string, result selection.ChainResult) (selection.Reservation, selection.Assignment, bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "selection-confirm:"+reservationID); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	value, _, err := loadReservation(tx.QueryRowContext(ctx, reservationSelect+` WHERE reservation_id=$1 FOR UPDATE`, reservationID))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Reservation{}, selection.Assignment{}, false, selection.ErrNotFound
	}
	if err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	if value.Status == selection.StatusConfirmed {
		assignment, loadErr := loadAssignment(tx.QueryRowContext(ctx, assignmentSelect+` WHERE reservation_id=$1`, reservationID))
		if loadErr != nil {
			return selection.Reservation{}, selection.Assignment{}, false, loadErr
		}
		if assignment.TransactionHash != result.TransactionHash {
			return selection.Reservation{}, selection.Assignment{}, false, selection.ErrContentConflict
		}
		_ = tx.Commit()
		return value, assignment, true, nil
	}
	if (value.Status != selection.StatusReserved && value.Status != selection.StatusSubmitted) || value.TransactionHash != "" && value.TransactionHash != result.TransactionHash || value.Proof != result.Proof || value.FormalPayable != result.FormalPayable || result.WorkNonce != 1 {
		return selection.Reservation{}, selection.Assignment{}, false, selection.ErrProofMismatch
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	assignment := selection.Assignment{ID: value.Proof.AssignmentID, TaskID: value.TaskID, ReservationID: value.ID, AgentID: value.AgentID, ProviderID: value.ProviderID, FormalPayable: value.FormalPayable, OverviewCredit: value.Proof.OverviewCredit, WorkNonce: 1, TransactionHash: result.TransactionHash, ConfirmedAt: now}
	if _, err = tx.ExecContext(ctx, `INSERT INTO assignments (assignment_id,task_id,reservation_id,agent_id,provider_id,formal_payable,overview_credit,work_nonce,transaction_hash,confirmed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$9)`, assignment.ID, assignment.TaskID, assignment.ReservationID, assignment.AgentID, assignment.ProviderID, assignment.FormalPayable, assignment.OverviewCredit, assignment.TransactionHash, now); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, mapConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO assignment_states (assignment_id,state,reason_code,occurred_at) VALUES ($1,'confirmed','chain_confirmation',$2)`, assignment.ID, now); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO active_assignments (task_id,assignment_id,activated_at) VALUES ($1,$2,$3)`, assignment.TaskID, assignment.ID, now); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, mapConflict(err)
	}
	resultUpdate, err := tx.ExecContext(ctx, `UPDATE tasks SET status='assigned',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2 AND status='awaiting_selection' AND deletion_requested_at IS NULL AND deleted_at IS NULL`, now, value.TaskID)
	if err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	if count, _ := resultUpdate.RowsAffected(); count != 1 {
		return selection.Reservation{}, selection.Assignment{}, false, selection.ErrInvalidState
	}
	if _, err = tx.ExecContext(ctx, `UPDATE selection_reservations SET status='confirmed',transaction_hash=$1,updated_at=$2 WHERE reservation_id=$3`, result.TransactionHash, now, reservationID); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	if err = insertEvent(ctx, tx, reservationID, assignment.ID, "confirmed", result.TransactionHash, map[string]any{"blockNumber": result.BlockNumber, "logIndex": result.LogIndex, "workNonce": 1}, now); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	value.Status, value.TransactionHash, value.UpdatedAt = selection.StatusConfirmed, result.TransactionHash, now
	if err = tx.Commit(); err != nil {
		return selection.Reservation{}, selection.Assignment{}, false, err
	}
	return value, assignment, false, nil
}

func (store *Store) Fail(ctx context.Context, reservationID, transactionHash, reason string) (selection.Reservation, bool, error) {
	return store.terminate(ctx, reservationID, transactionHash, reason, selection.StatusFailed, false)
}

func (store *Store) Expire(ctx context.Context, reservationID string) (selection.Reservation, bool, error) {
	return store.terminate(ctx, reservationID, "", "reservation_expired", selection.StatusExpired, true)
}

func (store *Store) terminate(ctx context.Context, reservationID, transactionHash, reason, status string, requireExpiry bool) (selection.Reservation, bool, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return selection.Reservation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	value, _, err := loadReservation(tx.QueryRowContext(ctx, reservationSelect+` WHERE reservation_id=$1 FOR UPDATE`, reservationID))
	if errors.Is(err, sql.ErrNoRows) {
		return selection.Reservation{}, false, selection.ErrNotFound
	}
	if err != nil {
		return selection.Reservation{}, false, err
	}
	if value.Status == status {
		_ = tx.Commit()
		return value, false, nil
	}
	if value.Status != selection.StatusReserved && value.Status != selection.StatusSubmitted {
		return selection.Reservation{}, false, selection.ErrInvalidState
	}
	if value.TransactionHash != "" && transactionHash != "" && value.TransactionHash != transactionHash {
		return selection.Reservation{}, false, selection.ErrContentConflict
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return selection.Reservation{}, false, err
	}
	if requireExpiry && now.Before(time.Unix(int64(value.Proof.Deadline), 0)) {
		return selection.Reservation{}, false, selection.ErrInvalidState
	}
	if transactionHash == "" {
		transactionHash = value.TransactionHash
	}
	if _, err = tx.ExecContext(ctx, `UPDATE selection_reservations SET status=$1,transaction_hash=$2,failure_reason_code=$3,updated_at=$4 WHERE reservation_id=$5`, status, nullable(transactionHash), reason, now, reservationID); err != nil {
		return selection.Reservation{}, false, err
	}
	if err = insertEvent(ctx, tx, reservationID, "", status, transactionHash, map[string]any{"reasonCode": reason}, now); err != nil {
		return selection.Reservation{}, false, err
	}
	value.Status, value.TransactionHash, value.FailureReasonCode, value.UpdatedAt = status, transactionHash, reason, now
	if err = tx.Commit(); err != nil {
		return selection.Reservation{}, false, err
	}
	return value, true, nil
}

const eligibilitySelect = `SELECT task.task_id,funding.chain_task_id,funding.chain_id::text,funding.contract_address,task.deadline,batch.snapshot_id,batch.task_spec_hash,batch.match_revision,snapshot.policy_hash,batch.batch_id,slot.slot_id,
slot.agent_id,slot.provider_id,agent.controller_address,agent.payout_address,slot.price_version,slot.quote_hash,slot.allocation_id,
slot.overview_price::text,price.formal_package_gross_price::text
FROM tasks task
JOIN task_funding_intents funding ON funding.task_id=task.task_id AND funding.status='confirmed'
 AND funding.asset_address IS NOT NULL AND funding.platform_task_key IS NOT NULL
 AND funding.task_spec_hash IS NOT NULL AND funding.funding_deadline IS NOT NULL
JOIN overview_batches batch ON batch.batch_id=$3 AND batch.task_id=task.task_id AND batch.status='completed'
JOIN overview_slots slot ON slot.slot_id=$4 AND slot.batch_id=batch.batch_id AND slot.status='valid' AND slot.billing_status='captured'
JOIN match_snapshots snapshot ON snapshot.snapshot_id=batch.snapshot_id AND snapshot.sealed_at IS NOT NULL
JOIN match_snapshot_candidates candidate ON candidate.snapshot_id=snapshot.snapshot_id AND candidate.agent_id=slot.agent_id AND candidate.qualified
JOIN agents agent ON agent.agent_id=slot.agent_id AND agent.owner_id=slot.provider_id AND agent.status='active' AND agent.health='healthy' AND agent.health_valid_until>clock_timestamp() AND agent.current_price_version=slot.price_version
JOIN agent_price_versions price ON price.agent_id=agent.agent_id AND price.version_no=slot.price_version
JOIN fund_allocations allocation ON allocation.allocation_id=slot.allocation_id AND allocation.status='captured' AND allocation.captured_overview=slot.overview_price
WHERE task.publisher_id=$1 AND task.task_id=$2 AND task.status='awaiting_selection' AND task.formal_budget>=price.formal_package_gross_price
AND task.deletion_requested_at IS NULL AND task.deleted_at IS NULL
AND candidate.price_version=slot.price_version AND candidate.formal_price=price.formal_package_gross_price::text
AND NOT EXISTS (SELECT 1 FROM match_snapshots newer WHERE newer.task_id=task.task_id AND newer.sealed_at IS NOT NULL AND newer.match_revision>batch.match_revision)`

func requireSelectableTask(ctx context.Context, tx *sql.Tx, taskID string) error {
	var selectedTaskID string
	if err := tx.QueryRowContext(ctx, `SELECT task_id FROM tasks WHERE task_id=$1 AND status='awaiting_selection' AND deletion_requested_at IS NULL AND deleted_at IS NULL FOR UPDATE`, taskID).Scan(&selectedTaskID); errors.Is(err, sql.ErrNoRows) {
		return selection.ErrInvalidState
	} else if err != nil {
		return err
	}
	return nil
}

func loadEligibility(row *sql.Row) (value selection.Eligibility, err error) {
	err = row.Scan(&value.TaskID, &value.ChainTaskID, &value.ChainID, &value.ContractAddress, &value.TaskDeadline, &value.SnapshotID, &value.TaskSpecHash, &value.MatchRevision, &value.PolicyHash, &value.BatchID, &value.SlotID, &value.AgentID, &value.ProviderID, &value.AgentController, &value.Payout, &value.PriceVersion, &value.QuoteHash, &value.AllocationID, &value.OverviewPrice, &value.FormalGrossPrice)
	return value, err
}

const reservationSelect = `SELECT reservation_id,publisher_id,publisher_wallet,request_hash,task_id,batch_id,slot_id,snapshot_id,agent_id,provider_id,chain_id::text,contract_address,
proof_task_id,assignment_id,agent_controller,payout_address,overview_id,proof_allocation_id,quote_hash,task_spec_hash,match_revision,price_version,overview_price::text,formal_gross_price::text,overview_credit::text,policy_hash,selection_nonce,proof_deadline,
proof_payload_hash,proof_digest,formal_payable::text,capacity_fencing_token,capacity_expires_at,status,transaction_hash,failure_reason_code,created_at,updated_at FROM selection_reservations`

type rowScanner interface{ Scan(...any) error }

func loadReservation(row rowScanner) (value selection.Reservation, requestHash string, err error) {
	var transactionHash, failureReason sql.NullString
	err = row.Scan(&value.ID, &value.PublisherID, &value.PublisherWallet, &requestHash, &value.TaskID, &value.BatchID, &value.SlotID, &value.SnapshotID, &value.AgentID, &value.ProviderID, &value.ChainID, &value.ContractAddress,
		&value.Proof.TaskID, &value.Proof.AssignmentID, &value.Proof.AgentController, &value.Proof.Payout, &value.Proof.OverviewID, &value.Proof.AllocationID, &value.Proof.QuoteHash, &value.Proof.TaskSpecHash, &value.Proof.MatchRevision, &value.Proof.PriceVersion, &value.Proof.OverviewPrice, &value.Proof.FormalGrossPrice, &value.Proof.OverviewCredit, &value.Proof.PolicyHash, &value.Proof.Nonce, &value.Proof.Deadline,
		&value.ProofPayloadHash, &value.ProofDigest, &value.FormalPayable, &value.CapacityFencingToken, &value.CapacityExpiresAt, &value.Status, &transactionHash, &failureReason, &value.CreatedAt, &value.UpdatedAt)
	value.TransactionHash, value.FailureReasonCode = transactionHash.String, failureReason.String
	return value, requestHash, err
}

const assignmentSelect = `SELECT assignment_id,task_id,reservation_id,agent_id,provider_id,formal_payable::text,overview_credit::text,work_nonce,transaction_hash,confirmed_at FROM assignments`

func loadAssignment(row rowScanner) (value selection.Assignment, err error) {
	err = row.Scan(&value.ID, &value.TaskID, &value.ReservationID, &value.AgentID, &value.ProviderID, &value.FormalPayable, &value.OverviewCredit, &value.WorkNonce, &value.TransactionHash, &value.ConfirmedAt)
	return value, err
}

func matches(value selection.Reservation, eligible selection.Eligibility) bool {
	return value.TaskID == eligible.TaskID && value.Proof.TaskID == eligible.ChainTaskID && value.BatchID == eligible.BatchID && value.SlotID == eligible.SlotID && value.SnapshotID == eligible.SnapshotID && value.AgentID == eligible.AgentID && value.ProviderID == eligible.ProviderID && value.Proof.AgentController == eligible.AgentController && value.Proof.Payout == eligible.Payout && value.Proof.MatchRevision == eligible.MatchRevision && value.Proof.PriceVersion == eligible.PriceVersion && value.Proof.OverviewPrice == eligible.OverviewPrice && value.Proof.FormalGrossPrice == eligible.FormalGrossPrice && value.Proof.OverviewCredit == eligible.OverviewPrice && value.Proof.QuoteHash == toBytes32(eligible.QuoteHash) && value.Proof.TaskSpecHash == toBytes32(eligible.TaskSpecHash) && value.Proof.PolicyHash == toBytes32(eligible.PolicyHash) && value.Proof.AllocationID == toBytes32(eligible.AllocationID) && value.Proof.OverviewID == toBytes32(eligible.SlotID)
}

func toBytes32(value string) string {
	if len(value) == 71 && value[:7] == "sha256:" {
		return "0x" + value[7:]
	}
	return ""
}

func insertEvent(ctx context.Context, tx *sql.Tx, reservationID, assignmentID, eventType, transactionHash string, payload any, now time.Time) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID := eventDigest(reservationID, eventType)
	_, err = tx.ExecContext(ctx, `INSERT INTO selection_events (event_id,reservation_id,assignment_id,event_type,transaction_hash,payload,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (reservation_id,event_type) DO NOTHING`, eventID, reservationID, nullable(assignmentID), eventType, nullable(transactionHash), body, now)
	return err
}

func eventDigest(parts ...string) string {
	value := "selection-event"
	for _, part := range parts {
		value += fmt.Sprintf("%d:%s", len(part), part)
	}
	return fmt.Sprintf("sha256:%x", sha256Sum([]byte(value)))
}

func sha256Sum(value []byte) [32]byte {
	return sha256.Sum256(value)
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mapConflict(err error) error {
	var databaseError *pq.Error
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		return selection.ErrInvalidState
	}
	return err
}

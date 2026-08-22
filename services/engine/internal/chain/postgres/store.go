package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	"github.com/example/agent-platform/engine/internal/selection"
)

type Store struct{ db *sql.DB }

var _ chainprojection.Repository = (*Store)(nil)

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) Cursor(ctx context.Context, scope chainprojection.Scope) (chainprojection.Cursor, error) {
	var value chainprojection.Cursor
	err := store.db.QueryRowContext(ctx, `SELECT block_number,block_hash FROM chain_projection_cursors WHERE chain_id=$1 AND contract_address=$2`, scope.ChainID, scope.Contract).Scan(&value.Height, &value.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return value, nil
	}
	value.Set = err == nil
	return value, err
}

func (store *Store) CanonicalHash(ctx context.Context, scope chainprojection.Scope, height uint64) (string, bool, error) {
	var hash string
	err := store.db.QueryRowContext(ctx, `SELECT block_hash FROM chain_canonical_blocks WHERE chain_id=$1 AND contract_address=$2 AND block_number=$3`, scope.ChainID, scope.Contract, height).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return hash, err == nil, err
}

func (store *Store) ApplyBlock(ctx context.Context, scope chainprojection.Scope, block chainprojection.Block, events []chainprojection.Event) error {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chain-projector:"+scope.ChainID+":"+scope.Contract); err != nil {
		return err
	}
	var cursorHeight uint64
	var cursorHash string
	cursorErr := tx.QueryRowContext(ctx, `SELECT block_number,block_hash FROM chain_projection_cursors WHERE chain_id=$1 AND contract_address=$2 FOR UPDATE`, scope.ChainID, scope.Contract).Scan(&cursorHeight, &cursorHash)
	if cursorErr == nil && (block.Number != cursorHeight+1 || block.ParentHash != cursorHash) {
		return chainprojection.ErrGap
	}
	if cursorErr != nil && !errors.Is(cursorErr, sql.ErrNoRows) {
		return cursorErr
	}
	if errors.Is(cursorErr, sql.ErrNoRows) && block.Number != scope.StartBlock {
		return chainprojection.ErrGap
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chain_blocks (chain_id,contract_address,block_hash,block_number,parent_hash,block_timestamp,observed_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, scope.ChainID, scope.Contract, block.Hash, block.Number, block.ParentHash, block.Timestamp, now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chain_canonical_blocks (chain_id,contract_address,block_number,block_hash) VALUES ($1,$2,$3,$4)`, scope.ChainID, scope.Contract, block.Number, block.Hash); err != nil {
		return mapConflict(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chain_block_states (chain_id,contract_address,block_hash,state,reason_code,observed_at) VALUES ($1,$2,$3,'canonical','confirmation_depth_reached',$4)`, scope.ChainID, scope.Contract, block.Hash, now); err != nil {
		return err
	}
	for _, transaction := range block.Transactions {
		inputHash := sha256.Sum256([]byte(strings.ToLower(transaction.Input)))
		selectionCall := chainprojection.IsSelectionInput(transaction.Input)
		if _, err = tx.ExecContext(ctx, `INSERT INTO chain_transactions (chain_id,contract_address,block_hash,transaction_hash,transaction_status,input_hash,selection_call,observed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, scope.ChainID, scope.Contract, block.Hash, transaction.Hash, transaction.Status, "sha256:"+hex.EncodeToString(inputHash[:]), selectionCall, now); err != nil {
			return err
		}
	}
	for _, event := range events {
		payload, marshalErr := json.Marshal(event.Payload)
		if marshalErr != nil {
			return marshalErr
		}
		var proof any
		var payable any
		var workNonce any
		if event.Selection != nil {
			proof, marshalErr = json.Marshal(event.Selection.Proof)
			if marshalErr != nil {
				return marshalErr
			}
			payable, workNonce = event.Selection.FormalPayable, event.Selection.WorkNonce
		} else if event.Type == chainprojection.EventWorkNonce {
			workNonce, err = projectedWorkNonce(event)
			if err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO chain_events (event_id,chain_id,contract_address,block_hash,block_number,transaction_hash,log_index,event_type,task_chain_id,assignment_chain_id,payload,selection_proof,formal_payable,work_nonce,observed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT DO NOTHING`, event.ID, scope.ChainID, scope.Contract, block.Hash, block.Number, event.TransactionHash, event.LogIndex, event.Type, nullable(event.TaskID), nullable(event.AssignmentID), payload, proof, payable, workNonce, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO chain_event_states (event_id,state,reason_code,observed_at) VALUES ($1,'canonical','confirmation_depth_reached',$2)`, event.ID, now); err != nil {
			return err
		}
		if err = projectSettlementEvent(ctx, tx, scope, event, now); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO chain_projection_cursors (chain_id,contract_address,block_number,block_hash,projection_version,updated_at) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (chain_id,contract_address) DO UPDATE SET block_number=EXCLUDED.block_number,block_hash=EXCLUDED.block_hash,projection_version=EXCLUDED.projection_version,updated_at=EXCLUDED.updated_at`, scope.ChainID, scope.Contract, block.Number, block.Hash, chainprojection.ProjectionVersion, now); err != nil {
		return err
	}
	return tx.Commit()
}

func projectedWorkNonce(event chainprojection.Event) (any, error) {
	value, ok := event.Payload["workNonce"].(uint64)
	if !ok || value < 2 {
		return nil, errors.New("work nonce event is missing a valid workNonce")
	}
	return value, nil
}

func (store *Store) Rewind(ctx context.Context, scope chainprojection.Scope, ancestor uint64, reason string) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chain-projector:"+scope.ChainID+":"+scope.Contract); err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT block_hash FROM chain_canonical_blocks WHERE chain_id=$1 AND contract_address=$2 AND block_number>$3 ORDER BY block_number DESC FOR UPDATE`, scope.ChainID, scope.Contract, ancestor)
	if err != nil {
		return err
	}
	var hashes []string
	for rows.Next() {
		var hash string
		if err = rows.Scan(&hash); err != nil {
			_ = rows.Close()
			return err
		}
		hashes = append(hashes, hash)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, hash := range hashes {
		if err = reverseSettlementBlock(ctx, tx, scope, hash, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO chain_block_states (chain_id,contract_address,block_hash,state,reason_code,observed_at) VALUES ($1,$2,$3,'orphaned',$4,$5)`, scope.ChainID, scope.Contract, hash, reason, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO chain_event_states (event_id,state,reason_code,observed_at) SELECT event_id,'orphaned',$4,$5 FROM chain_events WHERE chain_id=$1 AND contract_address=$2 AND block_hash=$3`, scope.ChainID, scope.Contract, hash, reason, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tasks task SET status='chain_reorg_pending',aggregate_version=aggregate_version+1,updated_at=$4
FROM assignments assignment JOIN active_assignments active ON active.assignment_id=assignment.assignment_id
JOIN chain_events event ON event.transaction_hash=assignment.transaction_hash AND event.event_type='selection_confirmed'
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3 AND task.task_id=assignment.task_id AND task.status<>'chain_reorg_pending'`, scope.ChainID, scope.Contract, hash, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO assignment_states (assignment_id,state,reason_code,occurred_at)
SELECT assignment.assignment_id,'orphaned','chain_reorganization',$4 FROM assignments assignment
JOIN active_assignments active ON active.assignment_id=assignment.assignment_id
JOIN chain_events event ON event.transaction_hash=assignment.transaction_hash AND event.event_type='selection_confirmed'
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3`, scope.ChainID, scope.Contract, hash, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE selection_reservations reservation SET status='orphaned',failure_reason_code='chain_reorganization',updated_at=$4
FROM assignments assignment JOIN active_assignments active ON active.assignment_id=assignment.assignment_id
JOIN chain_events event ON event.transaction_hash=assignment.transaction_hash AND event.event_type='selection_confirmed'
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3 AND reservation.reservation_id=assignment.reservation_id AND reservation.status='confirmed'`, scope.ChainID, scope.Contract, hash, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM active_assignments active USING assignments assignment,chain_events event
WHERE active.assignment_id=assignment.assignment_id AND event.transaction_hash=assignment.transaction_hash AND event.event_type='selection_confirmed'
AND event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3`, scope.ChainID, scope.Contract, hash); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM chain_canonical_blocks WHERE chain_id=$1 AND contract_address=$2 AND block_number>$3`, scope.ChainID, scope.Contract, ancestor); err != nil {
		return err
	}
	var ancestorHash string
	err = tx.QueryRowContext(ctx, `SELECT block_hash FROM chain_canonical_blocks WHERE chain_id=$1 AND contract_address=$2 AND block_number=$3`, scope.ChainID, scope.Contract, ancestor).Scan(&ancestorHash)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `DELETE FROM chain_projection_cursors WHERE chain_id=$1 AND contract_address=$2`, scope.ChainID, scope.Contract); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if _, err = tx.ExecContext(ctx, `UPDATE chain_projection_cursors SET block_number=$3,block_hash=$4,updated_at=$5 WHERE chain_id=$1 AND contract_address=$2`, scope.ChainID, scope.Contract, ancestor, ancestorHash, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *Store) SelectionResult(ctx context.Context, scope chainprojection.Scope, transactionHash string) (selection.ChainResult, bool, error) {
	var result selection.ChainResult
	var proofJSON []byte
	err := store.db.QueryRowContext(ctx, `SELECT event.block_number,event.log_index,event.selection_proof,event.formal_payable::text,event.work_nonce
FROM chain_events event JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.transaction_hash=$3 AND event.event_type='selection_confirmed'`, scope.ChainID, scope.Contract, transactionHash).Scan(&result.BlockNumber, &result.LogIndex, &proofJSON, &result.FormalPayable, &result.WorkNonce)
	if err == nil {
		if err = json.Unmarshal(proofJSON, &result.Proof); err != nil {
			return result, false, err
		}
		result.Status, result.TransactionHash = selection.ChainConfirmed, transactionHash
		return result, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, false, err
	}
	var status string
	err = store.db.QueryRowContext(ctx, `SELECT transaction.transaction_status FROM chain_transactions transaction JOIN chain_canonical_blocks canonical ON canonical.chain_id=transaction.chain_id AND canonical.contract_address=transaction.contract_address AND canonical.block_hash=transaction.block_hash WHERE transaction.chain_id=$1 AND transaction.contract_address=$2 AND transaction.transaction_hash=$3 AND transaction.selection_call=true`, scope.ChainID, scope.Contract, transactionHash).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if status == chainprojection.TxFailed {
		return selection.ChainResult{Status: selection.ChainFailed, TransactionHash: transactionHash, FailureReasonCode: "chain_transaction_failed"}, true, nil
	}
	return result, false, nil
}

func (store *Store) ExpectedInventory(ctx context.Context, scope chainprojection.Scope) (map[string]string, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT reservation.proof_task_id,assignment.assignment_id,assignment.formal_payable::text,assignment.work_nonce FROM assignments assignment JOIN active_assignments active ON active.assignment_id=assignment.assignment_id JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id WHERE reservation.chain_id=$1 AND reservation.contract_address=$2`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var taskID, assignmentID, amount, nonce string
		if err = rows.Scan(&taskID, &assignmentID, &amount, &nonce); err != nil {
			return nil, err
		}
		result["assignment:"+taskID] = assignmentID
		result["task_amount:"+taskID] = amount
		result["task_status:"+taskID] = "2"
		result["work_nonce:"+taskID] = nonce
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	chainRows, err := store.db.QueryContext(ctx, `SELECT event.task_chain_id,event.assignment_chain_id,event.formal_payable::text,event.work_nonce::text
FROM chain_events event JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.event_type='selection_confirmed'`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer chainRows.Close()
	for chainRows.Next() {
		var taskID, assignmentID, amount, nonce string
		if err = chainRows.Scan(&taskID, &assignmentID, &amount, &nonce); err != nil {
			return nil, err
		}
		if _, exists := result["assignment:"+taskID]; !exists {
			result["assignment:"+taskID] = ""
			result["task_amount:"+taskID] = ""
			result["task_status:"+taskID] = ""
			result["work_nonce:"+taskID] = ""
		}
	}
	if err = chainRows.Err(); err != nil {
		return nil, err
	}
	ledgerRows, err := store.db.QueryContext(ctx, `SELECT reservation.proof_task_id,account.balance::text
FROM assignments assignment JOIN active_assignments active ON active.assignment_id=assignment.assignment_id
JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id
JOIN fund_accounts account ON account.task_id=assignment.task_id AND account.account_type='formal_escrow'
WHERE reservation.chain_id=$1 AND reservation.contract_address=$2`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer ledgerRows.Close()
	for ledgerRows.Next() {
		var taskID, balance string
		if err = ledgerRows.Scan(&taskID, &balance); err != nil {
			return nil, err
		}
		result["ledger_formal_balance:"+taskID] = balance
	}
	if err = ledgerRows.Err(); err != nil {
		return nil, err
	}
	settlementRows, err := store.db.QueryContext(ctx, `SELECT task_chain_id,contract_status,locked_amount FROM chain_task_settlement_positions WHERE chain_id=$1 AND contract_address=$2`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer settlementRows.Close()
	for settlementRows.Next() {
		var taskID, status string
		var amount sql.NullString
		if err = settlementRows.Scan(&taskID, &status, &amount); err != nil {
			return nil, err
		}
		result["task_status:"+taskID] = status
		if amount.Valid {
			result["task_amount:"+taskID] = amount.String
		}
	}
	if err = settlementRows.Err(); err != nil {
		return nil, err
	}
	earningsRows, err := store.db.QueryContext(ctx, `SELECT agent_controller,payout_address,claimable_amount::text FROM chain_agent_earnings_positions WHERE chain_id=$1 AND contract_address=$2`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer earningsRows.Close()
	for earningsRows.Next() {
		var controller, payout, amount string
		if err = earningsRows.Scan(&controller, &payout, &amount); err != nil {
			return nil, err
		}
		result["claimable:"+controller+":"+payout] = amount
	}
	if err = earningsRows.Err(); err != nil {
		return nil, err
	}
	ledgerEarningsRows, err := store.db.QueryContext(ctx, `SELECT DISTINCT account.reference_id,account.balance::text
FROM fund_accounts account JOIN selection_reservations reservation
  ON account.reference_id=reservation.agent_controller || ':' || reservation.payout_address
JOIN active_assignments active ON active.task_id=reservation.task_id
JOIN fund_accounts formal ON formal.task_id=reservation.task_id AND formal.account_type='formal_escrow'
WHERE account.account_type='formal_agent_receivable'
  AND account.asset_key=formal.asset_key
  AND reservation.chain_id=$1 AND reservation.contract_address=$2`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer ledgerEarningsRows.Close()
	for ledgerEarningsRows.Next() {
		var reference, amount string
		if err = ledgerEarningsRows.Scan(&reference, &amount); err != nil {
			return nil, err
		}
		result["ledger_claimable:"+reference] = amount
	}
	if err = ledgerEarningsRows.Err(); err != nil {
		return nil, err
	}
	yieldRows, err := store.db.QueryContext(ctx, `SELECT task_chain_id,eligible_principal::text FROM chain_yield_positions WHERE chain_id=$1 AND contract_address=$2`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer yieldRows.Close()
	for yieldRows.Next() {
		var taskID, amount string
		if err = yieldRows.Scan(&taskID, &amount); err != nil {
			return nil, err
		}
		result["yield_principal:"+taskID] = amount
	}
	if err = yieldRows.Err(); err != nil {
		return nil, err
	}
	orphanRows, err := store.db.QueryContext(ctx, `SELECT proof_task_id FROM selection_reservations WHERE chain_id=$1 AND contract_address=$2 AND status='orphaned'`, scope.ChainID, scope.Contract)
	if err != nil {
		return nil, err
	}
	defer orphanRows.Close()
	for orphanRows.Next() {
		var taskID string
		if err = orphanRows.Scan(&taskID); err != nil {
			return nil, err
		}
		result["reorg_quarantine:"+taskID] = "chain_reorganization"
	}
	return result, orphanRows.Err()
}

func (store *Store) RecordReconciliation(ctx context.Context, run chainprojection.ReconciliationRun) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `INSERT INTO chain_reconciliation_runs (reconciliation_id,chain_id,contract_address,safe_block_number,status,started_at,finished_at) VALUES ($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, run.ID, run.Scope.ChainID, run.Scope.Contract, run.SafeHeight, run.Status, run.StartedAt, run.FinishedAt)
	if err != nil {
		return err
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		return tx.Commit()
	}
	for index, difference := range run.Differences {
		if _, err = tx.ExecContext(ctx, `INSERT INTO chain_reconciliation_differences (reconciliation_id,difference_index,category,resource_id,expected_value,observed_value,severity,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, run.ID, index+1, difference.Category, difference.ResourceID, difference.ExpectedValue, difference.ObservedValue, difference.Severity, run.FinishedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func mapConflict(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "duplicate key") {
		return chainprojection.ErrGap
	}
	return err
}

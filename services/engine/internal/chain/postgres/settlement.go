package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	"github.com/example/agent-platform/engine/internal/selection"
)

type settlementEntry struct {
	accountID, accountType, direction, amount, asset string
}

func projectSettlementEvent(ctx context.Context, tx *sql.Tx, scope chainprojection.Scope, event chainprojection.Event, now time.Time) error {
	switch event.Type {
	case chainprojection.EventEarnings:
		controller, _ := event.Payload["agentController"].(string)
		payout, _ := event.Payload["payout"].(string)
		amount, _ := event.Payload["amount"].(string)
		var taskID, formalID, asset string
		err := tx.QueryRowContext(ctx, `SELECT reservation.task_id,account.account_id,account.asset_key
FROM selection_reservations reservation
JOIN active_assignments active ON active.task_id=reservation.task_id
JOIN assignments assignment ON assignment.assignment_id=active.assignment_id AND assignment.reservation_id=reservation.reservation_id
JOIN fund_accounts account ON account.task_id=reservation.task_id AND account.account_type='formal_escrow'
WHERE reservation.chain_id=$1 AND reservation.contract_address=$2 AND reservation.proof_task_id=$3
  AND reservation.agent_controller=$4 AND reservation.payout_address=$5`, scope.ChainID, scope.Contract, event.TaskID, controller, payout).Scan(&taskID, &formalID, &asset)
		if err != nil {
			return err
		}
		if amount == "0" {
			_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='settled',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'settled'`, taskID, now)
			return err
		}
		referenceID := controller + ":" + payout
		receivableID := settlementDigest("fund-system-account", "formal_agent_receivable", referenceID, asset, "double-entry-v1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','formal_agent_receivable',$2,$3,'open',0,$4,$4) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, receivableID, referenceID, asset, now); err != nil {
			return err
		}
		if err = insertSettlementJournal(ctx, tx, event.ID, "settlement_release", taskID, event.ID, "formal_accepted", now, []settlementEntry{{formalID, "formal_escrow", "debit", amount, asset}, {receivableID, "formal_agent_receivable", "credit", amount, asset}}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='settled',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'settled'`, taskID, now)
		return err

	case chainprojection.EventWithdrawal:
		controller, _ := event.Payload["agentController"].(string)
		payout, _ := event.Payload["payout"].(string)
		amount, _ := event.Payload["amount"].(string)
		var receivableID, asset string
		err := tx.QueryRowContext(ctx, `SELECT account.account_id,account.asset_key
FROM selection_reservations reservation
JOIN fund_accounts account ON account.account_type='formal_agent_receivable'
 AND account.reference_id=reservation.agent_controller || ':' || reservation.payout_address
WHERE reservation.chain_id=$1 AND reservation.contract_address=$2
  AND reservation.agent_controller=$3 AND reservation.payout_address=$4
  AND account.balance >= $5
ORDER BY reservation.created_at LIMIT 1 FOR UPDATE`, scope.ChainID, scope.Contract, controller, payout, amount).Scan(&receivableID, &asset)
		if err != nil {
			return err
		}
		if amount == "0" {
			return nil
		}
		controlID := settlementDigest("fund-system-account", "funding_control", asset, "double-entry-v1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','funding_control','funding',$2,'open',0,$3,$3) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, controlID, asset, now); err != nil {
			return err
		}
		return insertSettlementJournal(ctx, tx, event.ID, "earnings_withdrawal", "", event.ID, "agent_withdrawal", now, []settlementEntry{{receivableID, "formal_agent_receivable", "debit", amount, asset}, {controlID, "funding_control", "credit", amount, asset}})

	case chainprojection.EventRefunded:
		amount, _ := event.Payload["amount"].(string)
		taskID, formalID, asset, err := formalAccountForChainTask(ctx, tx, event.TaskID)
		if err != nil {
			return err
		}
		if amount != "0" {
			controlID := settlementDigest("fund-system-account", "funding_control", asset, "double-entry-v1")
			if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','funding_control','funding',$2,'open',0,$3,$3) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, controlID, asset, now); err != nil {
				return err
			}
			if err = insertSettlementJournal(ctx, tx, event.ID, "settlement_refund", taskID, event.ID, "publisher_refund", now, []settlementEntry{{formalID, "formal_escrow", "debit", amount, asset}, {controlID, "funding_control", "credit", amount, asset}}); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='refunded',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'refunded'`, taskID, now)
		return err

	case chainprojection.EventDisputeDone:
		recipient, _ := event.Payload["recipient"].(string)
		amount, _ := event.Payload["amount"].(string)
		var taskID, payout, formalID, asset string
		err := tx.QueryRowContext(ctx, `SELECT reservation.task_id,reservation.payout_address,account.account_id,account.asset_key
FROM selection_reservations reservation
JOIN active_assignments active ON active.task_id=reservation.task_id
JOIN fund_accounts account ON account.task_id=reservation.task_id AND account.account_type='formal_escrow'
WHERE reservation.chain_id=$1 AND reservation.contract_address=$2 AND reservation.proof_task_id=$3`, scope.ChainID, scope.Contract, event.TaskID).Scan(&taskID, &payout, &formalID, &asset)
		if errors.Is(err, sql.ErrNoRows) || recipient == payout {
			return nil
		}
		if err != nil {
			return err
		}
		if amount == "0" {
			_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='refunded',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'refunded'`, taskID, now)
			return err
		}
		controlID := settlementDigest("fund-system-account", "funding_control", asset, "double-entry-v1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','funding_control','funding',$2,'open',0,$3,$3) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, controlID, asset, now); err != nil {
			return err
		}
		if err = insertSettlementJournal(ctx, tx, event.ID, "settlement_refund", taskID, event.ID, "dispute_refund", now, []settlementEntry{{formalID, "formal_escrow", "debit", amount, asset}, {controlID, "funding_control", "credit", amount, asset}}); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='refunded',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'refunded'`, taskID, now)
		return err

	case chainprojection.EventDisputeAlloc:
		publisherAmount, _ := event.Payload["publisherAmount"].(string)
		agentAmount, _ := event.Payload["agentAmount"].(string)
		feeAmount, _ := event.Payload["feeAmount"].(string)
		taskID, formalID, asset, err := formalAccountForChainTask(ctx, tx, event.TaskID)
		if err != nil {
			return err
		}
		entries := []settlementEntry{}
		if publisherAmount != "0" {
			controlID := settlementDigest("fund-system-account", "funding_control", asset, "double-entry-v1")
			if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','funding_control','funding',$2,'open',0,$3,$3) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, controlID, asset, now); err != nil {
				return err
			}
			entries = append(entries, settlementEntry{formalID, "formal_escrow", "debit", publisherAmount, asset}, settlementEntry{controlID, "funding_control", "credit", publisherAmount, asset})
		}
		if feeAmount != "0" {
			feeID := settlementDigest("fund-task-account", "dispute_fee_pool", taskID, asset, "double-entry-v1")
			if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,task_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'task','dispute_fee_pool',$2,$2,$3,'open',0,$4,$4) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, feeID, taskID, asset, now); err != nil {
				return err
			}
			entries = append(entries, settlementEntry{formalID, "formal_escrow", "debit", feeAmount, asset}, settlementEntry{feeID, "dispute_fee_pool", "credit", feeAmount, asset})
		}
		if len(entries) > 0 {
			if err = insertSettlementJournal(ctx, tx, event.ID, "dispute_allocation", taskID, event.ID, "final_dispute_allocation", now, entries); err != nil {
				return err
			}
		}
		status := "settled"
		if publisherAmount != "0" && agentAmount != "0" {
			status = "partially_settled"
		} else if publisherAmount != "0" {
			status = "refunded"
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status=$2,aggregate_version=aggregate_version+1,updated_at=$3 WHERE task_id=$1 AND status<>$2`, taskID, status, now)
		return err
	}
	return nil
}

func insertSettlementJournal(ctx context.Context, tx *sql.Tx, journalID, journalType, taskID, sourceRef, reason string, now time.Time, entries []settlementEntry) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO fund_journals (journal_id,idempotency_key,request_hash,ledger_version,journal_type,task_id,source_ref,reason_code,created_at) VALUES ($1,$2,$1,'double-entry-v1',$3,$4,$5,$6,$7) ON CONFLICT (idempotency_key) DO NOTHING`, journalID, "chain:"+journalID, journalType, nullable(taskID), sourceRef, reason, now)
	if err != nil {
		return err
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		return nil
	}
	for index, entry := range entries {
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_entries (journal_id,entry_index,account_id,account_type,direction,amount,asset_key,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, journalID, index+1, entry.accountID, entry.accountType, entry.direction, entry.amount, entry.asset, now); err != nil {
			return err
		}
	}
	return nil
}

func reverseSettlementBlock(ctx context.Context, tx *sql.Tx, scope chainprojection.Scope, blockHash string, now time.Time) error {
	rows, err := tx.QueryContext(ctx, `SELECT journal.journal_id,journal.task_id,entry.account_id,entry.account_type,entry.direction,entry.amount::text,entry.asset_key
FROM chain_events event JOIN fund_journals journal ON journal.source_ref=event.event_id
JOIN fund_entries entry ON entry.journal_id=journal.journal_id
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3
  AND journal.journal_type IN ('settlement_release','settlement_refund','earnings_withdrawal','change_order_release','change_order_residual','dispute_allocation')
ORDER BY journal.journal_id,entry.entry_index`, scope.ChainID, scope.Contract, blockHash)
	if err != nil {
		return err
	}
	type reversal struct {
		taskID  string
		entries []settlementEntry
	}
	values := make(map[string]*reversal)
	var order []string
	for rows.Next() {
		var journalID, accountID, accountType, direction, amount, asset string
		var taskID sql.NullString
		if err = rows.Scan(&journalID, &taskID, &accountID, &accountType, &direction, &amount, &asset); err != nil {
			_ = rows.Close()
			return err
		}
		value := values[journalID]
		if value == nil {
			value = &reversal{}
			if taskID.Valid {
				value.taskID = taskID.String
			}
			values[journalID], order = value, append(order, journalID)
		}
		inverse := "credit"
		if direction == "credit" {
			inverse = "debit"
		}
		value.entries = append(value.entries, settlementEntry{accountID, accountType, inverse, amount, asset})
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, originalID := range order {
		reversalID := settlementDigest("chain-settlement-reversal", originalID)
		value := values[originalID]
		if err = insertSettlementReversal(ctx, tx, reversalID, originalID, value.taskID, now, value.entries); err != nil {
			return err
		}
		if value.taskID != "" {
			if _, err = tx.ExecContext(ctx, `UPDATE tasks SET status='chain_reorg_pending',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'chain_reorg_pending'`, value.taskID, now); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO formal_acceptance_states (acceptance_intent_id,aggregate_version,state,transaction_hash,chain_event_id,reason_code,occurred_at)
SELECT intent.acceptance_intent_id,latest.aggregate_version+1,'orphaned',latest.transaction_hash,latest.chain_event_id,'chain_reorganization',$4
FROM formal_acceptance_intents intent
JOIN LATERAL (SELECT state.* FROM formal_acceptance_states state WHERE state.acceptance_intent_id=intent.acceptance_intent_id ORDER BY state.aggregate_version DESC LIMIT 1) latest ON true
JOIN chain_events event ON event.event_id=latest.chain_event_id
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3 AND latest.state='confirmed'`, scope.ChainID, scope.Contract, blockHash, now); err != nil {
		return err
	}
	return nil
}

func insertSettlementReversal(ctx context.Context, tx *sql.Tx, journalID, originalID, taskID string, now time.Time, entries []settlementEntry) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO fund_journals (journal_id,idempotency_key,request_hash,ledger_version,journal_type,task_id,reversal_of,source_ref,reason_code,created_at) VALUES ($1,$2,$1,'double-entry-v1','reversal',$3,$4,$4,'chain_reorganization',$5) ON CONFLICT (idempotency_key) DO NOTHING`, journalID, "chain:"+journalID, nullable(taskID), originalID, now)
	if err != nil {
		return err
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		return nil
	}
	for index, entry := range entries {
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_entries (journal_id,entry_index,account_id,account_type,direction,amount,asset_key,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, journalID, index+1, entry.accountID, entry.accountType, entry.direction, entry.amount, entry.asset, now); err != nil {
			return err
		}
	}
	return nil
}

func settlementDigest(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		_, _ = hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func formalAccountForChainTask(ctx context.Context, tx *sql.Tx, chainTaskID string) (string, string, string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT task_id,account_id,asset_key FROM fund_accounts WHERE account_type='formal_escrow'`)
	if err != nil {
		return "", "", "", err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID, accountID, asset string
		if err = rows.Scan(&taskID, &accountID, &asset); err != nil {
			return "", "", "", err
		}
		if selection.TaskChainID(taskID) == chainTaskID {
			return taskID, accountID, asset, nil
		}
	}
	if err = rows.Err(); err != nil {
		return "", "", "", err
	}
	return "", "", "", sql.ErrNoRows
}

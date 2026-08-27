package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	"github.com/example/agent-platform/engine/internal/selection"
)

type settlementEntry struct {
	accountID, accountType, direction, amount, asset string
}

func projectSettlementEvent(ctx context.Context, tx *sql.Tx, scope chainprojection.Scope, event chainprojection.Event, now time.Time) error {
	switch event.Type {
	case chainprojection.EventTaskCreated:
		publisher, _ := event.Payload["publisher"].(string)
		amount, _ := event.Payload["amount"].(string)
		var intentID, taskID, publisherID, publisherWallet, expectedAmount, overviewAmount, formalAmount, externalAmount, submittedHash, status string
		var aggregateVersion int64
		err := tx.QueryRowContext(ctx, `SELECT intent_id,task_id,publisher_id,publisher_wallet,total_amount::text,overview_amount::text,formal_amount::text,external_cost_amount::text,COALESCE(transaction_hash,''),status,aggregate_version
FROM task_funding_intents WHERE chain_id=$1 AND contract_address=$2 AND chain_task_id=$3 FOR UPDATE`, scope.ChainID, scope.Contract, event.TaskID).Scan(&intentID, &taskID, &publisherID, &publisherWallet, &expectedAmount, &overviewAmount, &formalAmount, &externalAmount, &submittedHash, &status, &aggregateVersion)
		if errors.Is(err, sql.ErrNoRows) {
			// Unknown deposits are retained as chain evidence but never attached to
			// an off-chain task or ledger account.
			return nil
		}
		if err != nil {
			return err
		}
		if publisher != publisherWallet || amount != expectedAmount || submittedHash != "" && submittedHash != event.TransactionHash || status != "prepared" && status != "submitted" && status != "confirmed" {
			return nil
		}
		if status == "confirmed" {
			return nil
		}
		asset := "evm:" + scope.ChainID + "/native"
		discoveryID := settlementDigest("fund-account", "discovery_pool", taskID, asset, "double-entry-v1")
		formalID := settlementDigest("fund-account", "formal_escrow", taskID, asset, "double-entry-v1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts(account_id,account_class,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance,created_at,updated_at)
VALUES($1,'business','discovery_pool',$2,$2,$3,$4,$4,'task-funding-v1','open',0,$5,$5) ON CONFLICT(account_type,reference_id,asset_key) DO NOTHING`, discoveryID, taskID, asset, publisherID, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts(account_id,account_class,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance,created_at,updated_at)
VALUES($1,'business','formal_escrow',$2,$2,$3,$4,$4,'task-funding-v1','open',0,$5,$5) ON CONFLICT(account_type,reference_id,asset_key) DO NOTHING`, formalID, taskID, asset, publisherID, now); err != nil {
			return err
		}
		discoveryAmount, ok := addCanonicalAmounts(overviewAmount, externalAmount)
		if !ok {
			return errors.New("task funding amounts are invalid")
		}
		controlID := settlementDigest("fund-system-account", "funding_control", asset, "double-entry-v1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts(account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES($1,'system','funding_control',$2,$2,'open',0,$3,$3) ON CONFLICT(account_type,reference_id,asset_key) DO NOTHING`, controlID, asset, now); err != nil {
			return err
		}
		if discoveryAmount != "0" {
			journalID := settlementDigest("task-funding", event.ID, "discovery")
			if err = insertSettlementJournal(ctx, tx, journalID, "funding", taskID, event.ID, "escrow_funded", now, []settlementEntry{{controlID, "funding_control", "debit", discoveryAmount, asset}, {discoveryID, "discovery_pool", "credit", discoveryAmount, asset}}); err != nil {
				return err
			}
		}
		journalID := settlementDigest("task-funding", event.ID, "formal")
		if err = insertSettlementJournal(ctx, tx, journalID, "funding", taskID, event.ID, "escrow_funded", now, []settlementEntry{{controlID, "funding_control", "debit", formalAmount, asset}, {formalID, "formal_escrow", "credit", formalAmount, asset}}); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE tasks SET status='escrowed',aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2 AND publisher_id=$3 AND status='pending_escrow'`, now, taskID, publisherID)
		if err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return errors.New("task funding state transition failed")
		}
		aggregateVersion++
		if _, err = tx.ExecContext(ctx, `UPDATE task_funding_intents SET status='confirmed',transaction_hash=$1,chain_event_id=$2,aggregate_version=$3,updated_at=$4 WHERE intent_id=$5`, event.TransactionHash, event.ID, aggregateVersion, now, intentID); err != nil {
			return err
		}
		stateID := settlementDigest("task-funding-state", intentID, "confirmed", fmt.Sprintf("%d", aggregateVersion))
		_, err = tx.ExecContext(ctx, `INSERT INTO task_funding_intent_events(event_id,intent_id,aggregate_version,state,transaction_hash,chain_event_id,reason_code,occurred_at) VALUES($1,$2,$3,'confirmed',$4,$5,'confirmation_depth_reached',$6)`, stateID, intentID, aggregateVersion, event.TransactionHash, event.ID, now)
		return err

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
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='refunded',deleted_at=CASE WHEN deletion_requested_at IS NOT NULL THEN $2 ELSE deleted_at END,aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status<>'refunded'`, taskID, now)
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
	rows, err := tx.QueryContext(ctx, `SELECT journal.journal_id,journal.journal_type,journal.task_id,entry.account_id,entry.account_type,entry.direction,entry.amount::text,entry.asset_key
FROM chain_events event JOIN fund_journals journal ON journal.source_ref=event.event_id
JOIN fund_entries entry ON entry.journal_id=journal.journal_id
WHERE event.chain_id=$1 AND event.contract_address=$2 AND event.block_hash=$3
  AND journal.journal_type IN ('funding','settlement_release','settlement_refund','earnings_withdrawal','change_order_release','change_order_residual','dispute_allocation')
ORDER BY journal.journal_id,entry.entry_index`, scope.ChainID, scope.Contract, blockHash)
	if err != nil {
		return err
	}
	type reversal struct {
		taskID  string
		funding bool
		entries []settlementEntry
	}
	values := make(map[string]*reversal)
	var order []string
	for rows.Next() {
		var journalID, journalType, accountID, accountType, direction, amount, asset string
		var taskID sql.NullString
		if err = rows.Scan(&journalID, &journalType, &taskID, &accountID, &accountType, &direction, &amount, &asset); err != nil {
			_ = rows.Close()
			return err
		}
		value := values[journalID]
		if value == nil {
			value = &reversal{}
			if taskID.Valid {
				value.taskID = taskID.String
			}
			value.funding = journalType == "funding"
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
			target := "chain_reorg_pending"
			if value.funding {
				target = "pending_escrow"
			}
			if _, err = tx.ExecContext(ctx, `UPDATE tasks SET status=$2,aggregate_version=aggregate_version+1,updated_at=$3 WHERE task_id=$1 AND status<>$2`, value.taskID, target, now); err != nil {
				return err
			}
			if value.funding {
				var intentID string
				var version int64
				if err = tx.QueryRowContext(ctx, `UPDATE task_funding_intents SET status='orphaned',aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$1 AND status='confirmed' RETURNING intent_id,aggregate_version`, value.taskID, now).Scan(&intentID, &version); err != nil && !errors.Is(err, sql.ErrNoRows) {
					return err
				}
				if err == nil {
					stateID := settlementDigest("task-funding-state", intentID, "orphaned", fmt.Sprintf("%d", version))
					if _, err = tx.ExecContext(ctx, `INSERT INTO task_funding_intent_events(event_id,intent_id,aggregate_version,state,reason_code,occurred_at) VALUES($1,$2,$3,'orphaned','chain_reorganization',$4)`, stateID, intentID, version, now); err != nil {
						return err
					}
				}
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

func addCanonicalAmounts(values ...string) (string, bool) {
	total := new(big.Int)
	for _, value := range values {
		number, ok := new(big.Int).SetString(value, 10)
		if !ok || number.Sign() < 0 || number.String() != value {
			return "", false
		}
		total.Add(total, number)
	}
	return total.String(), true
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

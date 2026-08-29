package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/persistence"
)

func (store *Store) CreateAcceptanceIntent(ctx context.Context, mutation delivery.Mutation, taskID string, input delivery.AcceptanceIntentInput, draft delivery.AcceptanceIntent) (delivery.AcceptanceIntent, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockAcceptance(ctx, tx, mutation, taskID); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if replay, found, replayErr := replayAcceptance(ctx, tx, mutation, "create"); found || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return replay, true, replayErr
	}
	var packageVersion int64
	var allocated int
	var publisher string
	err = tx.QueryRowContext(ctx, `SELECT package.aggregate_version,package.allocated_version,package.publisher_id,reservation.chain_id::text,reservation.contract_address,reservation.publisher_wallet,reservation.proof_task_id
FROM formal_packages package JOIN assignments assignment ON assignment.assignment_id=package.assignment_id
JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id
WHERE package.package_id=$1 AND package.task_id=$2 FOR UPDATE OF package`, input.PackageID, taskID).Scan(&packageVersion, &allocated, &publisher, &draft.ChainID, &draft.ContractAddress, &draft.PublisherWallet, &draft.ChainTaskID)
	if errors.Is(err, sql.ErrNoRows) || publisher != mutation.PublisherID {
		return delivery.AcceptanceIntent{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if packageVersion != input.ExpectedPackageVersion || allocated != input.FormalVersion {
		return delivery.AcceptanceIntent{}, false, delivery.ErrStaleVersion
	}
	if reason, gateErr := store.acceptanceEligibility(ctx, tx, draft); gateErr != nil {
		return delivery.AcceptanceIntent{}, false, gateErr
	} else if reason != "" {
		return delivery.AcceptanceIntent{}, false, delivery.ErrStaleVersion
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	draft.CreatedAt, draft.UpdatedAt = now, now
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_acceptance_intents (acceptance_intent_id,acceptance_version,package_id,task_id,formal_version,content_hash,proof_digest,work_nonce,package_aggregate_version,publisher_id,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, draft.ID, delivery.AcceptanceVersion, draft.PackageID, taskID, draft.FormalVersion, draft.ContentHash, draft.ProofDigest, draft.WorkNonce, draft.PackageAggregateVersion, mutation.PublisherID, now)
	if err != nil {
		return delivery.AcceptanceIntent{}, false, formalConflict(err)
	}
	if err = appendAcceptanceState(ctx, tx, draft, delivery.AcceptanceIntentRecorded, "", "", "", now); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if err = storeAcceptanceRequest(ctx, tx, mutation, "create", taskID, draft, now); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	return draft, false, nil
}

func (store *Store) SubmitAcceptance(ctx context.Context, mutation delivery.Mutation, taskID, intentID string, input delivery.AcceptanceTransitionInput) (delivery.AcceptanceIntent, bool, error) {
	return store.transitionAcceptance(ctx, mutation, taskID, intentID, input, false)
}

func (store *Store) ReconcileAcceptance(ctx context.Context, mutation delivery.Mutation, taskID, intentID string, input delivery.AcceptanceTransitionInput) (delivery.AcceptanceIntent, bool, error) {
	return store.transitionAcceptance(ctx, mutation, taskID, intentID, input, true)
}

func (store *Store) transitionAcceptance(ctx context.Context, mutation delivery.Mutation, taskID, intentID string, input delivery.AcceptanceTransitionInput, reconcile bool) (delivery.AcceptanceIntent, bool, error) {
	operation := "submit"
	if reconcile {
		operation = "reconcile"
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockAcceptance(ctx, tx, mutation, taskID); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if replay, found, replayErr := replayAcceptance(ctx, tx, mutation, operation); found || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return replay, true, replayErr
	}
	value, err := loadAcceptance(tx.QueryRowContext(ctx, acceptanceSelect+` WHERE intent.acceptance_intent_id=$1 AND intent.task_id=$2 AND intent.publisher_id=$3`, intentID, taskID, mutation.PublisherID))
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.AcceptanceIntent{}, false, delivery.ErrNotFound
	}
	if err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if value.AggregateVersion != input.ExpectedVersion {
		return delivery.AcceptanceIntent{}, false, delivery.ErrStaleVersion
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if !reconcile {
		if value.State != delivery.AcceptanceIntentRecorded && value.State != delivery.AcceptanceOrphaned {
			return delivery.AcceptanceIntent{}, false, delivery.ErrInvalidState
		}
		if reason, gateErr := store.acceptanceEligibility(ctx, tx, value); gateErr != nil {
			return delivery.AcceptanceIntent{}, false, gateErr
		} else if reason != "" {
			return delivery.AcceptanceIntent{}, false, delivery.ErrStaleVersion
		}
		value.AggregateVersion++
		value.State, value.TransactionHash, value.UpdatedAt = delivery.AcceptancePending, input.TransactionHash, now
		if err = appendAcceptanceState(ctx, tx, value, value.State, value.TransactionHash, "", "", now); err != nil {
			return delivery.AcceptanceIntent{}, false, formalConflict(err)
		}
	} else {
		if value.State != delivery.AcceptancePending {
			return delivery.AcceptanceIntent{}, false, delivery.ErrInvalidState
		}
		if reason, gateErr := store.acceptanceEligibility(ctx, tx, value); gateErr != nil {
			return delivery.AcceptanceIntent{}, false, gateErr
		} else if reason != "" {
			return delivery.AcceptanceIntent{}, false, delivery.ErrStaleVersion
		}
		var eventID string
		err = tx.QueryRowContext(ctx, `SELECT event.event_id FROM chain_events event
JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash
	JOIN formal_packages package ON package.package_id=$2
JOIN assignments assignment ON assignment.assignment_id=package.assignment_id
JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id
	WHERE event.chain_id=reservation.chain_id::text AND event.contract_address=reservation.contract_address AND event.transaction_hash=$1 AND event.event_type='funds_released'
	  AND event.task_chain_id=reservation.proof_task_id AND event.payload->>'recipient'=reservation.payout_address
	  AND event.payload->>'amount'=reservation.formal_payable::text`, value.TransactionHash, value.PackageID).Scan(&eventID)
		if errors.Is(err, sql.ErrNoRows) {
			return delivery.AcceptanceIntent{}, false, delivery.ErrDependencyPending
		}
		if err != nil {
			return delivery.AcceptanceIntent{}, false, err
		}
		value.AggregateVersion++
		value.State, value.ChainEventID, value.UpdatedAt = delivery.AcceptanceConfirmed, eventID, now
		if err = appendAcceptanceState(ctx, tx, value, value.State, value.TransactionHash, eventID, "", now); err != nil {
			return delivery.AcceptanceIntent{}, false, formalConflict(err)
		}
		if err = settleAcceptanceChangeOrder(ctx, tx, value, eventID, now); err != nil {
			return delivery.AcceptanceIntent{}, false, err
		}
	}
	value.Eligibility = delivery.SettlementEligibility{Eligible: true}
	if err = storeAcceptanceRequest(ctx, tx, mutation, operation, taskID, value, now); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	return value, false, nil
}

func (store *Store) acceptanceEligibility(ctx context.Context, tx queryRower, value delivery.AcceptanceIntent) (string, error) {
	var packageVersion int64
	var allocated int
	var status, contentHash, proofDigest string
	var workNonce uint64
	err := tx.QueryRowContext(ctx, `SELECT package.aggregate_version,package.allocated_version,version.status,version.content_hash,proof.proof_digest,version.work_nonce
FROM formal_packages package JOIN formal_versions version ON version.package_id=package.package_id AND version.version_no=$2
JOIN formal_delivery_proofs proof ON proof.package_id=version.package_id AND proof.version_no=version.version_no
WHERE package.package_id=$1`, value.PackageID, value.FormalVersion).Scan(&packageVersion, &allocated, &status, &contentHash, &proofDigest, &workNonce)
	if errors.Is(err, sql.ErrNoRows) {
		return "proof_missing", nil
	}
	if err != nil {
		return "", err
	}
	var latest sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT max(COALESCE(event.work_nonce,(event.payload->>'workNonce')::bigint))
FROM chain_events event JOIN chain_canonical_blocks canonical ON canonical.chain_id=event.chain_id AND canonical.contract_address=event.contract_address AND canonical.block_hash=event.block_hash
	JOIN formal_packages package ON package.package_id=$1 JOIN assignments assignment ON assignment.assignment_id=package.assignment_id
	JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id
	WHERE event.chain_id=reservation.chain_id::text AND event.contract_address=reservation.contract_address AND event.event_type IN ('selection_confirmed','work_nonce_advanced')
	  AND event.task_chain_id=reservation.proof_task_id AND event.assignment_chain_id=assignment.assignment_id`, value.PackageID).Scan(&latest)
	if err != nil {
		return "", err
	}
	canonicalNonce := uint64(0)
	if latest.Valid {
		canonicalNonce = uint64(latest.Int64)
	}
	changeOrderReady := value.FormalVersion <= delivery.IncludedVersions
	if value.FormalVersion > delivery.IncludedVersions {
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM formal_versions version JOIN formal_change_orders change_order ON change_order.change_order_id=version.change_order_id WHERE version.package_id=$1 AND version.version_no=$2 AND change_order.status='consumed' AND change_order.target_version=version.version_no AND (change_order.responsibility='agent' OR EXISTS (SELECT 1 FROM fund_accounts account WHERE account.account_id=change_order.fund_account_id AND account.state='frozen' AND account.balance>=change_order.authorized_price)))`, value.PackageID, value.FormalVersion).Scan(&changeOrderReady); err != nil {
			return "", err
		}
	}
	gate := delivery.SettlementGate(delivery.SettlementGateSnapshot{IntentPackageAggregate: value.PackageAggregateVersion, CurrentPackageAggregate: packageVersion, IntentFormalVersion: value.FormalVersion, CurrentFormalVersion: allocated, VersionStatus: status, IntentContentHash: value.ContentHash, CurrentContentHash: contentHash, IntentProofDigest: value.ProofDigest, CurrentProofDigest: proofDigest, IntentWorkNonce: value.WorkNonce, VersionWorkNonce: workNonce, CanonicalWorkNonce: canonicalNonce, ChangeOrderReady: changeOrderReady})
	if gate.ReasonCode == "chain_projection_pending" {
		return "", delivery.ErrDependencyPending
	}
	return gate.ReasonCode, nil
}

func settleAcceptanceChangeOrder(ctx context.Context, tx *sql.Tx, value delivery.AcceptanceIntent, eventID string, now time.Time) error {
	var changeOrderID, accountID, amount, balance, asset, controller, payout, residualRecipient string
	err := tx.QueryRowContext(ctx, `SELECT change_order.change_order_id,COALESCE(change_order.fund_account_id,''),change_order.authorized_price::text,COALESCE(account.balance::text,'0'),COALESCE(account.asset_key,''),reservation.agent_controller,reservation.payout_address,change_order.residual_recipient_id
FROM formal_versions version JOIN formal_change_orders change_order ON change_order.change_order_id=version.change_order_id
JOIN formal_packages package ON package.package_id=version.package_id JOIN assignments assignment ON assignment.assignment_id=package.assignment_id
JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id LEFT JOIN fund_accounts account ON account.account_id=change_order.fund_account_id
WHERE version.package_id=$1 AND version.version_no=$2`, value.PackageID, value.FormalVersion).Scan(&changeOrderID, &accountID, &amount, &balance, &asset, &controller, &payout, &residualRecipient)
	if errors.Is(err, sql.ErrNoRows) || amount == "0" {
		return nil
	}
	if err != nil {
		return err
	}
	reference := controller + ":" + payout
	receivableID := digest("fund-system-account\x00formal_agent_receivable\x00" + reference + "\x00" + asset + "\x00double-entry-v1")
	if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','formal_agent_receivable',$2,$3,'open',0,$4,$4) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, receivableID, reference, asset, now); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT account_id FROM fund_accounts WHERE account_type='formal_agent_receivable' AND reference_id=$1 AND asset_key=$2`, reference, asset).Scan(&receivableID); err != nil {
		return err
	}
	journalID := digest("formal-change-order-release:" + eventID + ":" + changeOrderID)
	result, err := tx.ExecContext(ctx, `INSERT INTO fund_journals (journal_id,idempotency_key,request_hash,ledger_version,journal_type,task_id,source_ref,reason_code,created_at) VALUES ($1,$2,$1,'double-entry-v1','change_order_release',$3,$4,'formal_change_order_accepted',$5) ON CONFLICT (idempotency_key) DO NOTHING`, journalID, "acceptance:"+journalID, value.TaskID, eventID, now)
	if err != nil {
		return err
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO fund_entries (journal_id,entry_index,account_id,account_type,direction,amount,asset_key,created_at) VALUES ($1,1,$2,'change_order_escrow','debit',$3,$4,$5),($1,2,$6,'formal_agent_receivable','credit',$3,$4,$5)`, journalID, accountID, amount, asset, now, receivableID); err != nil {
		return err
	}
	balanceValue, amountValue := new(big.Int), new(big.Int)
	if _, ok := balanceValue.SetString(balance, 10); !ok {
		return delivery.ErrInvalidState
	}
	if _, ok := amountValue.SetString(amount, 10); !ok || balanceValue.Cmp(amountValue) < 0 {
		return delivery.ErrInvalidState
	}
	residual := new(big.Int).Sub(balanceValue, amountValue).String()
	var residualJournalID any
	if residual != "0" {
		controlID := digest("fund-system-account\x00funding_control\x00" + asset + "\x00double-entry-v1")
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','funding_control','funding',$2,'open',0,$3,$3) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, controlID, asset, now); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT account_id FROM fund_accounts WHERE account_type='funding_control' AND reference_id='funding' AND asset_key=$1`, asset).Scan(&controlID); err != nil {
			return err
		}
		residualID := digest("formal-change-order-residual:" + eventID + ":" + changeOrderID)
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_journals (journal_id,idempotency_key,request_hash,ledger_version,journal_type,task_id,source_ref,reason_code,created_at) VALUES ($1,$2,$1,'double-entry-v1','change_order_residual',$3,$4,'change_order_residual_return',$5)`, residualID, "acceptance:"+residualID, value.TaskID, eventID, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_entries (journal_id,entry_index,account_id,account_type,direction,amount,asset_key,created_at) VALUES ($1,1,$2,'change_order_escrow','debit',$3,$4,$5),($1,2,$6,'funding_control','credit',$3,$4,$5)`, residualID, accountID, residual, asset, now, controlID); err != nil {
			return err
		}
		residualJournalID = residualID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_change_order_settlements (change_order_id,acceptance_intent_id,chain_event_id,journal_id,residual_journal_id,amount,residual_amount,residual_recipient_id,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, changeOrderID, value.ID, eventID, journalID, residualJournalID, amount, residual, residualRecipient, now)
	return err
}

func lockAcceptance(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, taskID string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0)),pg_advisory_xact_lock(hashtextextended($2,0))`, "formal-acceptance:"+mutation.PublisherID+":"+mutation.IdempotencyKey, "formal-task:"+taskID)
	return err
}

func appendAcceptanceState(ctx context.Context, tx *sql.Tx, value delivery.AcceptanceIntent, state, transactionHash, eventID, reason string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO formal_acceptance_states (acceptance_intent_id,aggregate_version,state,transaction_hash,chain_event_id,reason_code,occurred_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, value.ID, value.AggregateVersion, state, nullableString(transactionHash), nullableString(eventID), nullableString(reason), now)
	return err
}

func replayAcceptance(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, operation string) (delivery.AcceptanceIntent, bool, error) {
	var requestHash, storedOperation string
	var body []byte
	err := tx.QueryRowContext(ctx, `SELECT request_hash,operation,response_body FROM formal_acceptance_requests WHERE publisher_id=$1 AND idempotency_key=$2`, mutation.PublisherID, mutation.IdempotencyKey).Scan(&requestHash, &storedOperation, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return delivery.AcceptanceIntent{}, false, nil
	}
	if err != nil {
		return delivery.AcceptanceIntent{}, false, err
	}
	if requestHash != mutation.RequestHash || storedOperation != operation {
		return delivery.AcceptanceIntent{}, true, persistence.ErrIdempotencyConflict
	}
	var value delivery.AcceptanceIntent
	if err = json.Unmarshal(body, &value); err != nil {
		return value, true, err
	}
	return value, true, nil
}

func storeAcceptanceRequest(ctx context.Context, tx *sql.Tx, mutation delivery.Mutation, operation, taskID string, value delivery.AcceptanceIntent, now time.Time) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO formal_acceptance_requests (publisher_id,idempotency_key,request_hash,operation,task_id,acceptance_intent_id,response_body,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, mutation.PublisherID, mutation.IdempotencyKey, mutation.RequestHash, operation, taskID, value.ID, body, now)
	return err
}

const acceptanceSelect = `SELECT intent.acceptance_intent_id,intent.package_id,intent.task_id,intent.formal_version,intent.content_hash,intent.proof_digest,intent.work_nonce,intent.package_aggregate_version,state.aggregate_version,state.state,COALESCE(state.transaction_hash,''),COALESCE(state.chain_event_id,''),reservation.chain_id::text,reservation.contract_address,reservation.publisher_wallet,reservation.proof_task_id,intent.created_at,state.occurred_at
FROM formal_acceptance_intents intent JOIN LATERAL (SELECT * FROM formal_acceptance_states candidate WHERE candidate.acceptance_intent_id=intent.acceptance_intent_id ORDER BY candidate.aggregate_version DESC LIMIT 1) state ON true
JOIN formal_packages package ON package.package_id=intent.package_id JOIN assignments assignment ON assignment.assignment_id=package.assignment_id JOIN selection_reservations reservation ON reservation.reservation_id=assignment.reservation_id`

func loadAcceptance(row scanner) (delivery.AcceptanceIntent, error) {
	var value delivery.AcceptanceIntent
	err := row.Scan(&value.ID, &value.PackageID, &value.TaskID, &value.FormalVersion, &value.ContentHash, &value.ProofDigest, &value.WorkNonce, &value.PackageAggregateVersion, &value.AggregateVersion, &value.State, &value.TransactionHash, &value.ChainEventID, &value.ChainID, &value.ContractAddress, &value.PublisherWallet, &value.ChainTaskID, &value.CreatedAt, &value.UpdatedAt)
	return value, err
}

func (store *Store) loadAcceptances(ctx context.Context, packageID string) ([]delivery.AcceptanceIntent, error) {
	rows, err := store.db.QueryContext(ctx, acceptanceSelect+` WHERE intent.package_id=$1 ORDER BY intent.created_at`, packageID)
	if err != nil {
		return nil, err
	}
	values := make([]delivery.AcceptanceIntent, 0)
	for rows.Next() {
		value, scanErr := loadAcceptance(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, scanErr
		}
		values = append(values, value)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for index := range values {
		value := &values[index]
		reason, gateErr := store.acceptanceEligibility(ctx, store.db, *value)
		if gateErr == nil {
			value.Eligibility = delivery.SettlementEligibility{Eligible: reason == "", ReasonCode: reason}
		} else if errors.Is(gateErr, delivery.ErrDependencyPending) {
			value.Eligibility = delivery.SettlementEligibility{ReasonCode: "chain_projection_pending"}
		} else {
			return nil, gateErr
		}
	}
	return values, nil
}

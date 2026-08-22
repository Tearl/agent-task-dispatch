package postgres

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"

	"github.com/example/agent-platform/engine/internal/financeview"
)

type Store struct{ db *sql.DB }

var _ financeview.Repository = (*Store)(nil)

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) Publisher(ctx context.Context, userID string) (financeview.PublisherView, error) {
	view := financeview.PublisherView{Totals: financeview.PublisherTotals{Discovery: "0", Formal: "0", ChangeOrders: "0", DisputeFees: "0", Refundable: "0", Refunded: "0"}, Tasks: []financeview.TaskFunds{}, Ledger: []financeview.LedgerRecord{}}
	if err := store.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&view.AsOf); err != nil {
		return view, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT task.task_id,task.title,task.status,task.updated_at,
account.account_type,account.asset_key,account.balance::text,
COALESCE(reservation.status,''),COALESCE(reservation.transaction_hash,'')
FROM tasks task JOIN fund_accounts account ON account.task_id=task.task_id AND account.account_class='business'
LEFT JOIN LATERAL (SELECT status,transaction_hash FROM selection_reservations
 WHERE task_id=task.task_id ORDER BY created_at DESC LIMIT 1) reservation ON true
WHERE task.publisher_id=$1 ORDER BY task.updated_at DESC,task.task_id,account.asset_key,account.account_type`, userID)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	type keyed struct {
		value      financeview.TaskFunds
		refundable *big.Int
	}
	positions := map[string]*keyed{}
	var order []string
	for rows.Next() {
		var taskID, title, lifecycle, accountType, asset, balance, reservationStatus, txHash string
		var updatedAt time.Time
		if err = rows.Scan(&taskID, &title, &lifecycle, &updatedAt, &accountType, &asset, &balance, &reservationStatus, &txHash); err != nil {
			return view, err
		}
		key := taskID + "\x00" + asset
		position := positions[key]
		if position == nil {
			chain := lifecycleChainState(lifecycle, chainState(reservationStatus, txHash))
			position = &keyed{value: financeview.TaskFunds{TaskID: taskID, Title: title, Asset: asset, Lifecycle: lifecycle, Discovery: "0", Formal: "0", ChangeOrders: "0", DisputeFees: "0", Refundable: "0", Terminal: terminal(lifecycle), Chain: chain, TransactionHash: txHash, UpdatedAt: updatedAt}, refundable: new(big.Int)}
			position.value.RefundStatus = refundState(lifecycle)
			positions[key], order = position, append(order, key)
		}
		switch accountType {
		case "discovery_pool":
			position.value.Discovery = balance
			view.Totals.Discovery = add(view.Totals.Discovery, balance)
		case "formal_escrow":
			position.value.Formal = balance
			view.Totals.Formal = add(view.Totals.Formal, balance)
		case "change_order_escrow":
			position.value.ChangeOrders = balance
			view.Totals.ChangeOrders = add(view.Totals.ChangeOrders, balance)
		case "dispute_fee_pool":
			position.value.DisputeFees = balance
			view.Totals.DisputeFees = add(view.Totals.DisputeFees, balance)
		}
		if position.value.RefundStatus == financeview.RefundAvailable {
			position.refundable.Add(position.refundable, money(balance))
		}
	}
	if err = rows.Err(); err != nil {
		return view, err
	}
	for _, key := range order {
		position := positions[key]
		position.value.Refundable = position.refundable.String()
		view.Totals.Refundable = add(view.Totals.Refundable, position.value.Refundable)
		view.Tasks = append(view.Tasks, position.value)
	}
	view.Ledger, err = store.publisherLedger(ctx, userID)
	if err != nil {
		return view, err
	}
	for _, record := range view.Ledger {
		if record.Type == "settlement_refund" {
			view.Totals.Refunded = add(view.Totals.Refunded, record.Amount)
		}
	}
	return view, nil
}

func (store *Store) publisherLedger(ctx context.Context, userID string) ([]financeview.LedgerRecord, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT journal.journal_id,COALESCE(journal.task_id,''),journal.journal_type,
COALESCE(sum(entry.amount) FILTER (WHERE entry.direction='debit'),0)::text,
COALESCE(min(entry.asset_key),''),journal.reason_code,COALESCE(event.transaction_hash,''),journal.created_at
FROM fund_journals journal JOIN tasks task ON task.task_id=journal.task_id
JOIN fund_entries entry ON entry.journal_id=journal.journal_id
LEFT JOIN chain_events event ON event.event_id=journal.source_ref
WHERE task.publisher_id=$1 GROUP BY journal.journal_id,event.transaction_hash
ORDER BY journal.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []financeview.LedgerRecord{}
	for rows.Next() {
		var v financeview.LedgerRecord
		if err = rows.Scan(&v.ID, &v.TaskID, &v.Type, &v.Amount, &v.Asset, &v.ReasonCode, &v.TransactionHash, &v.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (store *Store) Agent(ctx context.Context, userID string) (financeview.AgentView, error) {
	view := financeview.AgentView{Totals: financeview.AgentTotals{OverviewReceivable: "0", FormalClaimable: "0", TotalAvailable: "0"}, Positions: []financeview.EarningPosition{}, Records: []financeview.LedgerRecord{}}
	if err := store.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&view.AsOf); err != nil {
		return view, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT agent.agent_id,agent.name,agent.controller_address,agent.payout_address,
account.asset_key,account.account_type,account.balance::text
FROM agents agent JOIN fund_accounts account ON
 (account.account_type='agent_receivable' AND account.reference_id=agent.agent_id)
 OR (account.account_type='formal_agent_receivable' AND account.reference_id=agent.controller_address || ':' || agent.payout_address)
WHERE agent.owner_id=$1 ORDER BY agent.agent_id,account.asset_key,account.account_type`, userID)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	positions := map[string]*financeview.EarningPosition{}
	var order []string
	for rows.Next() {
		var agentID, name, controller, payout, asset, accountType, balance string
		if err = rows.Scan(&agentID, &name, &controller, &payout, &asset, &accountType, &balance); err != nil {
			return view, err
		}
		key := agentID + "\x00" + asset
		position := positions[key]
		if position == nil {
			position = &financeview.EarningPosition{AgentID: agentID, AgentName: name, Controller: controller, Payout: payout, Asset: asset, OverviewReceivable: "0", FormalClaimable: "0", ChainClaimable: "0", Chain: financeview.Confirmation{Submission: financeview.SubmissionNotSubmitted, Confirmation: financeview.ConfirmationNotObserved}}
			positions[key] = position
			order = append(order, key)
		}
		if accountType == "agent_receivable" {
			position.OverviewReceivable = balance
			view.Totals.OverviewReceivable = add(view.Totals.OverviewReceivable, balance)
		} else {
			position.FormalClaimable = balance
			view.Totals.FormalClaimable = add(view.Totals.FormalClaimable, balance)
		}
	}
	if err = rows.Err(); err != nil {
		return view, err
	}
	chainRows, err := store.db.QueryContext(ctx, `SELECT agent.agent_id,position.agent_controller,position.payout_address,position.claimable_amount::text
FROM agents agent JOIN chain_agent_earnings_positions position
 ON position.agent_controller=agent.controller_address AND position.payout_address=agent.payout_address
WHERE agent.owner_id=$1`, userID)
	if err != nil {
		return view, err
	}
	defer chainRows.Close()
	chainValues := map[string]string{}
	for chainRows.Next() {
		var agentID, controller, payout, amount string
		if err = chainRows.Scan(&agentID, &controller, &payout, &amount); err != nil {
			return view, err
		}
		chainValues[agentID] = add(chainValues[agentID], amount)
	}
	if err = chainRows.Err(); err != nil {
		return view, err
	}
	for _, key := range order {
		position := positions[key]
		position.ChainClaimable = chainValues[position.AgentID]
		if position.ChainClaimable == "" {
			position.ChainClaimable = "0"
		}
		if position.FormalClaimable != "0" || position.ChainClaimable != "0" {
			position.Chain = financeview.Confirmation{Submission: financeview.SubmissionSubmitted, Confirmation: financeview.ConfirmationConfirmed}
		}
		view.Positions = append(view.Positions, *position)
	}
	view.Totals.TotalAvailable = add(view.Totals.OverviewReceivable, view.Totals.FormalClaimable)
	view.Records, err = store.agentLedger(ctx, userID)
	return view, err
}

func (store *Store) agentLedger(ctx context.Context, userID string) ([]financeview.LedgerRecord, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT journal.journal_id,COALESCE(journal.task_id,''),journal.journal_type,
entry.amount::text,entry.asset_key,journal.reason_code,COALESCE(event.transaction_hash,''),journal.created_at
FROM fund_journals journal JOIN fund_entries entry ON entry.journal_id=journal.journal_id
JOIN fund_accounts account ON account.account_id=entry.account_id
JOIN agents agent ON agent.owner_id=$1 AND
 ((account.account_type='agent_receivable' AND account.reference_id=agent.agent_id)
 OR (account.account_type='formal_agent_receivable' AND account.reference_id=agent.controller_address || ':' || agent.payout_address))
LEFT JOIN chain_events event ON event.event_id=journal.source_ref
WHERE (entry.direction='credit' AND journal.journal_type IN ('overview_capture','settlement_release'))
 OR (entry.direction='debit' AND journal.journal_type='earnings_withdrawal')
ORDER BY journal.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []financeview.LedgerRecord{}
	for rows.Next() {
		var v financeview.LedgerRecord
		if err = rows.Scan(&v.ID, &v.TaskID, &v.Type, &v.Amount, &v.Asset, &v.ReasonCode, &v.TransactionHash, &v.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (store *Store) Reconciliation(ctx context.Context) (financeview.ReconciliationView, error) {
	view := financeview.ReconciliationView{Runs: []financeview.ReconciliationRun{}}
	if err := store.db.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&view.AsOf); err != nil {
		return view, err
	}
	rows, err := store.db.QueryContext(ctx, `SELECT reconciliation_id,chain_id::text,contract_address,safe_block_number,status,started_at,finished_at FROM chain_reconciliation_runs ORDER BY started_at DESC LIMIT 20`)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var run financeview.ReconciliationRun
		if err = rows.Scan(&run.ID, &run.ChainID, &run.Contract, &run.SafeBlock, &run.Status, &run.StartedAt, &run.FinishedAt); err != nil {
			return view, err
		}
		diffRows, qerr := store.db.QueryContext(ctx, `SELECT category,resource_id,expected_value,observed_value,severity FROM chain_reconciliation_differences WHERE reconciliation_id=$1 ORDER BY difference_index`, run.ID)
		if qerr != nil {
			return view, qerr
		}
		for diffRows.Next() {
			var diff financeview.ReconciliationDifference
			if qerr = diffRows.Scan(&diff.Category, &diff.ResourceID, &diff.Expected, &diff.Observed, &diff.Severity); qerr != nil {
				_ = diffRows.Close()
				return view, qerr
			}
			run.Differences = append(run.Differences, diff)
		}
		if qerr = diffRows.Close(); qerr != nil {
			return view, qerr
		}
		view.Runs = append(view.Runs, run)
	}
	return view, rows.Err()
}

func chainState(status, tx string) financeview.Confirmation {
	if tx == "" {
		return financeview.Confirmation{Submission: financeview.SubmissionNotSubmitted, Confirmation: financeview.ConfirmationNotObserved}
	}
	state := financeview.ConfirmationPending
	switch status {
	case "confirmed":
		state = financeview.ConfirmationConfirmed
	case "failed", "expired":
		state = financeview.ConfirmationFailed
	case "orphaned":
		state = financeview.ConfirmationOrphaned
	}
	return financeview.Confirmation{Submission: financeview.SubmissionSubmitted, Confirmation: state}
}
func lifecycleChainState(status string, fallback financeview.Confirmation) financeview.Confirmation {
	switch status {
	case "refund_pending", "settlement_pending":
		return financeview.Confirmation{Submission: financeview.SubmissionSubmitted, Confirmation: financeview.ConfirmationPending}
	case "refunded", "settled":
		return financeview.Confirmation{Submission: financeview.SubmissionSubmitted, Confirmation: financeview.ConfirmationConfirmed}
	case "chain_reorg_pending":
		return financeview.Confirmation{Submission: financeview.SubmissionSubmitted, Confirmation: financeview.ConfirmationOrphaned}
	default:
		return fallback
	}
}
func refundState(status string) string {
	switch status {
	case "refund_pending":
		return financeview.RefundPending
	case "refunded":
		return financeview.RefundConfirmed
	case "pending_escrow", "escrowed", "matching", "overview_generating", "awaiting_selection", "cancelled", "failed":
		return financeview.RefundAvailable
	default:
		return financeview.RefundUnavailable
	}
}
func terminal(status string) bool {
	switch status {
	case "settled", "cancelled", "refunded", "failed":
		return true
	}
	return false
}
func money(value string) *big.Int {
	result, ok := new(big.Int).SetString(value, 10)
	if !ok {
		return new(big.Int)
	}
	return result
}
func add(left, right string) string {
	if left == "" {
		left = "0"
	}
	return new(big.Int).Add(money(left), money(right)).String()
}

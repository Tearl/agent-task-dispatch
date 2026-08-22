package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"time"

	"github.com/example/agent-platform/engine/internal/funds"
)

type Store struct{ db *sql.DB }

var _ funds.Repository = (*Store)(nil)

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) OpenAccount(ctx context.Context, draft funds.Account) (funds.Account, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return funds.Account{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx, "fund-account:"+draft.ID, "fund-account-identity:"+draft.Type+":"+draft.ReferenceID+":"+draft.Asset); err != nil {
		return funds.Account{}, false, err
	}
	existing, err := loadAccount(ctx, tx, `account_id=$1 OR (account_type=$2 AND reference_id=$3 AND asset_key=$4) ORDER BY account_id=$1 DESC LIMIT 1`, draft.ID, draft.Type, draft.ReferenceID, draft.Asset)
	if err == nil {
		if !sameAccount(existing, draft) {
			return funds.Account{}, false, funds.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return funds.Account{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return funds.Account{}, false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return funds.Account{}, false, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance,created_at,updated_at) VALUES ($1,'business',$2,$3,$4,$5,$6,$7,$8,'open',0,$9,$9)`, draft.ID, draft.Type, draft.TaskID, draft.ReferenceID, draft.Asset, draft.PrincipalOwnerID, draft.ResidualRecipientID, draft.RefundPolicyVersion, now)
	if err != nil {
		return funds.Account{}, false, fmt.Errorf("insert fund account: %w", err)
	}
	draft.State, draft.Balance, draft.CreatedAt, draft.UpdatedAt = funds.AccountOpen, "0", now, now
	if err = tx.Commit(); err != nil {
		return funds.Account{}, false, err
	}
	return draft, false, nil
}

func (store *Store) GetAccount(ctx context.Context, accountID string) (funds.Account, error) {
	value, err := loadAccount(ctx, store.db, `account_id=$1`, accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return funds.Account{}, funds.ErrNotFound
	}
	return value, err
}

func (store *Store) PostFunding(ctx context.Context, journal funds.Journal, request funds.FundingRequest) (funds.Journal, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return funds.Journal{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx, "fund-journal-key:"+journal.IdempotencyKey, "fund-account:"+request.AccountID); err != nil {
		return funds.Journal{}, false, err
	}
	if existing, replay, replayErr := journalReplay(ctx, tx, journal); replay || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return existing, replay, replayErr
	}
	account, err := loadAccountForUpdate(ctx, tx, request.AccountID)
	if err != nil {
		return funds.Journal{}, false, mapNotFound(err)
	}
	if account.State != funds.AccountOpen || !businessType(account.Type) || account.Asset != journal.Entries[1].Asset {
		return funds.Journal{}, false, funds.ErrInvalidState
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&journal.CreatedAt); err != nil {
		return funds.Journal{}, false, err
	}
	control := journal.Entries[0]
	if _, err = tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system','funding_control',$2,$2,'open',0,$3,$3) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, control.AccountID, account.Asset, journal.CreatedAt); err != nil {
		return funds.Journal{}, false, err
	}
	if err = insertJournal(ctx, tx, journal); err != nil {
		return funds.Journal{}, false, err
	}
	created, err := loadJournal(ctx, tx, journal.ID)
	if err != nil {
		return funds.Journal{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return funds.Journal{}, false, err
	}
	return created, false, nil
}

func (store *Store) AuthorizeOverview(ctx context.Context, draft funds.Allocation) (funds.Allocation, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return funds.Allocation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx, "fund-allocation:"+draft.ID, "fund-allocation-key:"+draft.IdempotencyKey, "fund-discovery:"+draft.TaskID+":"+draft.Asset); err != nil {
		return funds.Allocation{}, false, err
	}
	existing, err := loadAllocationWhere(ctx, tx, `allocation_id=$1 OR idempotency_key=$2 ORDER BY allocation_id=$1 DESC LIMIT 1`, draft.ID, draft.IdempotencyKey)
	if err == nil {
		if existing.ID != draft.ID || existing.RequestHash != draft.RequestHash {
			return funds.Allocation{}, false, funds.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return funds.Allocation{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return funds.Allocation{}, false, err
	}
	var candidateBound bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM match_snapshot_candidates WHERE snapshot_id=$1 AND agent_id=$2 AND qualified=true AND price_version=$3 AND overview_price=$4 AND external_cost_cap=$5)`, draft.SnapshotID, draft.AgentID, draft.PriceVersion, draft.OverviewPrice, draft.CostCap).Scan(&candidateBound); err != nil {
		return funds.Allocation{}, false, err
	}
	if !candidateBound {
		return funds.Allocation{}, false, funds.ErrContentConflict
	}
	account, err := scanAccount(tx.QueryRowContext(ctx, accountSelect+` FROM fund_accounts WHERE account_type='discovery_pool' AND task_id=$1 AND reference_id=$1 AND asset_key=$2 FOR UPDATE`, draft.TaskID, draft.Asset))
	if err != nil {
		return funds.Allocation{}, false, mapNotFound(err)
	}
	if account.State != funds.AccountOpen {
		return funds.Allocation{}, false, funds.ErrInvalidState
	}
	var databaseNow time.Time
	var enough bool
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp(), $2::numeric <= balance-COALESCE((SELECT sum(reserve_amount) FROM fund_allocations WHERE account_id=$1 AND status='authorized'),0) FROM fund_accounts WHERE account_id=$1`, account.ID, draft.ReserveAmount).Scan(&databaseNow, &enough); err != nil {
		return funds.Allocation{}, false, err
	}
	if !draft.Deadline.After(databaseNow) {
		return funds.Allocation{}, false, funds.ErrInvalidInput
	}
	if !enough {
		return funds.Allocation{}, false, funds.ErrInsufficient
	}
	draft.AccountID, draft.CreatedAt, draft.UpdatedAt = account.ID, databaseNow, databaseNow
	_, err = tx.ExecContext(ctx, `INSERT INTO fund_allocations (allocation_id,idempotency_key,request_hash,ledger_version,purpose,account_id,asset_key,task_id,task_spec_hash,snapshot_id,match_revision,agent_id,price_version,quote_hash,overview_price,external_cost_cap,reserve_amount,status,captured_overview,captured_cost,deadline,created_at,updated_at) VALUES ($1,$2,$3,'double-entry-v1','overview',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,'authorized',0,0,$16,$17,$17)`, draft.ID, draft.IdempotencyKey, draft.RequestHash, draft.AccountID, draft.Asset, draft.TaskID, draft.TaskSpecHash, draft.SnapshotID, draft.MatchRevision, draft.AgentID, draft.PriceVersion, draft.QuoteHash, draft.OverviewPrice, draft.CostCap, draft.ReserveAmount, draft.Deadline, databaseNow)
	if err != nil {
		return funds.Allocation{}, false, fmt.Errorf("insert fund allocation: %w", err)
	}
	if err = insertAllocationEvent(ctx, tx, draft.ID, "authorized", draft.RequestHash, map[string]any{"accountId": draft.AccountID, "reserveAmount": draft.ReserveAmount}, databaseNow); err != nil {
		return funds.Allocation{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return funds.Allocation{}, false, err
	}
	return draft, false, nil
}

func (store *Store) CaptureOverview(ctx context.Context, allocationID string, claim funds.OverviewCapture, claimHash string) (funds.Allocation, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return funds.Allocation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx, "fund-allocation:"+allocationID); err != nil {
		return funds.Allocation{}, false, err
	}
	allocation, err := loadAllocationForUpdate(ctx, tx, allocationID)
	if err != nil {
		return funds.Allocation{}, false, mapNotFound(err)
	}
	if allocation.Status == funds.AllocationCaptured {
		if allocation.CaptureClaimHash != claimHash {
			return funds.Allocation{}, false, funds.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return funds.Allocation{}, false, err
		}
		return allocation, true, nil
	}
	if allocation.Status != funds.AllocationAuthorized {
		return funds.Allocation{}, false, funds.ErrInvalidState
	}
	if allocation.TaskID != claim.TaskID || allocation.TaskSpecHash != claim.TaskSpecHash || allocation.MatchRevision != claim.MatchRevision || allocation.AgentID != claim.AgentID || allocation.QuoteHash != claim.QuoteHash || allocation.OverviewPrice != claim.OverviewAmount || compareMoney(claim.UsedCost, allocation.CostCap) > 0 {
		return funds.Allocation{}, false, funds.ErrContentConflict
	}
	if _, err = tx.ExecContext(ctx, `SELECT 1 FROM fund_accounts WHERE account_id=$1 FOR UPDATE`, allocation.AccountID); err != nil {
		return funds.Allocation{}, false, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return funds.Allocation{}, false, err
	}
	total := addMoney(claim.OverviewAmount, claim.UsedCost)
	journalID := ""
	if total != "0" {
		journalID = stableDigest("fund-capture-journal", allocation.ID, claimHash, funds.LedgerVersion)
		entries := []funds.Entry{{Index: 1, AccountID: allocation.AccountID, Direction: funds.EntryDebit, Amount: total, Asset: allocation.Asset}}
		if claim.OverviewAmount != "0" {
			agentID := stableDigest("fund-system-account", funds.AccountAgentReceivable, claim.AgentID, allocation.Asset, funds.LedgerVersion)
			if err = ensureSystemAccount(ctx, tx, agentID, funds.AccountAgentReceivable, claim.AgentID, allocation.Asset, now); err != nil {
				return funds.Allocation{}, false, err
			}
			entries = append(entries, funds.Entry{Index: len(entries) + 1, AccountID: agentID, Direction: funds.EntryCredit, Amount: claim.OverviewAmount, Asset: allocation.Asset})
		}
		if claim.UsedCost != "0" {
			costID := stableDigest("fund-system-account", funds.AccountExternalClearing, allocation.ID, allocation.Asset, funds.LedgerVersion)
			if err = ensureSystemAccount(ctx, tx, costID, funds.AccountExternalClearing, allocation.ID, allocation.Asset, now); err != nil {
				return funds.Allocation{}, false, err
			}
			entries = append(entries, funds.Entry{Index: len(entries) + 1, AccountID: costID, Direction: funds.EntryCredit, Amount: claim.UsedCost, Asset: allocation.Asset})
		}
		journal := funds.Journal{ID: journalID, IdempotencyKey: "capture:" + allocation.ID, Type: "overview_capture", RequestHash: claimHash, TaskID: allocation.TaskID, AllocationID: allocation.ID, SourceRef: claim.LogicalExecutionID, ReasonCode: "overview_valid", Entries: entries, CreatedAt: now}
		if err = insertJournal(ctx, tx, journal); err != nil {
			return funds.Allocation{}, false, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE fund_allocations SET status='captured',capture_claim_hash=$1,captured_overview=$2,captured_cost=$3,capture_journal_id=$4,updated_at=$5 WHERE allocation_id=$6`, claimHash, claim.OverviewAmount, claim.UsedCost, nullable(journalID), now, allocationID)
	if err != nil {
		return funds.Allocation{}, false, err
	}
	if err = insertAllocationEvent(ctx, tx, allocationID, "captured", claimHash, map[string]any{"captureJournalId": journalID, "overviewAmount": claim.OverviewAmount, "usedCost": claim.UsedCost}, now); err != nil {
		return funds.Allocation{}, false, err
	}
	allocation, err = loadAllocationWhere(ctx, tx, `allocation_id=$1`, allocationID)
	if err != nil {
		return funds.Allocation{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return funds.Allocation{}, false, err
	}
	return allocation, false, nil
}

func (store *Store) ReleaseOverview(ctx context.Context, allocationID, reasonCode, requestHash string) (funds.Allocation, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return funds.Allocation{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx, "fund-allocation:"+allocationID); err != nil {
		return funds.Allocation{}, false, err
	}
	allocation, err := loadAllocationForUpdate(ctx, tx, allocationID)
	if err != nil {
		return funds.Allocation{}, false, mapNotFound(err)
	}
	if allocation.Status == funds.AllocationReleased {
		if releaseHash(allocation.ID, allocation.ReleaseReasonCode) != requestHash {
			return funds.Allocation{}, false, funds.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return funds.Allocation{}, false, err
		}
		return allocation, true, nil
	}
	if allocation.Status != funds.AllocationAuthorized {
		return funds.Allocation{}, false, funds.ErrInvalidState
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return funds.Allocation{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE fund_allocations SET status='released',release_reason_code=$1,updated_at=$2 WHERE allocation_id=$3`, reasonCode, now, allocationID); err != nil {
		return funds.Allocation{}, false, err
	}
	if err = insertAllocationEvent(ctx, tx, allocationID, "released", requestHash, map[string]any{"reasonCode": reasonCode}, now); err != nil {
		return funds.Allocation{}, false, err
	}
	allocation.Status, allocation.ReleaseReasonCode, allocation.UpdatedAt = funds.AllocationReleased, reasonCode, now
	if err = tx.Commit(); err != nil {
		return funds.Allocation{}, false, err
	}
	return allocation, false, nil
}

func (store *Store) ReverseJournal(ctx context.Context, journal funds.Journal) (funds.Journal, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return funds.Journal{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	identities := []string{"fund-journal-key:" + journal.IdempotencyKey, "fund-reversal:" + journal.ReversalOf}
	for _, entry := range journal.Entries {
		identities = append(identities, "fund-account:"+entry.AccountID)
	}
	if err = lock(ctx, tx, identities...); err != nil {
		return funds.Journal{}, false, err
	}
	if existing, replay, replayErr := journalReplay(ctx, tx, journal); replay || replayErr != nil {
		if replayErr == nil {
			replayErr = tx.Commit()
		}
		return existing, replay, replayErr
	}
	var existingID string
	err = tx.QueryRowContext(ctx, `SELECT journal_id FROM fund_journals WHERE reversal_of=$1`, journal.ReversalOf).Scan(&existingID)
	if err == nil {
		return funds.Journal{}, false, funds.ErrContentConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return funds.Journal{}, false, err
	}
	for _, entry := range journal.Entries {
		var state string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM fund_accounts WHERE account_id=$1 FOR UPDATE`, entry.AccountID).Scan(&state); err != nil {
			return funds.Journal{}, false, mapNotFound(err)
		}
		if state != funds.AccountOpen {
			return funds.Journal{}, false, funds.ErrInvalidState
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&journal.CreatedAt); err != nil {
		return funds.Journal{}, false, err
	}
	if err = insertJournal(ctx, tx, journal); err != nil {
		return funds.Journal{}, false, err
	}
	created, err := loadJournal(ctx, tx, journal.ID)
	if err != nil {
		return funds.Journal{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return funds.Journal{}, false, err
	}
	return created, false, nil
}

func (store *Store) GetAllocation(ctx context.Context, allocationID string) (funds.Allocation, error) {
	value, err := loadAllocationWhere(ctx, store.db, `allocation_id=$1`, allocationID)
	if errors.Is(err, sql.ErrNoRows) {
		return funds.Allocation{}, funds.ErrNotFound
	}
	return value, err
}

func (store *Store) GetJournal(ctx context.Context, journalID string) (funds.Journal, error) {
	value, err := loadJournal(ctx, store.db, journalID)
	if errors.Is(err, sql.ErrNoRows) {
		return funds.Journal{}, funds.ErrNotFound
	}
	return value, err
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadAccount(ctx context.Context, query queryer, where string, arguments ...any) (funds.Account, error) {
	return scanAccount(query.QueryRowContext(ctx, accountSelect+` FROM fund_accounts WHERE `+where, arguments...))
}

func loadAccountForUpdate(ctx context.Context, tx *sql.Tx, id string) (funds.Account, error) {
	return scanAccount(tx.QueryRowContext(ctx, accountSelect+` FROM fund_accounts WHERE account_id=$1 FOR UPDATE`, id))
}

func scanAccount(row interface{ Scan(...any) error }) (value funds.Account, err error) {
	var taskID, principal, residual, refund sql.NullString
	err = row.Scan(&value.ID, &value.Type, &taskID, &value.ReferenceID, &value.Asset, &principal, &residual, &refund, &value.State, &value.Balance, &value.CreatedAt, &value.UpdatedAt)
	if taskID.Valid {
		value.TaskID = taskID.String
	}
	if principal.Valid {
		value.PrincipalOwnerID = principal.String
	}
	if residual.Valid {
		value.ResidualRecipientID = residual.String
	}
	if refund.Valid {
		value.RefundPolicyVersion = refund.String
	}
	return value, err
}

func loadAllocationForUpdate(ctx context.Context, tx *sql.Tx, id string) (funds.Allocation, error) {
	return scanAllocation(tx.QueryRowContext(ctx, allocationSelect+` FROM fund_allocations WHERE allocation_id=$1 FOR UPDATE`, id))
}

func loadAllocationWhere(ctx context.Context, query queryer, where string, arguments ...any) (funds.Allocation, error) {
	return scanAllocation(query.QueryRowContext(ctx, allocationSelect+` FROM fund_allocations WHERE `+where, arguments...))
}

func scanAllocation(row interface{ Scan(...any) error }) (value funds.Allocation, err error) {
	var claimHash, journalID, reason sql.NullString
	err = row.Scan(&value.ID, &value.IdempotencyKey, &value.RequestHash, &value.AccountID, &value.Asset, &value.TaskID, &value.TaskSpecHash, &value.SnapshotID, &value.MatchRevision, &value.AgentID, &value.PriceVersion, &value.QuoteHash, &value.OverviewPrice, &value.CostCap, &value.ReserveAmount, &value.Status, &claimHash, &value.CapturedOverview, &value.CapturedCost, &journalID, &reason, &value.Deadline, &value.CreatedAt, &value.UpdatedAt)
	if claimHash.Valid {
		value.CaptureClaimHash = claimHash.String
	}
	if journalID.Valid {
		value.CaptureJournalID = journalID.String
	}
	if reason.Valid {
		value.ReleaseReasonCode = reason.String
	}
	return value, err
}

func insertJournal(ctx context.Context, tx *sql.Tx, journal funds.Journal) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO fund_journals (journal_id,idempotency_key,request_hash,ledger_version,journal_type,task_id,allocation_id,reversal_of,source_ref,reason_code,created_at) VALUES ($1,$2,$3,'double-entry-v1',$4,$5,$6,$7,$8,$9,$10)`, journal.ID, journal.IdempotencyKey, journal.RequestHash, journal.Type, nullable(journal.TaskID), nullable(journal.AllocationID), nullable(journal.ReversalOf), journal.SourceRef, journal.ReasonCode, journal.CreatedAt)
	if err != nil {
		return err
	}
	for _, entry := range journal.Entries {
		var accountType string
		if err = tx.QueryRowContext(ctx, `SELECT account_type FROM fund_accounts WHERE account_id=$1`, entry.AccountID).Scan(&accountType); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO fund_entries (journal_id,entry_index,account_id,account_type,direction,amount,asset_key,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, journal.ID, entry.Index, entry.AccountID, accountType, entry.Direction, entry.Amount, entry.Asset, journal.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func loadJournal(ctx context.Context, query queryer, journalID string) (funds.Journal, error) {
	var value funds.Journal
	var taskID, allocationID, reversalOf sql.NullString
	err := query.QueryRowContext(ctx, `SELECT journal_id,idempotency_key,journal_type,request_hash,task_id,allocation_id,reversal_of,source_ref,reason_code,created_at FROM fund_journals WHERE journal_id=$1`, journalID).Scan(&value.ID, &value.IdempotencyKey, &value.Type, &value.RequestHash, &taskID, &allocationID, &reversalOf, &value.SourceRef, &value.ReasonCode, &value.CreatedAt)
	if err != nil {
		return funds.Journal{}, err
	}
	if taskID.Valid {
		value.TaskID = taskID.String
	}
	if allocationID.Valid {
		value.AllocationID = allocationID.String
	}
	if reversalOf.Valid {
		value.ReversalOf = reversalOf.String
	}
	rows, err := query.QueryContext(ctx, `SELECT entry_index,account_id,direction,amount::text,asset_key FROM fund_entries WHERE journal_id=$1 ORDER BY entry_index`, journalID)
	if err != nil {
		return funds.Journal{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry funds.Entry
		if err = rows.Scan(&entry.Index, &entry.AccountID, &entry.Direction, &entry.Amount, &entry.Asset); err != nil {
			return funds.Journal{}, err
		}
		value.Entries = append(value.Entries, entry)
	}
	return value, rows.Err()
}

func journalReplay(ctx context.Context, tx *sql.Tx, draft funds.Journal) (funds.Journal, bool, error) {
	var id, hash string
	err := tx.QueryRowContext(ctx, `SELECT journal_id,request_hash FROM fund_journals WHERE idempotency_key=$1`, draft.IdempotencyKey).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return funds.Journal{}, false, nil
	}
	if err != nil {
		return funds.Journal{}, false, err
	}
	if id != draft.ID || hash != draft.RequestHash {
		return funds.Journal{}, false, funds.ErrContentConflict
	}
	value, err := loadJournal(ctx, tx, id)
	return value, true, err
}

func ensureSystemAccount(ctx context.Context, tx *sql.Tx, id, accountType, referenceID, asset string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO fund_accounts (account_id,account_class,account_type,reference_id,asset_key,state,balance,created_at,updated_at) VALUES ($1,'system',$2,$3,$4,'open',0,$5,$5) ON CONFLICT (account_type,reference_id,asset_key) DO NOTHING`, id, accountType, referenceID, asset, now)
	return err
}

func insertAllocationEvent(ctx context.Context, tx *sql.Tx, allocationID, eventType, requestHash string, payload any, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID := stableDigest("fund-allocation-event", allocationID, eventType, funds.LedgerVersion)
	_, err = tx.ExecContext(ctx, `INSERT INTO fund_allocation_events (event_id,allocation_id,event_type,request_hash,payload,occurred_at) VALUES ($1,$2,$3,$4,$5,$6)`, eventID, allocationID, eventType, requestHash, string(encoded), now)
	return err
}

func lock(ctx context.Context, tx *sql.Tx, identities ...string) error {
	sort.Strings(identities)
	for _, identity := range identities {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, identity); err != nil {
			return err
		}
	}
	return nil
}

func sameAccount(left, right funds.Account) bool {
	return left.ID == right.ID && left.Type == right.Type && left.TaskID == right.TaskID && left.ReferenceID == right.ReferenceID && left.Asset == right.Asset && left.PrincipalOwnerID == right.PrincipalOwnerID && left.ResidualRecipientID == right.ResidualRecipientID && left.RefundPolicyVersion == right.RefundPolicyVersion
}

func businessType(value string) bool {
	return value == funds.AccountDiscoveryPool || value == funds.AccountFormalEscrow || value == funds.AccountChangeOrder || value == funds.AccountDisputeFee
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return funds.ErrNotFound
	}
	return err
}
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func compareMoney(left, right string) int {
	l, _ := new(big.Int).SetString(left, 10)
	r, _ := new(big.Int).SetString(right, 10)
	return l.Cmp(r)
}
func addMoney(left, right string) string {
	l, _ := new(big.Int).SetString(left, 10)
	r, _ := new(big.Int).SetString(right, 10)
	return new(big.Int).Add(l, r).String()
}

func stableDigest(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		_, _ = hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func releaseHash(allocationID, reasonCode string) string {
	encoded, _ := json.Marshal(struct {
		AllocationID string
		ReasonCode   string
	}{allocationID, reasonCode})
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

const accountSelect = `SELECT account_id,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance::text,created_at,updated_at`
const allocationSelect = `SELECT allocation_id,idempotency_key,request_hash,account_id,asset_key,task_id,task_spec_hash,snapshot_id,match_revision,agent_id,price_version,quote_hash,overview_price::text,external_cost_cap::text,reserve_amount::text,status,capture_claim_hash,captured_overview::text,captured_cost::text,capture_journal_id,release_reason_code,deadline,created_at,updated_at`

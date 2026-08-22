package funds

import (
	"context"
	"math/big"
	"reflect"
	"sort"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu          sync.Mutex
	accounts    map[string]Account
	allocations map[string]Allocation
	journals    map[string]Journal
	journalKeys map[string]string
	reversals   map[string]string
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{accounts: make(map[string]Account), allocations: make(map[string]Allocation), journals: make(map[string]Journal), journalKeys: make(map[string]string), reversals: make(map[string]string)}
}

func (repository *MemoryRepository) OpenAccount(_ context.Context, draft Account) (Account, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.accounts[draft.ID]; ok {
		if !sameAccountIdentity(existing, draft) {
			return Account{}, false, ErrContentConflict
		}
		return existing, true, nil
	}
	for _, existing := range repository.accounts {
		if existing.Type == draft.Type && existing.ReferenceID == draft.ReferenceID && existing.Asset == draft.Asset {
			return Account{}, false, ErrContentConflict
		}
	}
	repository.accounts[draft.ID] = draft
	return draft, false, nil
}

func (repository *MemoryRepository) GetAccount(_ context.Context, accountID string) (Account, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	account, ok := repository.accounts[accountID]
	if !ok {
		return Account{}, ErrNotFound
	}
	return account, nil
}

func (repository *MemoryRepository) PostFunding(_ context.Context, journal Journal, request FundingRequest) (Journal, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, replay, err := repository.journalReplay(journal); replay || err != nil {
		return existing, replay, err
	}
	account, ok := repository.accounts[request.AccountID]
	if !ok {
		return Journal{}, false, ErrNotFound
	}
	if account.State != AccountOpen || !businessAccountType(account.Type) || account.Asset != journal.Entries[1].Asset {
		return Journal{}, false, ErrInvalidState
	}
	control := Account{ID: journal.Entries[0].AccountID, Type: AccountFundingControl, ReferenceID: account.Asset, Asset: account.Asset, State: AccountOpen, Balance: "0", CreatedAt: journal.CreatedAt, UpdatedAt: journal.CreatedAt}
	if _, exists := repository.accounts[control.ID]; !exists {
		repository.accounts[control.ID] = control
	}
	if err := repository.postJournal(journal); err != nil {
		return Journal{}, false, err
	}
	return cloneJournal(journal), false, nil
}

func (repository *MemoryRepository) AuthorizeOverview(_ context.Context, draft Allocation) (Allocation, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.allocations[draft.ID]; ok {
		if !sameAllocationIdentity(existing, draft) {
			return Allocation{}, false, ErrContentConflict
		}
		return existing, true, nil
	}
	for _, existing := range repository.allocations {
		if existing.IdempotencyKey == draft.IdempotencyKey {
			return Allocation{}, false, ErrContentConflict
		}
	}
	var source Account
	found := false
	for _, account := range repository.accounts {
		if account.Type == AccountDiscoveryPool && account.TaskID == draft.TaskID && account.Asset == draft.Asset {
			if found {
				return Allocation{}, false, ErrContentConflict
			}
			source, found = account, true
		}
	}
	if !found {
		return Allocation{}, false, ErrNotFound
	}
	if source.State != AccountOpen {
		return Allocation{}, false, ErrInvalidState
	}
	available := source.Balance
	for _, allocation := range repository.allocations {
		if allocation.AccountID == source.ID && allocation.Status == AllocationAuthorized {
			available = subtractMoney(available, allocation.ReserveAmount)
		}
	}
	if compareMoney(available, draft.ReserveAmount) < 0 {
		return Allocation{}, false, ErrInsufficient
	}
	draft.AccountID = source.ID
	repository.allocations[draft.ID] = draft
	return draft, false, nil
}

func (repository *MemoryRepository) CaptureOverview(_ context.Context, allocationID string, claim OverviewCapture, claimHash string) (Allocation, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	allocation, ok := repository.allocations[allocationID]
	if !ok {
		return Allocation{}, false, ErrNotFound
	}
	if allocation.Status == AllocationCaptured {
		if allocation.CaptureClaimHash != claimHash {
			return Allocation{}, false, ErrContentConflict
		}
		return allocation, true, nil
	}
	if allocation.Status != AllocationAuthorized {
		return Allocation{}, false, ErrInvalidState
	}
	if allocation.TaskID != claim.TaskID || allocation.TaskSpecHash != claim.TaskSpecHash || allocation.MatchRevision != claim.MatchRevision || allocation.AgentID != claim.AgentID || allocation.QuoteHash != claim.QuoteHash || allocation.OverviewPrice != claim.OverviewAmount || compareMoney(claim.UsedCost, allocation.CostCap) > 0 {
		return Allocation{}, false, ErrContentConflict
	}
	total := addMoney(claim.OverviewAmount, claim.UsedCost)
	journalID := ""
	if total != "0" {
		journalID = stableID("fund-capture-journal", allocation.ID, claimHash, LedgerVersion)
		entries := []Entry{{Index: 1, AccountID: allocation.AccountID, Direction: EntryDebit, Amount: total, Asset: allocation.Asset}}
		if claim.OverviewAmount != "0" {
			agentID := stableID("fund-system-account", AccountAgentReceivable, claim.AgentID, allocation.Asset, LedgerVersion)
			repository.ensureSystemAccount(agentID, AccountAgentReceivable, claim.AgentID, allocation.Asset, allocation.UpdatedAt)
			entries = append(entries, Entry{Index: len(entries) + 1, AccountID: agentID, Direction: EntryCredit, Amount: claim.OverviewAmount, Asset: allocation.Asset})
		}
		if claim.UsedCost != "0" {
			costID := stableID("fund-system-account", AccountExternalClearing, allocation.ID, allocation.Asset, LedgerVersion)
			repository.ensureSystemAccount(costID, AccountExternalClearing, allocation.ID, allocation.Asset, allocation.UpdatedAt)
			entries = append(entries, Entry{Index: len(entries) + 1, AccountID: costID, Direction: EntryCredit, Amount: claim.UsedCost, Asset: allocation.Asset})
		}
		journal := Journal{ID: journalID, IdempotencyKey: "capture:" + allocation.ID, Type: "overview_capture", RequestHash: claimHash, TaskID: allocation.TaskID, AllocationID: allocation.ID, SourceRef: claim.LogicalExecutionID, ReasonCode: "overview_valid", Entries: entries, CreatedAt: time.Now().UTC()}
		if err := repository.postJournal(journal); err != nil {
			return Allocation{}, false, err
		}
	}
	allocation.Status = AllocationCaptured
	allocation.CaptureClaimHash = claimHash
	allocation.CapturedOverview = claim.OverviewAmount
	allocation.CapturedCost = claim.UsedCost
	allocation.CaptureJournalID = journalID
	allocation.UpdatedAt = time.Now().UTC()
	repository.allocations[allocationID] = allocation
	return allocation, false, nil
}

func (repository *MemoryRepository) ReleaseOverview(_ context.Context, allocationID, reasonCode, requestHash string) (Allocation, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	allocation, ok := repository.allocations[allocationID]
	if !ok {
		return Allocation{}, false, ErrNotFound
	}
	if allocation.Status == AllocationReleased {
		if hashJSON(struct {
			AllocationID string
			ReasonCode   string
		}{allocationID, allocation.ReleaseReasonCode}) != requestHash {
			return Allocation{}, false, ErrContentConflict
		}
		return allocation, true, nil
	}
	if allocation.Status != AllocationAuthorized {
		return Allocation{}, false, ErrInvalidState
	}
	allocation.Status = AllocationReleased
	allocation.ReleaseReasonCode = reasonCode
	allocation.UpdatedAt = time.Now().UTC()
	repository.allocations[allocationID] = allocation
	return allocation, false, nil
}

func (repository *MemoryRepository) ReverseJournal(_ context.Context, journal Journal) (Journal, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, replay, err := repository.journalReplay(journal); replay || err != nil {
		return existing, replay, err
	}
	if existingID, ok := repository.reversals[journal.ReversalOf]; ok {
		existing := repository.journals[existingID]
		if existing.RequestHash == journal.RequestHash {
			return cloneJournal(existing), true, nil
		}
		return Journal{}, false, ErrContentConflict
	}
	if _, ok := repository.journals[journal.ReversalOf]; !ok {
		return Journal{}, false, ErrNotFound
	}
	if err := repository.postJournal(journal); err != nil {
		return Journal{}, false, err
	}
	repository.reversals[journal.ReversalOf] = journal.ID
	return cloneJournal(journal), false, nil
}

func (repository *MemoryRepository) GetAllocation(_ context.Context, allocationID string) (Allocation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	allocation, ok := repository.allocations[allocationID]
	if !ok {
		return Allocation{}, ErrNotFound
	}
	return allocation, nil
}

func (repository *MemoryRepository) GetJournal(_ context.Context, journalID string) (Journal, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	journal, ok := repository.journals[journalID]
	if !ok {
		return Journal{}, ErrNotFound
	}
	return cloneJournal(journal), nil
}

func (repository *MemoryRepository) postJournal(journal Journal) error {
	if len(journal.Entries) < 2 {
		return ErrInvalidInput
	}
	debits, credits := "0", "0"
	ids := make([]string, 0, len(journal.Entries))
	for index, entry := range journal.Entries {
		if entry.Index != index+1 || !positiveMoney(entry.Amount) || entry.Direction != EntryDebit && entry.Direction != EntryCredit {
			return ErrInvalidInput
		}
		account, ok := repository.accounts[entry.AccountID]
		if !ok || account.Asset != entry.Asset {
			return ErrContentConflict
		}
		ids = append(ids, account.ID)
		if entry.Direction == EntryDebit {
			debits = addMoney(debits, entry.Amount)
		} else {
			credits = addMoney(credits, entry.Amount)
		}
	}
	if debits != credits {
		return ErrInvalidInput
	}
	sort.Strings(ids)
	updated := make(map[string]Account)
	for _, entry := range journal.Entries {
		account := repository.accounts[entry.AccountID]
		if changed, ok := updated[entry.AccountID]; ok {
			account = changed
		}
		if entry.Direction == EntryDebit {
			account.Balance = subtractMoney(account.Balance, entry.Amount)
		} else {
			account.Balance = addMoney(account.Balance, entry.Amount)
		}
		if businessAccountType(account.Type) && compareMoney(account.Balance, "0") < 0 {
			return ErrInsufficient
		}
		account.UpdatedAt = journal.CreatedAt
		updated[account.ID] = account
	}
	for id, account := range updated {
		repository.accounts[id] = account
	}
	repository.journals[journal.ID] = cloneJournal(journal)
	repository.journalKeys[journal.IdempotencyKey] = journal.ID
	return nil
}

func (repository *MemoryRepository) journalReplay(draft Journal) (Journal, bool, error) {
	if id, ok := repository.journalKeys[draft.IdempotencyKey]; ok {
		existing := repository.journals[id]
		if existing.ID != draft.ID || existing.RequestHash != draft.RequestHash {
			return Journal{}, false, ErrContentConflict
		}
		return cloneJournal(existing), true, nil
	}
	return Journal{}, false, nil
}

func (repository *MemoryRepository) ensureSystemAccount(id, accountType, referenceID, asset string, now time.Time) {
	if _, ok := repository.accounts[id]; !ok {
		repository.accounts[id] = Account{ID: id, Type: accountType, ReferenceID: referenceID, Asset: asset, State: AccountOpen, Balance: "0", CreatedAt: now, UpdatedAt: now}
	}
}

func sameAccountIdentity(left, right Account) bool {
	left.Balance, right.Balance = "", ""
	left.State, right.State = "", ""
	left.CreatedAt, right.CreatedAt = time.Time{}, time.Time{}
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(left, right)
}

func sameAllocationIdentity(left, right Allocation) bool {
	left.AccountID, right.AccountID = "", ""
	left.Status, right.Status = "", ""
	left.CaptureClaimHash, right.CaptureClaimHash = "", ""
	left.CapturedOverview, right.CapturedOverview = "", ""
	left.CapturedCost, right.CapturedCost = "", ""
	left.CaptureJournalID, right.CaptureJournalID = "", ""
	left.ReleaseReasonCode, right.ReleaseReasonCode = "", ""
	left.CreatedAt, right.CreatedAt = time.Time{}, time.Time{}
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(left, right)
}

func cloneJournal(source Journal) Journal {
	copy := source
	copy.Entries = append([]Entry(nil), source.Entries...)
	return copy
}

func subtractMoney(left, right string) string {
	leftValue, _ := new(big.Int).SetString(left, 10)
	rightValue, _ := new(big.Int).SetString(right, 10)
	return new(big.Int).Sub(leftValue, rightValue).String()
}

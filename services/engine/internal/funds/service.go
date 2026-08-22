package funds

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"time"
)

var safeCode = regexp.MustCompile(`^[a-z0-9][a-z0-9_,.-]{0,255}$`)
var safeAsset = regexp.MustCompile(`^[a-z0-9][a-z0-9:/._-]{0,127}$`)

type Service struct {
	repository Repository
	asset      string
	now        func() time.Time
}

func NewService(repository Repository, asset string) (*Service, error) {
	if repository == nil || !validAsset(asset) {
		return nil, ErrInvalidInput
	}
	return &Service{repository: repository, asset: asset, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (service *Service) OpenAccount(ctx context.Context, request OpenAccountRequest) (Account, bool, error) {
	if !businessAccountType(request.Type) || strings.TrimSpace(request.TaskID) == "" || strings.TrimSpace(request.ReferenceID) == "" || !validAsset(request.Asset) || strings.TrimSpace(request.PrincipalOwnerID) == "" || strings.TrimSpace(request.ResidualRecipientID) == "" || !safeCode.MatchString(request.RefundPolicyVersion) {
		return Account{}, false, ErrInvalidInput
	}
	now := service.now()
	account := Account{ID: stableID("fund-account", request.Type, request.ReferenceID, request.Asset, LedgerVersion), Type: request.Type, TaskID: request.TaskID, ReferenceID: request.ReferenceID, Asset: request.Asset, PrincipalOwnerID: request.PrincipalOwnerID, ResidualRecipientID: request.ResidualRecipientID, RefundPolicyVersion: request.RefundPolicyVersion, State: AccountOpen, Balance: "0", CreatedAt: now, UpdatedAt: now}
	return service.repository.OpenAccount(ctx, account)
}

func (service *Service) RecordFunding(ctx context.Context, request FundingRequest) (Journal, bool, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.AccountID) == "" || !positiveMoney(request.Amount) || strings.TrimSpace(request.ExternalRef) == "" {
		return Journal{}, false, ErrInvalidInput
	}
	account, err := service.repository.GetAccount(ctx, request.AccountID)
	if err != nil {
		return Journal{}, false, err
	}
	controlID := stableID("fund-system-account", AccountFundingControl, account.Asset, LedgerVersion)
	journal := Journal{ID: stableID("fund-journal", request.IdempotencyKey, LedgerVersion), IdempotencyKey: request.IdempotencyKey, Type: "funding", RequestHash: hashJSON(request), TaskID: account.TaskID, SourceRef: request.ExternalRef, ReasonCode: "escrow_funded", Entries: []Entry{{Index: 1, AccountID: controlID, Direction: EntryDebit, Amount: request.Amount, Asset: account.Asset}, {Index: 2, AccountID: account.ID, Direction: EntryCredit, Amount: request.Amount, Asset: account.Asset}}, CreatedAt: service.now()}
	return service.repository.PostFunding(ctx, journal, request)
}

func (service *Service) AuthorizeOverview(ctx context.Context, request OverviewAuthorization) (Allocation, bool, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.TaskID) == "" || !validDigest(request.TaskSpecHash) || !validDigest(request.SnapshotID) || request.MatchRevision < 1 || strings.TrimSpace(request.AgentID) == "" || request.PriceVersion < 1 || !validDigest(request.QuoteHash) || !validMoney(request.OverviewPrice) || !validMoney(request.ExternalCostCap) || !request.Deadline.After(service.now()) {
		return Allocation{}, false, ErrInvalidInput
	}
	reserve := addMoney(request.OverviewPrice, request.ExternalCostCap)
	allocation := Allocation{ID: stableID("fund-allocation", request.IdempotencyKey, LedgerVersion), IdempotencyKey: request.IdempotencyKey, RequestHash: hashJSON(request), Asset: service.asset, TaskID: request.TaskID, TaskSpecHash: request.TaskSpecHash, SnapshotID: request.SnapshotID, MatchRevision: request.MatchRevision, AgentID: request.AgentID, PriceVersion: request.PriceVersion, QuoteHash: request.QuoteHash, OverviewPrice: request.OverviewPrice, CostCap: request.ExternalCostCap, ReserveAmount: reserve, Status: AllocationAuthorized, CapturedOverview: "0", CapturedCost: "0", Deadline: request.Deadline.UTC(), CreatedAt: service.now(), UpdatedAt: service.now()}
	return service.repository.AuthorizeOverview(ctx, allocation)
}

func (service *Service) CaptureOverview(ctx context.Context, allocationID string, claim OverviewCapture) (Allocation, bool, error) {
	if strings.TrimSpace(allocationID) == "" || strings.TrimSpace(claim.TaskID) == "" || !validDigest(claim.TaskSpecHash) || claim.MatchRevision < 1 || strings.TrimSpace(claim.LogicalExecutionID) == "" || strings.TrimSpace(claim.AgentID) == "" || !validDigest(claim.QuoteHash) || !validDigest(claim.ContentHash) || !validMoney(claim.OverviewAmount) || !validMoney(claim.UsedCost) {
		return Allocation{}, false, ErrInvalidInput
	}
	claimHash := hashJSON(claim)
	return service.repository.CaptureOverview(ctx, allocationID, claim, claimHash)
}

func (service *Service) ReleaseOverview(ctx context.Context, allocationID, reasonCode string) (Allocation, bool, error) {
	if strings.TrimSpace(allocationID) == "" || !safeCode.MatchString(reasonCode) {
		return Allocation{}, false, ErrInvalidInput
	}
	return service.repository.ReleaseOverview(ctx, allocationID, reasonCode, hashJSON(struct {
		AllocationID string
		ReasonCode   string
	}{allocationID, reasonCode}))
}

func (service *Service) ReverseJournal(ctx context.Context, request ReverseRequest) (Journal, bool, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" || strings.TrimSpace(request.JournalID) == "" || !safeCode.MatchString(request.ReasonCode) {
		return Journal{}, false, ErrInvalidInput
	}
	original, err := service.repository.GetJournal(ctx, request.JournalID)
	if err != nil {
		return Journal{}, false, err
	}
	entries := make([]Entry, len(original.Entries))
	for index, entry := range original.Entries {
		direction := EntryCredit
		if entry.Direction == EntryCredit {
			direction = EntryDebit
		}
		entries[index] = Entry{Index: index + 1, AccountID: entry.AccountID, Direction: direction, Amount: entry.Amount, Asset: entry.Asset}
	}
	journal := Journal{ID: stableID("fund-journal", request.IdempotencyKey, LedgerVersion), IdempotencyKey: request.IdempotencyKey, Type: "reversal", RequestHash: hashJSON(request), TaskID: original.TaskID, ReversalOf: original.ID, SourceRef: original.ID, ReasonCode: request.ReasonCode, Entries: entries, CreatedAt: service.now()}
	return service.repository.ReverseJournal(ctx, journal)
}

func (service *Service) GetAllocation(ctx context.Context, allocationID string) (Allocation, error) {
	return service.repository.GetAllocation(ctx, allocationID)
}

func businessAccountType(value string) bool {
	return slices.Contains([]string{AccountDiscoveryPool, AccountFormalEscrow, AccountChangeOrder, AccountDisputeFee}, value)
}

func validAsset(value string) bool {
	return safeAsset.MatchString(value) && value == strings.ToLower(value)
}

func validMoney(value string) bool {
	if value == "" || len(value) > 78 || value != "0" && strings.HasPrefix(value, "0") {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	_, ok := new(big.Int).SetString(value, 10)
	return ok
}

func positiveMoney(value string) bool { return validMoney(value) && value != "0" }

func addMoney(left, right string) string {
	leftValue, _ := new(big.Int).SetString(left, 10)
	rightValue, _ := new(big.Int).SetString(right, 10)
	return new(big.Int).Add(leftValue, rightValue).String()
}

func compareMoney(left, right string) int {
	leftValue, _ := new(big.Int).SetString(left, 10)
	rightValue, _ := new(big.Int).SetString(right, 10)
	return leftValue.Cmp(rightValue)
}

func hashJSON(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func stableID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte{byte(len(part) >> 24), byte(len(part) >> 16), byte(len(part) >> 8), byte(len(part))})
		_, _ = hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

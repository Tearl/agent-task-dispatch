package funds

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalidInput    = errors.New("invalid funds input")
	ErrNotFound        = errors.New("funds resource not found")
	ErrContentConflict = errors.New("funds idempotency content conflict")
	ErrInvalidState    = errors.New("invalid funds state")
	ErrInsufficient    = errors.New("insufficient available funds")
)

const (
	LedgerVersion = "double-entry-v1"

	AccountDiscoveryPool    = "discovery_pool"
	AccountFormalEscrow     = "formal_escrow"
	AccountChangeOrder      = "change_order_escrow"
	AccountDisputeFee       = "dispute_fee_pool"
	AccountFundingControl   = "funding_control"
	AccountAgentReceivable  = "agent_receivable"
	AccountFormalReceivable = "formal_agent_receivable"
	AccountExternalClearing = "external_cost_clearing"

	AccountOpen   = "open"
	AccountFrozen = "frozen"
	AccountClosed = "closed"

	AllocationAuthorized = "authorized"
	AllocationCaptured   = "captured"
	AllocationReleased   = "released"

	EntryDebit  = "debit"
	EntryCredit = "credit"
)

type Account struct {
	ID                  string
	Type                string
	TaskID              string
	ReferenceID         string
	Asset               string
	PrincipalOwnerID    string
	ResidualRecipientID string
	RefundPolicyVersion string
	State               string
	Balance             string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type OpenAccountRequest struct {
	Type                string
	TaskID              string
	ReferenceID         string
	Asset               string
	PrincipalOwnerID    string
	ResidualRecipientID string
	RefundPolicyVersion string
}

type FundingRequest struct {
	IdempotencyKey string
	AccountID      string
	Amount         string
	ExternalRef    string
}

type OverviewAuthorization struct {
	IdempotencyKey  string
	TaskID          string
	TaskSpecHash    string
	SnapshotID      string
	MatchRevision   int
	AgentID         string
	PriceVersion    int
	QuoteHash       string
	OverviewPrice   string
	ExternalCostCap string
	Deadline        time.Time
}

type OverviewCapture struct {
	TaskID             string
	TaskSpecHash       string
	MatchRevision      int
	LogicalExecutionID string
	AgentID            string
	QuoteHash          string
	ContentHash        string
	OverviewAmount     string
	UsedCost           string
}

type Allocation struct {
	ID                string
	IdempotencyKey    string
	RequestHash       string
	AccountID         string
	Asset             string
	TaskID            string
	TaskSpecHash      string
	SnapshotID        string
	MatchRevision     int
	AgentID           string
	PriceVersion      int
	QuoteHash         string
	OverviewPrice     string
	CostCap           string
	ReserveAmount     string
	Status            string
	CaptureClaimHash  string
	CapturedOverview  string
	CapturedCost      string
	CaptureJournalID  string
	ReleaseReasonCode string
	Deadline          time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func CompatibleAuthorizationReplay(existing, draft Allocation) bool {
	return existing.Status == AllocationAuthorized &&
		draft.Status == AllocationAuthorized &&
		existing.ID == draft.ID &&
		existing.IdempotencyKey == draft.IdempotencyKey &&
		existing.Asset == draft.Asset &&
		existing.TaskID == draft.TaskID &&
		existing.TaskSpecHash == draft.TaskSpecHash &&
		existing.SnapshotID == draft.SnapshotID &&
		existing.MatchRevision == draft.MatchRevision &&
		existing.AgentID == draft.AgentID &&
		existing.PriceVersion == draft.PriceVersion &&
		existing.QuoteHash == draft.QuoteHash &&
		existing.OverviewPrice == draft.OverviewPrice &&
		existing.CostCap == draft.CostCap &&
		existing.ReserveAmount == draft.ReserveAmount &&
		!draft.Deadline.Before(existing.Deadline)
}

type Entry struct {
	Index     int
	AccountID string
	Direction string
	Amount    string
	Asset     string
}

type Journal struct {
	ID             string
	IdempotencyKey string
	Type           string
	RequestHash    string
	TaskID         string
	AllocationID   string
	ReversalOf     string
	SourceRef      string
	ReasonCode     string
	Entries        []Entry
	CreatedAt      time.Time
}

type ReverseRequest struct {
	IdempotencyKey string
	JournalID      string
	ReasonCode     string
}

type Repository interface {
	OpenAccount(context.Context, Account) (Account, bool, error)
	GetAccount(context.Context, string) (Account, error)
	PostFunding(context.Context, Journal, FundingRequest) (Journal, bool, error)
	AuthorizeOverview(context.Context, Allocation) (Allocation, bool, error)
	CaptureOverview(context.Context, string, OverviewCapture, string) (Allocation, bool, error)
	ReleaseOverview(context.Context, string, string, string) (Allocation, bool, error)
	ReverseJournal(context.Context, Journal) (Journal, bool, error)
	GetAllocation(context.Context, string) (Allocation, error)
	GetJournal(context.Context, string) (Journal, error)
}

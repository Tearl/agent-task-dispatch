package financeview

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden = errors.New("finance view forbidden")
	ErrInvalid   = errors.New("invalid finance view")
)

const (
	SubmissionNotSubmitted  = "not_submitted"
	SubmissionSubmitted     = "submitted"
	ConfirmationNotObserved = "not_observed"
	ConfirmationPending     = "pending"
	ConfirmationConfirmed   = "confirmed"
	ConfirmationFailed      = "failed"
	ConfirmationOrphaned    = "orphaned"
	RefundAvailable         = "available"
	RefundPending           = "pending"
	RefundConfirmed         = "confirmed"
	RefundUnavailable       = "unavailable"
)

type Confirmation struct {
	Submission   string `json:"submission"`
	Confirmation string `json:"confirmation"`
}

type PublisherTotals struct {
	Discovery    string `json:"discovery"`
	Formal       string `json:"formal"`
	ChangeOrders string `json:"changeOrders"`
	DisputeFees  string `json:"disputeFees"`
	Refundable   string `json:"refundable"`
	Refunded     string `json:"refunded"`
}

type TaskFunds struct {
	TaskID          string       `json:"taskId"`
	Title           string       `json:"title"`
	Asset           string       `json:"asset"`
	Lifecycle       string       `json:"lifecycle"`
	Discovery       string       `json:"discovery"`
	Formal          string       `json:"formal"`
	ChangeOrders    string       `json:"changeOrders"`
	DisputeFees     string       `json:"disputeFees"`
	Refundable      string       `json:"refundable"`
	RefundStatus    string       `json:"refundStatus"`
	Terminal        bool         `json:"terminal"`
	Chain           Confirmation `json:"chain"`
	TransactionHash string       `json:"transactionHash,omitempty"`
	UpdatedAt       time.Time    `json:"updatedAt"`
}

type LedgerRecord struct {
	ID              string    `json:"id"`
	TaskID          string    `json:"taskId,omitempty"`
	Type            string    `json:"type"`
	Amount          string    `json:"amount"`
	Asset           string    `json:"asset"`
	ReasonCode      string    `json:"reasonCode"`
	TransactionHash string    `json:"transactionHash,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
}

type PublisherView struct {
	AsOf   time.Time       `json:"asOf"`
	Totals PublisherTotals `json:"totals"`
	Tasks  []TaskFunds     `json:"tasks"`
	Ledger []LedgerRecord  `json:"ledger"`
}

type AgentTotals struct {
	OverviewReceivable string `json:"overviewReceivable"`
	FormalClaimable    string `json:"formalClaimable"`
	TotalAvailable     string `json:"totalAvailable"`
}

type EarningPosition struct {
	AgentID            string       `json:"agentId"`
	AgentName          string       `json:"agentName"`
	Controller         string       `json:"controller"`
	Payout             string       `json:"payout"`
	Asset              string       `json:"asset"`
	OverviewReceivable string       `json:"overviewReceivable"`
	FormalClaimable    string       `json:"formalClaimable"`
	ChainClaimable     string       `json:"chainClaimable"`
	Chain              Confirmation `json:"chain"`
}

type AgentView struct {
	AsOf      time.Time         `json:"asOf"`
	Totals    AgentTotals       `json:"totals"`
	Positions []EarningPosition `json:"positions"`
	Records   []LedgerRecord    `json:"records"`
}

type ReconciliationDifference struct {
	Category   string `json:"category"`
	ResourceID string `json:"resourceId"`
	Expected   string `json:"expected"`
	Observed   string `json:"observed"`
	Severity   string `json:"severity"`
}

type ReconciliationRun struct {
	ID          string                     `json:"id"`
	ChainID     string                     `json:"chainId"`
	Contract    string                     `json:"contract"`
	SafeBlock   uint64                     `json:"safeBlock"`
	Status      string                     `json:"status"`
	StartedAt   time.Time                  `json:"startedAt"`
	FinishedAt  time.Time                  `json:"finishedAt"`
	Differences []ReconciliationDifference `json:"differences"`
}

type ReconciliationView struct {
	AsOf time.Time           `json:"asOf"`
	Runs []ReconciliationRun `json:"runs"`
}

type Repository interface {
	Publisher(context.Context, string) (PublisherView, error)
	Agent(context.Context, string) (AgentView, error)
	Reconciliation(context.Context) (ReconciliationView, error)
}

package selection

import (
	"context"
	"errors"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
)

var (
	ErrForbidden           = errors.New("selection operation forbidden")
	ErrInvalidInput        = errors.New("invalid selection input")
	ErrNotFound            = errors.New("selection reservation not found")
	ErrInvalidState        = errors.New("invalid selection state")
	ErrContentConflict     = errors.New("selection content conflict")
	ErrProofMismatch       = errors.New("selection chain proof mismatch")
	ErrDependencyPending   = errors.New("selection chain result pending")
	ErrCapacityUnavailable = errors.New("selection capacity unavailable")
)

const (
	Version      = "selection-reservation-v1"
	ProofVersion = "escrow-selection-v1"

	StatusReserved  = "reserved"
	StatusSubmitted = "submitted"
	StatusConfirmed = "confirmed"
	StatusFailed    = "failed"
	StatusExpired   = "expired"
	StatusOrphaned  = "orphaned"

	ChainPending   = "pending"
	ChainConfirmed = "confirmed"
	ChainFailed    = "failed"
)

type Request struct {
	BatchID string `json:"batchId"`
	SlotID  string `json:"slotId"`
}

type ReconcileRequest struct {
	TransactionHash string `json:"transactionHash"`
}

type Mutation struct {
	PublisherID    string
	IdempotencyKey string
	RequestHash    string
	Now            time.Time
}

type Eligibility struct {
	TaskID           string
	TaskDeadline     time.Time
	SnapshotID       string
	TaskSpecHash     string
	MatchRevision    uint64
	PolicyHash       string
	BatchID          string
	SlotID           string
	AgentID          string
	ProviderID       string
	AgentController  string
	Payout           string
	PriceVersion     uint64
	QuoteHash        string
	AllocationID     string
	OverviewPrice    string
	FormalGrossPrice string
}

// Proof field order mirrors TaskEscrow.SelectionProof exactly.
type Proof struct {
	TaskID           string `json:"taskId"`
	AssignmentID     string `json:"assignmentId"`
	AgentController  string `json:"agentController"`
	Payout           string `json:"payout"`
	OverviewID       string `json:"overviewId"`
	AllocationID     string `json:"allocationId"`
	QuoteHash        string `json:"quoteHash"`
	TaskSpecHash     string `json:"taskSpecHash"`
	MatchRevision    uint64 `json:"matchRevision"`
	PriceVersion     uint64 `json:"priceVersion"`
	OverviewPrice    string `json:"overviewPrice"`
	FormalGrossPrice string `json:"formalGrossPrice"`
	OverviewCredit   string `json:"overviewCredit"`
	PolicyHash       string `json:"policyHash"`
	Nonce            string `json:"nonce"`
	Deadline         uint64 `json:"deadline"`
}

type Reservation struct {
	ID                   string    `json:"id"`
	PublisherID          string    `json:"publisherId"`
	PublisherWallet      string    `json:"publisherWallet"`
	TaskID               string    `json:"taskId"`
	BatchID              string    `json:"batchId"`
	SlotID               string    `json:"slotId"`
	SnapshotID           string    `json:"snapshotId"`
	AgentID              string    `json:"agentId"`
	ProviderID           string    `json:"providerId"`
	ChainID              string    `json:"chainId"`
	ContractAddress      string    `json:"contractAddress"`
	Proof                Proof     `json:"proof"`
	ProofPayloadHash     string    `json:"proofPayloadHash"`
	ProofDigest          string    `json:"proofDigest"`
	FormalPayable        string    `json:"formalPayable"`
	CapacityFencingToken int64     `json:"-"`
	CapacityExpiresAt    time.Time `json:"capacityExpiresAt"`
	Status               string    `json:"status"`
	TransactionHash      string    `json:"transactionHash,omitempty"`
	FailureReasonCode    string    `json:"failureReasonCode,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type Intent struct {
	Reservation       Reservation `json:"reservation"`
	PlatformSignature string      `json:"platformSignature"`
}

type Assignment struct {
	ID              string    `json:"id"`
	TaskID          string    `json:"taskId"`
	ReservationID   string    `json:"reservationId"`
	AgentID         string    `json:"agentId"`
	ProviderID      string    `json:"providerId"`
	FormalPayable   string    `json:"formalPayable"`
	OverviewCredit  string    `json:"overviewCredit"`
	WorkNonce       uint64    `json:"workNonce"`
	TransactionHash string    `json:"transactionHash"`
	ConfirmedAt     time.Time `json:"confirmedAt"`
}

type ChainResult struct {
	Status            string
	TransactionHash   string
	BlockNumber       uint64
	LogIndex          uint
	Proof             Proof
	FormalPayable     string
	WorkNonce         uint64
	FailureReasonCode string
}

type Repository interface {
	Replay(context.Context, string, string, string) (Reservation, bool, error)
	Eligibility(context.Context, string, string, string, string) (Eligibility, error)
	Prepare(context.Context, Mutation, Reservation) (Reservation, bool, error)
	Get(context.Context, string, string) (Reservation, error)
	RecordSubmitted(context.Context, string, string) (Reservation, error)
	Confirm(context.Context, string, ChainResult) (Reservation, Assignment, bool, error)
	Fail(context.Context, string, string, string) (Reservation, bool, error)
	Expire(context.Context, string) (Reservation, bool, error)
}

type CapacityGateway interface {
	ReserveCapacity(context.Context, string, string, time.Duration) (agent.CapacityLease, error)
	ReleaseCapacity(context.Context, string, int64) error
}

type ProofSigner interface {
	Sign(Proof) (payloadHash string, digest string, signature string, err error)
}

type ChainVerifier interface {
	VerifySelection(context.Context, string) (ChainResult, error)
}

// PendingChainVerifier is the fail-closed fallback for deployments that do not
// install an authoritative chain receipt/event projector.
type PendingChainVerifier struct{}

func (PendingChainVerifier) VerifySelection(context.Context, string) (ChainResult, error) {
	return ChainResult{}, ErrDependencyPending
}

package execution

import (
	"context"
	"errors"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
)

var (
	ErrInvalidInput    = errors.New("invalid execution input")
	ErrNotFound        = errors.New("logical execution not found")
	ErrInvalidState    = errors.New("invalid execution state")
	ErrStaleFence      = errors.New("stale execution fencing token")
	ErrInvalidCallback = errors.New("invalid signed callback")
	ErrCallbackReplay  = errors.New("callback nonce already consumed")
	ErrContentConflict = errors.New("idempotent execution returned conflicting content")
	ErrCostCapExceeded = errors.New("execution cost cap exceeded")
)

const (
	ProtocolVersion = "agent-execution-v1"
	StageOverview   = "overview"
	StageFormal     = "formal"

	ToolPolicyReadOnly = "read_only"
	ToolPolicyScoped   = "scoped"

	ExecutionPending         = "pending"
	ExecutionRunning         = "running"
	ExecutionCancelRequested = "cancel_requested"
	ExecutionCancelled       = "cancelled"
	ExecutionSucceeded       = "succeeded"
	ExecutionFailed          = "failed"
	ExecutionCostStopped     = "cost_stopped"

	AttemptPrepared  = "prepared"
	AttemptActive    = "active"
	AttemptCompleted = "completed"
	AttemptFailed    = "failed"
	AttemptExpired   = "expired"
	AttemptCancelled = "cancelled"

	CallbackRunning   = "running"
	CallbackSucceeded = "succeeded"
	CallbackFailed    = "failed"

	CallbackAccepted   = "accepted"
	CallbackLate       = "late"
	CallbackStaleFence = "stale_fence"
	CallbackCostStop   = "cost_stop"
)

type ToolPolicy struct {
	Mode         string   `json:"mode"`
	AllowedTools []string `json:"allowedTools"`
}

type OverviewBinding struct {
	MatchRevision int    `json:"matchRevision"`
	AllocationID  string `json:"allocationId"`
	QuoteHash     string `json:"quoteHash"`
}

type FormalBinding struct {
	AssignmentID     string `json:"assignmentId"`
	Package          string `json:"package"`
	Version          int    `json:"version"`
	AggregateVersion int64  `json:"aggregateVersion"`
	WorkNonce        int64  `json:"workNonce"`
}

type Spec struct {
	LogicalExecutionID string           `json:"logicalExecutionId"`
	Stage              string           `json:"stage"`
	TaskID             string           `json:"taskId"`
	TaskSpecHash       string           `json:"taskSpecHash"`
	AgentID            string           `json:"agentId"`
	AgentEndpoint      string           `json:"agentEndpoint"`
	ResponsibilityCode string           `json:"responsibilityCode"`
	CostCap            string           `json:"costCap"`
	ToolPolicy         ToolPolicy       `json:"toolPolicy"`
	Deadline           time.Time        `json:"deadline"`
	IdempotencyKey     string           `json:"idempotencyKey"`
	Overview           *OverviewBinding `json:"overview,omitempty"`
	Formal             *FormalBinding   `json:"formal,omitempty"`
}

type Execution struct {
	Spec           Spec       `json:"spec"`
	Status         string     `json:"status"`
	CurrentAttempt int        `json:"currentAttempt"`
	UsedCost       string     `json:"usedCost"`
	ContentHash    string     `json:"contentHash,omitempty"`
	DeliverableRef string     `json:"deliverableRef,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	CancelledAt    *time.Time `json:"cancelledAt,omitempty"`
}

type Attempt struct {
	LogicalExecutionID string     `json:"logicalExecutionId"`
	Number             int        `json:"number"`
	AttemptID          string     `json:"attemptId"`
	ReservationID      string     `json:"reservationId"`
	Status             string     `json:"status"`
	FencingToken       int64      `json:"fencingToken"`
	LeaseExpiresAt     time.Time  `json:"leaseExpiresAt"`
	CallbackNonceHash  string     `json:"callbackNonceHash"`
	NonceKeyVersion    string     `json:"nonceKeyVersion"`
	DispatchCount      int        `json:"dispatchCount"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	TerminalAt         *time.Time `json:"terminalAt,omitempty"`
}

type Envelope struct {
	ProtocolVersion    string           `json:"protocolVersion"`
	Operation          string           `json:"operation"`
	Stage              string           `json:"stage"`
	LogicalExecutionID string           `json:"logicalExecutionId"`
	AttemptID          string           `json:"attemptId"`
	AgentID            string           `json:"agentId"`
	TaskID             string           `json:"taskId"`
	TaskSpecHash       string           `json:"taskSpecHash"`
	ResponsibilityCode string           `json:"responsibilityCode"`
	CostCap            string           `json:"costCap"`
	ToolPolicy         ToolPolicy       `json:"toolPolicy"`
	Deadline           time.Time        `json:"deadline"`
	IdempotencyKey     string           `json:"idempotencyKey"`
	CallbackURL        string           `json:"callbackUrl"`
	CallbackNonce      string           `json:"callbackNonce"`
	FencingToken       int64            `json:"fencingToken"`
	Overview           *OverviewBinding `json:"overview,omitempty"`
	Formal             *FormalBinding   `json:"formal,omitempty"`
}

type CreateResponse struct {
	Accepted bool   `json:"accepted"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

type StatusResponse struct {
	Status   string `json:"status"`
	UsedCost string `json:"usedCost"`
}

type CancelResponse struct {
	Accepted bool `json:"accepted"`
}

type DeliverableResponse struct {
	ContentHash    string `json:"contentHash"`
	DeliverableRef string `json:"deliverableRef"`
}

type Client interface {
	Create(context.Context, string, Envelope) (CreateResponse, error)
	Status(context.Context, string, Envelope) (StatusResponse, error)
	Cancel(context.Context, string, Envelope) (CancelResponse, error)
	Deliverable(context.Context, string, Envelope) (DeliverableResponse, error)
}

type CapacityLeaser interface {
	ReserveCapacity(context.Context, string, string, time.Duration) (agent.CapacityLease, error)
	ReleaseCapacity(context.Context, string, int64) error
}

type Callback struct {
	ProtocolVersion    string    `json:"protocolVersion"`
	LogicalExecutionID string    `json:"logicalExecutionId"`
	AttemptID          string    `json:"attemptId"`
	AgentID            string    `json:"agentId"`
	FencingToken       int64     `json:"fencingToken"`
	Status             string    `json:"status"`
	UsedCost           string    `json:"usedCost"`
	ContentHash        string    `json:"contentHash,omitempty"`
	DeliverableRef     string    `json:"deliverableRef,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
	Nonce              string    `json:"nonce"`
	KeyVersion         string    `json:"keyVersion"`
}

type VerifiedCallback struct {
	Callback    Callback
	NonceHash   string
	PayloadHash string
}

type CallbackResult struct {
	Execution    Execution `json:"execution"`
	Outcome      string    `json:"outcome"`
	Replay       bool      `json:"replay"`
	ShouldCancel bool      `json:"shouldCancel"`
}

type Repository interface {
	GetOrCreate(context.Context, Spec) (Execution, bool, error)
	Get(context.Context, string) (Execution, error)
	CurrentAttempt(context.Context, string) (Attempt, error)
	PrepareAttempt(context.Context, string, time.Duration) (Execution, Attempt, bool, error)
	ActivateAttempt(context.Context, string, int, agent.CapacityLease, string, string) (Execution, Attempt, error)
	RecordDispatch(context.Context, string, int) error
	FailAttempt(context.Context, string, int, int64, string) error
	RequestCancel(context.Context, string) (Execution, Attempt, bool, error)
	CompleteCancel(context.Context, string, int, int64) (Execution, error)
	RecordUsage(context.Context, string, int, int64, string) (Execution, bool, error)
	ApplyCallback(context.Context, VerifiedCallback) (CallbackResult, error)
}

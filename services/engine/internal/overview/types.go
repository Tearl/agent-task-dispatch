package overview

import (
	"context"
	"errors"
	"time"

	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/matching"
)

var (
	ErrInvalidInput      = errors.New("invalid overview input")
	ErrNotFound          = errors.New("overview batch not found")
	ErrContentConflict   = errors.New("overview idempotency content conflict")
	ErrInvalidState      = errors.New("invalid overview state")
	ErrObsolete          = errors.New("overview batch is obsolete")
	ErrDependencyPending = errors.New("overview dependency is not ready")
)

const (
	OrchestrationVersion = "overview-orchestration-v1"
	ResultSchemaVersion  = "overview-result-v1"
	ReplacementVersion   = "overview-replacement-v1"

	BatchRunning   = "running"
	BatchCompleted = "completed"
	BatchObsolete  = "obsolete"

	SlotPlanned    = "planned"
	SlotDispatched = "dispatched"
	SlotValid      = "valid"
	SlotInvalid    = "invalid"
	SlotFailed     = "failed"
	SlotObsolete   = "obsolete"

	BillingAuthorized = "authorized"
	BillingCaptured   = "captured"
	BillingReleased   = "released"
)

type BriefHandle struct {
	Ref       string
	Hash      string
	ExpiresAt time.Time
}

// BriefProvider owns sanitization and short-lived access. The overview
// orchestrator never accepts or persists the full task input.
type BriefProvider interface {
	PrepareOverviewBrief(context.Context, string, string, time.Time) (BriefHandle, error)
}

type DispatchTarget struct {
	AgentID         string
	ProviderID      string
	Endpoint        string
	PriceVersion    int
	OverviewPrice   string
	ExternalCostCap string
	QuoteHash       string
}

type TargetResolver interface {
	ResolveOverviewTarget(context.Context, string, int) (DispatchTarget, error)
}

type AllocationRequest struct {
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

type Allocation struct {
	ID      string
	CostCap string
}

type BillingClaim struct {
	TaskID             string
	TaskSpecHash       string
	MatchRevision      int
	LogicalExecutionID string
	AgentID            string
	QuoteHash          string
	ContentHash        string
	Amount             string
	UsedCost           string
}

// AllocationGateway is implemented by T-401. Allocation ID is the idempotency
// boundary: authorize, capture, and release must each be replay-safe.
type AllocationGateway interface {
	AuthorizeOverview(context.Context, AllocationRequest) (Allocation, bool, error)
	CaptureOverview(context.Context, string, BillingClaim) (bool, error)
	ReleaseOverview(context.Context, string, string) (bool, error)
}

type ArtifactReader interface {
	Read(context.Context, string, string, int64) ([]byte, error)
}

type ToolEvidence struct {
	Complete              bool
	Tools                 []string
	ExternalWriteAttempts int
}

type ToolEvidenceReader interface {
	Evidence(context.Context, string) (ToolEvidence, error)
}

type SnapshotReader interface {
	Get(context.Context, string) (matching.Snapshot, error)
}

type ExecutionGateway interface {
	Create(context.Context, execution.Spec) (execution.Execution, bool, error)
	Dispatch(context.Context, string) (execution.Execution, execution.Attempt, bool, error)
	Get(context.Context, string) (execution.Execution, error)
	Deliverable(context.Context, string) (execution.DeliverableResponse, error)
	Cancel(context.Context, string) (execution.Execution, error)
}

type Validation struct {
	Valid bool     `json:"valid"`
	Codes []string `json:"codes"`
}

type Slot struct {
	ID                 string     `json:"id"`
	BatchID            string     `json:"batchId"`
	Ordinal            int        `json:"ordinal"`
	SourcePosition     int        `json:"sourcePosition"`
	Replacement        bool       `json:"replacement"`
	AgentID            string     `json:"agentId"`
	ProviderID         string     `json:"providerId"`
	PriceVersion       int        `json:"priceVersion"`
	QuoteHash          string     `json:"quoteHash"`
	OverviewPrice      string     `json:"overviewPrice"`
	ExternalCostCap    string     `json:"externalCostCap"`
	AllocationID       string     `json:"allocationId"`
	LogicalExecutionID string     `json:"logicalExecutionId"`
	Status             string     `json:"status"`
	BillingStatus      string     `json:"billingStatus"`
	Validation         Validation `json:"validation"`
	ContentHash        string     `json:"contentHash,omitempty"`
	DeliverableRef     string     `json:"deliverableRef,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

type Batch struct {
	ID                   string    `json:"id"`
	SnapshotID           string    `json:"snapshotId"`
	TaskID               string    `json:"taskId"`
	TaskSpecHash         string    `json:"taskSpecHash"`
	MatchRevision        int       `json:"matchRevision"`
	AlgorithmVersion     string    `json:"algorithmVersion"`
	BriefRef             string    `json:"briefRef"`
	BriefHash            string    `json:"briefHash"`
	Deadline             time.Time `json:"deadline"`
	Status               string    `json:"status"`
	ReplacementUsed      bool      `json:"replacementUsed"`
	ReplacementExhausted bool      `json:"replacementExhausted"`
	Slots                []Slot    `json:"slots"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

type StartRequest struct {
	SnapshotID string
	Deadline   time.Time
}

type Repository interface {
	GetOrCreate(context.Context, Batch) (Batch, bool, error)
	Get(context.Context, string) (Batch, error)
	RecordDispatched(context.Context, string, string) (Batch, error)
	RecordValidation(context.Context, string, string, Validation, string, string) (Batch, Slot, bool, error)
	RecordBilling(context.Context, string, string, string) (Batch, error)
	AddReplacement(context.Context, string, Slot) (Batch, bool, error)
	ExhaustReplacement(context.Context, string) (Batch, error)
	MarkObsoleteBefore(context.Context, string, int) ([]Batch, error)
}

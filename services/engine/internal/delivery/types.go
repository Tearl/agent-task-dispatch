package delivery

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden         = errors.New("formal delivery operation forbidden")
	ErrInvalidInput      = errors.New("invalid formal delivery input")
	ErrNotFound          = errors.New("formal package not found")
	ErrInvalidState      = errors.New("invalid formal delivery state")
	ErrStaleVersion      = errors.New("stale formal package version")
	ErrContentConflict   = errors.New("formal delivery content conflict")
	ErrDependencyPending = errors.New("formal delivery dependency pending")
)

const (
	ProtocolVersion  = "formal-delivery-v1"
	ScopeVersion     = "formal-scope-v1"
	FeedbackVersion  = "formal-feedback-v1"
	ProofVersion     = "formal-proof-v1"
	StandardPackage  = "standard"
	IncludedVersions = 3
	MaximumVersions  = 5

	PackageActive = "active"

	VersionAllocated  = "allocated"
	VersionGenerating = "generating"
	VersionReview     = "review"
	VersionFailed     = "failed"

	ResultSucceeded = "succeeded"
	ResultFailed    = "failed"

	ChangeOrderVersion          = "formal-change-order-v1"
	ResponsibilityPublisher     = "publisher"
	ResponsibilityAgent         = "agent"
	ResponsibilityPlatform      = "platform"
	FundingPublisher            = "publisher"
	FundingAgentAbsorbed        = "agent_absorbed"
	FundingPlatformIncident     = "platform_incident"
	ChangeResponsibilityPending = "responsibility_pending"
	ChangeAwaitingAcceptance    = "awaiting_acceptance"
	ChangeAwaitingFunding       = "awaiting_funding"
	ChangeReady                 = "ready_to_activate"
	ChangeEffective             = "effective"
	ChangeConsumed              = "consumed"
	AcceptanceVersion           = "formal-acceptance-v1"
	AcceptanceIntentRecorded    = "intent_recorded"
	AcceptancePending           = "pending_confirmation"
	AcceptanceConfirmed         = "confirmed"
	AcceptanceOrphaned          = "orphaned"
)

type AcceptanceIntentInput struct {
	PackageID              string `json:"packageId"`
	ExpectedPackageVersion int64  `json:"expectedPackageVersion"`
	FormalVersion          int    `json:"formalVersion"`
	ContentHash            string `json:"contentHash"`
	ProofDigest            string `json:"proofDigest"`
	WorkNonce              uint64 `json:"workNonce"`
}

type AcceptanceTransitionInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	TransactionHash string `json:"transactionHash,omitempty"`
}

type SettlementEligibility struct {
	Eligible   bool   `json:"eligible"`
	ReasonCode string `json:"reasonCode,omitempty"`
}

type AcceptanceIntent struct {
	ID                      string                `json:"id"`
	PackageID               string                `json:"packageId"`
	TaskID                  string                `json:"taskId"`
	FormalVersion           int                   `json:"formalVersion"`
	ContentHash             string                `json:"contentHash"`
	ProofDigest             string                `json:"proofDigest"`
	WorkNonce               uint64                `json:"workNonce"`
	PackageAggregateVersion int64                 `json:"packageAggregateVersion"`
	AggregateVersion        int64                 `json:"aggregateVersion"`
	State                   string                `json:"state"`
	TransactionHash         string                `json:"transactionHash,omitempty"`
	ChainEventID            string                `json:"chainEventId,omitempty"`
	ChainID                 string                `json:"chainId"`
	ContractAddress         string                `json:"contractAddress"`
	PublisherWallet         string                `json:"publisherWallet"`
	ChainTaskID             string                `json:"chainTaskId"`
	Eligibility             SettlementEligibility `json:"settlementEligibility"`
	CreatedAt               time.Time             `json:"createdAt"`
	UpdatedAt               time.Time             `json:"updatedAt"`
}

type RevisionBinding struct {
	ParentVersion            int    `json:"parentVersion"`
	ParentContentHash        string `json:"parentContentHash"`
	FeedbackSetID            string `json:"feedbackSetId"`
	FeedbackDigest           string `json:"feedbackDigest"`
	FeedbackAggregateVersion int64  `json:"feedbackAggregateVersion"`
}

type FeedbackItemInput struct {
	CriterionID     string `json:"criterionId"`
	Category        string `json:"category"`
	Priority        string `json:"priority"`
	Target          string `json:"target"`
	Description     string `json:"description"`
	ExpectedOutcome string `json:"expectedOutcome"`
	ScopeClaim      string `json:"scopeClaim"`
}

type FeedbackInput struct {
	PackageID              string              `json:"packageId"`
	ExpectedPackageVersion int64               `json:"expectedPackageVersion"`
	ParentVersion          int                 `json:"parentVersion"`
	ParentContentHash      string              `json:"parentContentHash"`
	Items                  []FeedbackItemInput `json:"items"`
}

type FeedbackItem struct {
	ID      string `json:"id"`
	Ordinal int    `json:"ordinal"`
	FeedbackItemInput
}

type FeedbackSet struct {
	ID                      string         `json:"id"`
	PackageID               string         `json:"packageId"`
	ParentVersion           int            `json:"parentVersion"`
	ParentContentHash       string         `json:"parentContentHash"`
	ScopeID                 string         `json:"scopeId"`
	ScopeHash               string         `json:"scopeHash"`
	Digest                  string         `json:"digest"`
	PackageAggregateVersion int64          `json:"packageAggregateVersion"`
	Items                   []FeedbackItem `json:"items"`
	CreatedAt               time.Time      `json:"createdAt"`
}

type FeedbackResponse struct {
	FeedbackItemID string `json:"feedbackItemId"`
	Disposition    string `json:"disposition"`
	Summary        string `json:"summary"`
}

type Change struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	BeforeHash string `json:"beforeHash,omitempty"`
	AfterHash  string `json:"afterHash,omitempty"`
}

type Proof struct {
	Version                 string `json:"version"`
	TaskID                  string `json:"taskId"`
	AssignmentID            string `json:"assignmentId"`
	DeliveryUnit            string `json:"deliveryUnit"`
	PackageID               string `json:"packageId"`
	ScopeHash               string `json:"scopeHash"`
	FormalVersion           int    `json:"formalVersion"`
	PackageAggregateVersion int64  `json:"packageAggregateVersion"`
	WorkNonce               uint64 `json:"workNonce"`
	AgentID                 string `json:"agentId"`
	ContentHash             string `json:"contentHash"`
	ParentContentHash       string `json:"parentContentHash,omitempty"`
	FeedbackDigest          string `json:"feedbackDigest,omitempty"`
	ChangeOrderID           string `json:"changeOrderId,omitempty"`
	AgentResponseHash       string `json:"agentResponseHash"`
	ChangeSummaryHash       string `json:"changeSummaryHash"`
	PolicyHash              string `json:"policyHash"`
	Deadline                int64  `json:"deadline"`
}

type ProofRecord struct {
	Proof       Proof  `json:"proof"`
	PayloadHash string `json:"payloadHash"`
	Digest      string `json:"digest"`
	Signature   string `json:"signature"`
}

type StartInput struct {
	ExpectedPackageVersion int64            `json:"expectedPackageVersion"`
	WorkNonce              uint64           `json:"workNonce"`
	Revision               *RevisionBinding `json:"revision,omitempty"`
	ChangeOrderID          string           `json:"changeOrderId,omitempty"`
}

type ScopeDifference struct {
	Path                 string `json:"path"`
	Kind                 string `json:"kind"`
	BeforeHash           string `json:"beforeHash,omitempty"`
	AfterHash            string `json:"afterHash,omitempty"`
	Description          string `json:"description"`
	WorkloadDeltaPercent int    `json:"workloadDeltaPercent"`
}

type ProposeChangeOrderInput struct {
	PackageID              string            `json:"packageId"`
	ExpectedPackageVersion int64             `json:"expectedPackageVersion"`
	TriggerVersion         int               `json:"triggerVersion"`
	TriggerContentHash     string            `json:"triggerContentHash"`
	FeedbackSetID          string            `json:"feedbackSetId"`
	FeedbackDigest         string            `json:"feedbackDigest"`
	NewSpecHash            string            `json:"newSpecHash"`
	Differences            []ScopeDifference `json:"differences"`
	RequestedPrice         string            `json:"requestedPrice"`
	Deadline               time.Time         `json:"deadline"`
}

type DecideChangeOrderInput struct {
	ExpectedVersion                  int64  `json:"expectedVersion"`
	Responsibility                   string `json:"responsibility"`
	ReasonCode                       string `json:"reasonCode"`
	PublisherCompensationIrrevocable bool   `json:"publisherCompensationIrrevocable"`
}

type ChangeOrderVersionInput struct {
	ExpectedVersion int64 `json:"expectedVersion"`
}

type ChangeOrder struct {
	ID                               string            `json:"id"`
	PackageID                        string            `json:"packageId"`
	TaskID                           string            `json:"taskId"`
	TargetVersion                    int               `json:"targetVersion"`
	TriggerVersion                   int               `json:"triggerVersion"`
	TriggerContentHash               string            `json:"triggerContentHash"`
	FeedbackSetID                    string            `json:"feedbackSetId"`
	FeedbackDigest                   string            `json:"feedbackDigest"`
	BaseScopeID                      string            `json:"baseScopeId"`
	BaseScopeHash                    string            `json:"baseScopeHash"`
	NewScopeID                       string            `json:"newScopeId,omitempty"`
	NewScopeHash                     string            `json:"newScopeHash,omitempty"`
	NewScopeRevision                 int               `json:"newScopeRevision,omitempty"`
	NewSpecHash                      string            `json:"newSpecHash"`
	DifferenceDigest                 string            `json:"differenceDigest"`
	Differences                      []ScopeDifference `json:"differences"`
	RequestedPrice                   string            `json:"requestedPrice"`
	AuthorizedPrice                  string            `json:"authorizedPrice"`
	Responsibility                   string            `json:"responsibility,omitempty"`
	ResponsibilityReasonCode         string            `json:"responsibilityReasonCode,omitempty"`
	FundingSource                    string            `json:"fundingSource,omitempty"`
	FundAccountID                    string            `json:"fundAccountId,omitempty"`
	PrincipalOwnerID                 string            `json:"principalOwnerId,omitempty"`
	ResidualRecipientID              string            `json:"residualRecipientId,omitempty"`
	PublisherCompensationIrrevocable bool              `json:"publisherCompensationIrrevocable"`
	PackageAggregateVersion          int64             `json:"packageAggregateVersion"`
	AggregateVersion                 int64             `json:"aggregateVersion"`
	Status                           string            `json:"status"`
	Deadline                         time.Time         `json:"deadline"`
	AcceptedAt                       *time.Time        `json:"acceptedAt,omitempty"`
	EffectiveAt                      *time.Time        `json:"effectiveAt,omitempty"`
	ConsumedAt                       *time.Time        `json:"consumedAt,omitempty"`
	CreatedAt                        time.Time         `json:"createdAt"`
	UpdatedAt                        time.Time         `json:"updatedAt"`
}

type Scope struct {
	ID                 string            `json:"id"`
	PackageID          string            `json:"packageId"`
	Revision           int               `json:"revision"`
	ContentHash        string            `json:"contentHash"`
	TaskSpecHash       string            `json:"taskSpecHash"`
	SelectedOverviewID string            `json:"selectedOverviewId"`
	OverviewHash       string            `json:"overviewHash"`
	OverviewRef        string            `json:"overviewRef"`
	Inputs             []string          `json:"inputs"`
	AcceptanceHash     string            `json:"acceptanceHash"`
	AcceptanceCriteria []map[string]any  `json:"acceptanceCriteria"`
	OutputConstraints  map[string]any    `json:"outputConstraints"`
	AllowedTools       []string          `json:"allowedTools"`
	ExternalCostCap    string            `json:"externalCostCap"`
	Exclusions         []string          `json:"exclusions"`
	ChangeOrderID      string            `json:"changeOrderId,omitempty"`
	Differences        []ScopeDifference `json:"differences,omitempty"`
	CreatedAt          time.Time         `json:"createdAt"`
}

type Package struct {
	ID               string    `json:"id"`
	TaskID           string    `json:"taskId"`
	AssignmentID     string    `json:"assignmentId"`
	DeliveryUnit     string    `json:"deliveryUnit"`
	Kind             string    `json:"kind"`
	ScopeID          string    `json:"scopeId"`
	ScopeRevision    int       `json:"scopeRevision"`
	AgentID          string    `json:"agentId"`
	ProviderID       string    `json:"providerId"`
	PublisherID      string    `json:"publisherId"`
	IncludedVersions int       `json:"includedVersions"`
	MaximumVersions  int       `json:"maximumVersions"`
	AllocatedVersion int       `json:"allocatedVersion"`
	AggregateVersion int64     `json:"aggregateVersion"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Version struct {
	PackageID          string             `json:"packageId"`
	Number             int                `json:"number"`
	AggregateVersion   int64              `json:"aggregateVersion"`
	ScopeID            string             `json:"scopeId"`
	ScopeHash          string             `json:"scopeHash"`
	WorkNonce          uint64             `json:"workNonce"`
	Revision           *RevisionBinding   `json:"revision,omitempty"`
	ChangeOrderID      string             `json:"changeOrderId,omitempty"`
	LogicalExecutionID string             `json:"logicalExecutionId"`
	Status             string             `json:"status"`
	ContentHash        string             `json:"contentHash,omitempty"`
	DeliverableRef     string             `json:"deliverableRef,omitempty"`
	UsedCost           string             `json:"usedCost"`
	FailureReasonCode  string             `json:"failureReasonCode,omitempty"`
	ResultHash         string             `json:"-"`
	FeedbackResponses  []FeedbackResponse `json:"feedbackResponses,omitempty"`
	Changes            []Change           `json:"changes,omitempty"`
	Proof              *ProofRecord       `json:"proof,omitempty"`
	CreatedAt          time.Time          `json:"createdAt"`
	UpdatedAt          time.Time          `json:"updatedAt"`
}

type StartResult struct {
	Package Package `json:"package"`
	Scope   Scope   `json:"scope"`
	Version Version `json:"version"`
}

type View struct {
	Package      Package            `json:"package"`
	Scope        Scope              `json:"scope"`
	Versions     []Version          `json:"versions"`
	Feedback     []FeedbackSet      `json:"feedback"`
	ChangeOrders []ChangeOrder      `json:"changeOrders"`
	Acceptances  []AcceptanceIntent `json:"acceptances"`
	Chain        ChainBinding       `json:"chain"`
}

type ChainBinding struct {
	ChainID         string `json:"chainId"`
	ContractAddress string `json:"contractAddress"`
	PublisherWallet string `json:"publisherWallet"`
	TaskID          string `json:"taskId"`
	AssignmentID    string `json:"assignmentId"`
	WorkNonce       uint64 `json:"workNonce"`
}

type Mutation struct {
	PublisherID    string
	IdempotencyKey string
	RequestHash    string
}

type ExecutionResult struct {
	LogicalExecutionID string
	Status             string
	ContentHash        string
	DeliverableRef     string
	UsedCost           string
	FailureReasonCode  string
	FeedbackResponses  []FeedbackResponse
	Changes            []Change
}

type ProofContext struct {
	TaskID                  string
	AssignmentID            string
	DeliveryUnit            string
	PackageID               string
	ScopeHash               string
	Version                 int
	PackageAggregateVersion int64
	WorkNonce               uint64
	AgentID                 string
	ParentContentHash       string
	FeedbackDigest          string
	ChangeOrderID           string
	FeedbackItemIDs         []string
	PolicyHash              string
	Deadline                time.Time
}

type Repository interface {
	Start(context.Context, Mutation, string, StartInput) (StartResult, bool, error)
	SubmitFeedback(context.Context, Mutation, string, FeedbackInput, FeedbackSet) (FeedbackSet, bool, error)
	Get(context.Context, string, string) (View, error)
	ProofContext(context.Context, string) (ProofContext, error)
	RecordDispatched(context.Context, string) (Version, bool, error)
	RecordResult(context.Context, ExecutionResult, *ProofRecord) (Version, bool, error)
	ProposeChangeOrder(context.Context, Mutation, string, ProposeChangeOrderInput, ChangeOrder) (ChangeOrder, bool, error)
	DecideChangeOrder(context.Context, Mutation, bool, string, string, DecideChangeOrderInput) (ChangeOrder, bool, error)
	AcceptChangeOrder(context.Context, Mutation, string, string, ChangeOrderVersionInput) (ChangeOrder, bool, error)
	ActivateChangeOrder(context.Context, Mutation, bool, string, string, ChangeOrderVersionInput) (ChangeOrder, bool, error)
	CreateAcceptanceIntent(context.Context, Mutation, string, AcceptanceIntentInput, AcceptanceIntent) (AcceptanceIntent, bool, error)
	SubmitAcceptance(context.Context, Mutation, string, string, AcceptanceTransitionInput) (AcceptanceIntent, bool, error)
	ReconcileAcceptance(context.Context, Mutation, string, string, AcceptanceTransitionInput) (AcceptanceIntent, bool, error)
}

type RevisionAuthorizer interface {
	AuthorizeRevision(context.Context, string, string, StartInput) error
}

type PendingRevisionAuthorizer struct{}

func (PendingRevisionAuthorizer) AuthorizeRevision(context.Context, string, string, StartInput) error {
	return ErrDependencyPending
}

type ProofSigner interface {
	Sign(Proof) (payloadHash string, digest string, signature string, err error)
}

type PendingProofSigner struct{}

func (PendingProofSigner) Sign(Proof) (string, string, string, error) {
	return "", "", "", ErrDependencyPending
}

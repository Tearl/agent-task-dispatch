package dispute

import (
	"context"
	"errors"
	"time"
)

const (
	PolicyVersion = "platform-dispute-v1"
	StateSoftLock = "soft_lock_pending"
	StateFrozen   = "frozen"
	StateEvidence = "evidence"
	StateDecided  = "decided"
	StateReview   = "review_pending"
	StateFinal    = "final"
	StateOrphaned = "orphaned"
)

var (
	ErrInvalidInput       = errors.New("invalid dispute input")
	ErrForbidden          = errors.New("dispute operation forbidden")
	ErrNotFound           = errors.New("dispute not found")
	ErrInvalidState       = errors.New("invalid dispute state")
	ErrConflict           = errors.New("dispute conflict")
	ErrPending            = errors.New("chain confirmation pending")
	ErrEvidenceIncomplete = errors.New("evidence manifest incomplete")
)

type Mutation struct{ ActorID, IdempotencyKey, RequestHash string }

type Context struct {
	TaskID          string    `json:"taskId"`
	AssignmentID    string    `json:"assignmentId"`
	DeliveryUnitID  string    `json:"deliveryUnitId"`
	PublisherID     string    `json:"publisherId"`
	AgentProviderID string    `json:"agentProviderId"`
	ChainID         string    `json:"chainId"`
	ContractAddress string    `json:"contractAddress"`
	ChainTaskID     string    `json:"chainTaskId"`
	PublisherWallet string    `json:"publisherWallet"`
	AgentController string    `json:"agentController"`
	AgentPayout     string    `json:"agentPayout"`
	DisputeResolver string    `json:"disputeResolver"`
	FrozenAmount    string    `json:"frozenAmount"`
	Asset           string    `json:"asset"`
	FeeCap          string    `json:"feeCap"`
	Eligible        bool      `json:"eligible"`
	ReasonCode      string    `json:"reasonCode,omitempty"`
	DisputeDeadline time.Time `json:"disputeDeadline"`
}

type Claim struct {
	ID            string    `json:"id"`
	Side          string    `json:"side"`
	Kind          string    `json:"kind"`
	ReasonCode    string    `json:"reasonCode"`
	StatementHash string    `json:"statementHash"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Evidence struct {
	ID                   string    `json:"id"`
	ClaimID              string    `json:"claimId"`
	Category             string    `json:"category"`
	ObjectKey            string    `json:"objectKey"`
	CiphertextDigest     string    `json:"ciphertextDigest"`
	EnvelopeKeyReference string    `json:"envelopeKeyReference"`
	ObjectVersionID      string    `json:"objectVersionId"`
	RetentionMode        string    `json:"retentionMode"`
	RetainUntil          time.Time `json:"retainUntil"`
	CreatedAt            time.Time `json:"createdAt"`
	SubmittedBy          string    `json:"submittedBy"`
}

type AccessGrant struct {
	ID          string    `json:"id"`
	EvidenceID  string    `json:"evidenceId"`
	PrincipalID string    `json:"principalId"`
	Purpose     string    `json:"purpose"`
	ExpiresAt   time.Time `json:"expiresAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Assignment struct {
	ID                string    `json:"id"`
	Stage             string    `json:"stage"`
	AssigneeID        string    `json:"assigneeId"`
	ConflictCheckedAt time.Time `json:"conflictCheckedAt"`
	AssignedAt        time.Time `json:"assignedAt"`
}

type Decision struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	DecidedBy    string    `json:"decidedBy"`
	ReasonCode   string    `json:"reasonCode"`
	EvidenceRoot string    `json:"evidenceRoot"`
	PublisherBPS int       `json:"publisherBps"`
	CreatedAt    time.Time `json:"createdAt"`
}

type FrozenLeaf struct {
	Index   int    `json:"index"`
	Owner   string `json:"owner"`
	Account string `json:"account"`
	Cap     string `json:"cap"`
	Kind    string `json:"kind"`
}

type AdminOperation struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	ResourceType string    `json:"resourceType"`
	ResourceID   string    `json:"resourceId"`
	ReasonCode   string    `json:"reasonCode"`
	PayloadHash  string    `json:"payloadHash"`
	ActorID      string    `json:"actorId"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Case struct {
	ID                    string       `json:"id"`
	TaskID                string       `json:"taskId"`
	AssignmentID          string       `json:"assignmentId"`
	DeliveryUnitID        string       `json:"deliveryUnitId"`
	PolicyVersion         string       `json:"policyVersion"`
	PublisherID           string       `json:"publisherId"`
	AgentProviderID       string       `json:"agentProviderId"`
	State                 string       `json:"state"`
	AggregateVersion      int          `json:"aggregateVersion"`
	SoftLockedAt          *time.Time   `json:"softLockedAt,omitempty"`
	FreezeSubmittedAt     *time.Time   `json:"freezeSubmittedAt,omitempty"`
	FrozenAt              *time.Time   `json:"frozenAt,omitempty"`
	FreezeTransactionHash string       `json:"freezeTransactionHash,omitempty"`
	FreezeEventID         string       `json:"freezeEventId,omitempty"`
	FreezeRoot            string       `json:"freezeRoot,omitempty"`
	FrozenAmount          string       `json:"frozenAmount"`
	Asset                 string       `json:"asset"`
	EvidenceDeadline      time.Time    `json:"evidenceDeadline,omitempty"`
	DecisionDeadline      time.Time    `json:"decisionDeadline,omitempty"`
	ReviewDeadline        time.Time    `json:"reviewDeadline,omitempty"`
	Claims                []Claim      `json:"claims"`
	Evidence              []Evidence   `json:"evidence"`
	Assignments           []Assignment `json:"assignments"`
	Decisions             []Decision   `json:"decisions"`
	Leaves                []FrozenLeaf `json:"leaves"`
	ReputationPending     bool         `json:"reputationPending"`
	FinalizedAt           *time.Time   `json:"finalizedAt,omitempty"`
	CreatedAt             time.Time    `json:"createdAt"`
	UpdatedAt             time.Time    `json:"updatedAt"`
}

type View struct {
	Case            Case             `json:"case"`
	Context         Context          `json:"context"`
	AccessGrants    []AccessGrant    `json:"accessGrants"`
	AdminOperations []AdminOperation `json:"adminOperations"`
}

type OpenInput struct{ DeliveryUnitID, Kind, ReasonCode, StatementHash string }
type ClaimInput struct{ Kind, ReasonCode, StatementHash string }
type FreezeInput struct{ TransactionHash string }
type EvidenceInput struct {
	ClaimID, Category, ObjectKey, CiphertextDigest, EnvelopeKeyReference, ObjectVersionID, RetentionMode string
	RetainUntil                                                                                          time.Time
}
type AccessInput struct {
	EvidenceID, Purpose string
	TTLSeconds          int
}
type AssignInput struct {
	AssigneeID, Stage string
	// ConflictPartyIDs is accepted for backwards-compatible clients but never
	// trusted; repositories check append-only conflict declarations instead.
	ConflictPartyIDs []string
}
type DecisionInput struct {
	PublisherBPS             int
	ReasonCode, EvidenceRoot string
}
type SettlementInput struct {
	PublisherBPS       int    `json:"publisherBps"`
	ReasonCode         string `json:"reasonCode"`
	EvidenceRoot       string `json:"evidenceRoot"`
	AgreementHash      string `json:"agreementHash"`
	PublisherSignature string `json:"publisherSignature"`
	AgentSignature     string `json:"agentSignature"`
	Verified           bool   `json:"-"`
}
type ReviewInput struct {
	AssigneeID               string
	FeeAuthorized            bool
	ReasonCode, EvidenceRoot string
	PublisherBPS             int
}
type AdminInput struct {
	Kind, ResourceType, ResourceID, ReasonCode string
	Payload                                    map[string]any
}

type Command struct {
	Kind  string
	Input any
}

type Repository interface {
	Context(context.Context, string) (Context, error)
	HasConflict(context.Context, string, string) (bool, error)
	HasReviewFeeAuthorization(context.Context, string, string) (bool, error)
	AuditAccessDenial(context.Context, string, string, string, AccessInput) error
	VerifyEvidence(context.Context, EvidenceInput) error
	Execute(context.Context, Mutation, Context, string, Command) (View, bool, error)
	Get(context.Context, string, string) (View, error)
	List(context.Context, string, []string) ([]View, error)
}

package workflow

import (
	"errors"
	"time"

	"github.com/example/agent-platform/engine/internal/overview"
)

var (
	ErrForbidden         = errors.New("workflow operation forbidden")
	ErrNotFound          = errors.New("workflow resource not found")
	ErrInvalidInput      = errors.New("invalid workflow input")
	ErrDependencyPending = errors.New("workflow dependency is not ready")
)

type ExecutionView struct {
	LogicalExecutionID string    `json:"logicalExecutionId"`
	Stage              string    `json:"stage"`
	AgentID            string    `json:"agentId"`
	Status             string    `json:"status"`
	CurrentAttempt     int       `json:"currentAttempt"`
	UsedCost           string    `json:"usedCost"`
	CostCap            string    `json:"costCap"`
	ContentHash        string    `json:"contentHash,omitempty"`
	DeliverableRef     string    `json:"deliverableRef,omitempty"`
	Deadline           time.Time `json:"deadline"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type StartMatchingResult struct {
	SnapshotID    string `json:"snapshotId"`
	MatchRevision int    `json:"matchRevision"`
	Qualified     int    `json:"qualified"`
	Selected      int    `json:"selected"`
	Replay        bool   `json:"replay"`
}

type StartOverviewResult struct {
	Batch  overview.Batch `json:"batch"`
	Replay bool           `json:"replay"`
}

package matchingview

import (
	"context"
	"errors"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

var (
	ErrForbidden = errors.New("matching view forbidden")
	ErrNotFound  = errors.New("matching view task not found")
	ErrInvalid   = errors.New("invalid matching view")
)

type Task struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	SpecHash        string `json:"specHash"`
	DeletionPending bool   `json:"deletionPending"`
}

type Score struct {
	TaskMatch    int `json:"taskMatch"`
	Reputation   int `json:"reputation"`
	PriceTime    int `json:"priceTime"`
	Availability int `json:"availability"`
	Rule         int `json:"rule"`
	ModelDelta   int `json:"modelDelta"`
	Ranking      int `json:"ranking"`
}

type Overview struct {
	SlotID             string   `json:"slotId"`
	LogicalExecutionID string   `json:"logicalExecutionId"`
	Status             string   `json:"status"`
	BillingStatus      string   `json:"billingStatus"`
	ValidationCodes    []string `json:"validationCodes"`
	ContentHash        string   `json:"contentHash,omitempty"`
	Replacement        bool     `json:"replacement"`
}

type Candidate struct {
	AgentID                 string    `json:"agentId"`
	Name                    string    `json:"name"`
	Category                string    `json:"category"`
	Tags                    []string  `json:"tags"`
	EstimatedDurationSecond int64     `json:"estimatedDurationSeconds"`
	Position                int       `json:"position"`
	Exploration             bool      `json:"exploration"`
	OverviewPrice           string    `json:"overviewPrice"`
	FormalPrice             string    `json:"formalPrice"`
	ExternalCostCap         string    `json:"externalCostCap"`
	Score                   Score     `json:"score"`
	Overview                *Overview `json:"overview,omitempty"`
}

type Degradation struct {
	Dependency string `json:"dependency"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

type Snapshot struct {
	ID                   string        `json:"id"`
	Revision             int           `json:"revision"`
	AlgorithmVersion     string        `json:"algorithmVersion"`
	RuleVersion          string        `json:"ruleVersion"`
	ModelVersion         string        `json:"modelVersion"`
	SeedDigest           string        `json:"seedDigest"`
	ExplorationTriggered bool          `json:"explorationTriggered"`
	Candidates           []Candidate   `json:"candidates"`
	Degradations         []Degradation `json:"degradations"`
	CreatedAt            time.Time     `json:"createdAt"`
}

type Batch struct {
	ID                   string    `json:"id"`
	Status               string    `json:"status"`
	Deadline             time.Time `json:"deadline"`
	ReplacementUsed      bool      `json:"replacementUsed"`
	ReplacementExhausted bool      `json:"replacementExhausted"`
}

type Reservation struct {
	ID              string `json:"id"`
	AgentID         string `json:"agentId"`
	SlotID          string `json:"slotId"`
	Status          string `json:"status"`
	TransactionHash string `json:"transactionHash,omitempty"`
}

type View struct {
	AsOf                 time.Time    `json:"asOf"`
	Task                 Task         `json:"task"`
	Snapshot             *Snapshot    `json:"snapshot,omitempty"`
	OverviewFundingReady bool         `json:"overviewFundingReady"`
	Batch                *Batch       `json:"batch,omitempty"`
	Reservation          *Reservation `json:"reservation,omitempty"`
}

type Repository interface {
	Get(context.Context, string, string) (View, error)
}

type Service struct{ repository Repository }

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, ErrInvalid
	}
	return &Service{repository: repository}, nil
}

func (service *Service) Get(ctx context.Context, session auth.Session, taskID string) (View, error) {
	if session.UserID == "" || taskID == "" || !hasRole(session.Roles, "publisher") {
		return View{}, ErrForbidden
	}
	return service.repository.Get(ctx, session.UserID, taskID)
}

func hasRole(roles []string, expected string) bool {
	for _, role := range roles {
		if role == expected {
			return true
		}
	}
	return false
}

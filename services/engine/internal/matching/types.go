package matching

import (
	"context"
	"time"
)

const (
	ChannelDense   = "dense"
	ChannelLexical = "lexical"
	ChannelExact   = "exact"

	MaxRecallPerChannel = 100
	MaxScoredCandidates = 100
	MaxQualifiedPool    = 20
	QualificationFloor  = 60
	MaximumScoreGap     = 10
)

type Request struct {
	TaskID                  string
	PublisherID             string
	Category                string
	Language                string
	Terms                   []string
	RequiredCapabilities    []string
	RequiredProtocolVersion string
	RequiredVectorVersion   string
	OverviewBudget          string
	FormalBudget            string
	ExternalCostCap         string
	Deadline                time.Time
	Now                     time.Time
}

type Reputation struct {
	Quality       int
	Speed         int
	Reliability   int
	Communication int
	Compliance    int
}

type Candidate struct {
	AgentID           string
	ProviderID        string
	Status            string
	ApprovalStatus    string
	Health            string
	HealthCheckedAt   *time.Time
	HealthValidUntil  *time.Time
	MaxConcurrency    int
	ActiveCapacity    int
	Category          string
	Languages         []string
	Tags              []string
	Capabilities      []string
	ProtocolVersion   string
	RiskStatus        string
	PayoutAddress     string
	VectorVersion     string
	EstimatedDuration time.Duration
	OverviewPrice     string
	FormalPrice       string
	ExternalCostCap   string
	PriceVersion      int
	ExposureCount     int
	EffectiveSamples  int
	Reputation        Reputation
}

type Reason struct {
	Code    string
	Message string
}

type Exclusion struct {
	AgentID   string
	Candidate Candidate
	Reasons   []Reason
}

type RecallHit struct {
	AgentID   string
	Relevance int // 0..10_000, fixed-point and deterministic.
}

type RecallAdapter interface {
	Channel() string
	Recall(context.Context, Request, []Candidate, int) ([]RecallHit, error)
}

type RecallEvidence struct {
	Rank      int
	Relevance int
}

type ScoreBreakdown struct {
	TaskMatch    int
	Reputation   int
	PriceTime    int
	Availability int
	RuleScore    int
	ModelDelta   int
	RankingScore int
}

type RankingAdjuster interface {
	Adjust(context.Context, Request, Candidate, ScoreBreakdown) (int, error)
}

type ScoredCandidate struct {
	Candidate Candidate
	Recall    map[string]RecallEvidence
	Score     ScoreBreakdown
}

type Degradation struct {
	Dependency string
	Code       string
	Message    string
}

type Result struct {
	Qualified    []ScoredCandidate
	Scored       []ScoredCandidate
	Excluded     []Exclusion
	Degradations []Degradation
}

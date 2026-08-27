package orchestration

import (
	"context"
	"errors"
	"time"
)

var (
	ErrForbidden                  = errors.New("orchestration operation forbidden")
	ErrNotFound                   = errors.New("orchestration plan not found")
	ErrInvalidInput               = errors.New("invalid orchestration input")
	ErrNotReady                   = errors.New("task is not ready for orchestration")
	ErrMultiAgentExecutionPending = errors.New("multi-agent execution settlement is not available")
)

type Task struct {
	ID, PublisherID, Status, SpecHash, Title, Description, Category, Language string
	Deliverables, AllowedTools                                                []string
	Deadline                                                                  time.Time
}

type Agent struct {
	AgentID, Category  string
	Tags, Capabilities []string
}

type Step struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Objective            string   `json:"objective"`
	RequiredCapabilities []string `json:"requiredCapabilities"`
	DependsOn            []string `json:"dependsOn"`
	Output               string   `json:"output"`
}

type Draft struct {
	Mode         string   `json:"mode"`
	Summary      string   `json:"summary"`
	Rationale    []string `json:"rationale"`
	Confidence   float64  `json:"confidence"`
	Steps        []Step   `json:"steps"`
	Model        string   `json:"model"`
	GraphVersion string   `json:"graphVersion"`
}

type Plan struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"taskId"`
	TaskSpecHash string    `json:"taskSpecHash"`
	Mode         string    `json:"mode"`
	Summary      string    `json:"summary"`
	Rationale    []string  `json:"rationale"`
	Confidence   float64   `json:"confidence"`
	Steps        []Step    `json:"steps"`
	Model        string    `json:"model"`
	GraphVersion string    `json:"graphVersion"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Planner interface {
	Plan(context.Context, Task, []Agent) (Draft, error)
}

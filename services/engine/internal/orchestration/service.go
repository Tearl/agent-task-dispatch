package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

type Service struct {
	store   Repository
	planner Planner
}

type Repository interface {
	Task(context.Context, string, string) (Task, error)
	Agents(context.Context) ([]Agent, error)
	Save(context.Context, string, string, string, Task, Draft) (Plan, bool, error)
	Latest(context.Context, string, string) (Plan, error)
	ByOperation(context.Context, string, string, string) (Plan, error)
}

func NewService(store Repository, planner Planner) (*Service, error) {
	if store == nil || planner == nil {
		return nil, ErrInvalidInput
	}
	return &Service{store: store, planner: planner}, nil
}
func (service *Service) Create(ctx context.Context, session auth.Session, operationID, taskID string) (Plan, bool, error) {
	if !hasRole(session, "publisher") {
		return Plan{}, false, ErrForbidden
	}
	if strings.TrimSpace(operationID) == "" || len(operationID) > 200 {
		return Plan{}, false, ErrInvalidInput
	}
	if existing, err := service.store.ByOperation(ctx, session.UserID, taskID, operationID); err == nil {
		return existing, true, nil
	}
	task, err := service.store.Task(ctx, session.UserID, taskID)
	if err != nil {
		return Plan{}, false, err
	}
	if task.Status != "escrowed" || !task.Deadline.After(time.Now()) {
		return Plan{}, false, ErrNotReady
	}
	agents, err := service.store.Agents(ctx)
	if err != nil {
		return Plan{}, false, err
	}
	payload, _ := json.Marshal(struct {
		SpecHash string
		Agents   []Agent
	}{task.SpecHash, agents})
	sum := sha256.Sum256(payload)
	inputHash := "sha256:" + hex.EncodeToString(sum[:])
	draft, err := service.planner.Plan(ctx, task, agents)
	if err != nil {
		return Plan{}, false, err
	}
	if validateDraft(draft, agents) != nil {
		return Plan{}, false, ErrInvalidInput
	}
	return service.store.Save(ctx, session.UserID, operationID, inputHash, task, draft)
}
func (service *Service) Latest(ctx context.Context, session auth.Session, taskID string) (Plan, error) {
	if !hasRole(session, "publisher") {
		return Plan{}, ErrForbidden
	}
	task, err := service.store.Task(ctx, session.UserID, taskID)
	if err != nil {
		return Plan{}, err
	}
	plan, err := service.store.Latest(ctx, session.UserID, taskID)
	if err == nil && plan.TaskSpecHash != task.SpecHash {
		return Plan{}, ErrNotFound
	}
	return plan, err
}
func validateDraft(d Draft, agents []Agent) error {
	if d.Mode != "single" && d.Mode != "multi" || d.Summary == "" || d.GraphVersion == "" || d.Model == "" || d.Confidence < 0 || d.Confidence > 1 || len(d.Steps) == 0 || d.Mode == "single" && len(d.Steps) != 1 || d.Mode == "multi" && len(d.Steps) < 2 {
		return ErrInvalidInput
	}
	seen := map[string]bool{}
	for _, s := range d.Steps {
		if s.ID == "" || s.Title == "" || s.Objective == "" || s.Output == "" || seen[s.ID] || len(s.RequiredCapabilities) == 0 {
			return ErrInvalidInput
		}
		for _, dep := range s.DependsOn {
			if !seen[dep] {
				return ErrInvalidInput
			}
		}
		seen[s.ID] = true
	}
	return nil
}
func hasRole(session auth.Session, role string) bool {
	for _, value := range session.Roles {
		if value == role {
			return true
		}
	}
	return false
}

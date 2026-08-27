package orchestration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

func TestPlanUsesBrowserContractFieldNames(t *testing.T) {
	encoded, err := json.Marshal(Plan{ID: "plan-1", TaskID: "task-1", Rationale: []string{"reason"}, Steps: []Step{}})
	if err != nil {
		t.Fatal(err)
	}
	value := string(encoded)
	for _, field := range []string{`"id"`, `"taskId"`, `"rationale"`, `"steps"`} {
		if !strings.Contains(value, field) {
			t.Fatalf("missing browser contract field %s in %s", field, value)
		}
	}
	if strings.Contains(value, `"ID"`) || strings.Contains(value, `"TaskID"`) {
		t.Fatalf("Go field names leaked into browser response: %s", value)
	}
}

type repositoryStub struct {
	task   Task
	agents []Agent
	plan   Plan
	saved  int
}

func (r *repositoryStub) Task(context.Context, string, string) (Task, error) { return r.task, nil }
func (r *repositoryStub) Agents(context.Context) ([]Agent, error)            { return r.agents, nil }
func (r *repositoryStub) Latest(context.Context, string, string) (Plan, error) {
	if r.plan.ID == "" {
		return Plan{}, ErrNotFound
	}
	return r.plan, nil
}
func (r *repositoryStub) ByOperation(context.Context, string, string, string) (Plan, error) {
	if r.plan.ID == "" {
		return Plan{}, ErrNotFound
	}
	return r.plan, nil
}
func (r *repositoryStub) Save(_ context.Context, _ string, _ string, _ string, task Task, draft Draft) (Plan, bool, error) {
	r.saved++
	r.plan = Plan{ID: "sha256:" + strings.Repeat("1", 64), TaskID: task.ID, TaskSpecHash: task.SpecHash, Mode: draft.Mode, Steps: draft.Steps}
	return r.plan, false, nil
}

type plannerStub struct {
	draft Draft
	calls int
}

func (p *plannerStub) Plan(context.Context, Task, []Agent) (Draft, error) {
	p.calls++
	return p.draft, nil
}

func TestCreateRequiresPublisherAndEscrow(t *testing.T) {
	repository := &repositoryStub{task: taskFixture("pending_escrow"), agents: agentFixtures()}
	planner := &plannerStub{draft: singleDraft()}
	service, _ := NewService(repository, planner)
	if _, _, err := service.Create(context.Background(), auth.Session{UserID: "publisher-1", Roles: []string{"agent"}}, "op-1", "task-1"); err != ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if _, _, err := service.Create(context.Background(), auth.Session{UserID: "publisher-1", Roles: []string{"publisher"}}, "op-1", "task-1"); err != ErrNotReady {
		t.Fatalf("expected escrow gate, got %v", err)
	}
	if planner.calls != 0 || repository.saved != 0 {
		t.Fatal("forbidden or unfunded task must not invoke planner or persistence")
	}
}

func TestCreateIsIdempotentAndPersistsValidatedPlan(t *testing.T) {
	repository := &repositoryStub{task: taskFixture("escrowed"), agents: agentFixtures()}
	planner := &plannerStub{draft: singleDraft()}
	service, _ := NewService(repository, planner)
	session := auth.Session{UserID: "publisher-1", Roles: []string{"publisher"}}
	first, replay, err := service.Create(context.Background(), session, "op-1", "task-1")
	if err != nil || replay || first.ID == "" {
		t.Fatalf("unexpected first result: %#v %v %v", first, replay, err)
	}
	second, replay, err := service.Create(context.Background(), session, "op-1", "task-1")
	if err != nil || !replay || second.ID != first.ID {
		t.Fatalf("unexpected replay: %#v %v %v", second, replay, err)
	}
	if planner.calls != 1 || repository.saved != 1 {
		t.Fatalf("expected one plan/save, got %d/%d", planner.calls, repository.saved)
	}
}

func TestCreatePlansBeforeAgentSupplyExists(t *testing.T) {
	repository := &repositoryStub{task: taskFixture("escrowed"), agents: nil}
	planner := &plannerStub{draft: singleDraft()}
	service, _ := NewService(repository, planner)
	plan, replay, err := service.Create(context.Background(), auth.Session{UserID: "publisher-1", Roles: []string{"publisher"}}, "op-empty-catalog", "task-1")
	if err != nil || replay || plan.ID == "" {
		t.Fatalf("task analysis must not depend on current agent supply: %#v %v %v", plan, replay, err)
	}
	if planner.calls != 1 || repository.saved != 1 {
		t.Fatalf("expected one plan/save, got %d/%d", planner.calls, repository.saved)
	}
}

func TestValidateDraftRejectsForwardDependency(t *testing.T) {
	draft := singleDraft()
	draft.Mode = "multi"
	draft.Steps = []Step{{ID: "step-1", Title: "first", Objective: "x", RequiredCapabilities: []string{"translation"}, DependsOn: []string{"step-2"}, Output: "x"}, {ID: "step-2", Title: "second", Objective: "x", RequiredCapabilities: []string{"translation"}, Output: "x"}}
	if validateDraft(draft, agentFixtures()) != ErrInvalidInput {
		t.Fatal("forward dependency must be rejected")
	}
}

func taskFixture(status string) Task {
	return Task{ID: "task-1", PublisherID: "publisher-1", Status: status, SpecHash: "sha256:" + strings.Repeat("a", 64), Title: "Translate", Description: "Translate document", Category: "translation", Language: "en", Deliverables: []string{"document"}, Deadline: time.Now().Add(time.Hour)}
}
func agentFixtures() []Agent {
	return []Agent{{AgentID: "agent-1", Category: "translation", Capabilities: []string{"translation"}}}
}
func singleDraft() Draft {
	return Draft{Mode: "single", Summary: "one agent", Rationale: []string{"one domain"}, Confidence: .9, Model: "local", GraphVersion: "langgraph-v1", Steps: []Step{{ID: "step-1", Title: "Translate", Objective: "Translate", RequiredCapabilities: []string{"translation"}, Output: "document"}}}
}

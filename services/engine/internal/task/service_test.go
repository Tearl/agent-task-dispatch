package task

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

type testStore struct {
	createCalls  int
	updateCalls  int
	publishCalls int
	lastMutation Mutation
	createReplay bool
	updateReplay bool
	actionTask   Task
	databaseNow  time.Time
	actionErr    error
}

func (s *testStore) Create(_ context.Context, mutation Mutation, input DraftInput, id string) (Task, bool, error) {
	s.createCalls++
	s.lastMutation = mutation
	return Task{ID: id, PublisherID: mutation.ActorID, Status: StatusDraft, Title: input.Title, AggregateVersion: 1}, s.createReplay, nil
}
func (s *testStore) UpdateDraft(_ context.Context, mutation Mutation, id string, input UpdateDraftInput) (Task, bool, error) {
	s.updateCalls++
	s.lastMutation = mutation
	return Task{ID: id, PublisherID: mutation.ActorID, Status: StatusDraft, AggregateVersion: input.ExpectedVersion + 1}, s.updateReplay, nil
}
func (s *testStore) Publish(_ context.Context, mutation Mutation, id string, input PublishInput) (Publication, bool, error) {
	s.publishCalls++
	s.lastMutation = mutation
	return Publication{Task: Task{ID: id, PublisherID: mutation.ActorID, Status: StatusPendingEscrow, AggregateVersion: input.ExpectedVersion + 1}}, false, nil
}
func (*testStore) Get(context.Context, string, string) (Task, error) { return Task{}, nil }
func (s *testStore) GetForActions(context.Context, string, string) (Task, time.Time, error) {
	return s.actionTask, s.databaseNow, s.actionErr
}

func TestTaskOperationsRequirePublisherRoleAndIdempotency(t *testing.T) {
	store := &testStore{}
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	input := validDraft(now)
	for _, session := range []auth.Session{
		{UserID: "provider", Roles: []string{"agent_provider"}},
		{UserID: "admin", Roles: []string{"admin"}},
		{UserID: "arbitrator", Roles: []string{"arbitrator"}},
	} {
		if _, _, callErr := service.Create(context.Background(), session, "key", input); !errors.Is(callErr, ErrForbidden) {
			t.Fatalf("%s: expected forbidden, got %v", session.UserID, callErr)
		}
	}
	publisher := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	if _, _, err = service.Create(context.Background(), publisher, "", input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("missing idempotency key: %v", err)
	}
	created, replay, err := service.Create(context.Background(), publisher, "create-1", input)
	if err != nil || replay || created.PublisherID != publisher.UserID || store.createCalls != 1 {
		t.Fatalf("create: value=%#v replay=%v calls=%d err=%v", created, replay, store.createCalls, err)
	}
	if store.lastMutation.ActorID != publisher.UserID || store.lastMutation.IdempotencyKey != "create-1" || store.lastMutation.RequestHash == "" || store.lastMutation.EventID == "" {
		t.Fatalf("mutation context incomplete: %#v", store.lastMutation)
	}
}

func TestDraftValidationRequiresDeadlineCanonicalBudgetsAndWeightedCriteria(t *testing.T) {
	store := &testStore{}
	service, _ := NewService(store)
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	publisher := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	tests := []struct {
		name   string
		change func(*DraftInput)
	}{
		{"blank title", func(input *DraftInput) { input.Title = " " }},
		{"missing deadline", func(input *DraftInput) { input.Deadline = time.Time{} }},
		{"negative budget", func(input *DraftInput) { input.FormalBudget = "-1" }},
		{"non-canonical budget", func(input *DraftInput) { input.OverviewBudget = "01" }},
		{"wrong total weight", func(input *DraftInput) { input.AcceptanceCriteria[0].Weight = 49 }},
		{"duplicate criterion id", func(input *DraftInput) { input.AcceptanceCriteria[1].ID = input.AcceptanceCriteria[0].ID }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := validDraft(now)
			test.change(&input)
			if _, _, callErr := service.Create(context.Background(), publisher, "key", input); !errors.Is(callErr, ErrInvalidInput) {
				t.Fatalf("expected invalid input, got %v", callErr)
			}
		})
	}
	if store.createCalls != 0 {
		t.Fatalf("invalid input reached store %d times", store.createCalls)
	}
}

func TestExpiredRequestCanReachStoreForStableIdempotentReplay(t *testing.T) {
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	store := &testStore{createReplay: true, updateReplay: true}
	service, _ := NewService(store)
	service.now = func() time.Time { return now }
	publisher := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	expired := validDraft(now)
	expired.Deadline = now.Add(-time.Hour)
	if _, replay, err := service.Create(context.Background(), publisher, "create-replay", expired); err != nil || !replay {
		t.Fatalf("expired create replay: replay=%v err=%v", replay, err)
	}
	if _, replay, err := service.UpdateDraft(context.Background(), publisher, "update-replay", "task-1", UpdateDraftInput{DraftInput: expired, ExpectedVersion: 1}); err != nil || !replay {
		t.Fatalf("expired update replay: replay=%v err=%v", replay, err)
	}
	if store.createCalls != 1 || store.updateCalls != 1 {
		t.Fatalf("time-dependent validation blocked store replay lookup: create=%d update=%d", store.createCalls, store.updateCalls)
	}
}

func TestUpdateAndPublishRequireAggregateVersion(t *testing.T) {
	store := &testStore{}
	service, _ := NewService(store)
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	publisher := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	if _, _, err := service.UpdateDraft(context.Background(), publisher, "update", "task-1", UpdateDraftInput{DraftInput: validDraft(now)}); !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("update without version: %v", err)
	}
	if _, _, err := service.Publish(context.Background(), publisher, "publish", "task-1", PublishInput{}); !errors.Is(err, ErrStaleVersion) {
		t.Fatalf("publish without version: %v", err)
	}
	if _, _, err := service.UpdateDraft(context.Background(), publisher, "update", "task-1", UpdateDraftInput{DraftInput: validDraft(now), ExpectedVersion: 1}); err != nil || store.updateCalls != 1 {
		t.Fatalf("valid update: calls=%d err=%v", store.updateCalls, err)
	}
	if _, _, err := service.Publish(context.Background(), publisher, "publish", "task-1", PublishInput{ExpectedVersion: 2}); err != nil || store.publishCalls != 1 {
		t.Fatalf("valid publish: calls=%d err=%v", store.publishCalls, err)
	}
}

func TestPublicationContentHashesAreDeterministicAndSeparated(t *testing.T) {
	now := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	input := validDraft(now)
	value := Task{ID: "task-1", AggregateVersion: 2, Title: input.Title, Description: input.Description, ExpertType: input.ExpertType, Language: input.Language, OverviewBudget: input.OverviewBudget, FormalBudget: input.FormalBudget, ExternalCostCap: input.ExternalCostCap, Deadline: input.Deadline, Inputs: input.Inputs, AllowedTools: input.AllowedTools, Exclusions: input.Exclusions, DeliveryFormat: input.DeliveryFormat, AcceptanceCriteria: input.AcceptanceCriteria}
	spec1, acceptance1, err := PublicationVersions(value, 1, now)
	if err != nil {
		t.Fatal(err)
	}
	spec2, acceptance2, err := PublicationVersions(value, 1, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if spec1.ContentHash != spec2.ContentHash || acceptance1.ContentHash != acceptance2.ContentHash {
		t.Fatalf("metadata changed content hashes: %s/%s %s/%s", spec1.ContentHash, spec2.ContentHash, acceptance1.ContentHash, acceptance2.ContentHash)
	}
	if spec1.ContentHash == acceptance1.ContentHash || len(spec1.ContentHash) != 71 || spec1.TaskAggregateVersion != 3 || acceptance1.TotalWeight != 100 {
		t.Fatalf("invalid publication versions: %#v %#v", spec1, acceptance1)
	}
	value.Description = "changed"
	changed, _, _ := PublicationVersions(value, 1, now)
	if changed.ContentHash == spec1.ContentHash {
		t.Fatal("changed spec retained old content hash")
	}
}

func TestTaskAvailableActionsComeFromStoredStateAndDatabaseTime(t *testing.T) {
	databaseNow := time.Date(2026, 8, 21, 4, 0, 0, 0, time.UTC)
	store := &testStore{databaseNow: databaseNow, actionTask: Task{ID: "task-1", PublisherID: "publisher", Status: StatusDraft, AggregateVersion: 4, Deadline: databaseNow.Add(time.Hour)}}
	service, _ := NewService(store)
	publisher := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	response, err := service.AvailableActions(context.Background(), publisher, "task-1")
	if err != nil || response.ResourceType != "task" || response.AggregateVersion != 4 || len(response.Actions) != 2 {
		t.Fatalf("available actions: %#v err=%v", response, err)
	}
	for _, decision := range response.Actions {
		if !decision.Allowed || len(decision.Reasons) != 0 {
			t.Fatalf("future draft action blocked: %#v", decision)
		}
	}
	store.actionTask.Status = StatusPendingEscrow
	store.actionTask.Deadline = databaseNow.Add(-time.Second)
	response, err = service.AvailableActions(context.Background(), publisher, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if response.Actions[0].Allowed || len(response.Actions[0].Reasons) != 1 || response.Actions[0].Reasons[0].Code != "task_not_draft" {
		t.Fatalf("invalid update decision: %#v", response.Actions[0])
	}
	if response.Actions[1].Allowed || len(response.Actions[1].Reasons) != 2 || response.Actions[1].Reasons[0].Code != "task_not_draft" || response.Actions[1].Reasons[1].Code != "deadline_expired" {
		t.Fatalf("invalid publish decision: %#v", response.Actions[1])
	}
	store.actionTask.Status = StatusDraft
	response, err = service.AvailableActions(context.Background(), publisher, "task-1")
	if err != nil || !response.Actions[0].Allowed || response.Actions[1].Allowed || len(response.Actions[1].Reasons) != 1 || response.Actions[1].Reasons[0].Code != "deadline_expired" {
		t.Fatalf("expired draft recovery decisions: %#v err=%v", response.Actions, err)
	}
	if _, err = service.AvailableActions(context.Background(), auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}, "task-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("provider actions: %v", err)
	}
	view, err := service.View(context.Background(), publisher, "task-1")
	if err != nil || view.Task.ID != "task-1" || view.Task.AggregateVersion != view.AvailableActions.AggregateVersion {
		t.Fatalf("single-snapshot task view: %#v err=%v", view, err)
	}
}

func validDraft(now time.Time) DraftInput {
	return DraftInput{
		Title: "Research market", Description: "Analyze the supplied market data", ExpertType: "research", Language: "zh-CN",
		OverviewBudget: "100", FormalBudget: "1000", ExternalCostCap: "50", Deadline: now.Add(24 * time.Hour),
		Inputs: []string{"market.csv"}, AllowedTools: []string{"web-search"}, Exclusions: []string{"personal data"}, DeliveryFormat: "Markdown report",
		AcceptanceCriteria: []AcceptanceCriterion{{ID: "accuracy", Title: "Accuracy", Description: "Claims cite evidence", Weight: 50}, {ID: "coverage", Title: "Coverage", Description: "All requested markets covered", Weight: 50}},
	}
}

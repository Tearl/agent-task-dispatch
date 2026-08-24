package workflow

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/matching"
	"github.com/example/agent-platform/engine/internal/overview"
)

const (
	ruleVersion  = "matching-rules-v1"
	modelVersion = "ranking-model-disabled-v1"
)

type Service struct {
	store     *Store
	matcher   *matching.Service
	snapshots *matching.SnapshotService
	overviews *overview.Service
	now       func() time.Time
}

func NewService(store *Store, matcher *matching.Service, snapshots *matching.SnapshotService, overviews *overview.Service) (*Service, error) {
	if store == nil || matcher == nil || snapshots == nil || overviews == nil {
		return nil, ErrInvalidInput
	}
	return &Service{store: store, matcher: matcher, snapshots: snapshots, overviews: overviews, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (service *Service) StartMatching(ctx context.Context, session auth.Session, operationID, taskID string) (StartMatchingResult, error) {
	if !publisher(session) {
		return StartMatchingResult{}, ErrForbidden
	}
	if strings.TrimSpace(operationID) == "" || len(operationID) > 200 {
		return StartMatchingResult{}, ErrInvalidInput
	}
	task, err := service.store.Task(ctx, session.UserID, taskID)
	if err != nil {
		return StartMatchingResult{}, err
	}
	now := service.now()
	if !workflowMayRun(task.Status) || !task.Deadline.After(now) {
		return StartMatchingResult{}, ErrInvalidInput
	}
	candidates, err := service.store.Candidates(ctx)
	if err != nil {
		return StartMatchingResult{}, err
	}
	request := matching.Request{TaskID: task.ID, PublisherID: task.PublisherID, Category: task.ExpertType, Language: task.Language, Terms: matchingTerms(task), RequiredCapabilities: []string{}, RequiredProtocolVersion: "agent-execution-v1", RequiredVectorVersion: vectorVersion, OverviewBudget: task.OverviewBudget, FormalBudget: task.FormalBudget, ExternalCostCap: task.ExternalCostCap, Deadline: task.Deadline, Now: now}
	result, err := service.matcher.Match(ctx, request, candidates)
	if err != nil {
		return StartMatchingResult{}, err
	}
	effectiveRequest := request
	effectiveRequest.Now = time.Time{}
	effectiveHash, err := matching.HashEffectiveInput(struct {
		OperationID string
		Request     matching.Request
		Candidates  []matching.Candidate
	}{operationID, effectiveRequest, candidates})
	if err != nil {
		return StartMatchingResult{}, err
	}
	snapshot, replay, err := service.snapshots.CreateRevision(ctx, matching.SnapshotDraft{Key: matching.SnapshotKey{TaskID: task.ID, TaskSpecHash: task.SpecHash, AlgorithmVersion: matching.FairShuffleAlgorithmVersion, EffectiveInputHash: effectiveHash}, RuleVersion: ruleVersion, ModelVersion: modelVersion, Result: result})
	if err != nil {
		return StartMatchingResult{}, err
	}
	if !replay {
		_, _ = service.overviews.ObsoleteBefore(ctx, task.ID, snapshot.MatchRevision)
	}
	if err = service.store.TransitionTask(ctx, session.UserID, task.ID, "matching", "task.matching_started"); err != nil {
		return StartMatchingResult{}, err
	}
	return StartMatchingResult{SnapshotID: snapshot.ID, MatchRevision: snapshot.MatchRevision, Qualified: len(snapshot.Result.Qualified), Selected: len(snapshot.Selections), Replay: replay}, nil
}

func (service *Service) StartOverview(ctx context.Context, session auth.Session, taskID string) (StartOverviewResult, error) {
	if !publisher(session) {
		return StartOverviewResult{}, ErrForbidden
	}
	task, err := service.store.Task(ctx, session.UserID, taskID)
	if err != nil {
		return StartOverviewResult{}, err
	}
	if !workflowMayRun(task.Status) || !task.Deadline.After(service.now()) {
		return StartOverviewResult{}, ErrInvalidInput
	}
	snapshot, err := service.snapshots.Latest(ctx, task.ID, task.SpecHash, matching.FairShuffleAlgorithmVersion)
	if err != nil {
		return StartOverviewResult{}, err
	}
	if err = service.store.TransitionTask(ctx, session.UserID, task.ID, "overview_generating", "task.overview_started"); err != nil {
		return StartOverviewResult{}, err
	}
	batch, replay, err := service.overviews.Start(ctx, overview.StartRequest{SnapshotID: snapshot.ID, Deadline: task.Deadline})
	if err != nil {
		return StartOverviewResult{}, err
	}
	latest, err := service.snapshots.Latest(ctx, task.ID, task.SpecHash, matching.FairShuffleAlgorithmVersion)
	if err != nil {
		return StartOverviewResult{}, err
	}
	if latest.ID != snapshot.ID {
		_, _ = service.overviews.ObsoleteBefore(ctx, task.ID, latest.MatchRevision)
		return StartOverviewResult{}, overview.ErrObsolete
	}
	return StartOverviewResult{Batch: batch, Replay: replay}, nil
}

func (service *Service) FinalizeOverviewSlot(ctx context.Context, session auth.Session, taskID, batchID, slotID string) (overview.Batch, error) {
	if !publisher(session) {
		return overview.Batch{}, ErrForbidden
	}
	if err := service.store.BatchOwned(ctx, session.UserID, taskID, batchID); err != nil {
		return overview.Batch{}, err
	}
	batch, err := service.overviews.FinalizeSlot(ctx, batchID, slotID)
	if err == nil && batch.Status == overview.BatchCompleted {
		err = service.store.TransitionTask(ctx, session.UserID, taskID, "awaiting_selection", "task.overview_ready")
	}
	return batch, err
}

func (service *Service) Executions(ctx context.Context, session auth.Session, taskID string) ([]ExecutionView, error) {
	if !publisher(session) {
		return nil, ErrForbidden
	}
	return service.store.ListExecutions(ctx, session.UserID, taskID)
}

func publisher(session auth.Session) bool {
	return session.UserID != "" && slices.Contains(session.Roles, "publisher")
}

func workflowMayRun(status string) bool {
	switch status {
	case "escrowed", "matching", "overview_generating", "awaiting_selection":
		return true
	default:
		return false
	}
}

func matchingTerms(task TaskInput) []string {
	values := strings.FieldsFunc(task.Title+" "+task.Description+" "+task.ExpertType, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == ';' || r == '，' || r == '。'
	})
	if len(values) > 100 {
		values = values[:100]
	}
	return values
}

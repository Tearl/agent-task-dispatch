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
	operation, err := service.store.LockMatchingOperation(ctx, session.UserID, operationID)
	if err != nil {
		return StartMatchingResult{}, err
	}
	defer operation.Close()
	if previous, completed, completedErr := operation.Completed(ctx, session.UserID, operationID, taskID); completedErr != nil {
		return StartMatchingResult{}, completedErr
	} else if completed {
		previous.Replay = true
		return previous, nil
	}
	task, err := service.store.Task(ctx, session.UserID, taskID)
	if err != nil {
		return StartMatchingResult{}, err
	}
	now := service.now().UTC()
	evaluatedAt, pending, err := operation.PendingEvaluation(ctx, session.UserID, operationID, taskID)
	if err != nil {
		return StartMatchingResult{}, err
	}
	if pending {
		now = evaluatedAt
	}
	if !workflowMayRun(task.Status) || !task.Deadline.After(now) {
		return StartMatchingResult{}, ErrInvalidInput
	}
	now, previous, exists, err := operation.Begin(ctx, session.UserID, operationID, taskID, now)
	if err != nil {
		return StartMatchingResult{}, err
	}
	if exists {
		previous.Replay = true
		return previous, nil
	}
	draft, frozen, err := operation.FrozenDraft(ctx, session.UserID, operationID, taskID)
	if err != nil {
		return StartMatchingResult{}, err
	}
	if !frozen {
		candidates, candidatesErr := service.store.Candidates(ctx)
		if candidatesErr != nil {
			return StartMatchingResult{}, candidatesErr
		}
		tags := matchingTags(task)
		request := matching.Request{TaskID: task.ID, PublisherID: task.PublisherID, Category: task.ExpertType, Language: task.Language, Terms: tags, Tags: tags, RequiredCapabilities: []string{}, RequiredProtocolVersion: "agent-execution-v1", RequiredVectorVersion: vectorVersion, OverviewBudget: task.OverviewBudget, FormalBudget: task.FormalBudget, ExternalCostCap: task.ExternalCostCap, Deadline: task.Deadline, Now: now}
		result, matchErr := service.matcher.Match(ctx, request, candidates)
		if matchErr != nil {
			return StartMatchingResult{}, matchErr
		}
		effectiveHash, hashErr := effectiveMatchingHash(request, candidates)
		if hashErr != nil {
			return StartMatchingResult{}, hashErr
		}
		draft, err = operation.FreezeDraft(ctx, session.UserID, operationID, taskID, matching.SnapshotDraft{Key: matching.SnapshotKey{TaskID: task.ID, TaskSpecHash: task.SpecHash, AlgorithmVersion: matching.FairShuffleAlgorithmVersion, EffectiveInputHash: effectiveHash}, RuleVersion: ruleVersion, ModelVersion: modelVersion, Result: result})
		if err != nil {
			return StartMatchingResult{}, err
		}
	}
	if draft.Key.TaskID != task.ID || draft.Key.TaskSpecHash != task.SpecHash || draft.Key.AlgorithmVersion != matching.FairShuffleAlgorithmVersion {
		return StartMatchingResult{}, ErrInvalidInput
	}
	snapshot, replay, err := service.snapshots.CreateRevision(ctx, draft)
	if err != nil {
		return StartMatchingResult{}, err
	}
	// This follow-up is idempotent and must also run when a pending operation
	// recovers a snapshot committed just before a process crash.
	_, _ = service.overviews.ObsoleteBefore(ctx, task.ID, snapshot.MatchRevision)
	response := StartMatchingResult{SnapshotID: snapshot.ID, MatchRevision: snapshot.MatchRevision, Qualified: len(snapshot.Result.Qualified), Selected: len(snapshot.Selections), Replay: replay}
	if err = service.store.TransitionTaskAndRecordMatchingOperation(ctx, session.UserID, operationID, task.ID, "matching", "task.matching_started", response); err != nil {
		return StartMatchingResult{}, err
	}
	return response, nil
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
	ready, err := service.store.OverviewFundingReady(ctx, session.UserID, task.ID, snapshot.ID)
	if err != nil {
		return StartOverviewResult{}, err
	}
	if !ready {
		return StartOverviewResult{}, ErrDependencyPending
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

func effectiveMatchingHash(request matching.Request, candidates []matching.Candidate) (string, error) {
	canonicalRequest := request
	// Matching eligibility and time-headroom scoring both depend on the exact
	// evaluation instant. Keep it in the snapshot identity so even a sub-second
	// health/deadline boundary cannot replay an older result.
	canonicalRequest.Now = request.Now.UTC()
	canonicalRequest.Terms = canonicalStrings(request.Terms)
	canonicalRequest.Tags = canonicalStrings(request.Tags)
	canonicalRequest.RequiredCapabilities = canonicalStrings(request.RequiredCapabilities)
	canonicalCandidates := slices.Clone(candidates)
	for index := range canonicalCandidates {
		canonicalCandidates[index].Languages = canonicalStrings(canonicalCandidates[index].Languages)
		canonicalCandidates[index].Tags = canonicalStrings(canonicalCandidates[index].Tags)
		canonicalCandidates[index].Capabilities = canonicalStrings(canonicalCandidates[index].Capabilities)
	}
	slices.SortFunc(canonicalCandidates, func(left, right matching.Candidate) int {
		return strings.Compare(left.AgentID, right.AgentID)
	})
	return matching.HashEffectiveInput(struct {
		Request    matching.Request
		Candidates []matching.Candidate
	}{canonicalRequest, canonicalCandidates})
}

func canonicalStrings(values []string) []string {
	result := slices.Clone(values)
	slices.Sort(result)
	return result
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

func matchingTags(task TaskInput) []string {
	if len(task.Tags) > 0 {
		return slices.Clone(task.Tags)
	}
	values := strings.FieldsFunc(task.Title+" "+task.Description+" "+task.ExpertType+" "+strings.Join(task.AllowedTools, " ")+" "+task.DeliveryFormat, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\t' || r == ',' || r == ';' || r == '，' || r == '。'
	})
	if len(values) > 100 {
		values = values[:100]
	}
	return values
}

package overview

import (
	"context"
	"crypto/sha256"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/matching"
)

type snapshotStub struct{ value matching.Snapshot }

func (stub snapshotStub) Get(_ context.Context, id string) (matching.Snapshot, error) {
	if id != stub.value.ID {
		return matching.Snapshot{}, matching.ErrSnapshotNotFound
	}
	return stub.value, nil
}

type briefStub struct {
	calls int
	value BriefHandle
}

func (stub *briefStub) PrepareOverviewBrief(_ context.Context, _, _ string, _ time.Time) (BriefHandle, error) {
	stub.calls++
	return stub.value, nil
}

type targetStub struct{ values map[string]DispatchTarget }

func (stub targetStub) ResolveOverviewTarget(_ context.Context, agentID string, _ int) (DispatchTarget, error) {
	value, ok := stub.values[agentID]
	if !ok {
		return DispatchTarget{}, ErrNotFound
	}
	return value, nil
}

type allocationStub struct {
	mu           sync.Mutex
	authorized   map[string]Allocation
	authorizeLog []AllocationRequest
	captures     map[string]int
	releases     map[string]int
}

func (stub *allocationStub) AuthorizeOverview(_ context.Context, request AllocationRequest) (Allocation, bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if existing, ok := stub.authorized[request.IdempotencyKey]; ok {
		return existing, true, nil
	}
	value := Allocation{ID: fmt.Sprintf("allocation-%d", len(stub.authorized)+1), CostCap: request.ExternalCostCap}
	stub.authorized[request.IdempotencyKey] = value
	stub.authorizeLog = append(stub.authorizeLog, request)
	return value, false, nil
}

func (stub *allocationStub) CaptureOverview(_ context.Context, allocationID string, _ BillingClaim) (bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	replay := stub.captures[allocationID] > 0
	stub.captures[allocationID]++
	return replay, nil
}

func (stub *allocationStub) ReleaseOverview(_ context.Context, allocationID, _ string) (bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	replay := stub.releases[allocationID] > 0
	stub.releases[allocationID]++
	return replay, nil
}

type executionStub struct {
	mu             sync.Mutex
	values         map[string]execution.Execution
	specs          map[string]execution.Spec
	deliverables   map[string]execution.DeliverableResponse
	createCalls    int
	dispatchCalls  int
	cancelCalls    int
	activeDispatch int
	maxDispatch    int
	barrier        chan struct{}
	barrierTarget  int
}

func (stub *executionStub) Create(_ context.Context, spec execution.Spec) (execution.Execution, bool, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if existing, ok := stub.values[spec.LogicalExecutionID]; ok {
		if !reflect.DeepEqual(stub.specs[spec.LogicalExecutionID], spec) {
			return execution.Execution{}, false, execution.ErrContentConflict
		}
		return existing, true, nil
	}
	stub.createCalls++
	value := execution.Execution{Spec: spec, Status: execution.ExecutionPending, UsedCost: "0"}
	stub.specs[spec.LogicalExecutionID] = spec
	stub.values[spec.LogicalExecutionID] = value
	return value, false, nil
}

func (stub *executionStub) Dispatch(_ context.Context, id string) (execution.Execution, execution.Attempt, bool, error) {
	stub.mu.Lock()
	value, ok := stub.values[id]
	if !ok {
		stub.mu.Unlock()
		return execution.Execution{}, execution.Attempt{}, false, execution.ErrNotFound
	}
	if value.Status == execution.ExecutionRunning {
		stub.mu.Unlock()
		return value, execution.Attempt{}, true, nil
	}
	stub.dispatchCalls++
	stub.activeDispatch++
	if stub.activeDispatch > stub.maxDispatch {
		stub.maxDispatch = stub.activeDispatch
	}
	if stub.barrier != nil && stub.activeDispatch == stub.barrierTarget {
		close(stub.barrier)
	}
	barrier := stub.barrier
	stub.mu.Unlock()
	if barrier != nil {
		<-barrier
	}
	stub.mu.Lock()
	stub.activeDispatch--
	value.Status = execution.ExecutionRunning
	stub.values[id] = value
	stub.mu.Unlock()
	return value, execution.Attempt{}, false, nil
}

func (stub *executionStub) Get(_ context.Context, id string) (execution.Execution, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	value, ok := stub.values[id]
	if !ok {
		return execution.Execution{}, execution.ErrNotFound
	}
	return value, nil
}

func (stub *executionStub) Deliverable(_ context.Context, id string) (execution.DeliverableResponse, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	value, ok := stub.deliverables[id]
	if !ok {
		return execution.DeliverableResponse{}, execution.ErrNotFound
	}
	return value, nil
}

func (stub *executionStub) Cancel(_ context.Context, id string) (execution.Execution, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	value, ok := stub.values[id]
	if !ok {
		return execution.Execution{}, execution.ErrNotFound
	}
	stub.cancelCalls++
	value.Status = execution.ExecutionCancelled
	stub.values[id] = value
	return value, nil
}

type artifactStub struct{ values map[string][]byte }

func (stub artifactStub) Read(_ context.Context, ref, _ string, _ int64) ([]byte, error) {
	value, ok := stub.values[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte{}, value...), nil
}

type evidenceStub struct{ values map[string]ToolEvidence }

func (stub evidenceStub) Evidence(_ context.Context, id string) (ToolEvidence, error) {
	value, ok := stub.values[id]
	if !ok {
		return ToolEvidence{}, ErrNotFound
	}
	return value, nil
}

func TestStartFansOutThreeBoundExecutionsAndReplays(t *testing.T) {
	service, repository, briefs, allocations, executions, _, _, clock := overviewFixture(t)
	deadline := clock.now.Add(30 * time.Minute)
	batch, replay, err := service.Start(context.Background(), StartRequest{SnapshotID: overviewSnapshot().ID, Deadline: deadline})
	if err != nil || replay || len(batch.Slots) != 3 || executions.maxDispatch != 3 {
		t.Fatalf("start: batch=%#v replay=%v maxDispatch=%d err=%v", batch, replay, executions.maxDispatch, err)
	}
	commonDeadline := clock.now.Add(10 * time.Minute)
	seenAllocations := make(map[string]struct{})
	seenExecutions := make(map[string]struct{})
	for _, slot := range batch.Slots {
		spec := executions.specs[slot.LogicalExecutionID]
		if !spec.Deadline.Equal(commonDeadline) || spec.ToolPolicy.Mode != execution.ToolPolicyReadOnly || !reflect.DeepEqual(spec.ToolPolicy.AllowedTools, []string{"read", "search"}) || spec.InputRef != briefs.value.Ref || spec.InputHash != briefs.value.Hash || spec.Overview == nil || spec.Overview.AllocationID != slot.AllocationID {
			t.Fatalf("slot lost execution binding: slot=%#v spec=%#v", slot, spec)
		}
		seenAllocations[slot.AllocationID] = struct{}{}
		seenExecutions[slot.LogicalExecutionID] = struct{}{}
	}
	if len(seenAllocations) != 3 || len(seenExecutions) != 3 || len(allocations.authorizeLog) != 3 || executions.createCalls != 3 || briefs.calls != 1 {
		t.Fatalf("fanout identities: allocations=%d executions=%d auth=%d creates=%d briefs=%d", len(seenAllocations), len(seenExecutions), len(allocations.authorizeLog), executions.createCalls, briefs.calls)
	}
	replayed, replay, err := service.Start(context.Background(), StartRequest{SnapshotID: overviewSnapshot().ID, Deadline: deadline})
	if err != nil || !replay || len(allocations.authorizeLog) != 3 || executions.createCalls != 3 || executions.dispatchCalls != 3 || !reflect.DeepEqual(batch, replayed) {
		t.Fatalf("start replay changed effects: replay=%v auth=%d creates=%d dispatch=%d err=%v", replay, len(allocations.authorizeLog), executions.createCalls, executions.dispatchCalls, err)
	}
	stored, _ := repository.Get(context.Background(), batch.ID)
	if stored.Status != BatchRunning {
		t.Fatalf("unexpected batch status: %#v", stored)
	}
}

func TestValidationBillingDuplicateAndOneDeterministicReplacement(t *testing.T) {
	service, _, _, allocations, executions, artifacts, evidence, clock := overviewFixture(t)
	batch, _, err := service.Start(context.Background(), StartRequest{SnapshotID: overviewSnapshot().ID, Deadline: clock.now.Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	shared := validResultBody("shared")
	completeExecution(executions, artifacts, evidence, batch.Slots[0], shared, clock.now.Add(time.Minute))
	batch, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[0].ID)
	if err != nil || batch.Slots[0].Status != SlotValid || allocations.captures[batch.Slots[0].AllocationID] != 1 {
		t.Fatalf("first valid result: batch=%#v captures=%v err=%v", batch, allocations.captures, err)
	}
	completeExecution(executions, artifacts, evidence, batch.Slots[1], shared, clock.now.Add(time.Minute))
	batch, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[1].ID)
	if err != nil || len(batch.Slots) != 4 || !batch.ReplacementUsed || batch.Slots[1].Status != SlotInvalid || !reflect.DeepEqual(batch.Slots[1].Validation.Codes, []string{"duplicate_content"}) || !batch.Slots[3].Replacement || batch.Slots[3].AgentID != "agent-4" || allocations.releases[batch.Slots[1].AllocationID] != 1 {
		t.Fatalf("duplicate/replacement: batch=%#v releases=%v err=%v", batch, allocations.releases, err)
	}
	if _, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[1].ID); err != nil || allocations.releases[batch.Slots[1].AllocationID] != 1 {
		t.Fatalf("duplicate validation replay changed release: releases=%v err=%v", allocations.releases, err)
	}
	completeExecution(executions, artifacts, evidence, batch.Slots[2], []byte(`{"schemaVersion":"wrong"}`), clock.now.Add(time.Minute))
	batch, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[2].ID)
	if err != nil || len(batch.Slots) != 4 {
		t.Fatalf("second invalid created another replacement: slots=%d err=%v", len(batch.Slots), err)
	}
	completeExecution(executions, artifacts, evidence, batch.Slots[3], validResultBody("replacement"), clock.now.Add(time.Minute))
	batch, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[3].ID)
	if err != nil || batch.Status != BatchCompleted || batch.Slots[3].Status != SlotValid || allocations.captures[batch.Slots[3].AllocationID] != 1 {
		t.Fatalf("replacement completion: batch=%#v captures=%v err=%v", batch, allocations.captures, err)
	}
	_, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[0].ID)
	if err != nil || allocations.captures[batch.Slots[0].AllocationID] != 1 {
		t.Fatalf("billing replay duplicated capture: captures=%v err=%v", allocations.captures, err)
	}
}

func TestDeadlineAndToolSafetyFailuresAreNotBillable(t *testing.T) {
	service, _, _, allocations, executions, artifacts, evidence, clock := overviewFixture(t)
	batch, _, err := service.Start(context.Background(), StartRequest{SnapshotID: overviewSnapshot().ID, Deadline: clock.now.Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	completeExecution(executions, artifacts, evidence, batch.Slots[0], validResultBody("unsafe"), clock.now.Add(time.Minute))
	evidence.values[batch.Slots[0].LogicalExecutionID] = ToolEvidence{Complete: true, Tools: []string{"read", "email"}, ExternalWriteAttempts: 1}
	batch, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[0].ID)
	if err != nil || batch.Slots[0].Status != SlotInvalid || allocations.releases[batch.Slots[0].AllocationID] != 1 || allocations.captures[batch.Slots[0].AllocationID] != 0 {
		t.Fatalf("unsafe result billed: batch=%#v captures=%v releases=%v err=%v", batch, allocations.captures, allocations.releases, err)
	}
	clock.now = batch.Deadline.Add(time.Second)
	second := batch.Slots[1]
	batch, err = service.FinalizeSlot(context.Background(), batch.ID, second.ID)
	if err != nil || executions.cancelCalls != 1 || allocations.releases[second.AllocationID] != 1 {
		t.Fatalf("deadline did not cancel/release: batch=%#v cancels=%d releases=%v err=%v", batch, executions.cancelCalls, allocations.releases, err)
	}
}

func TestNewRevisionObsoletesAndReleasesOldUnsettledSlots(t *testing.T) {
	service, repository, _, allocations, executions, _, _, clock := overviewFixture(t)
	batch, _, err := service.Start(context.Background(), StartRequest{SnapshotID: overviewSnapshot().ID, Deadline: clock.now.Add(5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	// A valid result whose capture is still pending must be releasable when a
	// newer matching revision makes the whole batch obsolete.
	batch, _, _, err = repository.RecordValidation(context.Background(), batch.ID, batch.Slots[0].ID, Validation{Valid: true}, digestBytes([]byte("valid-before-obsolete")), "artifact://valid-before-obsolete")
	if err != nil {
		t.Fatal(err)
	}
	count, err := service.ObsoleteBefore(context.Background(), batch.TaskID, batch.MatchRevision+1)
	if err != nil || count != 1 || executions.cancelCalls != 2 {
		t.Fatalf("obsolete: count=%d cancels=%d err=%v", count, executions.cancelCalls, err)
	}
	for _, slot := range batch.Slots {
		if allocations.releases[slot.AllocationID] != 1 {
			t.Fatalf("obsolete allocation %q was not released: %v", slot.AllocationID, allocations.releases)
		}
	}
	stored, err := repository.Get(context.Background(), batch.ID)
	if err != nil || stored.Slots[0].Status != SlotValid || stored.Slots[0].BillingStatus != BillingReleased {
		t.Fatalf("valid uncaptured result was not released safely: slot=%#v err=%v", stored.Slots[0], err)
	}
	if _, err = service.FinalizeSlot(context.Background(), batch.ID, batch.Slots[0].ID); err != ErrObsolete {
		t.Fatalf("obsolete batch remained finalizable: %v", err)
	}
}

type mutableClock struct{ now time.Time }

func overviewFixture(t *testing.T) (*Service, *MemoryRepository, *briefStub, *allocationStub, *executionStub, *artifactStub, *evidenceStub, *mutableClock) {
	t.Helper()
	clock := &mutableClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
	snapshot := overviewSnapshot()
	repository := NewMemoryRepository()
	repository.now = func() time.Time { return clock.now }
	briefs := &briefStub{value: BriefHandle{Ref: "brief://task-1/sanitized", Hash: digestBytes([]byte("sanitized-only")), ExpiresAt: clock.now.Add(time.Hour)}}
	targets := targetStub{values: make(map[string]DispatchTarget)}
	for _, scored := range snapshot.Result.Qualified {
		candidate := scored.Candidate
		targets.values[candidate.AgentID] = DispatchTarget{AgentID: candidate.AgentID, ProviderID: candidate.ProviderID, Endpoint: "https://" + candidate.AgentID + ".example", PriceVersion: candidate.PriceVersion, OverviewPrice: candidate.OverviewPrice, ExternalCostCap: candidate.ExternalCostCap, QuoteHash: digestBytes([]byte("quote:" + candidate.AgentID))}
	}
	allocations := &allocationStub{authorized: make(map[string]Allocation), captures: make(map[string]int), releases: make(map[string]int)}
	executions := &executionStub{values: make(map[string]execution.Execution), specs: make(map[string]execution.Spec), deliverables: make(map[string]execution.DeliverableResponse), barrier: make(chan struct{}), barrierTarget: 3}
	artifacts := &artifactStub{values: make(map[string][]byte)}
	evidence := &evidenceStub{values: make(map[string]ToolEvidence)}
	service, err := NewService(repository, snapshotStub{value: snapshot}, briefs, targets, allocations, executions, artifacts, evidence, Config{MaximumDuration: 10 * time.Minute, AllowedTools: []string{"search", "read"}})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return clock.now }
	return service, repository, briefs, allocations, executions, artifacts, evidence, clock
}

func overviewSnapshot() matching.Snapshot {
	qualified := make([]matching.ScoredCandidate, 0, 4)
	for index := 1; index <= 4; index++ {
		qualified = append(qualified, matching.ScoredCandidate{Candidate: matching.Candidate{AgentID: fmt.Sprintf("agent-%d", index), ProviderID: fmt.Sprintf("provider-%d", index), PriceVersion: 1, OverviewPrice: "10", FormalPrice: "100", ExternalCostCap: "5"}, Score: matching.ScoreBreakdown{RuleScore: 90 - index, RankingScore: 90 - index}})
	}
	selections := make([]matching.Selection, 0, 3)
	for index := range 3 {
		selections = append(selections, matching.Selection{Candidate: qualified[index], Position: index + 1})
	}
	return matching.Snapshot{ID: digestBytes([]byte("snapshot-1")), TaskID: "task-1", TaskSpecHash: digestBytes([]byte("task-spec-1")), MatchRevision: 1, AlgorithmVersion: matching.FairShuffleAlgorithmVersion, Result: matching.Result{Qualified: qualified}, Selections: selections}
}

func completeExecution(executions *executionStub, artifacts *artifactStub, evidence *evidenceStub, slot Slot, body []byte, completedAt time.Time) {
	contentHash := digestBytes(body)
	ref := "artifact://" + slot.ID
	executions.mu.Lock()
	value := executions.values[slot.LogicalExecutionID]
	value.Status = execution.ExecutionSucceeded
	value.UsedCost = "3"
	value.ContentHash = contentHash
	value.DeliverableRef = ref
	value.UpdatedAt = completedAt
	executions.values[slot.LogicalExecutionID] = value
	executions.deliverables[slot.LogicalExecutionID] = execution.DeliverableResponse{ContentHash: contentHash, DeliverableRef: ref}
	executions.mu.Unlock()
	artifacts.values[ref] = append([]byte{}, body...)
	evidence.values[slot.LogicalExecutionID] = ToolEvidence{Complete: true, Tools: []string{"read"}}
}

func validResultBody(marker string) []byte {
	return []byte(fmt.Sprintf(`{"schemaVersion":"%s","understandingSummary":"Summary %s","approach":["Analyze"],"deliverableStructure":["Report"],"keyRisks":["Data"],"estimatedDurationSeconds":3600,"sample":"Example"}`, ResultSchemaVersion, marker))
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("sha256:%x", digest)
}

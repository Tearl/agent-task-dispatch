package overview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/matching"
)

type Config struct {
	MaximumDuration time.Duration
	AllowedTools    []string
}

type Service struct {
	repository  Repository
	snapshots   SnapshotReader
	briefs      BriefProvider
	targets     TargetResolver
	allocations AllocationGateway
	executions  ExecutionGateway
	artifacts   ArtifactReader
	tools       ToolEvidenceReader
	config      Config
	now         func() time.Time
}

func NewService(repository Repository, snapshots SnapshotReader, briefs BriefProvider, targets TargetResolver, allocations AllocationGateway, executions ExecutionGateway, artifacts ArtifactReader, tools ToolEvidenceReader, config Config) (*Service, error) {
	if repository == nil || snapshots == nil || briefs == nil || targets == nil || allocations == nil || executions == nil || artifacts == nil || tools == nil || config.MaximumDuration <= 0 || config.MaximumDuration > 24*time.Hour || len(config.AllowedTools) == 0 {
		return nil, ErrInvalidInput
	}
	allowed := slices.Clone(config.AllowedTools)
	slices.Sort(allowed)
	for index, tool := range allowed {
		if strings.TrimSpace(tool) == "" || index > 0 && tool == allowed[index-1] {
			return nil, ErrInvalidInput
		}
	}
	config.AllowedTools = allowed
	return &Service{repository: repository, snapshots: snapshots, briefs: briefs, targets: targets, allocations: allocations, executions: executions, artifacts: artifacts, tools: tools, config: config, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (service *Service) Start(ctx context.Context, request StartRequest) (Batch, bool, error) {
	if !validDigest(request.SnapshotID) {
		return Batch{}, false, ErrInvalidInput
	}
	batchID := stableID("overview-batch", request.SnapshotID, OrchestrationVersion)
	if existing, err := service.repository.Get(ctx, batchID); err == nil {
		updated, dispatchErr := service.dispatchOutstanding(ctx, existing)
		return updated, true, dispatchErr
	} else if !errors.Is(err, ErrNotFound) {
		return Batch{}, false, err
	}
	snapshot, err := service.snapshots.Get(ctx, request.SnapshotID)
	if err != nil {
		return Batch{}, false, err
	}
	if err = validateSnapshotForOverview(snapshot, request.SnapshotID); err != nil {
		return Batch{}, false, err
	}
	now := service.now()
	deadline := request.Deadline.UTC()
	maximum := now.Add(service.config.MaximumDuration)
	if deadline.After(maximum) {
		deadline = maximum
	}
	if !deadline.After(now) {
		return Batch{}, false, ErrInvalidInput
	}
	brief, err := service.briefs.PrepareOverviewBrief(ctx, snapshot.TaskID, snapshot.TaskSpecHash, deadline)
	if err != nil {
		return Batch{}, false, err
	}
	if strings.TrimSpace(brief.Ref) == "" || !validDigest(brief.Hash) || brief.ExpiresAt.Before(deadline) {
		return Batch{}, false, ErrInvalidInput
	}
	batch := Batch{ID: batchID, SnapshotID: snapshot.ID, TaskID: snapshot.TaskID, TaskSpecHash: snapshot.TaskSpecHash, MatchRevision: snapshot.MatchRevision, AlgorithmVersion: snapshot.AlgorithmVersion, BriefRef: brief.Ref, BriefHash: brief.Hash, Deadline: deadline, Status: BatchRunning, CreatedAt: now, UpdatedAt: now}
	for index, selection := range snapshot.Selections {
		slot, planErr := service.planSlot(ctx, batch, selection.Candidate.Candidate, index+1, selection.Position, false)
		if planErr != nil {
			return Batch{}, false, planErr
		}
		batch.Slots = append(batch.Slots, slot)
	}
	if len(batch.Slots) == 0 || len(batch.Slots) > matching.DefaultSelectionLimit {
		return Batch{}, false, ErrInvalidInput
	}
	created, replay, err := service.repository.GetOrCreate(ctx, batch)
	if err != nil {
		return Batch{}, false, err
	}
	updated, dispatchErr := service.dispatchOutstanding(ctx, created)
	return updated, replay, dispatchErr
}

func (service *Service) FinalizeSlot(ctx context.Context, batchID, slotID string) (Batch, error) {
	batch, err := service.repository.Get(ctx, batchID)
	if err != nil {
		return Batch{}, err
	}
	if batch.Status == BatchObsolete {
		return Batch{}, ErrObsolete
	}
	slot, err := findSlot(batch, slotID)
	if err != nil {
		return Batch{}, err
	}
	if slot.Status == SlotValid || slot.Status == SlotInvalid {
		return service.settleAndReplace(ctx, batch, slot)
	}
	if slot.Status != SlotDispatched {
		return Batch{}, ErrInvalidState
	}
	work, err := service.executions.Get(ctx, slot.LogicalExecutionID)
	if err != nil {
		return Batch{}, err
	}
	var validation Validation
	contentHash, deliverableRef := work.ContentHash, work.DeliverableRef
	switch work.Status {
	case execution.ExecutionSucceeded:
		deliverable, deliverableErr := service.executions.Deliverable(ctx, slot.LogicalExecutionID)
		if deliverableErr != nil {
			return Batch{}, ErrDependencyPending
		}
		body, readErr := service.artifacts.Read(ctx, deliverable.DeliverableRef, deliverable.ContentHash, MaxArtifactBytes)
		if readErr != nil {
			return Batch{}, ErrDependencyPending
		}
		evidence, evidenceErr := service.tools.Evidence(ctx, slot.LogicalExecutionID)
		if evidenceErr != nil {
			return Batch{}, ErrDependencyPending
		}
		contentHash, deliverableRef = deliverable.ContentHash, deliverable.DeliverableRef
		validation = ValidateArtifact(body, contentHash, work.UpdatedAt, batch.Deadline, evidence, service.config.AllowedTools)
	case execution.ExecutionFailed, execution.ExecutionCancelled, execution.ExecutionCostStopped:
		validation = Validation{Valid: false, Codes: []string{"execution_failed"}}
	case execution.ExecutionPending, execution.ExecutionRunning, execution.ExecutionCancelRequested:
		if !service.now().After(batch.Deadline) {
			return Batch{}, ErrDependencyPending
		}
		_, _ = service.executions.Cancel(ctx, slot.LogicalExecutionID)
		validation = Validation{Valid: false, Codes: []string{"deadline_exceeded"}}
	default:
		return Batch{}, ErrInvalidState
	}
	updated, storedSlot, _, err := service.repository.RecordValidation(ctx, batch.ID, slot.ID, validation, contentHash, deliverableRef)
	if err != nil {
		return Batch{}, err
	}
	return service.settleAndReplace(ctx, updated, storedSlot)
}

func (service *Service) ObsoleteBefore(ctx context.Context, taskID string, matchRevision int) (int, error) {
	changed, err := service.repository.MarkObsoleteBefore(ctx, taskID, matchRevision)
	if err != nil {
		return 0, err
	}
	var combined error
	for _, batch := range changed {
		for _, slot := range batch.Slots {
			if slot.Status == SlotObsolete {
				_, cancelErr := service.executions.Cancel(ctx, slot.LogicalExecutionID)
				if cancelErr != nil && !errors.Is(cancelErr, execution.ErrInvalidState) {
					combined = errors.Join(combined, cancelErr)
				}
			}
			if slot.BillingStatus == BillingAuthorized {
				_, releaseErr := service.allocations.ReleaseOverview(ctx, slot.AllocationID, "matching_revision_obsolete")
				if releaseErr == nil {
					_, releaseErr = service.repository.RecordBilling(ctx, batch.ID, slot.ID, BillingReleased)
				}
				combined = errors.Join(combined, releaseErr)
			}
		}
	}
	return len(changed), combined
}

func (service *Service) planSlot(ctx context.Context, batch Batch, candidate matching.Candidate, ordinal, sourcePosition int, replacement bool) (Slot, error) {
	target, err := service.targets.ResolveOverviewTarget(ctx, candidate.AgentID, candidate.PriceVersion)
	if err != nil {
		return Slot{}, err
	}
	if target.AgentID != candidate.AgentID || target.ProviderID != candidate.ProviderID || target.PriceVersion != candidate.PriceVersion || target.OverviewPrice != candidate.OverviewPrice || target.ExternalCostCap != candidate.ExternalCostCap || !validDigest(target.QuoteHash) || !validBaseURL(target.Endpoint) {
		return Slot{}, ErrContentConflict
	}
	slotID := stableID("overview-slot", batch.ID, fmt.Sprintf("%d", ordinal), target.AgentID, fmt.Sprintf("%t", replacement), ReplacementVersion)
	allocationKey := stableID("overview-allocation", slotID, target.QuoteHash)
	allocation, _, err := service.allocations.AuthorizeOverview(ctx, AllocationRequest{IdempotencyKey: allocationKey, TaskID: batch.TaskID, TaskSpecHash: batch.TaskSpecHash, SnapshotID: batch.SnapshotID, MatchRevision: batch.MatchRevision, AgentID: target.AgentID, PriceVersion: target.PriceVersion, QuoteHash: target.QuoteHash, OverviewPrice: target.OverviewPrice, ExternalCostCap: target.ExternalCostCap, Deadline: batch.Deadline})
	if err != nil {
		return Slot{}, err
	}
	if strings.TrimSpace(allocation.ID) == "" || allocation.CostCap != target.ExternalCostCap {
		return Slot{}, ErrContentConflict
	}
	logicalID := stableID("overview-execution", slotID, OrchestrationVersion)
	_, _, err = service.executions.Create(ctx, execution.Spec{LogicalExecutionID: logicalID, Stage: execution.StageOverview, TaskID: batch.TaskID, TaskSpecHash: batch.TaskSpecHash, InputRef: batch.BriefRef, InputHash: batch.BriefHash, AgentID: target.AgentID, AgentEndpoint: target.Endpoint, ResponsibilityCode: "overview_candidate", CostCap: allocation.CostCap, ToolPolicy: execution.ToolPolicy{Mode: execution.ToolPolicyReadOnly, AllowedTools: slices.Clone(service.config.AllowedTools)}, Deadline: batch.Deadline, IdempotencyKey: logicalID, Overview: &execution.OverviewBinding{MatchRevision: batch.MatchRevision, AllocationID: allocation.ID, QuoteHash: target.QuoteHash}})
	if err != nil {
		return Slot{}, err
	}
	now := service.now()
	return Slot{ID: slotID, BatchID: batch.ID, Ordinal: ordinal, SourcePosition: sourcePosition, Replacement: replacement, AgentID: target.AgentID, ProviderID: target.ProviderID, PriceVersion: target.PriceVersion, QuoteHash: target.QuoteHash, OverviewPrice: target.OverviewPrice, ExternalCostCap: target.ExternalCostCap, AllocationID: allocation.ID, LogicalExecutionID: logicalID, Status: SlotPlanned, BillingStatus: BillingAuthorized, CreatedAt: now, UpdatedAt: now}, nil
}

func (service *Service) dispatchOutstanding(ctx context.Context, batch Batch) (Batch, error) {
	type outcome struct {
		slotID string
		err    error
	}
	results := make(chan outcome, len(batch.Slots))
	var group sync.WaitGroup
	for _, slot := range batch.Slots {
		if slot.Status != SlotPlanned {
			continue
		}
		group.Add(1)
		go func(slot Slot) {
			defer group.Done()
			_, _, _, err := service.executions.Dispatch(ctx, slot.LogicalExecutionID)
			results <- outcome{slotID: slot.ID, err: err}
		}(slot)
	}
	group.Wait()
	close(results)
	var combined error
	for result := range results {
		if result.err != nil {
			combined = errors.Join(combined, result.err)
			continue
		}
		var err error
		batch, err = service.repository.RecordDispatched(ctx, batch.ID, result.slotID)
		combined = errors.Join(combined, err)
	}
	return batch, combined
}

func (service *Service) settleAndReplace(ctx context.Context, batch Batch, slot Slot) (Batch, error) {
	var err error
	if slot.BillingStatus == BillingAuthorized {
		if slot.Status == SlotValid {
			work, getErr := service.executions.Get(ctx, slot.LogicalExecutionID)
			if getErr != nil {
				return Batch{}, getErr
			}
			_, err = service.allocations.CaptureOverview(ctx, slot.AllocationID, BillingClaim{TaskID: batch.TaskID, TaskSpecHash: batch.TaskSpecHash, MatchRevision: batch.MatchRevision, LogicalExecutionID: slot.LogicalExecutionID, AgentID: slot.AgentID, QuoteHash: slot.QuoteHash, ContentHash: slot.ContentHash, Amount: slot.OverviewPrice, UsedCost: work.UsedCost})
			if err == nil {
				batch, err = service.repository.RecordBilling(ctx, batch.ID, slot.ID, BillingCaptured)
			}
		} else {
			_, err = service.allocations.ReleaseOverview(ctx, slot.AllocationID, strings.Join(slot.Validation.Codes, ","))
			if err == nil {
				batch, err = service.repository.RecordBilling(ctx, batch.ID, slot.ID, BillingReleased)
			}
		}
		if err != nil {
			return batch, err
		}
	}
	if slot.Status != SlotInvalid || batch.ReplacementUsed || batch.ReplacementExhausted {
		return batch, nil
	}
	return service.addReplacement(ctx, batch)
}

func (service *Service) addReplacement(ctx context.Context, batch Batch) (Batch, error) {
	snapshot, err := service.snapshots.Get(ctx, batch.SnapshotID)
	if err != nil {
		return batch, err
	}
	usedAgents := make(map[string]struct{}, len(batch.Slots))
	usedProviders := make(map[string]struct{}, len(batch.Slots))
	for _, slot := range batch.Slots {
		usedAgents[slot.AgentID] = struct{}{}
		usedProviders[slot.ProviderID] = struct{}{}
	}
	for index, scored := range snapshot.Result.Qualified {
		candidate := scored.Candidate
		if _, used := usedAgents[candidate.AgentID]; used {
			continue
		}
		if _, capped := usedProviders[candidate.ProviderID]; capped {
			continue
		}
		replacement, planErr := service.planSlot(ctx, batch, candidate, len(batch.Slots)+1, index+1, true)
		if planErr != nil {
			return batch, planErr
		}
		batch, _, err = service.repository.AddReplacement(ctx, batch.ID, replacement)
		if err != nil {
			return batch, err
		}
		return service.dispatchOutstanding(ctx, batch)
	}
	return service.repository.ExhaustReplacement(ctx, batch.ID)
}

func validateSnapshotForOverview(snapshot matching.Snapshot, requestedID string) error {
	if snapshot.ID != requestedID || !validDigest(snapshot.ID) || !validDigest(snapshot.TaskSpecHash) || snapshot.TaskID == "" || snapshot.MatchRevision < 1 || snapshot.AlgorithmVersion != matching.FairShuffleAlgorithmVersion || len(snapshot.Selections) == 0 || len(snapshot.Selections) > matching.DefaultSelectionLimit {
		return ErrInvalidInput
	}
	return nil
}

func findSlot(batch Batch, slotID string) (Slot, error) {
	for _, slot := range batch.Slots {
		if slot.ID == slotID {
			return slot, nil
		}
	}
	return Slot{}, ErrNotFound
}

func stableID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(fmt.Sprintf("%d:", len(part))))
		_, _ = hash.Write([]byte(part))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

func validDigest(value string) bool {
	if len(value) != len("sha256:")+sha256.Size*2 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validBaseURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/")
}

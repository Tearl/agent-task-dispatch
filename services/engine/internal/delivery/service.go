package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/example/agent-platform/engine/internal/auth"
)

var moneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

type Service struct {
	repository Repository
	revisions  RevisionAuthorizer
	proofs     ProofSigner
}

func NewService(repository Repository) (*Service, error) {
	return NewServiceWithDependencies(repository, PendingRevisionAuthorizer{}, PendingProofSigner{})
}

func NewServiceWithRevisionAuthorizer(repository Repository, revisions RevisionAuthorizer) (*Service, error) {
	return NewServiceWithDependencies(repository, revisions, PendingProofSigner{})
}

func NewServiceWithDependencies(repository Repository, revisions RevisionAuthorizer, proofs ProofSigner) (*Service, error) {
	if repository == nil || revisions == nil || proofs == nil {
		return nil, ErrInvalidInput
	}
	return &Service{repository: repository, revisions: revisions, proofs: proofs}, nil
}

func (service *Service) Start(ctx context.Context, session auth.Session, key, taskID string, input StartInput) (StartResult, bool, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return StartResult{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(key) == "" || len(key) > 200 || input.ExpectedPackageVersion < 0 || input.WorkNonce < 1 || input.WorkNonce > math.MaxInt64 || !validRevision(input.Revision) || (input.ChangeOrderID != "" && !validDigest(input.ChangeOrderID)) {
		return StartResult{}, false, ErrInvalidInput
	}
	requestHash, err := hashJSON(struct {
		TaskID string     `json:"taskId"`
		Input  StartInput `json:"input"`
	}{taskID, input})
	if err != nil {
		return StartResult{}, false, err
	}
	if input.Revision != nil {
		if err = service.revisions.AuthorizeRevision(ctx, session.UserID, taskID, input); err != nil {
			return StartResult{}, false, err
		}
	}
	return service.repository.Start(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, taskID, input)
}

func (service *Service) Get(ctx context.Context, session auth.Session, taskID string) (View, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return View{}, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" {
		return View{}, ErrInvalidInput
	}
	return service.repository.Get(ctx, session.UserID, taskID)
}

func (service *Service) SubmitFeedback(ctx context.Context, session auth.Session, key, taskID string, input FeedbackInput) (FeedbackSet, bool, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return FeedbackSet{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(key) == "" || len(key) > 200 || !validFeedbackInput(input) {
		return FeedbackSet{}, false, ErrInvalidInput
	}
	canonicalItems := make([]FeedbackItem, len(input.Items))
	for index, item := range input.Items {
		itemID, err := hashJSON(struct {
			Version           string            `json:"version"`
			PackageID         string            `json:"packageId"`
			ParentContentHash string            `json:"parentContentHash"`
			Ordinal           int               `json:"ordinal"`
			Item              FeedbackItemInput `json:"item"`
		}{FeedbackVersion, input.PackageID, input.ParentContentHash, index + 1, item})
		if err != nil {
			return FeedbackSet{}, false, err
		}
		canonicalItems[index] = FeedbackItem{ID: itemID, Ordinal: index + 1, FeedbackItemInput: item}
	}
	digest, err := hashJSON(struct {
		Version           string         `json:"version"`
		PackageID         string         `json:"packageId"`
		ParentVersion     int            `json:"parentVersion"`
		ParentContentHash string         `json:"parentContentHash"`
		Items             []FeedbackItem `json:"items"`
	}{FeedbackVersion, input.PackageID, input.ParentVersion, input.ParentContentHash, canonicalItems})
	if err != nil {
		return FeedbackSet{}, false, err
	}
	set := FeedbackSet{ID: digest, PackageID: input.PackageID, ParentVersion: input.ParentVersion, ParentContentHash: input.ParentContentHash, Digest: digest, Items: canonicalItems}
	requestHash, err := hashJSON(struct {
		TaskID string        `json:"taskId"`
		Input  FeedbackInput `json:"input"`
	}{taskID, input})
	if err != nil {
		return FeedbackSet{}, false, err
	}
	return service.repository.SubmitFeedback(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, taskID, input, set)
}

func (service *Service) ProposeChangeOrder(ctx context.Context, session auth.Session, key, taskID string, input ProposeChangeOrderInput) (ChangeOrder, bool, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return ChangeOrder{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || !validMutationKey(key) || !validChangeProposal(input) {
		return ChangeOrder{}, false, ErrInvalidInput
	}
	differenceDigest, err := hashJSON(input.Differences)
	if err != nil {
		return ChangeOrder{}, false, err
	}
	requestHash, err := hashJSON(struct {
		TaskID string                  `json:"taskId"`
		Input  ProposeChangeOrderInput `json:"input"`
	}{taskID, input})
	if err != nil {
		return ChangeOrder{}, false, err
	}
	id, err := hashJSON(struct {
		Version, PackageID, FeedbackSetID, DifferenceDigest string
		TargetVersion                                       int
	}{ChangeOrderVersion, input.PackageID, input.FeedbackSetID, differenceDigest, input.TriggerVersion + 1})
	if err != nil {
		return ChangeOrder{}, false, err
	}
	draft := ChangeOrder{ID: id, PackageID: input.PackageID, TaskID: taskID, TargetVersion: input.TriggerVersion + 1, TriggerVersion: input.TriggerVersion, TriggerContentHash: input.TriggerContentHash, FeedbackSetID: input.FeedbackSetID, FeedbackDigest: input.FeedbackDigest, NewSpecHash: input.NewSpecHash, DifferenceDigest: differenceDigest, Differences: input.Differences, RequestedPrice: input.RequestedPrice, AuthorizedPrice: "0", AggregateVersion: 1, Status: ChangeResponsibilityPending, Deadline: input.Deadline.UTC()}
	return service.repository.ProposeChangeOrder(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, taskID, input, draft)
}

func (service *Service) DecideChangeOrder(ctx context.Context, session auth.Session, key, taskID, changeOrderID string, input DecideChangeOrderInput) (ChangeOrder, bool, error) {
	isAdmin := slices.Contains(session.Roles, "admin") || slices.Contains(session.Roles, "arbitrator")
	if session.UserID == "" || !isAdmin {
		return ChangeOrder{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || !validDigest(changeOrderID) || !validMutationKey(key) || input.ExpectedVersion < 1 || !slices.Contains([]string{ResponsibilityPublisher, ResponsibilityAgent, ResponsibilityPlatform}, input.Responsibility) || !validReason(input.ReasonCode) {
		return ChangeOrder{}, false, ErrInvalidInput
	}
	requestHash, err := hashJSON(struct {
		TaskID, ChangeOrderID string
		Input                 DecideChangeOrderInput
	}{taskID, changeOrderID, input})
	if err != nil {
		return ChangeOrder{}, false, err
	}
	return service.repository.DecideChangeOrder(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, true, taskID, changeOrderID, input)
}

func (service *Service) AcceptChangeOrder(ctx context.Context, session auth.Session, key, taskID, changeOrderID string, input ChangeOrderVersionInput) (ChangeOrder, bool, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return ChangeOrder{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || !validDigest(changeOrderID) || !validMutationKey(key) || input.ExpectedVersion < 1 {
		return ChangeOrder{}, false, ErrInvalidInput
	}
	requestHash, err := hashJSON(struct {
		TaskID, ChangeOrderID string
		Input                 ChangeOrderVersionInput
	}{taskID, changeOrderID, input})
	if err != nil {
		return ChangeOrder{}, false, err
	}
	return service.repository.AcceptChangeOrder(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, taskID, changeOrderID, input)
}

func (service *Service) ActivateChangeOrder(ctx context.Context, session auth.Session, key, taskID, changeOrderID string, input ChangeOrderVersionInput) (ChangeOrder, bool, error) {
	isAdmin := slices.Contains(session.Roles, "admin") || slices.Contains(session.Roles, "arbitrator")
	isPublisher := slices.Contains(session.Roles, "publisher")
	if session.UserID == "" || (!isAdmin && !isPublisher) {
		return ChangeOrder{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || !validDigest(changeOrderID) || !validMutationKey(key) || input.ExpectedVersion < 1 {
		return ChangeOrder{}, false, ErrInvalidInput
	}
	requestHash, err := hashJSON(struct {
		TaskID, ChangeOrderID string
		Input                 ChangeOrderVersionInput
	}{taskID, changeOrderID, input})
	if err != nil {
		return ChangeOrder{}, false, err
	}
	return service.repository.ActivateChangeOrder(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, isAdmin, taskID, changeOrderID, input)
}

func (service *Service) CreateAcceptanceIntent(ctx context.Context, session auth.Session, key, taskID string, input AcceptanceIntentInput) (AcceptanceIntent, bool, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return AcceptanceIntent{}, false, ErrForbidden
	}
	if strings.TrimSpace(taskID) == "" || !validMutationKey(key) || !validDigest(input.PackageID) || input.ExpectedPackageVersion < 1 || input.FormalVersion < 1 || input.FormalVersion > MaximumVersions || !validDigest(input.ContentHash) || !validDigest(input.ProofDigest) || input.WorkNonce < 1 || input.WorkNonce > math.MaxInt64 {
		return AcceptanceIntent{}, false, ErrInvalidInput
	}
	requestHash, err := hashJSON(struct {
		TaskID string
		Input  AcceptanceIntentInput
	}{taskID, input})
	if err != nil {
		return AcceptanceIntent{}, false, err
	}
	id, err := hashJSON(struct {
		Version, PackageID, ContentHash, ProofDigest string
		FormalVersion                                int
		WorkNonce                                    uint64
	}{AcceptanceVersion, input.PackageID, input.ContentHash, input.ProofDigest, input.FormalVersion, input.WorkNonce})
	if err != nil {
		return AcceptanceIntent{}, false, err
	}
	draft := AcceptanceIntent{ID: id, PackageID: input.PackageID, TaskID: taskID, FormalVersion: input.FormalVersion, ContentHash: input.ContentHash, ProofDigest: input.ProofDigest, WorkNonce: input.WorkNonce, PackageAggregateVersion: input.ExpectedPackageVersion, AggregateVersion: 1, State: AcceptanceIntentRecorded, Eligibility: SettlementEligibility{Eligible: true}}
	return service.repository.CreateAcceptanceIntent(ctx, Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}, taskID, input, draft)
}

func (service *Service) SubmitAcceptance(ctx context.Context, session auth.Session, key, taskID, intentID string, input AcceptanceTransitionInput) (AcceptanceIntent, bool, error) {
	return service.acceptanceTransition(ctx, session, key, taskID, intentID, input, false)
}

func (service *Service) ReconcileAcceptance(ctx context.Context, session auth.Session, key, taskID, intentID string, input AcceptanceTransitionInput) (AcceptanceIntent, bool, error) {
	return service.acceptanceTransition(ctx, session, key, taskID, intentID, input, true)
}

func (service *Service) acceptanceTransition(ctx context.Context, session auth.Session, key, taskID, intentID string, input AcceptanceTransitionInput, reconcile bool) (AcceptanceIntent, bool, error) {
	if session.UserID == "" || !slices.Contains(session.Roles, "publisher") {
		return AcceptanceIntent{}, false, ErrForbidden
	}
	validTx := strings.HasPrefix(input.TransactionHash, "0x") && len(input.TransactionHash) == 66
	if strings.TrimSpace(taskID) == "" || !validDigest(intentID) || !validMutationKey(key) || input.ExpectedVersion < 1 || (!reconcile && !validTx) || (reconcile && input.TransactionHash != "") {
		return AcceptanceIntent{}, false, ErrInvalidInput
	}
	if validTx {
		if _, err := hex.DecodeString(input.TransactionHash[2:]); err != nil {
			return AcceptanceIntent{}, false, ErrInvalidInput
		}
		input.TransactionHash = strings.ToLower(input.TransactionHash)
	}
	requestHash, err := hashJSON(struct {
		TaskID, IntentID, Operation string
		Input                       AcceptanceTransitionInput
	}{taskID, intentID, map[bool]string{true: "reconcile", false: "submit"}[reconcile], input})
	if err != nil {
		return AcceptanceIntent{}, false, err
	}
	mutation := Mutation{PublisherID: session.UserID, IdempotencyKey: key, RequestHash: requestHash}
	if reconcile {
		return service.repository.ReconcileAcceptance(ctx, mutation, taskID, intentID, input)
	}
	return service.repository.SubmitAcceptance(ctx, mutation, taskID, intentID, input)
}

// RecordDispatched and RecordResult are trusted worker boundaries. Public HTTP
// callers never choose formal version state or billing outcomes.
func (service *Service) RecordDispatched(ctx context.Context, logicalExecutionID string) (Version, bool, error) {
	if strings.TrimSpace(logicalExecutionID) == "" {
		return Version{}, false, ErrInvalidInput
	}
	return service.repository.RecordDispatched(ctx, logicalExecutionID)
}

func (service *Service) RecordResult(ctx context.Context, result ExecutionResult) (Version, bool, error) {
	if strings.TrimSpace(result.LogicalExecutionID) == "" || !moneyPattern.MatchString(result.UsedCost) {
		return Version{}, false, ErrInvalidInput
	}
	switch result.Status {
	case ResultSucceeded:
		if !validDigest(result.ContentHash) || strings.TrimSpace(result.DeliverableRef) == "" || result.FailureReasonCode != "" || !validResponses(result.FeedbackResponses) || !validChanges(result.Changes) {
			return Version{}, false, ErrInvalidInput
		}
	case ResultFailed:
		if result.ContentHash != "" || result.DeliverableRef != "" || !validReason(result.FailureReasonCode) {
			return Version{}, false, ErrInvalidInput
		}
	default:
		return Version{}, false, ErrInvalidInput
	}
	if result.Status == ResultFailed {
		return service.repository.RecordResult(ctx, result, nil)
	}
	proofContext, err := service.repository.ProofContext(ctx, result.LogicalExecutionID)
	if err != nil {
		return Version{}, false, err
	}
	if !sameFeedbackItems(proofContext.FeedbackItemIDs, result.FeedbackResponses) {
		return Version{}, false, ErrInvalidInput
	}
	if len(proofContext.FeedbackItemIDs) > 0 {
		byID := make(map[string]FeedbackResponse, len(result.FeedbackResponses))
		for _, response := range result.FeedbackResponses {
			byID[response.FeedbackItemID] = response
		}
		ordered := make([]FeedbackResponse, 0, len(proofContext.FeedbackItemIDs))
		for _, id := range proofContext.FeedbackItemIDs {
			ordered = append(ordered, byID[id])
		}
		result.FeedbackResponses = ordered
	}
	responseHash, err := hashJSON(result.FeedbackResponses)
	if err != nil {
		return Version{}, false, err
	}
	changeHash, err := hashJSON(result.Changes)
	if err != nil {
		return Version{}, false, err
	}
	proof := Proof{Version: ProofVersion, TaskID: proofContext.TaskID, AssignmentID: proofContext.AssignmentID, DeliveryUnit: proofContext.DeliveryUnit, PackageID: proofContext.PackageID, ScopeHash: proofContext.ScopeHash, FormalVersion: proofContext.Version, PackageAggregateVersion: proofContext.PackageAggregateVersion, WorkNonce: proofContext.WorkNonce, AgentID: proofContext.AgentID, ContentHash: result.ContentHash, ParentContentHash: proofContext.ParentContentHash, FeedbackDigest: proofContext.FeedbackDigest, ChangeOrderID: proofContext.ChangeOrderID, AgentResponseHash: responseHash, ChangeSummaryHash: changeHash, PolicyHash: proofContext.PolicyHash, Deadline: proofContext.Deadline.Unix()}
	payloadHash, proofDigest, signature, err := service.proofs.Sign(proof)
	if err != nil {
		return Version{}, false, err
	}
	return service.repository.RecordResult(ctx, result, &ProofRecord{Proof: proof, PayloadHash: payloadHash, Digest: proofDigest, Signature: signature})
}

func validRevision(value *RevisionBinding) bool {
	if value == nil {
		return true
	}
	return value.ParentVersion > 0 && value.FeedbackAggregateVersion > 0 && validDigest(value.ParentContentHash) && validDigest(value.FeedbackSetID) && validDigest(value.FeedbackDigest)
}

func validMutationKey(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 200 }

func validChangeProposal(value ProposeChangeOrderInput) bool {
	if !validDigest(value.PackageID) || value.ExpectedPackageVersion < 1 || value.TriggerVersion < IncludedVersions || value.TriggerVersion >= MaximumVersions || !validDigest(value.TriggerContentHash) || !validDigest(value.FeedbackSetID) || !validDigest(value.FeedbackDigest) || !validDigest(value.NewSpecHash) || !moneyPattern.MatchString(value.RequestedPrice) || value.RequestedPrice == "0" || len(value.Differences) < 1 || len(value.Differences) > 100 || value.Deadline.IsZero() {
		return false
	}
	seen := make(map[string]bool, len(value.Differences))
	for _, difference := range value.Differences {
		if strings.TrimSpace(difference.Path) == "" || len(difference.Path) > 1000 || strings.TrimSpace(difference.Description) == "" || len(difference.Description) > 4000 || !slices.Contains([]string{"added", "modified", "deleted"}, difference.Kind) || difference.WorkloadDeltaPercent < 0 || difference.WorkloadDeltaPercent > 10000 || (difference.BeforeHash != "" && !validDigest(difference.BeforeHash)) || (difference.AfterHash != "" && !validDigest(difference.AfterHash)) || seen[difference.Path] {
			return false
		}
		if (difference.Kind == "added" && (difference.BeforeHash != "" || difference.AfterHash == "")) || (difference.Kind == "deleted" && (difference.BeforeHash == "" || difference.AfterHash != "")) || (difference.Kind == "modified" && (difference.BeforeHash == "" || difference.AfterHash == "")) {
			return false
		}
		seen[difference.Path] = true
	}
	return true
}

func validFeedbackInput(value FeedbackInput) bool {
	if !validDigest(value.PackageID) || value.ExpectedPackageVersion < 1 || value.ParentVersion < 1 || !validDigest(value.ParentContentHash) || len(value.Items) < 1 || len(value.Items) > 100 {
		return false
	}
	seen := make(map[string]bool, len(value.Items))
	for _, item := range value.Items {
		if strings.TrimSpace(item.CriterionID) == "" || len(item.CriterionID) > 200 || strings.TrimSpace(item.Target) == "" || len(item.Target) > 500 || strings.TrimSpace(item.Description) == "" || len(item.Description) > 4000 || strings.TrimSpace(item.ExpectedOutcome) == "" || len(item.ExpectedOutcome) > 4000 || !slices.Contains([]string{"defect", "omission", "security", "runtime", "clarification"}, item.Category) || !slices.Contains([]string{"low", "medium", "high", "blocker"}, item.Priority) || !slices.Contains([]string{"in_scope", "out_of_scope", "uncertain"}, item.ScopeClaim) {
			return false
		}
		key := item.CriterionID + "\x00" + item.Target
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

func validResponses(values []FeedbackResponse) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !validDigest(value.FeedbackItemID) || !slices.Contains([]string{"resolved", "not_reproduced", "declined"}, value.Disposition) || strings.TrimSpace(value.Summary) == "" || len(value.Summary) > 4000 || seen[value.FeedbackItemID] {
			return false
		}
		seen[value.FeedbackItemID] = true
	}
	return true
}

func validChanges(values []Change) bool {
	if len(values) > 500 {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value.Path) == "" || len(value.Path) > 1000 || !slices.Contains([]string{"added", "modified", "deleted"}, value.Kind) || (value.BeforeHash != "" && !validDigest(value.BeforeHash)) || (value.AfterHash != "" && !validDigest(value.AfterHash)) || (value.BeforeHash == "" && value.AfterHash == "") {
			return false
		}
		if (value.Kind == "added" && (value.BeforeHash != "" || value.AfterHash == "")) || (value.Kind == "deleted" && (value.BeforeHash == "" || value.AfterHash != "")) || (value.Kind == "modified" && (value.BeforeHash == "" || value.AfterHash == "")) {
			return false
		}
	}
	return true
}

func sameFeedbackItems(expected []string, responses []FeedbackResponse) bool {
	if len(expected) != len(responses) {
		return false
	}
	seen := make(map[string]bool, len(responses))
	for _, response := range responses {
		seen[response.FeedbackItemID] = true
	}
	for _, id := range expected {
		if !seen[id] {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != 71 {
		return false
	}
	_, err := hex.DecodeString(value[7:])
	return err == nil
}

func validReason(value string) bool {
	if len(value) < 1 || len(value) > 100 {
		return false
	}
	for _, character := range value {
		if character != '_' && character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func hashJSON(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

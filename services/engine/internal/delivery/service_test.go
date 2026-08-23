package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/example/agent-platform/engine/internal/auth"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type repositoryStub struct {
	startInput      StartInput
	mutation        Mutation
	result          StartResult
	proofContext    ProofContext
	executionResult ExecutionResult
	proof           *ProofRecord
	err             error
}

type revisionAuthorizerStub struct{ err error }

func (stub revisionAuthorizerStub) AuthorizeRevision(context.Context, string, string, StartInput) error {
	return stub.err
}

func (repository *repositoryStub) Start(_ context.Context, mutation Mutation, _ string, input StartInput) (StartResult, bool, error) {
	repository.mutation, repository.startInput = mutation, input
	return repository.result, false, repository.err
}
func (repository *repositoryStub) Get(context.Context, string, string) (View, error) {
	return View{}, repository.err
}
func (repository *repositoryStub) SubmitFeedback(_ context.Context, _ Mutation, _ string, _ FeedbackInput, set FeedbackSet) (FeedbackSet, bool, error) {
	return set, false, repository.err
}
func (repository *repositoryStub) ProofContext(context.Context, string) (ProofContext, error) {
	return repository.proofContext, repository.err
}
func (repository *repositoryStub) RecordDispatched(context.Context, string) (Version, bool, error) {
	return Version{}, false, repository.err
}
func (repository *repositoryStub) RecordResult(context.Context, ExecutionResult, *ProofRecord) (Version, bool, error) {
	return Version{}, false, repository.err
}
func (repository *repositoryStub) ProposeChangeOrder(context.Context, Mutation, string, ProposeChangeOrderInput, ChangeOrder) (ChangeOrder, bool, error) {
	return ChangeOrder{}, false, repository.err
}
func (repository *repositoryStub) DecideChangeOrder(context.Context, Mutation, bool, string, string, DecideChangeOrderInput) (ChangeOrder, bool, error) {
	return ChangeOrder{}, false, repository.err
}
func (repository *repositoryStub) AcceptChangeOrder(context.Context, Mutation, string, string, ChangeOrderVersionInput) (ChangeOrder, bool, error) {
	return ChangeOrder{}, false, repository.err
}
func (repository *repositoryStub) ActivateChangeOrder(context.Context, Mutation, bool, string, string, ChangeOrderVersionInput) (ChangeOrder, bool, error) {
	return ChangeOrder{}, false, repository.err
}
func (repository *repositoryStub) CreateAcceptanceIntent(context.Context, Mutation, string, AcceptanceIntentInput, AcceptanceIntent) (AcceptanceIntent, bool, error) {
	return AcceptanceIntent{}, false, repository.err
}
func (repository *repositoryStub) SubmitAcceptance(context.Context, Mutation, string, string, AcceptanceTransitionInput) (AcceptanceIntent, bool, error) {
	return AcceptanceIntent{}, false, repository.err
}
func (repository *repositoryStub) ReconcileAcceptance(context.Context, Mutation, string, string, AcceptanceTransitionInput) (AcceptanceIntent, bool, error) {
	return AcceptanceIntent{}, false, repository.err
}

type proofSignerStub struct{ proof Proof }

func (signer *proofSignerStub) Sign(proof Proof) (string, string, string, error) {
	signer.proof = proof
	return testDigest, testDigest, "0x" + strings.Repeat("a", 130), nil
}

type recordingRepository struct{ repositoryStub }

type acceptanceRecordingRepository struct {
	repositoryStub
	intent   AcceptanceIntent
	mutation Mutation
}

func (repository *acceptanceRecordingRepository) CreateAcceptanceIntent(_ context.Context, mutation Mutation, _ string, _ AcceptanceIntentInput, intent AcceptanceIntent) (AcceptanceIntent, bool, error) {
	repository.intent, repository.mutation = intent, mutation
	return intent, false, repository.err
}

func (repository *recordingRepository) RecordResult(_ context.Context, result ExecutionResult, proof *ProofRecord) (Version, bool, error) {
	repository.executionResult, repository.proof = result, proof
	return Version{}, false, repository.err
}

func TestStartRequiresPublisherBeforeRepository(t *testing.T) {
	repository := &repositoryStub{err: errors.New("must not be called")}
	service, _ := NewServiceWithRevisionAuthorizer(repository, revisionAuthorizerStub{})
	_, _, err := service.Start(context.Background(), auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}, "operation", "task", StartInput{WorkNonce: 1})
	if !errors.Is(err, ErrForbidden) || repository.mutation.PublisherID != "" {
		t.Fatalf("authorization boundary failed: mutation=%#v err=%v", repository.mutation, err)
	}
}

func TestSubmitFeedbackCreatesStableStructuredIdentity(t *testing.T) {
	repository := &repositoryStub{}
	service, _ := NewService(repository)
	input := FeedbackInput{PackageID: testDigest, ExpectedPackageVersion: 2, ParentVersion: 1, ParentContentHash: testDigest, Items: []FeedbackItemInput{{CriterionID: "criterion-1", Category: "defect", Priority: "high", Target: "output.png", Description: "incorrect size", ExpectedOutcome: "1024x1024", ScopeClaim: "in_scope"}}}
	first, _, err := service.SubmitFeedback(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "feedback-1", "task", input)
	if err != nil || !validDigest(first.ID) || first.ID != first.Digest || len(first.Items) != 1 || !validDigest(first.Items[0].ID) {
		t.Fatalf("feedback=%#v err=%v", first, err)
	}
	second, _, _ := service.SubmitFeedback(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "feedback-1", "task", input)
	if first.ID != second.ID || first.Items[0].ID != second.Items[0].ID {
		t.Fatal("feedback identity is not deterministic")
	}
}

func TestSuccessfulResultRequiresCompleteFeedbackAndBuildsBoundProof(t *testing.T) {
	itemA := "sha256:" + strings.Repeat("b", 64)
	itemB := "sha256:" + strings.Repeat("c", 64)
	repository := &recordingRepository{repositoryStub: repositoryStub{proofContext: ProofContext{TaskID: "task", AssignmentID: "assignment", DeliveryUnit: "default", PackageID: testDigest, ScopeHash: testDigest, Version: 2, PackageAggregateVersion: 4, WorkNonce: 2, AgentID: "agent", ParentContentHash: testDigest, FeedbackDigest: testDigest, FeedbackItemIDs: []string{itemA, itemB}, PolicyHash: testDigest}}}
	signer := &proofSignerStub{}
	service, _ := NewServiceWithDependencies(repository, revisionAuthorizerStub{}, signer)
	result := ExecutionResult{LogicalExecutionID: "execution", Status: ResultSucceeded, ContentHash: testDigest, DeliverableRef: "artifact", UsedCost: "0", FeedbackResponses: []FeedbackResponse{{FeedbackItemID: itemB, Disposition: "resolved", Summary: "b"}, {FeedbackItemID: itemA, Disposition: "resolved", Summary: "a"}}, Changes: []Change{{Path: "output.png", Kind: "added", AfterHash: testDigest}}}
	if _, _, err := service.RecordResult(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	if repository.proof == nil || signer.proof.WorkNonce != 2 || signer.proof.FeedbackDigest != testDigest || repository.executionResult.FeedbackResponses[0].FeedbackItemID != itemA {
		t.Fatalf("proof=%#v result=%#v", repository.proof, repository.executionResult)
	}
	result.FeedbackResponses = result.FeedbackResponses[:1]
	if _, _, err := service.RecordResult(context.Background(), result); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("partial feedback accepted: %v", err)
	}
}

func TestRevisionStartFailsClosedWithoutAuthoritativeGate(t *testing.T) {
	repository := &repositoryStub{}
	service, _ := NewService(repository)
	input := StartInput{ExpectedPackageVersion: 2, WorkNonce: 2, Revision: &RevisionBinding{ParentVersion: 1, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest, FeedbackAggregateVersion: 2}}
	_, _, err := service.Start(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "operation", "task", input)
	if !errors.Is(err, ErrDependencyPending) || repository.mutation.PublisherID != "" {
		t.Fatalf("unverified revision reached repository: mutation=%#v err=%v", repository.mutation, err)
	}
}

func TestStartBindsIdempotencyToRevisionAndWorkNonce(t *testing.T) {
	repository := &repositoryStub{}
	service, _ := NewServiceWithRevisionAuthorizer(repository, revisionAuthorizerStub{})
	input := StartInput{ExpectedPackageVersion: 2, WorkNonce: 2, Revision: &RevisionBinding{ParentVersion: 1, ParentContentHash: testDigest, FeedbackSetID: testDigest, FeedbackDigest: testDigest, FeedbackAggregateVersion: 2}}
	_, _, err := service.Start(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "operation", "task", input)
	if err != nil || repository.mutation.RequestHash == "" || repository.startInput.WorkNonce != 2 {
		t.Fatalf("valid start was not forwarded: mutation=%#v input=%#v err=%v", repository.mutation, repository.startInput, err)
	}
	firstHash := repository.mutation.RequestHash
	input.WorkNonce = 3
	_, _, _ = service.Start(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "operation", "task", input)
	if repository.mutation.RequestHash == firstHash {
		t.Fatal("work nonce was not bound to request hash")
	}
}

func TestStartRejectsPartialRevisionBinding(t *testing.T) {
	service, _ := NewService(&repositoryStub{})
	_, _, err := service.Start(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "operation", "task", StartInput{WorkNonce: 2, Revision: &RevisionBinding{ParentVersion: 1}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("partial binding accepted: %v", err)
	}
}

func TestRecordResultValidatesTrustedWorkerPayload(t *testing.T) {
	service, _ := NewService(&repositoryStub{})
	for _, result := range []ExecutionResult{
		{LogicalExecutionID: "execution", Status: ResultSucceeded, ContentHash: "bad", DeliverableRef: "artifact", UsedCost: "0"},
		{LogicalExecutionID: "execution", Status: ResultFailed, UsedCost: "0", FailureReasonCode: "untrusted free text"},
		{LogicalExecutionID: "execution", Status: ResultSucceeded, ContentHash: testDigest, DeliverableRef: "artifact", UsedCost: "-1"},
	} {
		if _, _, err := service.RecordResult(context.Background(), result); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid worker result accepted: %#v err=%v", result, err)
		}
	}
}

func TestAcceptanceIntentIsPublisherAuthorizedAndProofBound(t *testing.T) {
	repository := &acceptanceRecordingRepository{}
	service, _ := NewService(repository)
	input := AcceptanceIntentInput{PackageID: testDigest, ExpectedPackageVersion: 2, FormalVersion: 1, ContentHash: testDigest, ProofDigest: testDigest, WorkNonce: 1}
	if _, _, err := service.CreateAcceptanceIntent(context.Background(), auth.Session{UserID: "provider", Roles: []string{"agent_provider"}}, "accept-1", "task", input); !errors.Is(err, ErrForbidden) {
		t.Fatalf("provider created acceptance intent: %v", err)
	}
	first, _, err := service.CreateAcceptanceIntent(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "accept-1", "task", input)
	if err != nil || !validDigest(first.ID) || first.State != AcceptanceIntentRecorded || repository.mutation.RequestHash == "" {
		t.Fatalf("intent=%#v mutation=%#v err=%v", first, repository.mutation, err)
	}
	input.WorkNonce = 2
	second, _, _ := service.CreateAcceptanceIntent(context.Background(), auth.Session{UserID: "publisher", Roles: []string{"publisher"}}, "accept-2", "task", input)
	if first.ID == second.ID {
		t.Fatal("work nonce was not bound to acceptance identity")
	}
}

func TestAcceptanceTransitionsValidateTransactionBoundary(t *testing.T) {
	service, _ := NewService(&repositoryStub{})
	session := auth.Session{UserID: "publisher", Roles: []string{"publisher"}}
	if _, _, err := service.SubmitAcceptance(context.Background(), session, "submit", "task", testDigest, AcceptanceTransitionInput{ExpectedVersion: 1, TransactionHash: "bad"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid tx hash accepted: %v", err)
	}
	if _, _, err := service.ReconcileAcceptance(context.Background(), session, "reconcile", "task", testDigest, AcceptanceTransitionInput{ExpectedVersion: 2, TransactionHash: "0x" + strings.Repeat("a", 64)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("reconcile accepted caller-selected transaction: %v", err)
	}
}

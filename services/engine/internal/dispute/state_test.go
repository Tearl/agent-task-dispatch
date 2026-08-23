package dispute

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

func testContext() Context {
	return Context{TaskID: "task-1", AssignmentID: "assignment-1", DeliveryUnitID: "package-1", PublisherID: "publisher-1", AgentProviderID: "provider-1", ChainID: "1", ContractAddress: "0x1111111111111111111111111111111111111111", ChainTaskID: "0x" + repeat("1", 64), PublisherWallet: "0x" + repeat("2", 40), AgentController: "0x" + repeat("3", 40), AgentPayout: "0x" + repeat("4", 40), DisputeResolver: "0x" + repeat("5", 40), FrozenAmount: "100", Asset: "evm:1/native", FeeCap: "0", Eligible: true, DisputeDeadline: time.Now().UTC().Add(time.Hour)}
}
func testDigest(seed string) string        { return digest("test", seed) }
func session(id, role string) auth.Session { return auth.Session{UserID: id, Roles: []string{role}} }

func TestIndependentCounterclaimAndIdempotency(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	opened, replay, err := service.Open(ctx, session("publisher-1", "publisher"), "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "acceptance_failed", StatementHash: testDigest("publisher")})
	if err != nil || replay {
		t.Fatalf("open: replay=%v err=%v", replay, err)
	}
	_, replay, err = service.Open(ctx, session("publisher-1", "publisher"), "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "acceptance_failed", StatementHash: testDigest("publisher")})
	if err != nil || !replay {
		t.Fatalf("idempotency replay: %v %v", replay, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for index, side := range []struct{ id, role, key, kind string }{{"provider-1", "agent_provider", "counter", "scope"}, {"publisher-1", "publisher", "duplicate", "quality"}} {
		wg.Add(1)
		go func(index int, side struct{ id, role, key, kind string }) {
			defer wg.Done()
			_, _, claimErr := service.AddClaim(ctx, session(side.id, side.role), side.key, opened.Case.ID, ClaimInput{Kind: side.kind, ReasonCode: "claim_reason", StatementHash: testDigest(side.key)})
			errs <- claimErr
		}(index, side)
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for claimErr := range errs {
		if claimErr == nil {
			success++
		} else if claimErr == ErrConflict {
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("counterclaim was preempted: success=%d conflict=%d", success, conflict)
	}
}

func TestSoftLockEvidenceDecisionAndSingleReview(t *testing.T) {
	now := time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)
	c := testContext()
	c.DisputeDeadline = now.Add(time.Hour)
	view, err := Apply(nil, c, "publisher-1", Command{Kind: "open", Input: OpenInput{DeliveryUnitID: c.DeliveryUnitID, Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")}}, now)
	if err != nil || view.Case.State != StateSoftLock || view.Case.FrozenAt != nil {
		t.Fatalf("soft lock: %#v %v", view.Case, err)
	}
	view, err = Apply(&view, c, "publisher-1", Command{Kind: "freeze_submit", Input: FreezeInput{TransactionHash: "0x" + repeat("a", 64)}}, now.Add(time.Minute))
	if err != nil || view.Case.State != StateSoftLock {
		t.Fatalf("submission became frozen: %v %v", view.Case.State, err)
	}
	view, err = Apply(&view, c, "publisher-1", Command{Kind: "freeze_confirm", Input: FreezeInput{TransactionHash: "0x" + repeat("a", 64)}}, now.Add(2*time.Minute))
	if err != nil || view.Case.State != StateFrozen || view.Case.FrozenAt == nil {
		t.Fatalf("confirm: %v %v", view.Case.State, err)
	}
	claimID := view.Case.Claims[0].ID
	for i, category := range requiredEvidence {
		input := EvidenceInput{ClaimID: claimID, Category: category, ObjectKey: "case/object/" + category, CiphertextDigest: testDigest(category), EnvelopeKeyReference: "kms:key/case", ObjectVersionID: "version-" + category, RetentionMode: "COMPLIANCE", RetainUntil: now.Add(365 * 24 * time.Hour)}
		view, err = Apply(&view, c, "publisher-1", Command{Kind: "evidence", Input: input}, now.Add(time.Duration(3+i)*time.Minute))
		if err != nil {
			t.Fatalf("evidence %s: %v", category, err)
		}
	}
	view, err = Apply(&view, c, "admin-1", Command{Kind: "assign", Input: AssignInput{AssigneeID: "arb-1", Stage: "initial"}}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	view, err = Apply(&view, c, "admin-1", Command{Kind: "assign", Input: AssignInput{AssigneeID: "arb-2", Stage: "review"}}, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	bad := DecisionInput{PublisherBPS: 3300, ReasonCode: "evidence_weight", EvidenceRoot: testDigest("root")}
	_, err = Apply(&view, c, "arb-1", Command{Kind: "decision", Input: bad}, view.Case.EvidenceDeadline.Add(time.Minute))
	if err != ErrEvidenceIncomplete {
		t.Fatalf("invalid tier accepted: %v", err)
	}
	good := bad
	good.PublisherBPS = 5000
	view, err = Apply(&view, c, "arb-1", Command{Kind: "decision", Input: good}, view.Case.EvidenceDeadline.Add(time.Minute))
	if err != nil || view.Case.State != StateDecided || !view.Case.ReputationPending {
		t.Fatalf("decision: %v %v", view.Case.State, err)
	}
	review := ReviewInput{AssigneeID: "arb-1", FeeAuthorized: true, ReasonCode: "uphold", EvidenceRoot: testDigest("review"), PublisherBPS: 5000}
	_, err = Apply(&view, c, "arb-1", Command{Kind: "review", Input: review}, view.Case.EvidenceDeadline.Add(2*time.Minute))
	if err != ErrInvalidState {
		t.Fatalf("same-person review accepted: %v", err)
	}
	review.AssigneeID = "arb-2"
	view, err = Apply(&view, c, "arb-2", Command{Kind: "review", Input: review}, view.Case.EvidenceDeadline.Add(2*time.Minute))
	if err != nil || view.Case.State != StateFinal || view.Case.ReputationPending {
		t.Fatalf("review: %v %v", view.Case.State, err)
	}
	_, err = Apply(&view, c, "arb-2", Command{Kind: "review", Input: review}, view.Case.EvidenceDeadline.Add(3*time.Minute))
	if err == nil {
		t.Fatal("second review accepted")
	}
}

func TestSettlementAllowsBasisPointsButRequiresDistinctPartySignatures(t *testing.T) {
	now := time.Now().UTC()
	c := testContext()
	view, _ := Apply(nil, c, "publisher-1", Command{Kind: "open", Input: OpenInput{DeliveryUnitID: c.DeliveryUnitID, Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")}}, now)
	view, _ = Apply(&view, c, "publisher-1", Command{Kind: "freeze_submit", Input: FreezeInput{TransactionHash: "0x" + repeat("a", 64)}}, now)
	view, _ = Apply(&view, c, "publisher-1", Command{Kind: "freeze_confirm", Input: FreezeInput{TransactionHash: "0x" + repeat("a", 64)}}, now)
	input := SettlementInput{PublisherBPS: 3333, ReasonCode: "signed_settlement", EvidenceRoot: testDigest("root"), AgreementHash: testDigest("agreement")}
	if _, err := Apply(&view, c, "publisher-1", Command{Kind: "settlement", Input: input}, now); err == nil {
		t.Fatal("same signatures accepted")
	}
	input.Verified = true
	view, err := Apply(&view, c, "publisher-1", Command{Kind: "settlement", Input: input}, now)
	if err != nil || view.Case.State != StateFinal || view.Case.ReputationPending {
		t.Fatalf("settlement: %v %v", view.Case.State, err)
	}
}

func TestEvidenceRequiresTrustedWORMReceipt(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	publisher := session("publisher-1", "publisher")
	view, _, _ := service.Open(ctx, publisher, "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	tx := "0x" + repeat("a", 64)
	view, _, _ = service.SubmitFreeze(ctx, publisher, "submit", view.Case.ID, FreezeInput{TransactionHash: tx})
	view, _, _ = service.ReconcileFreeze(ctx, publisher, "confirm", view.Case.ID, FreezeInput{TransactionHash: tx})
	input := EvidenceInput{ClaimID: view.Case.Claims[0].ID, Category: "specification", ObjectKey: "worm/case/spec", CiphertextDigest: testDigest("cipher"), EnvelopeKeyReference: "kms:key/case", ObjectVersionID: "v1", RetentionMode: "COMPLIANCE", RetainUntil: time.Now().UTC().Add(365 * 24 * time.Hour)}
	if _, _, err := service.AppendEvidence(ctx, publisher, "evidence", view.Case.ID, input); err != ErrInvalidState {
		t.Fatalf("unverified WORM object accepted: %v", err)
	}
	repo.RecordEvidenceReceipt(input)
	if _, _, err := service.AppendEvidence(ctx, publisher, "evidence", view.Case.ID, input); err != nil {
		t.Fatalf("verified WORM object rejected: %v", err)
	}
}

func TestEvidenceAccessAdvancesVersionAndRejectsUnknownEvidence(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	publisher := session("publisher-1", "publisher")
	view, _, _ := service.Open(ctx, publisher, "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	tx := "0x" + repeat("a", 64)
	view, _, _ = service.SubmitFreeze(ctx, publisher, "submit", view.Case.ID, FreezeInput{TransactionHash: tx})
	view, _, _ = service.ReconcileFreeze(ctx, publisher, "confirm", view.Case.ID, FreezeInput{TransactionHash: tx})
	input := EvidenceInput{ClaimID: view.Case.Claims[0].ID, Category: "specification", ObjectKey: "worm/case/access", CiphertextDigest: testDigest("access"), EnvelopeKeyReference: "kms:key/case", ObjectVersionID: "v1", RetentionMode: "COMPLIANCE", RetainUntil: time.Now().UTC().Add(365 * 24 * time.Hour)}
	repo.RecordEvidenceReceipt(input)
	view, _, _ = service.AppendEvidence(ctx, publisher, "evidence", view.Case.ID, input)
	before := view.Case.AggregateVersion
	view, _, err := service.GrantAccess(ctx, publisher, "grant", view.Case.ID, AccessInput{EvidenceID: view.Case.Evidence[0].ID, Purpose: "case_review", TTLSeconds: 60})
	if err != nil || view.Case.AggregateVersion != before+1 || len(view.AccessGrants) != 1 {
		t.Fatalf("access grant: version=%d grants=%d err=%v", view.Case.AggregateVersion, len(view.AccessGrants), err)
	}
	_, _, err = service.GrantAccess(ctx, publisher, "missing", view.Case.ID, AccessInput{EvidenceID: testDigest("missing"), Purpose: "case_review", TTLSeconds: 60})
	if err != ErrNotFound {
		t.Fatalf("unknown evidence accepted: %v", err)
	}
}

func TestAssignmentUsesTrustedConflictDeclarations(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	view, _, _ := service.Open(ctx, session("publisher-1", "publisher"), "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	repo.DeclareConflict(view.Case.ID, "arb-conflicted")
	_, _, err := service.Assign(ctx, session("admin-1", "admin"), "assign", view.Case.ID, AssignInput{AssigneeID: "arb-conflicted", Stage: "initial"})
	if err != ErrConflict {
		t.Fatalf("declared conflict accepted: %v", err)
	}
}

func TestArbitratorsOnlyListAssignedCases(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	view, _, _ := service.Open(ctx, session("publisher-1", "publisher"), "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	values, err := service.List(ctx, session("arb-1", "arbitrator"))
	if err != nil || len(values) != 0 {
		t.Fatalf("unassigned arbitrator saw cases: count=%d err=%v", len(values), err)
	}
	_, _, err = service.Assign(ctx, session("admin-1", "admin"), "assign", view.Case.ID, AssignInput{AssigneeID: "arb-1", Stage: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	values, err = service.List(ctx, session("arb-1", "arbitrator"))
	if err != nil || len(values) != 1 {
		t.Fatalf("assigned arbitrator could not see case: count=%d err=%v", len(values), err)
	}
}

func TestOrphanedFreezeCanBeResubmittedAndReconfirmed(t *testing.T) {
	now := time.Now().UTC()
	c := testContext()
	view, _ := Apply(nil, c, "publisher-1", Command{Kind: "open", Input: OpenInput{DeliveryUnitID: c.DeliveryUnitID, Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")}}, now)
	oldTx := "0x" + repeat("a", 64)
	view, _ = Apply(&view, c, "publisher-1", Command{Kind: "freeze_submit", Input: FreezeInput{TransactionHash: oldTx}}, now)
	view, _ = Apply(&view, c, "publisher-1", Command{Kind: "freeze_confirm", Input: FreezeInput{TransactionHash: oldTx}}, now)
	view.Case.State = StateOrphaned
	view.Case.FrozenAt = nil
	newTx := "0x" + repeat("b", 64)
	view, err := Apply(&view, c, "provider-1", Command{Kind: "freeze_submit", Input: FreezeInput{TransactionHash: newTx}}, now.Add(time.Minute))
	if err != nil || view.Case.State != StateOrphaned || view.Case.FreezeTransactionHash != newTx {
		t.Fatalf("orphan resubmission: state=%s tx=%s err=%v", view.Case.State, view.Case.FreezeTransactionHash, err)
	}
	view, err = Apply(&view, c, "provider-1", Command{Kind: "freeze_confirm", Input: FreezeInput{TransactionHash: newTx}}, now.Add(2*time.Minute))
	if err != nil || view.Case.State != StateFrozen || view.Case.FrozenAt == nil {
		t.Fatalf("orphan reconfirmation: state=%s err=%v", view.Case.State, err)
	}
}

func TestReviewUsesTrustedFeeAuthorization(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	view, _, _ := service.Open(ctx, session("publisher-1", "publisher"), "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	view, _, _ = service.Assign(ctx, session("admin-1", "admin"), "assign-review", view.Case.ID, AssignInput{AssigneeID: "arb-1", Stage: "review"})
	input := ReviewInput{AssigneeID: "arb-1", FeeAuthorized: true, ReasonCode: "uphold", EvidenceRoot: EvidenceRoot(view.Case.Evidence), PublisherBPS: 5000}
	_, _, err := service.Review(ctx, session("arb-1", "arbitrator"), "review", view.Case.ID, input)
	if err != ErrForbidden {
		t.Fatalf("client-declared fee authorization accepted: %v", err)
	}
	repo.AuthorizeReviewFee(view.Case.ID, "arb-1")
	_, _, err = service.Review(ctx, session("arb-1", "arbitrator"), "review-authorized", view.Case.ID, input)
	if err != ErrInvalidState {
		t.Fatalf("trusted fee authorization was not used: %v", err)
	}
}

func TestUnauthorizedEvidenceAccessIsAudited(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	service, _ := NewService(repo)
	ctx := context.Background()
	view, _, _ := service.Open(ctx, session("publisher-1", "publisher"), "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	_, _, err := service.GrantAccess(ctx, session("stranger", "publisher"), "denied", view.Case.ID, AccessInput{EvidenceID: testDigest("evidence"), Purpose: "case_review", TTLSeconds: 60})
	if err != ErrForbidden || repo.AccessDenialCount() != 1 {
		t.Fatalf("access denial was not audited: count=%d err=%v", repo.AccessDenialCount(), err)
	}
}

type verifierStub struct {
	reject   string
	expected []string
}

func (v *verifierStub) Verify(_ string, signature, expected string) error {
	v.expected = append(v.expected, expected)
	if signature == v.reject {
		return errors.New("bad signature")
	}
	return nil
}
func TestSignedSettlementVerifiesBothBoundWallets(t *testing.T) {
	repo := NewMemoryRepository(testContext())
	verifier := &verifierStub{}
	service, _ := NewServiceWithVerifier(repo, verifier)
	ctx := context.Background()
	publisher := session("publisher-1", "publisher")
	view, _, _ := service.Open(ctx, publisher, "open", "task-1", OpenInput{DeliveryUnitID: "package-1", Kind: "quality", ReasonCode: "failed", StatementHash: testDigest("claim")})
	tx := "0x" + repeat("a", 64)
	view, _, _ = service.SubmitFreeze(ctx, publisher, "submit", view.Case.ID, FreezeInput{TransactionHash: tx})
	view, _, _ = service.ReconcileFreeze(ctx, publisher, "confirm", view.Case.ID, FreezeInput{TransactionHash: tx})
	input := SettlementInput{PublisherBPS: 3333, ReasonCode: "signed_settlement", EvidenceRoot: EvidenceRoot(view.Case.Evidence), AgreementHash: testDigest("agreement"), PublisherSignature: "0x" + repeat("a", 130), AgentSignature: "0x" + repeat("b", 130)}
	result, _, err := service.Settle(ctx, publisher, "settle", view.Case.ID, input)
	if err != nil || result.Case.State != StateFinal {
		t.Fatalf("settlement: %v %v", result.Case.State, err)
	}
	if len(verifier.expected) != 2 || verifier.expected[0] != view.Context.PublisherWallet || verifier.expected[1] != view.Context.AgentController {
		t.Fatalf("wrong signature bindings: %#v", verifier.expected)
	}
}

func repeat(value string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += value
	}
	return result
}

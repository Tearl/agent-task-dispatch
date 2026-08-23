package dispute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
var txPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)
var reasonPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,99}$`)
var signaturePattern = regexp.MustCompile(`^0x[0-9a-fA-F]{130}$`)

type SignatureVerifier interface {
	Verify(message, signature, expectedAddress string) error
}
type Service struct {
	repository Repository
	verifier   SignatureVerifier
}

func NewService(repository Repository) (*Service, error) {
	return NewServiceWithVerifier(repository, auth.EthereumVerifier{})
}
func NewServiceWithVerifier(repository Repository, verifier SignatureVerifier) (*Service, error) {
	if repository == nil || verifier == nil {
		return nil, ErrInvalidInput
	}
	return &Service{repository: repository, verifier: verifier}, nil
}

func (s *Service) Open(ctx context.Context, session auth.Session, key, taskID string, input OpenInput) (View, bool, error) {
	if !party(session) || !keyOK(key) || strings.TrimSpace(taskID) == "" || strings.TrimSpace(input.DeliveryUnitID) == "" || !reasonPattern.MatchString(input.Kind) || !reasonPattern.MatchString(input.ReasonCode) || !digestPattern.MatchString(input.StatementHash) {
		return View{}, false, ErrInvalidInput
	}
	c, err := s.repository.Context(ctx, taskID)
	if err != nil {
		return View{}, false, err
	}
	if sideFor(c, session.UserID) == "" {
		return View{}, false, ErrForbidden
	}
	if input.DeliveryUnitID != c.DeliveryUnitID {
		return View{}, false, ErrConflict
	}
	return s.execute(ctx, session.UserID, key, c, "", Command{Kind: "open", Input: input})
}
func (s *Service) AddClaim(ctx context.Context, session auth.Session, key, caseID string, input ClaimInput) (View, bool, error) {
	if !party(session) || !keyOK(key) || caseID == "" || !reasonPattern.MatchString(input.Kind) || !reasonPattern.MatchString(input.ReasonCode) || !digestPattern.MatchString(input.StatementHash) {
		return View{}, false, ErrInvalidInput
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "claim", Input: input})
}
func (s *Service) SubmitFreeze(ctx context.Context, session auth.Session, key, caseID string, input FreezeInput) (View, bool, error) {
	if !party(session) || !keyOK(key) || !txPattern.MatchString(input.TransactionHash) {
		return View{}, false, ErrInvalidInput
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "freeze_submit", Input: input})
}
func (s *Service) ReconcileFreeze(ctx context.Context, session auth.Session, key, caseID string, input FreezeInput) (View, bool, error) {
	if !party(session) || !keyOK(key) || !txPattern.MatchString(input.TransactionHash) {
		return View{}, false, ErrInvalidInput
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "freeze_confirm", Input: input})
}
func (s *Service) AppendEvidence(ctx context.Context, session auth.Session, key, caseID string, input EvidenceInput) (View, bool, error) {
	if !party(session) || !keyOK(key) || !digestPattern.MatchString(input.ClaimID) || !slices.Contains(requiredEvidence, input.Category) || strings.TrimSpace(input.ObjectKey) == "" || !digestPattern.MatchString(input.CiphertextDigest) || strings.TrimSpace(input.EnvelopeKeyReference) == "" || strings.Contains(strings.ToLower(input.EnvelopeKeyReference), "plaintext") || input.ObjectVersionID == "" || input.RetentionMode != "COMPLIANCE" || input.RetainUntil.Before(time.Now().UTC().Add(24*time.Hour)) {
		return View{}, false, ErrInvalidInput
	}
	if err := s.repository.VerifyEvidence(ctx, input); err != nil {
		return View{}, false, err
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "evidence", Input: input})
}
func (s *Service) Assign(ctx context.Context, session auth.Session, key, caseID string, input AssignInput) (View, bool, error) {
	if !has(session, "admin") || !keyOK(key) || input.AssigneeID == "" || !slices.Contains([]string{"initial", "review"}, input.Stage) {
		return View{}, false, ErrForbidden
	}
	conflicted, err := s.repository.HasConflict(ctx, caseID, input.AssigneeID)
	if err != nil {
		return View{}, false, err
	}
	if conflicted {
		return View{}, false, ErrConflict
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "assign", Input: input})
}
func (s *Service) Decide(ctx context.Context, session auth.Session, key, caseID string, input DecisionInput) (View, bool, error) {
	if !has(session, "arbitrator") || !keyOK(key) || !reasonPattern.MatchString(input.ReasonCode) || !digestPattern.MatchString(input.EvidenceRoot) {
		return View{}, false, ErrForbidden
	}
	view, err := s.Get(ctx, session, caseID)
	if err != nil {
		return View{}, false, err
	}
	if input.EvidenceRoot != EvidenceRoot(view.Case.Evidence) {
		return View{}, false, ErrConflict
	}
	conflicted, err := s.repository.HasConflict(ctx, caseID, session.UserID)
	if err != nil {
		return View{}, false, err
	}
	if conflicted {
		return View{}, false, ErrConflict
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "decision", Input: input})
}
func (s *Service) Settle(ctx context.Context, session auth.Session, key, caseID string, input SettlementInput) (View, bool, error) {
	if !party(session) || !keyOK(key) || input.PublisherBPS < 0 || input.PublisherBPS > 10000 || !reasonPattern.MatchString(input.ReasonCode) || !digestPattern.MatchString(input.EvidenceRoot) || !digestPattern.MatchString(input.AgreementHash) || !signaturePattern.MatchString(input.PublisherSignature) || !signaturePattern.MatchString(input.AgentSignature) || input.PublisherSignature == input.AgentSignature {
		return View{}, false, ErrInvalidInput
	}
	view, err := s.Get(ctx, session, caseID)
	if err != nil {
		return View{}, false, err
	}
	if input.EvidenceRoot != EvidenceRoot(view.Case.Evidence) {
		return View{}, false, ErrConflict
	}
	message := fmt.Sprintf("AgentTaskDisputeSettlement\nCase ID: %s\nAgreement Hash: %s\nEvidence Root: %s\nPublisher BPS: %d", caseID, input.AgreementHash, input.EvidenceRoot, input.PublisherBPS)
	if s.verifier.Verify(message, input.PublisherSignature, view.Context.PublisherWallet) != nil || s.verifier.Verify(message, input.AgentSignature, view.Context.AgentController) != nil {
		return View{}, false, ErrForbidden
	}
	input.PublisherSignature = ""
	input.AgentSignature = ""
	input.Verified = true
	return s.forCase(ctx, session, key, caseID, Command{Kind: "settlement", Input: input})
}
func (s *Service) Review(ctx context.Context, session auth.Session, key, caseID string, input ReviewInput) (View, bool, error) {
	if !has(session, "arbitrator") || !keyOK(key) || input.AssigneeID != session.UserID || !reasonPattern.MatchString(input.ReasonCode) || !digestPattern.MatchString(input.EvidenceRoot) {
		return View{}, false, ErrForbidden
	}
	view, err := s.Get(ctx, session, caseID)
	if err != nil {
		return View{}, false, err
	}
	if input.EvidenceRoot != EvidenceRoot(view.Case.Evidence) {
		return View{}, false, ErrConflict
	}
	conflicted, err := s.repository.HasConflict(ctx, caseID, session.UserID)
	if err != nil {
		return View{}, false, err
	}
	if conflicted {
		return View{}, false, ErrConflict
	}
	authorized, err := s.repository.HasReviewFeeAuthorization(ctx, caseID, session.UserID)
	if err != nil {
		return View{}, false, err
	}
	if !authorized {
		return View{}, false, ErrForbidden
	}
	input.FeeAuthorized = true
	return s.forCase(ctx, session, key, caseID, Command{Kind: "review", Input: input})
}
func (s *Service) Finalize(ctx context.Context, session auth.Session, key, caseID string) (View, bool, error) {
	if !has(session, "admin") || !keyOK(key) {
		return View{}, false, ErrForbidden
	}
	return s.forCase(ctx, session, key, caseID, Command{Kind: "finalize"})
}
func (s *Service) GrantAccess(ctx context.Context, session auth.Session, key, caseID string, input AccessInput) (View, bool, error) {
	if session.UserID == "" || !keyOK(key) || !digestPattern.MatchString(input.EvidenceID) || input.TTLSeconds < 60 || input.TTLSeconds > 900 || !reasonPattern.MatchString(input.Purpose) {
		return View{}, false, ErrInvalidInput
	}
	view, err := s.repository.Get(ctx, session.UserID, caseID)
	if err != nil {
		return View{}, false, err
	}
	if !canRead(view, session) {
		if auditErr := s.repository.AuditAccessDenial(ctx, caseID, session.UserID, key, input); auditErr != nil {
			return View{}, false, auditErr
		}
		return View{}, false, ErrForbidden
	}
	return s.execute(ctx, session.UserID, key, view.Context, caseID, Command{Kind: "access", Input: input})
}
func (s *Service) Admin(ctx context.Context, session auth.Session, key string, input AdminInput) (View, bool, error) {
	if !has(session, "admin") || !keyOK(key) || !slices.Contains([]string{"dlq_replay", "ledger_reversal", "reconciliation_repair", "state_migration"}, input.Kind) || !reasonPattern.MatchString(input.ReasonCode) || containsForbidden(input.Payload) {
		return View{}, false, ErrForbidden
	}
	c := Context{TaskID: input.ResourceID}
	return s.execute(ctx, session.UserID, key, c, "", Command{Kind: "admin", Input: input})
}
func (s *Service) Get(ctx context.Context, session auth.Session, caseID string) (View, error) {
	if session.UserID == "" || caseID == "" {
		return View{}, ErrForbidden
	}
	view, err := s.repository.Get(ctx, session.UserID, caseID)
	if err != nil {
		return View{}, err
	}
	if !canRead(view, session) {
		return View{}, ErrForbidden
	}
	return view, nil
}
func (s *Service) List(ctx context.Context, session auth.Session) ([]View, error) {
	if session.UserID == "" {
		return nil, ErrForbidden
	}
	values, err := s.repository.List(ctx, session.UserID, session.Roles)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(values, func(value View) bool { return !canRead(value, session) }), nil
}

func (s *Service) forCase(ctx context.Context, session auth.Session, key, caseID string, command Command) (View, bool, error) {
	view, err := s.repository.Get(ctx, session.UserID, caseID)
	if err != nil {
		return View{}, false, err
	}
	if !canRead(view, session) {
		return View{}, false, ErrForbidden
	}
	return s.execute(ctx, session.UserID, key, view.Context, caseID, command)
}
func (s *Service) execute(ctx context.Context, actor, key string, c Context, caseID string, command Command) (View, bool, error) {
	body, _ := json.Marshal(struct {
		CaseID  string
		Command Command
	}{caseID, command})
	sum := sha256.Sum256(body)
	return s.repository.Execute(ctx, Mutation{ActorID: actor, IdempotencyKey: key, RequestHash: "sha256:" + hex.EncodeToString(sum[:])}, c, caseID, command)
}
func party(s auth.Session) bool {
	return s.UserID != "" && (has(s, "publisher") || has(s, "agent_provider"))
}
func has(s auth.Session, role string) bool { return slices.Contains(s.Roles, role) }
func keyOK(v string) bool                  { return strings.TrimSpace(v) != "" && len(v) <= 200 }
func canRead(v View, s auth.Session) bool {
	return s.UserID == v.Case.PublisherID || s.UserID == v.Case.AgentProviderID || has(s, "admin") || (has(s, "arbitrator") && assignedToCase(v.Case.Assignments, s.UserID))
}

func assignedToCase(values []Assignment, actorID string) bool {
	return slices.ContainsFunc(values, func(value Assignment) bool { return value.AssigneeID == actorID })
}
func containsForbidden(value map[string]any) bool {
	body, _ := json.Marshal(value)
	text := strings.ToLower(string(body))
	for _, token := range []string{"privatekey", "private_key", "secret", "credential", "signature", "signedtransaction", "rawtransaction"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

package dispute

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

var requiredEvidence = []string{"specification", "overview", "acceptance", "formal_versions", "feedback", "change_orders", "executions", "usage", "messages", "callbacks", "fees", "policy"}

func Apply(current *View, context Context, actorID string, command Command, now time.Time) (View, error) {
	if current == nil {
		if command.Kind != "open" {
			return View{}, ErrNotFound
		}
		input := command.Input.(OpenInput)
		if !context.Eligible || now.After(context.DisputeDeadline) {
			return View{}, ErrInvalidState
		}
		side := sideFor(context, actorID)
		if side == "" {
			return View{}, ErrForbidden
		}
		claim := Claim{ID: digest("claim", context.TaskID, side, input.Kind), Side: side, Kind: input.Kind, ReasonCode: input.ReasonCode, StatementHash: input.StatementHash, CreatedAt: now}
		value := View{Context: context, AccessGrants: []AccessGrant{}, AdminOperations: []AdminOperation{}, Case: Case{ID: digest("case", context.AssignmentID, context.DeliveryUnitID), TaskID: context.TaskID, AssignmentID: context.AssignmentID, DeliveryUnitID: context.DeliveryUnitID, PolicyVersion: PolicyVersion, PublisherID: context.PublisherID, AgentProviderID: context.AgentProviderID, State: StateSoftLock, AggregateVersion: 1, SoftLockedAt: &now, FrozenAmount: context.FrozenAmount, Asset: context.Asset, Claims: []Claim{claim}, Evidence: []Evidence{}, Assignments: []Assignment{}, Decisions: []Decision{}, Leaves: []FrozenLeaf{}, ReputationPending: true, CreatedAt: now, UpdatedAt: now}}
		return value, nil
	}
	value := clone(*current)
	value.Context = context
	switch command.Kind {
	case "claim":
		if !slices.Contains([]string{StateSoftLock, StateFrozen, StateEvidence, StateOrphaned}, value.Case.State) {
			return View{}, ErrInvalidState
		}
		side := sideFor(context, actorID)
		if side == "" {
			return View{}, ErrForbidden
		}
		input := command.Input.(ClaimInput)
		for _, claim := range value.Case.Claims {
			if claim.Side == side && claim.Kind == input.Kind {
				return View{}, ErrConflict
			}
		}
		value.Case.Claims = append(value.Case.Claims, Claim{ID: digest("claim", value.Case.ID, side, input.Kind), Side: side, Kind: input.Kind, ReasonCode: input.ReasonCode, StatementHash: input.StatementHash, CreatedAt: now})
	case "freeze_submit":
		if value.Case.State != StateSoftLock && value.Case.State != StateOrphaned {
			return View{}, ErrInvalidState
		}
		input := command.Input.(FreezeInput)
		value.Case.FreezeTransactionHash = input.TransactionHash
		value.Case.FreezeSubmittedAt = &now
	case "freeze_confirm":
		if !slices.Contains([]string{StateSoftLock, StateOrphaned}, value.Case.State) || value.Case.FreezeTransactionHash == "" {
			return View{}, ErrInvalidState
		}
		input := command.Input.(FreezeInput)
		if input.TransactionHash != value.Case.FreezeTransactionHash {
			return View{}, ErrConflict
		}
		value.Case.State = StateFrozen
		value.Case.FreezeEventID = digest("freeze-event", input.TransactionHash)
		value.Case.FreezeRoot = digest("freeze-root", value.Case.ID, context.FrozenAmount)
		value.Case.FrozenAt = &now
		value.Case.EvidenceDeadline = now.Add(72 * time.Hour)
		value.Case.DecisionDeadline = value.Case.EvidenceDeadline.Add(72 * time.Hour)
		value.Case.ReviewDeadline = value.Case.DecisionDeadline.Add(48 * time.Hour)
		value.Case.Leaves = []FrozenLeaf{{Index: 0, Owner: context.PublisherWallet, Account: "publisher_refund", Cap: context.FrozenAmount, Kind: "principal"}, {Index: 1, Owner: context.AgentPayout, Account: "agent_receivable", Cap: context.FrozenAmount, Kind: "principal"}}
	case "evidence":
		if !slices.Contains([]string{StateFrozen, StateEvidence}, value.Case.State) || now.After(value.Case.EvidenceDeadline) {
			return View{}, ErrInvalidState
		}
		input := command.Input.(EvidenceInput)
		claim := findClaim(value.Case.Claims, input.ClaimID)
		if claim == nil || !ownsSide(context, actorID, claim.Side) {
			return View{}, ErrForbidden
		}
		for _, item := range value.Case.Evidence {
			if item.ObjectKey == input.ObjectKey || item.CiphertextDigest == input.CiphertextDigest {
				return View{}, ErrConflict
			}
		}
		value.Case.State = StateEvidence
		value.Case.Evidence = append(value.Case.Evidence, Evidence{ID: digest("evidence", value.Case.ID, input.ObjectKey, input.CiphertextDigest), ClaimID: input.ClaimID, Category: input.Category, ObjectKey: input.ObjectKey, CiphertextDigest: input.CiphertextDigest, EnvelopeKeyReference: input.EnvelopeKeyReference, ObjectVersionID: input.ObjectVersionID, RetentionMode: input.RetentionMode, RetainUntil: input.RetainUntil, SubmittedBy: actorID, CreatedAt: now})
	case "assign":
		input := command.Input.(AssignInput)
		if input.AssigneeID == context.PublisherID || input.AssigneeID == context.AgentProviderID {
			return View{}, ErrConflict
		}
		for _, a := range value.Case.Assignments {
			if a.Stage == input.Stage {
				return View{}, ErrConflict
			}
		}
		value.Case.Assignments = append(value.Case.Assignments, Assignment{ID: digest("assignment", value.Case.ID, input.Stage, input.AssigneeID), Stage: input.Stage, AssigneeID: input.AssigneeID, AssignedAt: now, ConflictCheckedAt: now})
	case "decision":
		input := command.Input.(DecisionInput)
		if now.Before(value.Case.EvidenceDeadline) || now.After(value.Case.DecisionDeadline) || !assigned(value.Case.Assignments, "initial", actorID) {
			return View{}, ErrInvalidState
		}
		if !completeEvidence(value.Case.Evidence) || !awardTier(input.PublisherBPS) {
			return View{}, ErrEvidenceIncomplete
		}
		value.Case.Decisions = append(value.Case.Decisions, Decision{ID: digest("decision", value.Case.ID, "initial"), Kind: "initial", DecidedBy: actorID, ReasonCode: input.ReasonCode, EvidenceRoot: input.EvidenceRoot, PublisherBPS: input.PublisherBPS, CreatedAt: now})
		value.Case.State = StateDecided
	case "settlement":
		input := command.Input.(SettlementInput)
		if !slices.Contains([]string{StateFrozen, StateEvidence, StateDecided}, value.Case.State) || !input.Verified {
			return View{}, ErrInvalidState
		}
		value.Case.Decisions = append(value.Case.Decisions, Decision{ID: digest("decision", value.Case.ID, "settlement"), Kind: "settlement", DecidedBy: "parties", ReasonCode: input.ReasonCode, EvidenceRoot: input.EvidenceRoot, PublisherBPS: input.PublisherBPS, CreatedAt: now})
		finish(&value, now)
	case "review":
		input := command.Input.(ReviewInput)
		initial := decision(value.Case.Decisions, "initial")
		if value.Case.State != StateDecided || initial == nil || now.After(value.Case.ReviewDeadline) || !input.FeeAuthorized || input.AssigneeID == initial.DecidedBy || actorID != input.AssigneeID || !assigned(value.Case.Assignments, "review", actorID) || !awardTier(input.PublisherBPS) {
			return View{}, ErrInvalidState
		}
		if decision(value.Case.Decisions, "review") != nil {
			return View{}, ErrConflict
		}
		value.Case.Decisions = append(value.Case.Decisions, Decision{ID: digest("decision", value.Case.ID, "review"), Kind: "review", DecidedBy: actorID, ReasonCode: input.ReasonCode, EvidenceRoot: input.EvidenceRoot, PublisherBPS: input.PublisherBPS, CreatedAt: now})
		finish(&value, now)
	case "finalize":
		if value.Case.State != StateDecided || now.Before(value.Case.ReviewDeadline) {
			return View{}, ErrInvalidState
		}
		finish(&value, now)
	default:
		return View{}, ErrInvalidInput
	}
	value.Case.AggregateVersion++
	value.Case.UpdatedAt = now
	return value, nil
}

func finish(value *View, now time.Time) {
	value.Case.State = StateFinal
	value.Case.FinalizedAt = &now
	value.Case.ReputationPending = false
}
func sideFor(c Context, actor string) string {
	if actor == c.PublisherID {
		return "publisher"
	}
	if actor == c.AgentProviderID {
		return "agent"
	}
	return ""
}
func ownsSide(c Context, actor, side string) bool { return sideFor(c, actor) == side }
func findClaim(values []Claim, id string) *Claim {
	for i := range values {
		if values[i].ID == id {
			return &values[i]
		}
	}
	return nil
}
func assigned(values []Assignment, stage, actor string) bool {
	for _, v := range values {
		if v.Stage == stage && v.AssigneeID == actor {
			return true
		}
	}
	return false
}
func decision(values []Decision, kind string) *Decision {
	for i := len(values) - 1; i >= 0; i-- {
		if values[i].Kind == kind {
			return &values[i]
		}
	}
	return nil
}
func awardTier(v int) bool { return slices.Contains([]int{0, 2500, 5000, 7500, 10000}, v) }
func completeEvidence(values []Evidence) bool {
	for _, category := range requiredEvidence {
		if !slices.ContainsFunc(values, func(v Evidence) bool { return v.Category == category }) {
			return false
		}
	}
	return true
}
func EvidenceRoot(values []Evidence) string {
	digests := make([]string, len(values))
	for index, value := range values {
		digests[index] = value.CiphertextDigest
	}
	body, _ := json.Marshal(digests)
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func clone(value View) View {
	body, _ := json.Marshal(value)
	var result View
	_ = json.Unmarshal(body, &result)
	return result
}
func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

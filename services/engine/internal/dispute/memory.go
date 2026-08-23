package dispute

import (
	"context"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu            sync.Mutex
	contexts      map[string]Context
	cases         map[string]View
	requests      map[string]request
	receipts      map[string]EvidenceInput
	conflicts     map[string]map[string]bool
	reviewFees    map[string]map[string]bool
	accessDenials int
}
type request struct {
	hash string
	view View
}

func NewMemoryRepository(contexts ...Context) *MemoryRepository {
	r := &MemoryRepository{contexts: map[string]Context{}, cases: map[string]View{}, requests: map[string]request{}, receipts: map[string]EvidenceInput{}, conflicts: map[string]map[string]bool{}, reviewFees: map[string]map[string]bool{}}
	for _, c := range contexts {
		r.contexts[c.TaskID] = c
	}
	return r
}

func (r *MemoryRepository) DeclareConflict(caseID, assigneeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conflicts[caseID] == nil {
		r.conflicts[caseID] = map[string]bool{}
	}
	r.conflicts[caseID][assigneeID] = true
}

func (r *MemoryRepository) HasConflict(_ context.Context, caseID, assigneeID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conflicts[caseID][assigneeID], nil
}

func (r *MemoryRepository) AuthorizeReviewFee(caseID, assigneeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reviewFees[caseID] == nil {
		r.reviewFees[caseID] = map[string]bool{}
	}
	r.reviewFees[caseID][assigneeID] = true
}

func (r *MemoryRepository) HasReviewFeeAuthorization(_ context.Context, caseID, assigneeID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reviewFees[caseID][assigneeID], nil
}

func (r *MemoryRepository) AuditAccessDenial(_ context.Context, _, _, _ string, _ AccessInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.accessDenials++
	return nil
}

func (r *MemoryRepository) AccessDenialCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.accessDenials
}

func (r *MemoryRepository) RecordEvidenceReceipt(input EvidenceInput) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.receipts[input.ObjectKey] = input
}
func (r *MemoryRepository) VerifyEvidence(_ context.Context, input EvidenceInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.receipts[input.ObjectKey]
	if !ok || value.CiphertextDigest != input.CiphertextDigest || value.ObjectVersionID != input.ObjectVersionID || value.RetentionMode != "COMPLIANCE" || value.RetainUntil.Before(input.RetainUntil) {
		return ErrInvalidState
	}
	return nil
}
func (r *MemoryRepository) Context(_ context.Context, taskID string) (Context, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.contexts[taskID]
	if !ok {
		return Context{}, ErrNotFound
	}
	return v, nil
}
func (r *MemoryRepository) Execute(_ context.Context, m Mutation, c Context, caseID string, command Command) (View, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := m.ActorID + ":" + m.IdempotencyKey
	if prior, ok := r.requests[key]; ok {
		if prior.hash != m.RequestHash {
			return View{}, false, ErrConflict
		}
		return clone(prior.view), true, nil
	}
	var current *View
	if caseID != "" {
		v, ok := r.cases[caseID]
		if !ok {
			return View{}, false, ErrNotFound
		}
		current = &v
	} else {
		for _, v := range r.cases {
			if v.Case.TaskID == c.TaskID && v.Case.State != StateFinal {
				return View{}, false, ErrConflict
			}
		}
	}
	if command.Kind == "access" {
		v := clone(*current)
		input := command.Input.(AccessInput)
		found := false
		for _, evidence := range v.Case.Evidence {
			if evidence.ID == input.EvidenceID {
				found = true
				break
			}
		}
		if !found {
			return View{}, false, ErrNotFound
		}
		now := time.Now().UTC()
		grant := AccessGrant{ID: digest("access", caseID, input.EvidenceID, m.ActorID, m.IdempotencyKey), EvidenceID: input.EvidenceID, PrincipalID: m.ActorID, Purpose: input.Purpose, CreatedAt: now, ExpiresAt: now.Add(time.Duration(input.TTLSeconds) * time.Second)}
		v.AccessGrants = append(v.AccessGrants, grant)
		v.Case.AggregateVersion++
		v.Case.UpdatedAt = now
		r.cases[caseID] = v
		r.requests[key] = request{m.RequestHash, v}
		return clone(v), false, nil
	}
	if command.Kind == "admin" {
		input := command.Input.(AdminInput)
		var v View
		if existing, ok := r.cases[input.ResourceID]; ok {
			v = existing
		}
		op := AdminOperation{ID: digest("admin", m.ActorID, m.IdempotencyKey), Kind: input.Kind, ResourceType: input.ResourceType, ResourceID: input.ResourceID, ReasonCode: input.ReasonCode, PayloadHash: m.RequestHash, ActorID: m.ActorID, Status: "recorded", CreatedAt: time.Now().UTC()}
		v.AdminOperations = append(v.AdminOperations, op)
		r.requests[key] = request{m.RequestHash, v}
		return clone(v), false, nil
	}
	if command.Kind == "assign" || command.Kind == "decision" || command.Kind == "review" {
		assigneeID := m.ActorID
		if command.Kind == "assign" {
			assigneeID = command.Input.(AssignInput).AssigneeID
		}
		if r.conflicts[caseID][assigneeID] {
			return View{}, false, ErrConflict
		}
	}
	if command.Kind == "review" {
		if !r.reviewFees[caseID][m.ActorID] {
			return View{}, false, ErrForbidden
		}
		input := command.Input.(ReviewInput)
		input.FeeAuthorized = true
		command.Input = input
	}
	next, err := Apply(current, c, m.ActorID, command, time.Now().UTC())
	if err != nil {
		return View{}, false, err
	}
	r.cases[next.Case.ID] = next
	r.requests[key] = request{m.RequestHash, next}
	return clone(next), false, nil
}
func (r *MemoryRepository) Get(_ context.Context, _ string, caseID string) (View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.cases[caseID]
	if !ok {
		return View{}, ErrNotFound
	}
	return clone(v), nil
}
func (r *MemoryRepository) List(_ context.Context, actor string, roles []string) ([]View, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []View{}
	admin := slicesContains(roles, "admin")
	arbitrator := slicesContains(roles, "arbitrator")
	for _, v := range r.cases {
		if admin || actor == v.Case.PublisherID || actor == v.Case.AgentProviderID || (arbitrator && assignedToCase(v.Case.Assignments, actor)) {
			result = append(result, clone(v))
		}
	}
	return result, nil
}
func slicesContains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

package api

import (
	"errors"
	"net/http"

	"github.com/example/agent-platform/engine/internal/dispute"
	"github.com/example/agent-platform/engine/internal/persistence"
)

func (h *handler) listDisputes(w http.ResponseWriter, r *http.Request) {
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, err := h.disputes.List(r.Context(), session)
	if err != nil {
		h.writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": value})
}
func (h *handler) getDispute(w http.ResponseWriter, r *http.Request) {
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, err := h.disputes.Get(r.Context(), session, r.PathValue("caseId"))
	if err != nil {
		h.writeDisputeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) openDispute(w http.ResponseWriter, r *http.Request) {
	var input dispute.OpenInput
	if !h.decodeDispute(w, r, &input, 512_000) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Open(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("id"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) addDisputeClaim(w http.ResponseWriter, r *http.Request) {
	var input dispute.ClaimInput
	if !h.decodeDispute(w, r, &input, 512_000) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.AddClaim(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) submitDisputeFreeze(w http.ResponseWriter, r *http.Request) {
	h.disputeFreeze(w, r, false)
}
func (h *handler) reconcileDisputeFreeze(w http.ResponseWriter, r *http.Request) {
	h.disputeFreeze(w, r, true)
}
func (h *handler) disputeFreeze(w http.ResponseWriter, r *http.Request, reconcile bool) {
	var input dispute.FreezeInput
	if !h.decodeDispute(w, r, &input, 8192) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	var value dispute.View
	var replay bool
	var err error
	if reconcile {
		value, replay, err = h.disputes.ReconcileFreeze(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	} else {
		value, replay, err = h.disputes.SubmitFreeze(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	}
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) appendDisputeEvidence(w http.ResponseWriter, r *http.Request) {
	var input dispute.EvidenceInput
	if !h.decodeDispute(w, r, &input, 512_000) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.AppendEvidence(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) grantDisputeEvidenceAccess(w http.ResponseWriter, r *http.Request) {
	var input dispute.AccessInput
	if !h.decodeDispute(w, r, &input, 8192) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.GrantAccess(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) assignDispute(w http.ResponseWriter, r *http.Request) {
	var input dispute.AssignInput
	if !h.decodeDispute(w, r, &input, 16384) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Assign(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) decideDispute(w http.ResponseWriter, r *http.Request) {
	var input dispute.DecisionInput
	if !h.decodeDispute(w, r, &input, 16384) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Decide(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) settleDispute(w http.ResponseWriter, r *http.Request) {
	var input dispute.SettlementInput
	if !h.decodeDispute(w, r, &input, 32768) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Settle(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) reviewDispute(w http.ResponseWriter, r *http.Request) {
	var input dispute.ReviewInput
	if !h.decodeDispute(w, r, &input, 16384) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Review(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) finalizeDispute(w http.ResponseWriter, r *http.Request) {
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Finalize(r.Context(), session, r.Header.Get("Idempotency-Key"), r.PathValue("caseId"))
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) adminOperation(w http.ResponseWriter, r *http.Request) {
	var input dispute.AdminInput
	if !h.decodeDispute(w, r, &input, 128_000) {
		return
	}
	session, ok := h.agentSession(w, r)
	if !ok {
		return
	}
	value, replay, err := h.disputes.Admin(r.Context(), session, r.Header.Get("Idempotency-Key"), input)
	h.writeDisputeMutation(w, value, replay, err)
}
func (h *handler) decodeDispute(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	if decodeJSON(w, r, limit, target) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid dispute request"})
		return false
	}
	return true
}
func (h *handler) writeDisputeMutation(w http.ResponseWriter, value dispute.View, replay bool, err error) {
	if err != nil {
		h.writeDisputeError(w, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(w, status, value)
}
func (h *handler) writeDisputeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, dispute.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, dispute.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "dispute not found"})
	case errors.Is(err, dispute.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid dispute request"})
	case errors.Is(err, dispute.ErrPending):
		writeJSON(w, http.StatusTooEarly, map[string]string{"error": "dispute chain confirmation pending"})
	case errors.Is(err, dispute.ErrConflict), errors.Is(err, dispute.ErrInvalidState), errors.Is(err, dispute.ErrEvidenceIncomplete), errors.Is(err, persistence.ErrIdempotencyConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "dispute conflict"})
	default:
		h.logger.Error("dispute operation failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "dispute service temporarily unavailable"})
	}
}

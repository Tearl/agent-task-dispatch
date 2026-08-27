package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/credential"
	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/dispute"
	"github.com/example/agent-platform/engine/internal/financeview"
	"github.com/example/agent-platform/engine/internal/matching"
	"github.com/example/agent-platform/engine/internal/matchingview"
	"github.com/example/agent-platform/engine/internal/orchestration"
	"github.com/example/agent-platform/engine/internal/overview"
	"github.com/example/agent-platform/engine/internal/persistence"
	"github.com/example/agent-platform/engine/internal/selection"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	"github.com/example/agent-platform/engine/internal/taskfunding"
	"github.com/example/agent-platform/engine/internal/workflow"
	"github.com/example/agent-platform/engine/internal/workspaceview"
)

type handler struct {
	logger        *slog.Logger
	auth          *auth.Service
	agents        *agent.Service
	credentials   *credential.Service
	tasks         *enginetask.Service
	selections    *selection.Service
	finance       *financeview.Service
	matching      *matchingview.Service
	deliveries    *delivery.Service
	disputes      *dispute.Service
	workflow      *workflow.Service
	workspace     *workspaceview.Service
	funding       *taskfunding.Service
	orchestration *orchestration.Service
}

type nonceRequest struct {
	WalletAddress string `json:"walletAddress"`
}

type nonceResponse struct {
	auth.Challenge
}

type verifyRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

func NewHandler(logger *slog.Logger) http.Handler {
	service, _ := auth.NewService(auth.NewMemoryStore(), auth.EthereumVerifier{}, auth.Config{Domain: "localhost:5173", ChainID: "11155111", Purpose: "login"})
	return NewHandlerWithAuth(logger, service)
}

func NewHandlerWithAuth(logger *slog.Logger, service *auth.Service) http.Handler {
	return NewHandlerWithServices(logger, service, nil)
}

func NewHandlerWithServices(logger *slog.Logger, authService *auth.Service, agentService *agent.Service) http.Handler {
	return NewHandlerWithCredentials(logger, authService, agentService, nil)
}

func NewHandlerWithCredentials(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service) http.Handler {
	return NewHandlerWithTaskService(logger, authService, agentService, credentialService, nil)
}

func NewHandlerWithTaskService(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service) http.Handler {
	return NewHandlerWithSelection(logger, authService, agentService, credentialService, taskService, nil)
}

func NewHandlerWithSelection(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service) http.Handler {
	return NewHandlerWithFinance(logger, authService, agentService, credentialService, taskService, selectionService, nil)
}

func NewHandlerWithFinance(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service) http.Handler {
	return NewHandlerWithMatchingView(logger, authService, agentService, credentialService, taskService, selectionService, financeService, nil)
}

func NewHandlerWithMatchingView(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service) http.Handler {
	return NewHandlerWithDelivery(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingService, nil)
}

func NewHandlerWithDelivery(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service, deliveryService *delivery.Service) http.Handler {
	return NewHandlerWithDisputes(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingService, deliveryService, nil)
}

func NewHandlerWithDisputes(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service, deliveryService *delivery.Service, disputeService *dispute.Service) http.Handler {
	return NewHandlerWithWorkflow(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingService, deliveryService, disputeService, nil)
}

func NewHandlerWithWorkflow(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service, deliveryService *delivery.Service, disputeService *dispute.Service, workflowService *workflow.Service) http.Handler {
	return NewHandlerWithWorkspace(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingService, deliveryService, disputeService, workflowService, nil)
}

func NewHandlerWithWorkspace(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service, deliveryService *delivery.Service, disputeService *dispute.Service, workflowService *workflow.Service, workspaceService *workspaceview.Service) http.Handler {
	return NewHandlerWithTaskFunding(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingService, deliveryService, disputeService, workflowService, workspaceService, nil)
}

func NewHandlerWithTaskFunding(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service, deliveryService *delivery.Service, disputeService *dispute.Service, workflowService *workflow.Service, workspaceService *workspaceview.Service, fundingService *taskfunding.Service) http.Handler {
	return NewHandlerWithOrchestration(logger, authService, agentService, credentialService, taskService, selectionService, financeService, matchingService, deliveryService, disputeService, workflowService, workspaceService, fundingService, nil)
}

func NewHandlerWithOrchestration(logger *slog.Logger, authService *auth.Service, agentService *agent.Service, credentialService *credential.Service, taskService *enginetask.Service, selectionService *selection.Service, financeService *financeview.Service, matchingService *matchingview.Service, deliveryService *delivery.Service, disputeService *dispute.Service, workflowService *workflow.Service, workspaceService *workspaceview.Service, fundingService *taskfunding.Service, orchestrationService *orchestration.Service) http.Handler {
	h := &handler{logger: logger, auth: authService, agents: agentService, credentials: credentialService, tasks: taskService, selections: selectionService, finance: financeService, matching: matchingService, deliveries: deliveryService, disputes: disputeService, workflow: workflowService, workspace: workspaceService, funding: fundingService, orchestration: orchestrationService}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/auth/nonce", h.createNonce)
	mux.HandleFunc("POST /v1/auth/verify", h.verify)
	mux.HandleFunc("GET /v1/auth/session", h.session)
	mux.HandleFunc("DELETE /v1/auth/session", h.logout)
	if agentService != nil {
		mux.HandleFunc("POST /v1/agents", h.createAgent)
		mux.HandleFunc("GET /v1/agents/{id}", h.getAgent)
		mux.HandleFunc("PUT /v1/agents/{id}/profile", h.updateAgentProfile)
		mux.HandleFunc("POST /v1/agents/{id}/lifecycle", h.transitionAgent)
		mux.HandleFunc("POST /v1/agents/{id}/health", h.updateAgentHealth)
		mux.HandleFunc("POST /v1/agents/{id}/capacity", h.updateAgentCapacity)
		mux.HandleFunc("POST /v1/agents/{id}/prices", h.publishAgentPrice)
		mux.HandleFunc("GET /v1/agents/{id}/available-actions", h.availableAgentActions)
		mux.HandleFunc("GET /v1/agents/{id}/view", h.agentView)
	}
	if credentialService != nil {
		mux.HandleFunc("POST /v1/agents/{id}/credentials", h.rotateAgentCredential)
	}
	if taskService != nil {
		mux.HandleFunc("POST /v1/tasks", h.createTask)
		mux.HandleFunc("GET /v1/tasks/{id}", h.getTask)
		mux.HandleFunc("PUT /v1/tasks/{id}/draft", h.updateTaskDraft)
		mux.HandleFunc("POST /v1/tasks/{id}/publish", h.publishTask)
		mux.HandleFunc("POST /v1/tasks/{id}/deletion-requests", h.deleteTask)
		mux.HandleFunc("GET /v1/tasks/{id}/available-actions", h.availableTaskActions)
		mux.HandleFunc("GET /v1/tasks/{id}/view", h.taskView)
	}
	if fundingService != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/funding-intents", h.prepareTaskFunding)
		mux.HandleFunc("GET /v1/tasks/{id}/funding-intent", h.getTaskFunding)
		mux.HandleFunc("POST /v1/tasks/{id}/funding-intents/{intentId}/submit", h.submitTaskFunding)
	}
	if selectionService != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/selection-reservations", h.reserveSelection)
		mux.HandleFunc("GET /v1/tasks/{id}/selection-reservations/{reservationId}", h.getSelection)
		mux.HandleFunc("POST /v1/tasks/{id}/selection-reservations/{reservationId}/reconcile", h.reconcileSelection)
	}
	if financeService != nil {
		mux.HandleFunc("GET /v1/finance/publisher", h.publisherFinance)
		mux.HandleFunc("GET /v1/finance/agent", h.agentFinance)
		mux.HandleFunc("GET /v1/finance/reconciliation", h.reconciliationFinance)
	}
	if matchingService != nil {
		mux.HandleFunc("GET /v1/tasks/{id}/matching-view", h.matchingView)
	}
	if workflowService != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/matching-runs", h.startMatching)
		mux.HandleFunc("POST /v1/tasks/{id}/overview-batches", h.startOverview)
		mux.HandleFunc("POST /v1/tasks/{id}/overview-batches/{batchId}/slots/{slotId}/finalize", h.finalizeOverviewSlot)
		mux.HandleFunc("GET /v1/tasks/{id}/executions", h.taskExecutions)
	}
	if orchestrationService != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/orchestration-plans", h.createOrchestrationPlan)
		mux.HandleFunc("GET /v1/tasks/{id}/orchestration-plan", h.getOrchestrationPlan)
	}
	if workspaceService != nil {
		mux.HandleFunc("GET /v1/workspace/tasks", h.workspaceTasks)
		mux.HandleFunc("GET /v1/workspace/agents", h.workspaceAgents)
		mux.HandleFunc("GET /v1/workspace/marketplace", h.workspaceMarketplace)
		mux.HandleFunc("GET /v1/workspace/notifications", h.workspaceNotifications)
	}
	if deliveryService != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/formal-packages/start", h.startFormalVersion)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-feedback", h.submitFormalFeedback)
		mux.HandleFunc("GET /v1/tasks/{id}/formal-package", h.formalPackage)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-change-orders", h.proposeFormalChangeOrder)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-change-orders/{changeOrderId}/decision", h.decideFormalChangeOrder)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-change-orders/{changeOrderId}/accept", h.acceptFormalChangeOrder)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-change-orders/{changeOrderId}/activate", h.activateFormalChangeOrder)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-acceptance-intents", h.createFormalAcceptanceIntent)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-acceptance-intents/{intentId}/submit", h.submitFormalAcceptance)
		mux.HandleFunc("POST /v1/tasks/{id}/formal-acceptance-intents/{intentId}/reconcile", h.reconcileFormalAcceptance)
	}
	if disputeService != nil {
		mux.HandleFunc("GET /v1/disputes", h.listDisputes)
		mux.HandleFunc("POST /v1/tasks/{id}/disputes", h.openDispute)
		mux.HandleFunc("GET /v1/disputes/{caseId}", h.getDispute)
		mux.HandleFunc("POST /v1/disputes/{caseId}/claims", h.addDisputeClaim)
		mux.HandleFunc("POST /v1/disputes/{caseId}/freeze-submission", h.submitDisputeFreeze)
		mux.HandleFunc("POST /v1/disputes/{caseId}/freeze-reconcile", h.reconcileDisputeFreeze)
		mux.HandleFunc("POST /v1/disputes/{caseId}/evidence", h.appendDisputeEvidence)
		mux.HandleFunc("POST /v1/disputes/{caseId}/evidence-access", h.grantDisputeEvidenceAccess)
		mux.HandleFunc("POST /v1/disputes/{caseId}/assignments", h.assignDispute)
		mux.HandleFunc("POST /v1/disputes/{caseId}/decisions", h.decideDispute)
		mux.HandleFunc("POST /v1/disputes/{caseId}/settlements", h.settleDispute)
		mux.HandleFunc("POST /v1/disputes/{caseId}/reviews", h.reviewDispute)
		mux.HandleFunc("POST /v1/disputes/{caseId}/finalize", h.finalizeDispute)
		mux.HandleFunc("POST /v1/admin/operations", h.adminOperation)
	}

	return requestLogging(logger, mux)
}

func (h *handler) workspaceTasks(writer http.ResponseWriter, request *http.Request) {
	h.workspaceRead(writer, request, "tasks")
}
func (h *handler) workspaceAgents(writer http.ResponseWriter, request *http.Request) {
	h.workspaceRead(writer, request, "agents")
}
func (h *handler) workspaceMarketplace(writer http.ResponseWriter, request *http.Request) {
	h.workspaceRead(writer, request, "marketplace")
}
func (h *handler) workspaceNotifications(writer http.ResponseWriter, request *http.Request) {
	h.workspaceRead(writer, request, "notifications")
}

func (h *handler) workspaceRead(writer http.ResponseWriter, request *http.Request, kind string) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	var value any
	var err error
	switch kind {
	case "tasks":
		value, err = h.workspace.Tasks(request.Context(), session)
	case "agents":
		value, err = h.workspace.Agents(request.Context(), session, false)
	case "marketplace":
		value, err = h.workspace.Agents(request.Context(), session, true)
	default:
		value, err = h.workspace.Notifications(request.Context(), session)
	}
	if err != nil {
		if errors.Is(err, workspaceview.ErrForbidden) {
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		h.logger.Error("workspace view failed", "kind", kind, "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "workspace view temporarily unavailable"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{kind: value})
}

func (h *handler) startMatching(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	if h.orchestration != nil {
		plan, err := h.orchestration.Latest(request.Context(), session, request.PathValue("id"))
		if err != nil {
			if errors.Is(err, orchestration.ErrNotFound) {
				writeJSON(writer, http.StatusTooEarly, map[string]string{"error": "orchestration plan is required before matching"})
				return
			}
			h.writeOrchestrationError(writer, err)
			return
		}
		if plan.Mode == "multi" {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "multi-agent execution requires step-level matching and escrow allocation"})
			return
		}
	}
	value, err := h.workflow.StartMatching(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"))
	if err != nil {
		h.writeWorkflowError(writer, err)
		return
	}
	status := http.StatusCreated
	if value.Replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) createOrchestrationPlan(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.orchestration.Create(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"))
	if err != nil {
		h.writeOrchestrationError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, map[string]any{"plan": value, "replay": replay})
}

func (h *handler) getOrchestrationPlan(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.orchestration.Latest(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeOrchestrationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"plan": value})
}

func (h *handler) writeOrchestrationError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orchestration.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, orchestration.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "orchestration plan not found"})
	case errors.Is(err, orchestration.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid orchestration request"})
	case errors.Is(err, orchestration.ErrNotReady):
		writeJSON(writer, http.StatusTooEarly, map[string]string{"error": "task or agents are not ready for orchestration"})
	default:
		h.logger.Error("orchestration planning failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "orchestration service temporarily unavailable"})
	}
}

func (h *handler) startOverview(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.workflow.StartOverview(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeWorkflowError(writer, err)
		return
	}
	status := http.StatusCreated
	if value.Replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) finalizeOverviewSlot(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.workflow.FinalizeOverviewSlot(request.Context(), session, request.PathValue("id"), request.PathValue("batchId"), request.PathValue("slotId"))
	if err != nil {
		h.writeWorkflowError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) taskExecutions(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.workflow.Executions(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeWorkflowError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) writeWorkflowError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflow.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, workflow.ErrNotFound), errors.Is(err, matching.ErrSnapshotNotFound), errors.Is(err, overview.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "workflow resource not found"})
	case errors.Is(err, workflow.ErrInvalidInput), errors.Is(err, overview.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid workflow request"})
	case errors.Is(err, workflow.ErrDependencyPending), errors.Is(err, overview.ErrDependencyPending):
		writeJSON(writer, http.StatusTooEarly, map[string]string{"error": "workflow dependency is not ready"})
	case errors.Is(err, overview.ErrInvalidState), errors.Is(err, overview.ErrContentConflict), errors.Is(err, overview.ErrObsolete):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "workflow conflict"})
	default:
		h.logger.Error("workflow operation failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "workflow temporarily unavailable"})
	}
}

func (h *handler) createFormalAcceptanceIntent(writer http.ResponseWriter, request *http.Request) {
	var input delivery.AcceptanceIntentInput
	if decodeJSON(writer, request, 16_384, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.deliveries.CreateAcceptanceIntent(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) submitFormalAcceptance(writer http.ResponseWriter, request *http.Request) {
	h.formalAcceptanceTransition(writer, request, false)
}

func (h *handler) reconcileFormalAcceptance(writer http.ResponseWriter, request *http.Request) {
	h.formalAcceptanceTransition(writer, request, true)
}

func (h *handler) formalAcceptanceTransition(writer http.ResponseWriter, request *http.Request, reconcile bool) {
	var input delivery.AcceptanceTransitionInput
	if decodeJSON(writer, request, 8_192, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	var value delivery.AcceptanceIntent
	var replay bool
	var err error
	if reconcile {
		value, replay, err = h.deliveries.ReconcileAcceptance(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), request.PathValue("intentId"), input)
	} else {
		value, replay, err = h.deliveries.SubmitAcceptance(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), request.PathValue("intentId"), input)
	}
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) proposeFormalChangeOrder(writer http.ResponseWriter, request *http.Request) {
	var input delivery.ProposeChangeOrderInput
	if decodeJSON(writer, request, 512_000, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.deliveries.ProposeChangeOrder(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) decideFormalChangeOrder(writer http.ResponseWriter, request *http.Request) {
	var input delivery.DecideChangeOrderInput
	if decodeJSON(writer, request, 16_384, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.deliveries.DecideChangeOrder(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), request.PathValue("changeOrderId"), input)
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) acceptFormalChangeOrder(writer http.ResponseWriter, request *http.Request) {
	h.changeOrderVersionTransition(writer, request, "accept")
}
func (h *handler) activateFormalChangeOrder(writer http.ResponseWriter, request *http.Request) {
	h.changeOrderVersionTransition(writer, request, "activate")
}
func (h *handler) changeOrderVersionTransition(writer http.ResponseWriter, request *http.Request, operation string) {
	var input delivery.ChangeOrderVersionInput
	if decodeJSON(writer, request, 8_192, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	var value delivery.ChangeOrder
	var replay bool
	var err error
	if operation == "accept" {
		value, replay, err = h.deliveries.AcceptChangeOrder(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), request.PathValue("changeOrderId"), input)
	} else {
		value, replay, err = h.deliveries.ActivateChangeOrder(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), request.PathValue("changeOrderId"), input)
	}
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) submitFormalFeedback(writer http.ResponseWriter, request *http.Request) {
	var input delivery.FeedbackInput
	if decodeJSON(writer, request, 512_000, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.deliveries.SubmitFeedback(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) startFormalVersion(writer http.ResponseWriter, request *http.Request) {
	var input delivery.StartInput
	if decodeJSON(writer, request, 8_192, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.deliveries.Start(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) formalPackage(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.deliveries.Get(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeDeliveryError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) writeDeliveryError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, delivery.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, delivery.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "formal package not found"})
	case errors.Is(err, delivery.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid formal delivery request"})
	case errors.Is(err, delivery.ErrDependencyPending):
		writeJSON(writer, http.StatusTooEarly, map[string]string{"error": "formal chain authorization or confirmation pending"})
	case errors.Is(err, delivery.ErrInvalidState), errors.Is(err, delivery.ErrStaleVersion), errors.Is(err, delivery.ErrContentConflict), errors.Is(err, persistence.ErrIdempotencyConflict):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "formal delivery conflict"})
	default:
		h.logger.Error("formal delivery operation failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "formal delivery service temporarily unavailable"})
	}
}

func (h *handler) matchingView(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.matching.Get(request.Context(), session, request.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, matchingview.ErrForbidden):
			writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
		case errors.Is(err, matchingview.ErrNotFound):
			writeJSON(writer, http.StatusNotFound, map[string]string{"error": "matching task not found"})
		default:
			h.logger.Error("matching view failed", "error", err)
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "matching view temporarily unavailable"})
		}
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) publisherFinance(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.finance.Publisher(request.Context(), session)
	if err != nil {
		h.writeFinanceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) agentFinance(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.finance.Agent(request.Context(), session)
	if err != nil {
		h.writeFinanceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) reconciliationFinance(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.finance.Reconciliation(request.Context(), session)
	if err != nil {
		h.writeFinanceError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) writeFinanceError(writer http.ResponseWriter, err error) {
	if errors.Is(err, financeview.ErrForbidden) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	h.logger.Error("finance view failed", "error", err)
	writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "finance view temporarily unavailable"})
}

func (h *handler) reserveSelection(writer http.ResponseWriter, request *http.Request) {
	var input selection.Request
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.selections.Reserve(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeSelectionError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) getSelection(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.selections.Get(request.Context(), session, request.PathValue("id"), request.PathValue("reservationId"))
	if err != nil {
		h.writeSelectionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) reconcileSelection(writer http.ResponseWriter, request *http.Request) {
	var input selection.ReconcileRequest
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	reservation, assignment, err := h.selections.Reconcile(request.Context(), session, request.PathValue("id"), request.PathValue("reservationId"), input)
	if err != nil {
		h.writeSelectionError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"reservation": reservation, "assignment": assignment})
}

func (h *handler) writeSelectionError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, selection.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, selection.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "selection reservation not found"})
	case errors.Is(err, selection.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid selection request"})
	case errors.Is(err, selection.ErrDependencyPending):
		writeJSON(writer, http.StatusTooEarly, map[string]string{"error": "chain confirmation pending"})
	case errors.Is(err, selection.ErrInvalidState), errors.Is(err, selection.ErrContentConflict), errors.Is(err, selection.ErrProofMismatch), errors.Is(err, selection.ErrCapacityUnavailable), errors.Is(err, persistence.ErrIdempotencyConflict):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "selection conflict"})
	default:
		h.logger.Error("selection operation failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "selection service temporarily unavailable"})
	}
}

func (h *handler) availableAgentActions(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.agents.AvailableActions(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) availableTaskActions(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.tasks.AvailableActions(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) agentView(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.agents.View(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) taskView(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.tasks.View(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) createTask(writer http.ResponseWriter, request *http.Request) {
	var input enginetask.DraftInput
	if decodeJSON(writer, request, 131_072, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.tasks.Create(request.Context(), session, request.Header.Get("Idempotency-Key"), input)
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (h *handler) getTask(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.tasks.Get(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) updateTaskDraft(writer http.ResponseWriter, request *http.Request) {
	var input enginetask.UpdateDraftInput
	if decodeJSON(writer, request, 131_072, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.tasks.UpdateDraft(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) publishTask(writer http.ResponseWriter, request *http.Request) {
	var input enginetask.PublishInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.tasks.Publish(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (h *handler) deleteTask(writer http.ResponseWriter, request *http.Request) {
	var input enginetask.DeleteInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.tasks.RequestDelete(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeTaskError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) prepareTaskFunding(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, replay, err := h.funding.Prepare(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"))
	if err != nil {
		h.writeTaskFundingError(writer, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	writeJSON(writer, status, value)
}

func (h *handler) getTaskFunding(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.funding.Get(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeTaskFundingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) submitTaskFunding(writer http.ResponseWriter, request *http.Request) {
	var input taskfunding.SubmitInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.funding.Submit(request.Context(), session, request.PathValue("id"), request.PathValue("intentId"), input)
	if err != nil {
		h.writeTaskFundingError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}

func (h *handler) writeTaskFundingError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, taskfunding.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, taskfunding.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task funding intent not found"})
	case errors.Is(err, taskfunding.ErrInvalidState), errors.Is(err, taskfunding.ErrConflict):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "task funding conflict"})
	case errors.Is(err, taskfunding.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid task funding request"})
	default:
		h.logger.Error("task funding operation failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "task funding service temporarily unavailable"})
	}
}

func (h *handler) writeTaskError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, enginetask.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, enginetask.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "task not found"})
	case errors.Is(err, enginetask.ErrStaleVersion), errors.Is(err, enginetask.ErrInvalidState), errors.Is(err, persistence.ErrIdempotencyConflict):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "task conflict"})
	case errors.Is(err, enginetask.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid task request"})
	default:
		h.logger.Error("task operation failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "task service temporarily unavailable"})
	}
}

func (h *handler) rotateAgentCredential(writer http.ResponseWriter, request *http.Request) {
	var input credential.RotateInput
	if decodeJSON(writer, request, 20_480, &input) != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	metadata, _, err := h.credentials.Rotate(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeCredentialError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, metadata)
}

func (h *handler) writeCredentialError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, credential.ErrForbidden):
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "forbidden"})
	case errors.Is(err, credential.ErrNotFound):
		writeJSON(writer, http.StatusNotFound, map[string]string{"error": "agent not found"})
	case errors.Is(err, credential.ErrStaleVersion), errors.Is(err, credential.ErrInvalidState), errors.Is(err, persistence.ErrIdempotencyConflict):
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "credential conflict"})
	case errors.Is(err, credential.ErrInvalidInput):
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid credential request"})
	default:
		h.logger.Error("credential operation failed", "error", err)
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "credential service temporarily unavailable"})
	}
}

func (h *handler) createAgent(writer http.ResponseWriter, request *http.Request) {
	var input agent.CreateInput
	if decodeJSON(writer, request, 32_768, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.Create(request.Context(), session, request.Header.Get("Idempotency-Key"), input)
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}
func (h *handler) getAgent(writer http.ResponseWriter, request *http.Request) {
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, err := h.agents.Get(request.Context(), session, request.PathValue("id"))
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, value)
}
func (h *handler) updateAgentProfile(writer http.ResponseWriter, request *http.Request) {
	var input agent.ProfileInput
	if decodeJSON(writer, request, 32_768, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.UpdateProfile(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, 200, value)
}
func (h *handler) transitionAgent(writer http.ResponseWriter, request *http.Request) {
	var input agent.LifecycleInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.Transition(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, 200, value)
}
func (h *handler) updateAgentHealth(writer http.ResponseWriter, request *http.Request) {
	var input agent.HealthCheckInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.CheckHealth(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, 200, value)
}
func (h *handler) updateAgentCapacity(writer http.ResponseWriter, request *http.Request) {
	var input agent.CapacityInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.UpdateCapacity(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, 200, value)
}
func (h *handler) publishAgentPrice(writer http.ResponseWriter, request *http.Request) {
	var input agent.PriceInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.PublishPrice(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
	if err != nil {
		h.writeAgentError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, value)
}

func (h *handler) agentSession(writer http.ResponseWriter, request *http.Request) (auth.Session, bool) {
	value := request.Header.Get("authorization")
	if len(value) < 8 || value[:7] != "Bearer " {
		writeJSON(writer, 401, map[string]string{"error": "unauthorized"})
		return auth.Session{}, false
	}
	session, err := h.auth.Session(request.Context(), value[7:])
	if err != nil {
		h.writeAuthenticationError(writer, err)
		return auth.Session{}, false
	}
	return session, true
}
func (h *handler) writeAgentError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrForbidden):
		writeJSON(writer, 403, map[string]string{"error": "forbidden"})
	case errors.Is(err, agent.ErrNotFound):
		writeJSON(writer, 404, map[string]string{"error": "agent not found"})
	case errors.Is(err, agent.ErrStaleVersion), errors.Is(err, agent.ErrInvalidState), errors.Is(err, agent.ErrCapacityUnavailable), errors.Is(err, persistence.ErrIdempotencyConflict):
		writeJSON(writer, 409, map[string]string{"error": "agent conflict"})
	case errors.Is(err, agent.ErrInvalidInput), errors.Is(err, agent.ErrInvalidPrice):
		writeJSON(writer, 400, map[string]string{"error": "invalid agent request"})
	default:
		h.logger.Error("agent operation failed", "error", err)
		writeJSON(writer, 503, map[string]string{"error": "agent service temporarily unavailable"})
	}
}

func (h *handler) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "distribution-engine",
	})
}

func (h *handler) createNonce(writer http.ResponseWriter, request *http.Request) {
	var payload nonceRequest
	if err := decodeJSON(writer, request, 4_096, &payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	challenge, err := h.auth.Issue(request.Context(), payload.WalletAddress)
	if errors.Is(err, auth.ErrRateLimited) {
		writeJSON(writer, http.StatusTooManyRequests, map[string]string{"error": "too many authentication requests"})
		return
	}
	if errors.Is(err, auth.ErrInvalidChallenge) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid wallet address"})
		return
	}
	if err != nil {
		h.logger.Error("nonce generation failed", "error", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "nonce generation failed"})
		return
	}
	writeJSON(writer, http.StatusCreated, nonceResponse{Challenge: challenge})
}

func (h *handler) verify(writer http.ResponseWriter, request *http.Request) {
	var payload verifyRequest
	if err := decodeJSON(writer, request, 16_384, &payload); err != nil || payload.Message == "" || payload.Signature == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	session, err := h.auth.Verify(request.Context(), auth.VerifyRequest{Message: payload.Message, Signature: payload.Signature})
	if err != nil {
		h.writeAuthenticationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, session)
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, limit int64, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func (h *handler) session(writer http.ResponseWriter, request *http.Request) {
	value := request.Header.Get("authorization")
	if len(value) < 8 || value[:7] != "Bearer " {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	session, err := h.auth.Session(request.Context(), value[7:])
	if err != nil {
		h.writeAuthenticationError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, session)
}

func (h *handler) logout(writer http.ResponseWriter, request *http.Request) {
	value := request.Header.Get("authorization")
	if len(value) < 8 || value[:7] != "Bearer " {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if err := h.auth.Revoke(request.Context(), value[7:]); err != nil {
		h.writeAuthenticationError(writer, err)
		return
	}
	writer.WriteHeader(http.StatusNoContent)
}

func (h *handler) writeAuthenticationError(writer http.ResponseWriter, err error) {
	if errors.Is(err, auth.ErrInvalidChallenge) || errors.Is(err, auth.ErrInvalidSignature) || errors.Is(err, auth.ErrNonceConsumed) {
		writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "authentication failed"})
		return
	}
	h.logger.Error("authentication store unavailable", "error", err)
	writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "authentication temporarily unavailable"})
}

func requestLogging(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(writer, request)
		logger.Info(
			"request completed",
			"method", request.Method,
			"path", request.URL.Path,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

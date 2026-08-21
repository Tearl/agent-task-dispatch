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
	"github.com/example/agent-platform/engine/internal/persistence"
)

type handler struct {
	logger *slog.Logger
	auth   *auth.Service
	agents *agent.Service
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
	h := &handler{logger: logger, auth: authService, agents: agentService}
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
	}

	return requestLogging(logger, mux)
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
	var input agent.HealthInput
	if decodeJSON(writer, request, 4_096, &input) != nil {
		writeJSON(writer, 400, map[string]string{"error": "invalid request body"})
		return
	}
	session, ok := h.agentSession(writer, request)
	if !ok {
		return
	}
	value, _, err := h.agents.UpdateHealth(request.Context(), session, request.Header.Get("Idempotency-Key"), request.PathValue("id"), input)
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

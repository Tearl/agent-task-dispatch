package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
)

type handler struct {
	logger *slog.Logger
	auth   *auth.Service
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
	h := &handler{logger: logger, auth: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/auth/nonce", h.createNonce)
	mux.HandleFunc("POST /v1/auth/verify", h.verify)
	mux.HandleFunc("GET /v1/auth/session", h.session)
	mux.HandleFunc("DELETE /v1/auth/session", h.logout)

	return requestLogging(logger, mux)
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

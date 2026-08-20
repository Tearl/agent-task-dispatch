package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type handler struct {
	logger *slog.Logger
}

type nonceRequest struct {
	WalletAddress string `json:"walletAddress"`
}

type nonceResponse struct {
	Nonce     string    `json:"nonce"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func NewHandler(logger *slog.Logger) http.Handler {
	h := &handler{logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("POST /v1/auth/nonce", h.createNonce)

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
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 4_096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if !isWalletAddress(payload.WalletAddress) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid wallet address"})
		return
	}

	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		h.logger.Error("nonce generation failed", "error", err)
		writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "nonce generation failed"})
		return
	}

	writeJSON(writer, http.StatusCreated, nonceResponse{
		Nonce:     hex.EncodeToString(bytes),
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	})
}

func isWalletAddress(address string) bool {
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return false
	}
	_, err := hex.DecodeString(address[2:])
	return err == nil
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

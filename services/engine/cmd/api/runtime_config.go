package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	enginecore "github.com/example/agent-platform/engine/internal/core"
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/outbox"
)

type runtimeCredentialJSON struct {
	BearerToken        string `json:"bearerToken"`
	CallbackKeyBase64  string `json:"callbackKeyBase64"`
	CallbackKeyVersion string `json:"callbackKeyVersion"`
}

func engineRuntimeConfig(logger *slog.Logger, fundsAsset string) enginecore.Config {
	workerID := os.Getenv("ENGINE_WORKER_ID")
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname + "-" + strconv.Itoa(os.Getpid())
	}
	return enginecore.Config{
		FundsAsset:             fundsAsset,
		MatchingSeedKeyVersion: requiredEnv(logger, "MATCHING_SHUFFLE_KEY_VERSION"),
		MatchingSeedSecret:     requiredBase64Env(logger, "MATCHING_SHUFFLE_SECRET_BASE64"),
		CallbackBaseURL:        requiredEnv(logger, "EXECUTION_CALLBACK_BASE_URL"),
		NonceKeyVersion:        requiredEnv(logger, "EXECUTION_NONCE_KEY_VERSION"),
		NonceSecret:            requiredBase64Env(logger, "EXECUTION_NONCE_SECRET_BASE64"),
		CallbackClockSkew:      durationEnv(logger, "EXECUTION_CALLBACK_CLOCK_SKEW", 5*time.Minute),
		ExecutionLeaseTTL:      durationEnv(logger, "EXECUTION_CAPACITY_LEASE_TTL", 10*time.Minute),
		AgentRequestTimeout:    durationEnv(logger, "EXECUTION_AGENT_REQUEST_TIMEOUT", 20*time.Second),
		RuntimeCredentials:     runtimeCredentialsEnv(logger),
		Outbox: outbox.Config{
			WorkerID:    workerID,
			BatchSize:   integerEnv(logger, "ENGINE_OUTBOX_BATCH_SIZE", 10),
			Lease:       durationEnv(logger, "ENGINE_OUTBOX_LEASE", 5*time.Minute),
			PollEvery:   durationEnv(logger, "ENGINE_OUTBOX_POLL_INTERVAL", time.Second),
			BaseBackoff: durationEnv(logger, "ENGINE_OUTBOX_BASE_BACKOFF", 5*time.Second),
			MaxBackoff:  durationEnv(logger, "ENGINE_OUTBOX_MAX_BACKOFF", 15*time.Minute),
			MaxAttempts: integerEnv(logger, "ENGINE_OUTBOX_MAX_ATTEMPTS", 10),
		},
	}
}

func runtimeCredentialsEnv(logger *slog.Logger) map[string]execution.RuntimeCredential {
	encoded := requiredEnv(logger, "ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON")
	credentials, err := decodeRuntimeCredentials(encoded)
	if err != nil {
		logger.Error("Agent runtime credentials JSON is invalid")
		os.Exit(1)
	}
	return credentials
}

func decodeRuntimeCredentials(encoded string) (map[string]execution.RuntimeCredential, error) {
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var values map[string]runtimeCredentialJSON
	if err := decoder.Decode(&values); err != nil {
		return nil, errors.New("invalid Agent runtime credentials JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, errors.New("Agent runtime credentials JSON contains trailing data")
	}
	credentials := make(map[string]execution.RuntimeCredential, len(values))
	for agentID, value := range values {
		key, err := base64.StdEncoding.DecodeString(value.CallbackKeyBase64)
		if err != nil {
			return nil, errors.New("Agent runtime callback key is not valid base64")
		}
		credentials[agentID] = execution.RuntimeCredential{
			BearerToken:        value.BearerToken,
			CallbackKey:        key,
			CallbackKeyVersion: value.CallbackKeyVersion,
		}
	}
	return credentials, nil
}

func requiredBase64Env(logger *slog.Logger, name string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(requiredEnv(logger, name))
	if err != nil {
		logger.Error("required environment variable is not valid base64", "name", name)
		os.Exit(1)
	}
	return decoded
}

func booleanEnv(logger *slog.Logger, name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		logger.Error("environment variable is not a boolean", "name", name)
		os.Exit(1)
	}
	return parsed
}

func integerEnv(logger *slog.Logger, name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		logger.Error("environment variable is not an integer", "name", name)
		os.Exit(1)
	}
	return parsed
}

func durationEnv(logger *slog.Logger, name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		logger.Error("environment variable is not a duration", "name", name)
		os.Exit(1)
	}
	return parsed
}

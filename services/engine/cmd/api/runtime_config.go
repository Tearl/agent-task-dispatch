package main

import (
	"encoding/base64"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	enginecore "github.com/example/agent-platform/engine/internal/core"
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/outbox"
)

func engineRuntimeConfig(logger *slog.Logger, fundsAsset string) enginecore.Config {
	executionConfig := engineExecutionRuntimeConfig(logger)
	workerID := os.Getenv("ENGINE_WORKER_ID")
	if workerID == "" {
		hostname, _ := os.Hostname()
		workerID = hostname + "-" + strconv.Itoa(os.Getpid())
	}
	return enginecore.Config{
		FundsAsset:              fundsAsset,
		MatchingSeedKeyVersion:  requiredEnv(logger, "MATCHING_SHUFFLE_KEY_VERSION"),
		MatchingSeedSecret:      requiredBase64Env(logger, "MATCHING_SHUFFLE_SECRET_BASE64"),
		CallbackBaseURL:         executionConfig.CallbackBaseURL,
		NonceKeyVersion:         executionConfig.NonceKeyVersion,
		NonceSecret:             executionConfig.NonceSecret,
		CallbackClockSkew:       executionConfig.CallbackClockSkew,
		ExecutionLeaseTTL:       executionConfig.ExecutionLeaseTTL,
		AgentRequestTimeout:     executionConfig.AgentRequestTimeout,
		RuntimeCredentials:      executionConfig.RuntimeCredentials,
		ExecutionInputBaseURL:   requiredEnv(logger, "EXECUTION_INPUT_BASE_URL"),
		OverviewMaximumDuration: durationEnv(logger, "OVERVIEW_MAXIMUM_DURATION", 15*time.Minute),
		OverviewAllowedTools:    stringListEnv("OVERVIEW_ALLOWED_TOOLS", []string{"read_task_input"}),
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

func stringListEnv(name string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func engineExecutionRuntimeConfig(logger *slog.Logger) enginecore.ExecutionConfig {
	return enginecore.ExecutionConfig{
		CallbackBaseURL:     requiredEnv(logger, "EXECUTION_CALLBACK_BASE_URL"),
		NonceKeyVersion:     requiredEnv(logger, "EXECUTION_NONCE_KEY_VERSION"),
		NonceSecret:         requiredBase64Env(logger, "EXECUTION_NONCE_SECRET_BASE64"),
		CallbackClockSkew:   durationEnv(logger, "EXECUTION_CALLBACK_CLOCK_SKEW", 5*time.Minute),
		ExecutionLeaseTTL:   durationEnv(logger, "EXECUTION_CAPACITY_LEASE_TTL", 10*time.Minute),
		AgentRequestTimeout: durationEnv(logger, "EXECUTION_AGENT_REQUEST_TIMEOUT", 20*time.Second),
		RuntimeCredentials:  runtimeCredentialsEnv(logger),
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
	return execution.DecodeRuntimeCredentialsJSON(encoded)
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

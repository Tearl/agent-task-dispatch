package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	enginecore "github.com/example/agent-platform/engine/internal/core"
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/sqsconsumer"
)

type consumerConfig struct {
	DatabaseURL    string
	AWSRegion      string
	AWSEndpointURL string
	Execution      enginecore.ExecutionConfig
	SQS            sqsconsumer.Config
}

func loadConsumerConfig(getenv func(string) string) (consumerConfig, error) {
	if getenv == nil {
		return consumerConfig{}, errors.New("environment reader is required")
	}
	required := func(name string) (string, error) {
		value := strings.TrimSpace(getenv(name))
		if value == "" {
			return "", fmt.Errorf("%s is required", name)
		}
		return value, nil
	}
	var result consumerConfig
	var err error
	if result.DatabaseURL, err = required("DATABASE_URL"); err != nil {
		return consumerConfig{}, err
	}
	if result.AWSRegion, err = required("AWS_REGION"); err != nil {
		return consumerConfig{}, err
	}
	if result.SQS.QueueURL, err = required("TASK_EXECUTION_QUEUE_URL"); err != nil {
		return consumerConfig{}, err
	}
	if strings.TrimSpace(getenv("ENGINE_FORMAL_EXECUTION_TRANSPORT")) != "sqs" {
		return consumerConfig{}, errors.New("ENGINE_FORMAL_EXECUTION_TRANSPORT must be sqs")
	}
	result.AWSEndpointURL = strings.TrimSpace(getenv("AWS_ENDPOINT_URL"))
	if result.Execution.CallbackBaseURL, err = required("EXECUTION_CALLBACK_BASE_URL"); err != nil {
		return consumerConfig{}, err
	}
	if result.Execution.NonceKeyVersion, err = required("EXECUTION_NONCE_KEY_VERSION"); err != nil {
		return consumerConfig{}, err
	}
	if result.Execution.NonceSecret, err = requiredBase64(getenv, "EXECUTION_NONCE_SECRET_BASE64"); err != nil {
		return consumerConfig{}, err
	}
	credentialsJSON, err := required("ENGINE_AGENT_RUNTIME_CREDENTIALS_JSON")
	if err != nil {
		return consumerConfig{}, err
	}
	if result.Execution.RuntimeCredentials, err = execution.DecodeRuntimeCredentialsJSON(credentialsJSON); err != nil {
		return consumerConfig{}, err
	}
	if result.Execution.CallbackClockSkew, err = durationValue(getenv, "EXECUTION_CALLBACK_CLOCK_SKEW", 5*time.Minute); err != nil {
		return consumerConfig{}, err
	}
	if result.Execution.ExecutionLeaseTTL, err = durationValue(getenv, "EXECUTION_CAPACITY_LEASE_TTL", 10*time.Minute); err != nil {
		return consumerConfig{}, err
	}
	if result.Execution.AgentRequestTimeout, err = durationValue(getenv, "EXECUTION_AGENT_REQUEST_TIMEOUT", 20*time.Second); err != nil {
		return consumerConfig{}, err
	}
	result.SQS.ConsumerName = strings.TrimSpace(getenv("FORMAL_EXECUTION_CONSUMER_NAME"))
	if result.SQS.ConsumerName == "" {
		result.SQS.ConsumerName = "formal-execution-v1"
	}
	result.SQS.ExpectedTopic = "agent.execution.formal.requested"
	if result.SQS.WaitTime, err = durationValue(getenv, "SQS_CONSUMER_WAIT_TIME", 20*time.Second); err != nil {
		return consumerConfig{}, err
	}
	if result.SQS.VisibilityTimeout, err = durationValue(getenv, "SQS_CONSUMER_VISIBILITY_TIMEOUT", 2*time.Minute); err != nil {
		return consumerConfig{}, err
	}
	if result.SQS.HeartbeatEvery, err = durationValue(getenv, "SQS_CONSUMER_HEARTBEAT_INTERVAL", 30*time.Second); err != nil {
		return consumerConfig{}, err
	}
	if result.SQS.APIRequestTimeout, err = durationValue(getenv, "SQS_CONSUMER_API_TIMEOUT", 25*time.Second); err != nil {
		return consumerConfig{}, err
	}
	if result.SQS.BaseBackoff, err = durationValue(getenv, "SQS_CONSUMER_BASE_BACKOFF", 5*time.Second); err != nil {
		return consumerConfig{}, err
	}
	if result.SQS.MaxBackoff, err = durationValue(getenv, "SQS_CONSUMER_MAX_BACKOFF", 5*time.Minute); err != nil {
		return consumerConfig{}, err
	}
	return result, nil
}

func requiredBase64(getenv func(string) string, name string) ([]byte, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64", name)
	}
	return decoded, nil
}

func durationValue(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s is not a duration", name)
	}
	return parsed, nil
}

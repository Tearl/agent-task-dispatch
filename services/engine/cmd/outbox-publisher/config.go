package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/outbox"
)

const (
	formalTransportDatabase = "database"
	formalTransportSQS      = "sqs"
)

type publisherConfig struct {
	DatabaseURL             string
	AWSRegion               string
	AWSEndpointURL          string
	TaskEventsTopicARN      string
	AgentEventsTopicARN     string
	FormalExecutionQueueURL string
	AdminOperationQueueURL  string
	FormalTransport         string
	PublishTimeout          time.Duration
	Outbox                  outbox.Config
}

func loadPublisherConfig(getenv func(string) string) (publisherConfig, error) {
	if getenv == nil {
		return publisherConfig{}, errors.New("environment reader is required")
	}
	required := func(name string) (string, error) {
		value := strings.TrimSpace(getenv(name))
		if value == "" {
			return "", fmt.Errorf("%s is required", name)
		}
		return value, nil
	}
	var result publisherConfig
	var err error
	if result.DatabaseURL, err = required("DATABASE_URL"); err != nil {
		return publisherConfig{}, err
	}
	if result.AWSRegion, err = required("AWS_REGION"); err != nil {
		return publisherConfig{}, err
	}
	if result.TaskEventsTopicARN, err = required("TASK_EVENTS_TOPIC_ARN"); err != nil {
		return publisherConfig{}, err
	}
	if result.AgentEventsTopicARN, err = required("AGENT_EVENTS_TOPIC_ARN"); err != nil {
		return publisherConfig{}, err
	}
	if result.AdminOperationQueueURL, err = required("ADMIN_OPERATION_QUEUE_URL"); err != nil {
		return publisherConfig{}, err
	}
	result.AWSEndpointURL = strings.TrimSpace(getenv("AWS_ENDPOINT_URL"))
	result.FormalTransport = strings.TrimSpace(getenv("ENGINE_FORMAL_EXECUTION_TRANSPORT"))
	if result.FormalTransport == "" {
		result.FormalTransport = formalTransportDatabase
	}
	if result.FormalTransport != formalTransportDatabase && result.FormalTransport != formalTransportSQS {
		return publisherConfig{}, errors.New("ENGINE_FORMAL_EXECUTION_TRANSPORT must be database or sqs")
	}
	if result.FormalTransport == formalTransportSQS {
		if result.FormalExecutionQueueURL, err = required("TASK_EXECUTION_QUEUE_URL"); err != nil {
			return publisherConfig{}, err
		}
	}
	if result.PublishTimeout, err = readDuration(getenv, "OUTBOX_PUBLISH_TIMEOUT", 10*time.Second); err != nil {
		return publisherConfig{}, err
	}
	result.Outbox = outbox.Config{}
	result.Outbox.WorkerID = strings.TrimSpace(getenv("OUTBOX_PUBLISHER_ID"))
	if result.Outbox.BatchSize, err = readInteger(getenv, "OUTBOX_PUBLISHER_BATCH_SIZE", 10); err != nil {
		return publisherConfig{}, err
	}
	if result.Outbox.Lease, err = readDuration(getenv, "OUTBOX_PUBLISHER_LEASE", 2*time.Minute); err != nil {
		return publisherConfig{}, err
	}
	if result.Outbox.PollEvery, err = readDuration(getenv, "OUTBOX_PUBLISHER_POLL_INTERVAL", time.Second); err != nil {
		return publisherConfig{}, err
	}
	if result.Outbox.BaseBackoff, err = readDuration(getenv, "OUTBOX_PUBLISHER_BASE_BACKOFF", 5*time.Second); err != nil {
		return publisherConfig{}, err
	}
	if result.Outbox.MaxBackoff, err = readDuration(getenv, "OUTBOX_PUBLISHER_MAX_BACKOFF", 15*time.Minute); err != nil {
		return publisherConfig{}, err
	}
	if result.Outbox.MaxAttempts, err = readInteger(getenv, "OUTBOX_PUBLISHER_MAX_ATTEMPTS", 10); err != nil {
		return publisherConfig{}, err
	}
	if result.PublishTimeout < time.Second || result.PublishTimeout > time.Minute {
		return publisherConfig{}, errors.New("OUTBOX_PUBLISH_TIMEOUT must be between 1s and 1m")
	}
	if result.Outbox.BatchSize < 1 || result.Outbox.BatchSize > 100 || result.Outbox.Lease <= result.PublishTimeout*time.Duration(result.Outbox.BatchSize) {
		return publisherConfig{}, errors.New("publisher lease must exceed the worst-case batch publish duration")
	}
	return result, nil
}

func readDuration(getenv func(string) string, name string, fallback time.Duration) (time.Duration, error) {
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

func readInteger(getenv func(string) string, name string, fallback int) (int, error) {
	value := strings.TrimSpace(getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s is not an integer", name)
	}
	return parsed, nil
}

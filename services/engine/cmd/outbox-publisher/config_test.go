package main

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/example/agent-platform/engine/internal/outbox/awspublisher"
)

type routeSNSStub struct{}

func (routeSNSStub) Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error) {
	return &sns.PublishOutput{}, nil
}

type routeSQSStub struct{}

func (routeSQSStub) SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	return &sqs.SendMessageOutput{}, nil
}

func TestPublisherConfigDefaultsToEventAndAdminPublishing(t *testing.T) {
	values := baseEnvironment()
	config, err := loadPublisherConfig(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	if config.FormalTransport != formalTransportDatabase || config.FormalExecutionQueueURL != "" || config.Outbox.BatchSize != 10 || config.PublishTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %#v", config)
	}
}

func TestPublisherConfigRequiresSQSQueueOnlyForExternalFormalTransport(t *testing.T) {
	values := baseEnvironment()
	values["ENGINE_FORMAL_EXECUTION_TRANSPORT"] = "sqs"
	if _, err := loadPublisherConfig(func(name string) string { return values[name] }); err == nil {
		t.Fatal("expected missing formal execution queue to fail")
	}
	values["TASK_EXECUTION_QUEUE_URL"] = "http://localhost:4566/000000000000/formal.fifo"
	if _, err := loadPublisherConfig(func(name string) string { return values[name] }); err != nil {
		t.Fatal(err)
	}
}

func TestPublisherRoutesCannotClaimFormalCommandsInDatabaseMode(t *testing.T) {
	values := baseEnvironment()
	config, err := loadPublisherConfig(func(name string) string { return values[name] })
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := publisherHandlers(config, routeSNSStub{}, routeSQSStub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(handlers) != 3 || handlers[awspublisher.FormalExecutionTopic] != nil {
		t.Fatalf("database transport exposed formal route: %#v", handlers)
	}
	config.FormalTransport = formalTransportSQS
	config.FormalExecutionQueueURL = "http://localhost:4566/000000000000/formal.fifo"
	handlers, err = publisherHandlers(config, routeSNSStub{}, routeSQSStub{})
	if err != nil {
		t.Fatal(err)
	}
	if len(handlers) != 4 || handlers[awspublisher.FormalExecutionTopic] == nil {
		t.Fatalf("SQS transport omitted formal route: %#v", handlers)
	}
}

func TestPublisherConfigRejectsLeaseShorterThanSequentialBatchBudget(t *testing.T) {
	values := baseEnvironment()
	values["OUTBOX_PUBLISHER_LEASE"] = "30s"
	values["OUTBOX_PUBLISHER_BATCH_SIZE"] = "10"
	values["OUTBOX_PUBLISH_TIMEOUT"] = "5s"
	if _, err := loadPublisherConfig(func(name string) string { return values[name] }); err == nil {
		t.Fatal("expected unsafe lease to fail")
	}
}

func baseEnvironment() map[string]string {
	return map[string]string{
		"DATABASE_URL":              "postgres://agent:agent@localhost/agent_platform",
		"AWS_REGION":                "us-east-1",
		"TASK_EVENTS_TOPIC_ARN":     "arn:aws:sns:us-east-1:000000000000:agent-task-events",
		"AGENT_EVENTS_TOPIC_ARN":    "arn:aws:sns:us-east-1:000000000000:agent-events",
		"ADMIN_OPERATION_QUEUE_URL": "http://localhost:4566/000000000000/admin-operations.fifo",
	}
}

package awspublisher

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/example/agent-platform/engine/internal/outbox"
)

type snsStub struct {
	input *sns.PublishInput
	err   error
}

func (stub *snsStub) Publish(_ context.Context, input *sns.PublishInput, _ ...func(*sns.Options)) (*sns.PublishOutput, error) {
	stub.input = input
	if stub.err != nil {
		return nil, stub.err
	}
	return &sns.PublishOutput{MessageId: aws.String("sns-message-1")}, nil
}

type sqsStub struct {
	inputs []*sqs.SendMessageInput
	err    error
}

func (stub *sqsStub) SendMessage(_ context.Context, input *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	stub.inputs = append(stub.inputs, input)
	if stub.err != nil {
		return nil, stub.err
	}
	return &sqs.SendMessageOutput{MessageId: aws.String("sqs-message-1")}, nil
}

func TestSNSHandlerPublishesVersionedEnvelopeAndIdempotencyAttributes(t *testing.T) {
	client := &snsStub{}
	handler, err := NewSNSHandler(client, "task-events", "arn:aws:sns:us-east-1:123456789012:task-events", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage("task-events", `{"eventType":"task.published","taskId":"task-1"}`)
	if err = handler.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(client.input.TopicArn) == "" || aws.ToString(client.input.MessageAttributes["message_id"].StringValue) != message.ID {
		t.Fatalf("unexpected SNS input: %#v", client.input)
	}
	var body map[string]json.RawMessage
	if err = json.Unmarshal([]byte(aws.ToString(client.input.Message)), &body); err != nil {
		t.Fatal(err)
	}
	if string(body["version"]) != `"`+outbox.EnvelopeVersion+`"` || string(body["payload"]) != string(message.Payload) {
		t.Fatalf("payload was not preserved as JSON: %s", aws.ToString(client.input.Message))
	}
}

func TestFIFOHandlerUsesStableMessageAndAggregateIdentifiers(t *testing.T) {
	client := &sqsStub{}
	handler, err := NewSQSHandler(client, "agent.execution.formal.requested", "http://localhost:4566/000000000000/formal.fifo", true, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	message := testMessage("agent.execution.formal.requested", `{"taskId":"task-1","logicalExecutionId":"logical-1"}`)
	if err = handler.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if err = handler.Handle(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	first, second := client.inputs[0], client.inputs[1]
	if aws.ToString(first.MessageDeduplicationId) == "" || aws.ToString(first.MessageDeduplicationId) != aws.ToString(second.MessageDeduplicationId) {
		t.Fatalf("deduplication ID is not stable: %#v %#v", first, second)
	}
	if aws.ToString(first.MessageGroupId) != stableID("group", message.Topic+"\x00task-1") || aws.ToString(first.MessageGroupId) != aws.ToString(second.MessageGroupId) {
		t.Fatalf("message group does not bind the task: %#v", first)
	}
}

func TestHandlersClassifyPayloadAndBrokerFailuresWithoutLeakingDetails(t *testing.T) {
	client := &snsStub{err: errors.New("untrusted AWS transport detail")}
	handler, err := NewSNSHandler(client, "task-events", "arn:aws:sns:us-east-1:123456789012:task-events", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err = handler.Handle(context.Background(), testMessage("task-events", `{}`)); err == nil || err.Error() != "sns_publish_failed" {
		t.Fatalf("unexpected broker error: %v", err)
	}
	invalid := testMessage("task-events", `{broken`)
	if err = handler.Handle(context.Background(), invalid); err == nil || err.Error() != "invalid_outbox_envelope" {
		t.Fatalf("unexpected invalid envelope error: %v", err)
	}
	wrongTopic := testMessage("agent-events", `{}`)
	if err = handler.Handle(context.Background(), wrongTopic); err == nil || err.Error() != "invalid_outbox_envelope" {
		t.Fatalf("unexpected route error: %v", err)
	}
	oversizedMetadata := testMessage("task-events", `{}`)
	oversizedMetadata.DedupeKey = string(make([]byte, 9*1024))
	if err = handler.Handle(context.Background(), oversizedMetadata); err == nil || err.Error() != "invalid_outbox_envelope" {
		t.Fatalf("unexpected metadata bound error: %v", err)
	}
}

func testMessage(topic, payload string) outbox.Message {
	return outbox.Message{
		ID:        "message-1",
		DedupeKey: "dedupe-1",
		Topic:     topic,
		Payload:   json.RawMessage(payload),
		CreatedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
	}
}

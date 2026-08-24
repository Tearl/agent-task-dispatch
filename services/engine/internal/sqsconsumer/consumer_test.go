package sqsconsumer

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/example/agent-platform/engine/internal/outbox"
)

type clientStub struct {
	mu         sync.Mutex
	messages   []types.Message
	deleted    []string
	visibility []int32
}

func (stub *clientStub) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.messages) == 0 {
		return &sqs.ReceiveMessageOutput{}, nil
	}
	message := stub.messages[0]
	stub.messages = stub.messages[1:]
	return &sqs.ReceiveMessageOutput{Messages: []types.Message{message}}, nil
}

func (stub *clientStub) DeleteMessage(_ context.Context, input *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.deleted = append(stub.deleted, aws.ToString(input.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

func (stub *clientStub) ChangeMessageVisibility(_ context.Context, input *sqs.ChangeMessageVisibilityInput, _ ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.visibility = append(stub.visibility, input.VisibilityTimeout)
	return &sqs.ChangeMessageVisibilityOutput{}, nil
}

type ledgerStub struct {
	records map[string]Consumption
}

func (stub *ledgerStub) Lookup(_ context.Context, consumerName, messageID string) (Consumption, bool, error) {
	value, found := stub.records[consumerName+"\x00"+messageID]
	return value, found, nil
}

func (stub *ledgerStub) Complete(_ context.Context, value Consumption) (bool, error) {
	key := value.ConsumerName + "\x00" + value.MessageID
	if existing, found := stub.records[key]; found {
		if !sameConsumption(existing, value) {
			return false, ErrLedgerConflict
		}
		return true, nil
	}
	stub.records[key] = value
	return false, nil
}

func TestConsumerProcessesOnceAndDeletesLedgerBackedReplay(t *testing.T) {
	raw := sqsMessage(t, "receipt-1", 1)
	client := &clientStub{messages: []types.Message{raw}}
	ledger := &ledgerStub{records: map[string]Consumption{}}
	calls := 0
	consumer := testConsumer(t, client, ledger, outbox.HandlerFunc(func(context.Context, outbox.Message) error {
		calls++
		return nil
	}))
	result, err := consumer.RunOnce(context.Background())
	if err != nil || result.Outcome != OutcomeProcessed || calls != 1 || len(client.deleted) != 1 || len(ledger.records) != 1 {
		t.Fatalf("first delivery: result=%#v calls=%d deleted=%#v ledger=%#v err=%v", result, calls, client.deleted, ledger.records, err)
	}
	raw.ReceiptHandle = aws.String("receipt-2")
	client.messages = append(client.messages, raw)
	result, err = consumer.RunOnce(context.Background())
	if err != nil || result.Outcome != OutcomeReplay || calls != 1 || len(client.deleted) != 2 {
		t.Fatalf("redelivery: result=%#v calls=%d deleted=%#v err=%v", result, calls, client.deleted, err)
	}
}

func TestConsumerRetriesHandlerFailureWithoutDeletingOrCompletingLedger(t *testing.T) {
	client := &clientStub{messages: []types.Message{sqsMessage(t, "receipt-1", 2)}}
	ledger := &ledgerStub{records: map[string]Consumption{}}
	consumer := testConsumer(t, client, ledger, outbox.HandlerFunc(func(context.Context, outbox.Message) error {
		return outbox.NewFailure("agent_capacity_unavailable", false)
	}))
	result, err := consumer.RunOnce(context.Background())
	if err != nil || result.Outcome != OutcomeRetry || result.FailureCode != "agent_capacity_unavailable" || result.Permanent || len(client.deleted) != 0 || len(ledger.records) != 0 || len(client.visibility) != 1 || client.visibility[0] != 10 {
		t.Fatalf("unexpected retry: result=%#v deleted=%#v visibility=%#v ledger=%#v err=%v", result, client.deleted, client.visibility, ledger.records, err)
	}
}

func TestConsumerRejectsAttributeSubstitutionBeforeCallingHandler(t *testing.T) {
	raw := sqsMessage(t, "receipt-1", 1)
	attribute := raw.MessageAttributes[outbox.AttributeDedupeKey]
	attribute.StringValue = aws.String("substituted")
	raw.MessageAttributes[outbox.AttributeDedupeKey] = attribute
	client := &clientStub{messages: []types.Message{raw}}
	calls := 0
	consumer := testConsumer(t, client, &ledgerStub{records: map[string]Consumption{}}, outbox.HandlerFunc(func(context.Context, outbox.Message) error {
		calls++
		return nil
	}))
	result, err := consumer.RunOnce(context.Background())
	if err != nil || result.Outcome != OutcomeRetry || result.FailureCode != "invalid_sqs_message" || !result.Permanent || calls != 0 || len(client.deleted) != 0 || len(client.visibility) != 1 {
		t.Fatalf("substituted message was not isolated: result=%#v calls=%d deleted=%#v visibility=%#v err=%v", result, calls, client.deleted, client.visibility, err)
	}
}

func TestConsumerExtendsVisibilityDuringLongHandler(t *testing.T) {
	client := &clientStub{messages: []types.Message{sqsMessage(t, "receipt-1", 1)}}
	consumer := testConsumer(t, client, &ledgerStub{records: map[string]Consumption{}}, outbox.HandlerFunc(func(context.Context, outbox.Message) error {
		time.Sleep(35 * time.Millisecond)
		return nil
	}))
	consumer.config.HeartbeatEvery = 10 * time.Millisecond
	result, err := consumer.RunOnce(context.Background())
	if err != nil || result.Outcome != OutcomeProcessed || len(client.deleted) != 1 || len(client.visibility) < 2 {
		t.Fatalf("visibility was not extended: result=%#v deleted=%#v visibility=%#v err=%v", result, client.deleted, client.visibility, err)
	}
	for _, value := range client.visibility {
		if value != 30 {
			t.Fatalf("heartbeat used unexpected timeout: %#v", client.visibility)
		}
	}
}

func testConsumer(t *testing.T, client *clientStub, ledger Ledger, handler outbox.Handler) *Consumer {
	t.Helper()
	consumer, err := New(client, ledger, handler, Config{
		ConsumerName:      "formal-execution-v1",
		QueueURL:          "http://localhost:4566/000000000000/formal.fifo",
		ExpectedTopic:     "agent.execution.formal.requested",
		VisibilityTimeout: 30 * time.Second,
		HeartbeatEvery:    10 * time.Second,
		APIRequestTimeout: 2 * time.Second,
		BaseBackoff:       5 * time.Second,
		MaxBackoff:        30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return consumer
}

func sqsMessage(t *testing.T, receipt string, receiveCount int) types.Message {
	t.Helper()
	message := outbox.Message{ID: "message-1", DedupeKey: "logical-1", Topic: "agent.execution.formal.requested", Payload: []byte(`{"taskId":"task-1","logicalExecutionId":"logical-1"}`), CreatedAt: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)}
	body, err := outbox.EncodeEnvelope(message)
	if err != nil {
		t.Fatal(err)
	}
	attribute := func(value string) types.MessageAttributeValue {
		return types.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(value)}
	}
	return types.Message{
		MessageId:     aws.String("broker-1"),
		ReceiptHandle: aws.String(receipt),
		Body:          aws.String(string(body)),
		Attributes:    map[string]string{string(types.MessageSystemAttributeNameApproximateReceiveCount): strconv.Itoa(receiveCount)},
		MessageAttributes: map[string]types.MessageAttributeValue{
			outbox.AttributeMessageID: attribute(message.ID),
			outbox.AttributeDedupeKey: attribute(message.DedupeKey),
			outbox.AttributeTopic:     attribute(message.Topic),
			outbox.AttributeVersion:   attribute(outbox.EnvelopeVersion),
		},
	}
}

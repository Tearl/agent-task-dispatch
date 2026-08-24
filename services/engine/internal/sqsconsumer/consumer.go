package sqsconsumer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/example/agent-platform/engine/internal/outbox"
)

type Client interface {
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(context.Context, *sqs.ChangeMessageVisibilityInput, ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

type Consumer struct {
	client  Client
	ledger  Ledger
	handler outbox.Handler
	config  Config
}

type receivedMessage struct {
	brokerMessageID string
	receiptHandle   string
	receiveCount    int
	body            []byte
	message         outbox.Message
	envelopeHash    string
}

func New(client Client, ledger Ledger, handler outbox.Handler, config Config) (*Consumer, error) {
	wholeSeconds := func(value time.Duration) bool { return value%time.Second == 0 }
	if client == nil || ledger == nil || handler == nil || !consumerNamePattern.MatchString(config.ConsumerName) || strings.TrimSpace(config.QueueURL) == "" || strings.TrimSpace(config.ExpectedTopic) == "" || config.WaitTime < 0 || config.WaitTime > 20*time.Second || !wholeSeconds(config.WaitTime) || config.VisibilityTimeout < 10*time.Second || config.VisibilityTimeout > 12*time.Hour || !wholeSeconds(config.VisibilityTimeout) || config.HeartbeatEvery < time.Second || config.HeartbeatEvery >= config.VisibilityTimeout || config.APIRequestTimeout < time.Second || config.APIRequestTimeout <= config.WaitTime || config.APIRequestTimeout > time.Minute || config.BaseBackoff < time.Second || config.MaxBackoff < config.BaseBackoff || config.MaxBackoff > 12*time.Hour {
		return nil, ErrInvalidInput
	}
	return &Consumer{client: client, ledger: ledger, handler: handler, config: config}, nil
}

func (consumer *Consumer) Run(ctx context.Context) error {
	for {
		if _, err := consumer.RunOnce(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func (consumer *Consumer) RunOnce(ctx context.Context) (Result, error) {
	requestID, err := receiveRequestID()
	if err != nil {
		return Result{}, fmt.Errorf("create SQS receive request ID: %w", err)
	}
	requestContext, cancel := context.WithTimeout(ctx, consumer.config.APIRequestTimeout)
	response, err := consumer.client.ReceiveMessage(requestContext, &sqs.ReceiveMessageInput{
		QueueUrl:                aws.String(consumer.config.QueueURL),
		MaxNumberOfMessages:     1,
		WaitTimeSeconds:         int32(consumer.config.WaitTime / time.Second),
		VisibilityTimeout:       int32(consumer.config.VisibilityTimeout / time.Second),
		ReceiveRequestAttemptId: aws.String(requestID),
		MessageAttributeNames: []string{
			outbox.AttributeMessageID,
			outbox.AttributeDedupeKey,
			outbox.AttributeTopic,
			outbox.AttributeVersion,
		},
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameApproximateReceiveCount},
	})
	cancel()
	if err != nil {
		return Result{}, fmt.Errorf("receive SQS message: %w", err)
	}
	if response == nil || len(response.Messages) == 0 {
		return Result{Outcome: OutcomeNoMessage}, nil
	}
	if len(response.Messages) != 1 {
		return Result{}, errors.New("SQS returned an invalid batch size")
	}
	received, decodeErr := consumer.decode(response.Messages[0])
	if decodeErr != nil {
		if received.receiptHandle == "" {
			return Result{}, decodeErr
		}
		if retryErr := consumer.retry(ctx, received); retryErr != nil {
			return Result{}, retryErr
		}
		return Result{Outcome: OutcomeRetry, MessageID: received.brokerMessageID, FailureCode: "invalid_sqs_message", Permanent: true, ReceiveCount: received.receiveCount}, nil
	}

	existing, found, err := consumer.ledger.Lookup(ctx, consumer.config.ConsumerName, received.message.ID)
	if err != nil {
		return Result{}, fmt.Errorf("lookup processed message: %w", err)
	}
	if found {
		if !sameConsumption(existing, consumer.consumption(received)) {
			if retryErr := consumer.retry(ctx, received); retryErr != nil {
				return Result{}, retryErr
			}
			return Result{Outcome: OutcomeRetry, MessageID: received.message.ID, FailureCode: "processed_message_conflict", Permanent: true, ReceiveCount: received.receiveCount}, nil
		}
		if err = consumer.delete(ctx, received.receiptHandle); err != nil {
			return Result{}, err
		}
		return Result{Outcome: OutcomeReplay, MessageID: received.message.ID, ReceiveCount: received.receiveCount}, nil
	}

	stopHeartbeat, heartbeatResult := consumer.startHeartbeat(ctx, received.receiptHandle)
	handleErr := consumer.handler.Handle(ctx, received.message)
	close(stopHeartbeat)
	heartbeatErr := <-heartbeatResult
	if handleErr != nil {
		code, permanent := failureDetails(handleErr)
		if retryErr := consumer.retry(ctx, received); retryErr != nil {
			return Result{}, retryErr
		}
		return Result{Outcome: OutcomeRetry, MessageID: received.message.ID, FailureCode: code, Permanent: permanent, ReceiveCount: received.receiveCount}, nil
	}
	if heartbeatErr != nil {
		return Result{}, heartbeatErr
	}
	replay, err := consumer.ledger.Complete(ctx, consumer.consumption(received))
	if errors.Is(err, ErrLedgerConflict) {
		if retryErr := consumer.retry(ctx, received); retryErr != nil {
			return Result{}, retryErr
		}
		return Result{Outcome: OutcomeRetry, MessageID: received.message.ID, FailureCode: "processed_message_conflict", Permanent: true, ReceiveCount: received.receiveCount}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("complete processed message: %w", err)
	}
	if err = consumer.delete(ctx, received.receiptHandle); err != nil {
		return Result{}, err
	}
	outcome := OutcomeProcessed
	if replay {
		outcome = OutcomeReplay
	}
	return Result{Outcome: outcome, MessageID: received.message.ID, ReceiveCount: received.receiveCount}, nil
}

func (consumer *Consumer) decode(raw types.Message) (receivedMessage, error) {
	value := receivedMessage{
		brokerMessageID: strings.TrimSpace(aws.ToString(raw.MessageId)),
		receiptHandle:   strings.TrimSpace(aws.ToString(raw.ReceiptHandle)),
		receiveCount:    1,
		body:            []byte(aws.ToString(raw.Body)),
	}
	if count, err := strconv.Atoi(raw.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]); err == nil && count > 0 {
		value.receiveCount = count
	}
	if value.brokerMessageID == "" || value.receiptHandle == "" {
		return value, errors.New("SQS message identity is missing")
	}
	message, err := outbox.DecodeEnvelope(value.body, consumer.config.ExpectedTopic)
	if err != nil {
		return value, err
	}
	expected := map[string]string{
		outbox.AttributeMessageID: message.ID,
		outbox.AttributeDedupeKey: message.DedupeKey,
		outbox.AttributeTopic:     message.Topic,
		outbox.AttributeVersion:   outbox.EnvelopeVersion,
	}
	for name, expectedValue := range expected {
		attribute, found := raw.MessageAttributes[name]
		if !found || aws.ToString(attribute.DataType) != "String" || aws.ToString(attribute.StringValue) != expectedValue {
			return value, errors.New("SQS message attributes do not match the envelope")
		}
	}
	digest := sha256.Sum256(value.body)
	value.message = message
	value.envelopeHash = "sha256:" + hex.EncodeToString(digest[:])
	return value, nil
}

func (consumer *Consumer) consumption(message receivedMessage) Consumption {
	return Consumption{
		ConsumerName:    consumer.config.ConsumerName,
		MessageID:       message.message.ID,
		Topic:           message.message.Topic,
		DedupeKey:       message.message.DedupeKey,
		EnvelopeHash:    message.envelopeHash,
		BrokerMessageID: message.brokerMessageID,
	}
}

func (consumer *Consumer) startHeartbeat(ctx context.Context, receiptHandle string) (chan struct{}, <-chan error) {
	stop := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(consumer.config.HeartbeatEvery)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				result <- nil
				return
			case <-ctx.Done():
				result <- ctx.Err()
				return
			case <-ticker.C:
				requestContext, cancel := context.WithTimeout(ctx, consumer.config.APIRequestTimeout)
				_, err := consumer.client.ChangeMessageVisibility(requestContext, &sqs.ChangeMessageVisibilityInput{QueueUrl: aws.String(consumer.config.QueueURL), ReceiptHandle: aws.String(receiptHandle), VisibilityTimeout: int32(consumer.config.VisibilityTimeout / time.Second)})
				cancel()
				if err != nil {
					result <- fmt.Errorf("extend SQS message visibility: %w", err)
					return
				}
			}
		}
	}()
	return stop, result
}

func (consumer *Consumer) retry(ctx context.Context, message receivedMessage) error {
	requestContext, cancel := context.WithTimeout(ctx, consumer.config.APIRequestTimeout)
	defer cancel()
	_, err := consumer.client.ChangeMessageVisibility(requestContext, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(consumer.config.QueueURL),
		ReceiptHandle:     aws.String(message.receiptHandle),
		VisibilityTimeout: int32(consumer.backoff(message.receiveCount) / time.Second),
	})
	if err != nil {
		return fmt.Errorf("schedule SQS message retry: %w", err)
	}
	return nil
}

func (consumer *Consumer) delete(ctx context.Context, receiptHandle string) error {
	requestContext, cancel := context.WithTimeout(ctx, consumer.config.APIRequestTimeout)
	defer cancel()
	_, err := consumer.client.DeleteMessage(requestContext, &sqs.DeleteMessageInput{QueueUrl: aws.String(consumer.config.QueueURL), ReceiptHandle: aws.String(receiptHandle)})
	if err != nil {
		return fmt.Errorf("delete SQS message: %w", err)
	}
	return nil
}

func (consumer *Consumer) backoff(receiveCount int) time.Duration {
	delay := consumer.config.BaseBackoff
	for range max(0, min(receiveCount-1, 30)) {
		if delay >= consumer.config.MaxBackoff/2 {
			return consumer.config.MaxBackoff
		}
		delay *= 2
	}
	return min(delay, consumer.config.MaxBackoff)
}

func sameConsumption(left, right Consumption) bool {
	return left.ConsumerName == right.ConsumerName && left.MessageID == right.MessageID && left.Topic == right.Topic && left.DedupeKey == right.DedupeKey && left.EnvelopeHash == right.EnvelopeHash
}

func failureDetails(err error) (string, bool) {
	var failure outbox.Failure
	if errors.As(err, &failure) {
		return failure.Code, failure.Permanent
	}
	return "handler_failed", false
}

func receiveRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

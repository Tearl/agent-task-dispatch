package awspublisher

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/example/agent-platform/engine/internal/outbox"
)

const (
	TaskEventsTopic      = "task-events"
	AgentEventsTopic     = "agent-events"
	FormalExecutionTopic = "agent.execution.formal.requested"
	AdminOperationTopic  = "admin.operation.requested"
)

type SNSClient interface {
	Publish(context.Context, *sns.PublishInput, ...func(*sns.Options)) (*sns.PublishOutput, error)
}

type SQSClient interface {
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type SNSHandler struct {
	client   SNSClient
	topic    string
	topicARN string
	timeout  time.Duration
}

type SQSHandler struct {
	client   SQSClient
	topic    string
	queueURL string
	fifo     bool
	timeout  time.Duration
}

func NewSNSHandler(client SNSClient, topic, topicARN string, timeout time.Duration) (*SNSHandler, error) {
	if client == nil || strings.TrimSpace(topic) == "" || strings.TrimSpace(topicARN) == "" || timeout < time.Second || timeout > time.Minute {
		return nil, outbox.ErrInvalidInput
	}
	return &SNSHandler{client: client, topic: topic, topicARN: topicARN, timeout: timeout}, nil
}

func NewSQSHandler(client SQSClient, topic, queueURL string, fifo bool, timeout time.Duration) (*SQSHandler, error) {
	if client == nil || strings.TrimSpace(topic) == "" || strings.TrimSpace(queueURL) == "" || timeout < time.Second || timeout > time.Minute {
		return nil, outbox.ErrInvalidInput
	}
	return &SQSHandler{client: client, topic: topic, queueURL: queueURL, fifo: fifo, timeout: timeout}, nil
}

func (handler *SNSHandler) Handle(ctx context.Context, message outbox.Message) error {
	if message.Topic != handler.topic {
		return outbox.NewFailure("invalid_outbox_envelope", true)
	}
	body, err := outbox.EncodeEnvelope(message)
	if err != nil {
		return err
	}
	requestContext, cancel := context.WithTimeout(ctx, handler.timeout)
	defer cancel()
	result, err := handler.client.Publish(requestContext, &sns.PublishInput{
		TopicArn:          aws.String(handler.topicARN),
		Message:           aws.String(string(body)),
		MessageAttributes: snsAttributes(message),
	})
	if err != nil || result == nil || strings.TrimSpace(aws.ToString(result.MessageId)) == "" {
		return outbox.NewFailure("sns_publish_failed", false)
	}
	return nil
}

func (handler *SQSHandler) Handle(ctx context.Context, message outbox.Message) error {
	if message.Topic != handler.topic {
		return outbox.NewFailure("invalid_outbox_envelope", true)
	}
	body, err := outbox.EncodeEnvelope(message)
	if err != nil {
		return err
	}
	input := &sqs.SendMessageInput{
		QueueUrl:          aws.String(handler.queueURL),
		MessageBody:       aws.String(string(body)),
		MessageAttributes: sqsAttributes(message),
	}
	if handler.fifo {
		input.MessageDeduplicationId = aws.String(stableID("dedupe", message.ID))
		input.MessageGroupId = aws.String(messageGroupID(message))
	}
	requestContext, cancel := context.WithTimeout(ctx, handler.timeout)
	defer cancel()
	result, err := handler.client.SendMessage(requestContext, input)
	if err != nil || result == nil || strings.TrimSpace(aws.ToString(result.MessageId)) == "" {
		return outbox.NewFailure("sqs_send_failed", false)
	}
	return nil
}

func messageGroupID(message outbox.Message) string {
	aggregate := message.DedupeKey
	var payload map[string]json.RawMessage
	if json.Unmarshal(message.Payload, &payload) == nil {
		for _, key := range []string{"taskId", "resourceId", "agentId", "operationId"} {
			var value string
			if raw, found := payload[key]; found && json.Unmarshal(raw, &value) == nil && strings.TrimSpace(value) != "" {
				aggregate = value
				break
			}
		}
	}
	return stableID("group", message.Topic+"\x00"+aggregate)
}

func stableID(namespace, value string) string {
	digest := sha256.Sum256([]byte(namespace + "\x00" + value))
	return hex.EncodeToString(digest[:])
}

func snsAttributes(message outbox.Message) map[string]snstypes.MessageAttributeValue {
	return map[string]snstypes.MessageAttributeValue{
		outbox.AttributeMessageID: stringSNSAttribute(message.ID),
		outbox.AttributeDedupeKey: stringSNSAttribute(message.DedupeKey),
		outbox.AttributeTopic:     stringSNSAttribute(message.Topic),
		outbox.AttributeVersion:   stringSNSAttribute(outbox.EnvelopeVersion),
	}
}

func sqsAttributes(message outbox.Message) map[string]sqstypes.MessageAttributeValue {
	return map[string]sqstypes.MessageAttributeValue{
		outbox.AttributeMessageID: stringSQSAttribute(message.ID),
		outbox.AttributeDedupeKey: stringSQSAttribute(message.DedupeKey),
		outbox.AttributeTopic:     stringSQSAttribute(message.Topic),
		outbox.AttributeVersion:   stringSQSAttribute(outbox.EnvelopeVersion),
	}
}

func stringSNSAttribute(value string) snstypes.MessageAttributeValue {
	return snstypes.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(value)}
}

func stringSQSAttribute(value string) sqstypes.MessageAttributeValue {
	return sqstypes.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(value)}
}

var _ outbox.Handler = (*SNSHandler)(nil)
var _ outbox.Handler = (*SQSHandler)(nil)

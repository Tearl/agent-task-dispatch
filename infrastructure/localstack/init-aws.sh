#!/usr/bin/env sh
set -eu

REGION="${AWS_DEFAULT_REGION:-us-east-1}"

create_queue() {
  queue_name="$1"
  case "$queue_name" in
    *.fifo)
      awslocal sqs create-queue \
        --region "$REGION" \
        --queue-name "$queue_name" \
        --attributes FifoQueue=true,ContentBasedDeduplication=false \
        --query QueueUrl \
        --output text
      ;;
    *)
      awslocal sqs create-queue \
        --region "$REGION" \
        --queue-name "$queue_name" \
        --query QueueUrl \
        --output text
      ;;
  esac
}

queue_arn() {
  awslocal sqs get-queue-attributes \
    --region "$REGION" \
    --queue-url "$1" \
    --attribute-names QueueArn \
    --query Attributes.QueueArn \
    --output text
}

configure_redrive() {
  source_url="$1"
  dead_letter_url="$2"
  dead_letter_arn="$(queue_arn "$dead_letter_url")"
  attributes="$(python3 -c '
import json
import sys

print(json.dumps({
    "RedrivePolicy": json.dumps({
        "deadLetterTargetArn": sys.argv[1],
        "maxReceiveCount": "5",
    })
}))
' "$dead_letter_arn")"
  awslocal sqs set-queue-attributes \
    --region "$REGION" \
    --queue-url "$source_url" \
    --attributes "$attributes"
}

configure_sns_subscription() {
  source_url="$1"
  dead_letter_url="$2"
  topic_arn="$3"
  source_arn="$(queue_arn "$source_url")"
  dead_letter_arn="$(queue_arn "$dead_letter_url")"
  attributes="$(python3 -c '
import json
import sys

source, dead, topic = sys.argv[1:]
print(json.dumps({
    "RedrivePolicy": json.dumps({
        "deadLetterTargetArn": dead,
        "maxReceiveCount": "5",
    }),
    "Policy": json.dumps({
        "Version": "2012-10-17",
        "Statement": [{
            "Sid": "AllowSNSSendMessage",
            "Effect": "Allow",
            "Principal": {"Service": "sns.amazonaws.com"},
            "Action": "sqs:SendMessage",
            "Resource": source,
            "Condition": {"ArnEquals": {"aws:SourceArn": topic}},
        }],
    }),
}))
' "$source_arn" "$dead_letter_arn" "$topic_arn")"
  awslocal sqs set-queue-attributes \
    --region "$REGION" \
    --queue-url "$source_url" \
    --attributes "$attributes"
  awslocal sns subscribe \
    --region "$REGION" \
    --topic-arn "$topic_arn" \
    --protocol sqs \
    --notification-endpoint "$source_arn" \
    --attributes RawMessageDelivery=true >/dev/null
}

TASK_EVENTS_TOPIC_ARN="$(awslocal sns create-topic \
  --region "$REGION" \
  --name agent-task-events \
  --query TopicArn \
  --output text)"

AGENT_EVENTS_TOPIC_ARN="$(awslocal sns create-topic \
  --region "$REGION" \
  --name agent-events \
  --query TopicArn \
  --output text)"

TASK_EVENTS_DLQ_URL="$(create_queue agent-task-events-projection-dlq)"
TASK_EVENTS_QUEUE_URL="$(create_queue agent-task-events-projection)"
configure_sns_subscription "$TASK_EVENTS_QUEUE_URL" "$TASK_EVENTS_DLQ_URL" "$TASK_EVENTS_TOPIC_ARN"

AGENT_EVENTS_DLQ_URL="$(create_queue agent-events-projection-dlq)"
AGENT_EVENTS_QUEUE_URL="$(create_queue agent-events-projection)"
configure_sns_subscription "$AGENT_EVENTS_QUEUE_URL" "$AGENT_EVENTS_DLQ_URL" "$AGENT_EVENTS_TOPIC_ARN"

FORMAL_EXECUTION_DLQ_URL="$(create_queue agent-formal-execution-dlq.fifo)"
FORMAL_EXECUTION_QUEUE_URL="$(create_queue agent-formal-execution.fifo)"
configure_redrive "$FORMAL_EXECUTION_QUEUE_URL" "$FORMAL_EXECUTION_DLQ_URL"

ADMIN_OPERATION_DLQ_URL="$(create_queue admin-operations-dlq.fifo)"
ADMIN_OPERATION_QUEUE_URL="$(create_queue admin-operations.fifo)"
configure_redrive "$ADMIN_OPERATION_QUEUE_URL" "$ADMIN_OPERATION_DLQ_URL"

echo "Local SNS/SQS resources are ready."

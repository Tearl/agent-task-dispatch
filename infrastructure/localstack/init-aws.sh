#!/usr/bin/env sh
set -eu

REGION="${AWS_DEFAULT_REGION:-us-east-1}"

DLQ_URL="$(awslocal sqs create-queue \
  --region "$REGION" \
  --queue-name agent-task-execution-dlq \
  --query QueueUrl \
  --output text)"

DLQ_ARN="$(awslocal sqs get-queue-attributes \
  --region "$REGION" \
  --queue-url "$DLQ_URL" \
  --attribute-names QueueArn \
  --query Attributes.QueueArn \
  --output text)"

TASK_QUEUE_URL="$(awslocal sqs create-queue \
  --region "$REGION" \
  --queue-name agent-task-execution \
  --query QueueUrl \
  --output text)"

REDRIVE_POLICY="$(jq -nc --arg arn "$DLQ_ARN" \
  '{RedrivePolicy: ({deadLetterTargetArn: $arn, maxReceiveCount: "3"} | tojson)}')"

awslocal sqs set-queue-attributes \
  --region "$REGION" \
  --queue-url "$TASK_QUEUE_URL" \
  --attributes "$REDRIVE_POLICY"

TOPIC_ARN="$(awslocal sns create-topic \
  --region "$REGION" \
  --name agent-task-events \
  --query TopicArn \
  --output text)"

TASK_QUEUE_ARN="$(awslocal sqs get-queue-attributes \
  --region "$REGION" \
  --queue-url "$TASK_QUEUE_URL" \
  --attribute-names QueueArn \
  --query Attributes.QueueArn \
  --output text)"

awslocal sns subscribe \
  --region "$REGION" \
  --topic-arn "$TOPIC_ARN" \
  --protocol sqs \
  --notification-endpoint "$TASK_QUEUE_ARN"

echo "Local SNS/SQS resources are ready."


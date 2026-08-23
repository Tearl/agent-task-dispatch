package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/outbox"
)

const FormalExecutionTopic = "agent.execution.formal.requested"

const maxFormalExecutionPayloadBytes = 256 * 1024

type ExecutionService interface {
	Create(context.Context, execution.Spec) (execution.Execution, bool, error)
	Dispatch(context.Context, string) (execution.Execution, execution.Attempt, bool, error)
}

type DeliveryRecorder interface {
	RecordDispatched(context.Context, string) (delivery.Version, bool, error)
}

type FormalExecutionHandler struct {
	executions ExecutionService
	deliveries DeliveryRecorder
}

func NewFormalExecutionHandler(executions ExecutionService, deliveries DeliveryRecorder) (*FormalExecutionHandler, error) {
	if executions == nil || deliveries == nil {
		return nil, outbox.ErrInvalidInput
	}
	return &FormalExecutionHandler{executions: executions, deliveries: deliveries}, nil
}

func (handler *FormalExecutionHandler) Handle(ctx context.Context, message outbox.Message) error {
	spec, err := decodeFormalSpec(message.Payload)
	if err != nil || message.Topic != FormalExecutionTopic || message.DedupeKey == "" || message.DedupeKey != spec.LogicalExecutionID || spec.Stage != execution.StageFormal {
		return outbox.NewFailure("invalid_formal_execution_payload", true)
	}
	if _, _, err = handler.executions.Create(ctx, spec); err != nil {
		return executionFailure(err)
	}
	if _, _, _, err := handler.executions.Dispatch(ctx, spec.LogicalExecutionID); err != nil {
		return executionFailure(err)
	}
	if _, _, err := handler.deliveries.RecordDispatched(ctx, spec.LogicalExecutionID); err != nil {
		if errors.Is(err, delivery.ErrInvalidInput) || errors.Is(err, delivery.ErrInvalidState) || errors.Is(err, delivery.ErrNotFound) {
			return outbox.NewFailure("formal_delivery_state_conflict", true)
		}
		return outbox.NewFailure("formal_delivery_record_failed", false)
	}
	return nil
}

func decodeFormalSpec(payload []byte) (execution.Spec, error) {
	if len(payload) == 0 || len(payload) > maxFormalExecutionPayloadBytes {
		return execution.Spec{}, errors.New("invalid formal execution payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var spec execution.Spec
	if err := decoder.Decode(&spec); err != nil {
		return execution.Spec{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return execution.Spec{}, errors.New("formal execution payload contains trailing data")
	}
	return spec, nil
}

func executionFailure(err error) error {
	switch {
	case errors.Is(err, execution.ErrInvalidInput), errors.Is(err, execution.ErrContentConflict):
		return outbox.NewFailure("invalid_execution_spec", true)
	case errors.Is(err, execution.ErrNotFound), errors.Is(err, execution.ErrInvalidState):
		return outbox.NewFailure("execution_state_conflict", true)
	case errors.Is(err, agent.ErrCapacityUnavailable):
		return outbox.NewFailure("agent_capacity_unavailable", false)
	default:
		return outbox.NewFailure("agent_dispatch_failed", false)
	}
}

var _ outbox.Handler = (*FormalExecutionHandler)(nil)

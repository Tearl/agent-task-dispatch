package worker

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/delivery"
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/outbox"
)

type executionStub struct {
	created    int
	dispatched int
	err        error
}

func (stub *executionStub) Create(_ context.Context, spec execution.Spec) (execution.Execution, bool, error) {
	stub.created++
	return execution.Execution{Spec: spec}, false, stub.err
}
func (stub *executionStub) Dispatch(_ context.Context, id string) (execution.Execution, execution.Attempt, bool, error) {
	stub.dispatched++
	return execution.Execution{Spec: execution.Spec{LogicalExecutionID: id}}, execution.Attempt{}, false, stub.err
}

type deliveryStub struct{ calls int }

func (stub *deliveryStub) RecordDispatched(context.Context, string) (delivery.Version, bool, error) {
	stub.calls++
	return delivery.Version{}, false, nil
}

func TestFormalExecutionHandlerDispatchesAndRecordsDelivery(t *testing.T) {
	executions, deliveries := &executionStub{}, &deliveryStub{}
	handler, err := NewFormalExecutionHandler(executions, deliveries)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(execution.Spec{LogicalExecutionID: "logical-1", Stage: execution.StageFormal, Deadline: time.Now().Add(time.Hour)})
	if err = handler.Handle(context.Background(), formalMessage(payload)); err != nil {
		t.Fatal(err)
	}
	if executions.created != 1 || executions.dispatched != 1 || deliveries.calls != 1 {
		t.Fatalf("created=%d dispatched=%d deliveries=%d", executions.created, executions.dispatched, deliveries.calls)
	}
}

func TestFormalExecutionHandlerClassifiesInvalidAndRetryableFailures(t *testing.T) {
	handler, _ := NewFormalExecutionHandler(&executionStub{}, &deliveryStub{})
	err := handler.Handle(context.Background(), outbox.Message{Topic: FormalExecutionTopic, DedupeKey: "logical-1", Payload: []byte(`{"logicalExecutionId":"logical-1","stage":"overview"}`)})
	var failure outbox.Failure
	if !errors.As(err, &failure) || !failure.Permanent || failure.Code != "invalid_formal_execution_payload" {
		t.Fatalf("unexpected invalid payload failure: %#v", err)
	}
	err = handler.Handle(context.Background(), formalMessage([]byte(`{"logicalExecutionId":"logical-1","stage":"formal","unexpected":true}`)))
	if !errors.As(err, &failure) || !failure.Permanent || failure.Code != "invalid_formal_execution_payload" {
		t.Fatalf("unexpected unknown field failure: %#v", err)
	}
	executions := &executionStub{err: errors.New("network detail must not escape")}
	handler, _ = NewFormalExecutionHandler(executions, &deliveryStub{})
	payload, _ := json.Marshal(execution.Spec{LogicalExecutionID: "logical-1", Stage: execution.StageFormal})
	err = handler.Handle(context.Background(), formalMessage(payload))
	if !errors.As(err, &failure) || failure.Permanent || failure.Code != "agent_dispatch_failed" {
		t.Fatalf("unexpected retryable failure: %#v", err)
	}
}

func formalMessage(payload []byte) outbox.Message {
	return outbox.Message{Topic: FormalExecutionTopic, DedupeKey: "logical-1", Payload: payload}
}

package execution

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
)

type keyProviderStub struct{ key []byte }

func (provider keyProviderStub) CallbackKey(context.Context, string, string) ([]byte, error) {
	return append([]byte{}, provider.key...), nil
}

type testClock struct{ value time.Time }

func (clock *testClock) Now() time.Time { return clock.value }

type leaserStub struct {
	now      func() time.Time
	next     int64
	leases   map[string]agent.CapacityLease
	releases []agent.CapacityLease
}

func (leaser *leaserStub) ReserveCapacity(_ context.Context, agentID, reservationID string, ttl time.Duration) (agent.CapacityLease, error) {
	if existing, ok := leaser.leases[reservationID]; ok {
		return existing, nil
	}
	leaser.next++
	lease := agent.CapacityLease{AgentID: agentID, ReservationID: reservationID, FencingToken: leaser.next, ExpiresAt: leaser.now().Add(ttl)}
	leaser.leases[reservationID] = lease
	return lease, nil
}

func (leaser *leaserStub) ReleaseCapacity(_ context.Context, reservationID string, fencingToken int64) error {
	lease, ok := leaser.leases[reservationID]
	if !ok || lease.FencingToken != fencingToken {
		return agent.ErrStaleVersion
	}
	leaser.releases = append(leaser.releases, lease)
	return nil
}

type clientStub struct {
	createCalls      []Envelope
	statusCalls      []Envelope
	cancelCalls      []Envelope
	deliverableCalls []Envelope
	createErrors     []error
	createAccepted   bool
	createStatus     string
	statusResponse   StatusResponse
	deliverable      DeliverableResponse
}

func (client *clientStub) Create(_ context.Context, _ string, envelope Envelope) (CreateResponse, error) {
	client.createCalls = append(client.createCalls, envelope)
	if len(client.createErrors) > 0 {
		err := client.createErrors[0]
		client.createErrors = client.createErrors[1:]
		return CreateResponse{}, err
	}
	status := client.createStatus
	if status == "" {
		status = ExecutionRunning
	}
	return CreateResponse{Accepted: client.createAccepted, Status: status, Reason: "rejected"}, nil
}
func (client *clientStub) Status(_ context.Context, _ string, envelope Envelope) (StatusResponse, error) {
	client.statusCalls = append(client.statusCalls, envelope)
	return client.statusResponse, nil
}
func (client *clientStub) Cancel(_ context.Context, _ string, envelope Envelope) (CancelResponse, error) {
	client.cancelCalls = append(client.cancelCalls, envelope)
	return CancelResponse{Accepted: true}, nil
}
func (client *clientStub) Deliverable(_ context.Context, _ string, envelope Envelope) (DeliverableResponse, error) {
	client.deliverableCalls = append(client.deliverableCalls, envelope)
	return client.deliverable, nil
}

func TestDispatchTransportRedeliveryReusesLogicalAndNetworkAttempt(t *testing.T) {
	service, repository, leaser, client, _, _ := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	client.createErrors = []error{errors.New("ambiguous timeout")}
	_, firstAttempt, _, err := service.Dispatch(context.Background(), "execution-1")
	if err == nil {
		t.Fatal("expected ambiguous transport failure")
	}
	_, secondAttempt, replay, err := service.Dispatch(context.Background(), "execution-1")
	if err != nil || !replay || firstAttempt.AttemptID != secondAttempt.AttemptID || firstAttempt.FencingToken != secondAttempt.FencingToken {
		t.Fatalf("redelivery changed attempt: first=%#v second=%#v replay=%v err=%v", firstAttempt, secondAttempt, replay, err)
	}
	if len(client.createCalls) != 2 || !reflect.DeepEqual(client.createCalls[0], client.createCalls[1]) || len(leaser.leases) != 1 {
		t.Fatalf("redelivery changed protocol request or lease: calls=%#v leases=%#v", client.createCalls, leaser.leases)
	}
	current, err := repository.Get(context.Background(), "execution-1")
	if err != nil || current.CurrentAttempt != 1 || current.Status != ExecutionRunning {
		t.Fatalf("unexpected current execution: %#v err=%v", current, err)
	}
}

func TestDefinitiveRejectionAllowsNewFencedAttempt(t *testing.T) {
	service, _, _, client, _, _ := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	client.createAccepted = false
	_, first, _, err := service.Dispatch(context.Background(), "execution-1")
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected definitive rejection, got %v", err)
	}
	client.createAccepted = true
	_, second, replay, err := service.Dispatch(context.Background(), "execution-1")
	if err != nil || replay || second.Number != 2 || second.FencingToken <= first.FencingToken || second.AttemptID == first.AttemptID {
		t.Fatalf("retry did not advance fenced network attempt: first=%#v second=%#v replay=%v err=%v", first, second, replay, err)
	}
	if client.createCalls[0].LogicalExecutionID != client.createCalls[1].LogicalExecutionID || client.createCalls[0].IdempotencyKey != client.createCalls[1].IdempotencyKey {
		t.Fatalf("logical identity changed across retry: %#v", client.createCalls)
	}
}

func TestAcceptedCreateWithInvalidProtocolStatusFailsAttempt(t *testing.T) {
	service, repository, leaser, client, _, _ := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	client.createStatus = ExecutionSucceeded
	if _, _, _, err := service.Dispatch(context.Background(), "execution-1"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("invalid accepted response was not rejected: %v", err)
	}
	current, getErr := repository.Get(context.Background(), "execution-1")
	attempt, attemptErr := repository.CurrentAttempt(context.Background(), "execution-1")
	if getErr != nil || attemptErr != nil || current.Status != ExecutionFailed || attempt.Status != AttemptFailed || len(leaser.releases) != 1 {
		t.Fatalf("invalid response cleanup: execution=%#v attempt=%#v releases=%d getErr=%v attemptErr=%v", current, attempt, len(leaser.releases), getErr, attemptErr)
	}
}

func TestSignedCallbackCompletesOnceAndProtectsDeliverable(t *testing.T) {
	service, _, leaser, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	_, attempt, _, err := service.Dispatch(context.Background(), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	callback := successCallback(client.createCalls[0], key, clock.Now())
	signature, _ := SignCallback(callback, key)
	result, err := service.HandleCallback(context.Background(), callback, signature)
	if err != nil || result.Outcome != CallbackAccepted || result.Replay || result.Execution.Status != ExecutionSucceeded || len(leaser.releases) != 1 {
		t.Fatalf("callback completion: result=%#v releases=%#v err=%v", result, leaser.releases, err)
	}
	replayed, err := service.HandleCallback(context.Background(), callback, signature)
	if err != nil || !replayed.Replay || replayed.Execution.ContentHash != callback.ContentHash || len(leaser.releases) != 1 {
		t.Fatalf("callback replay was not stable: result=%#v releases=%#v err=%v", replayed, leaser.releases, err)
	}
	conflicting := callback
	conflicting.ContentHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	conflictingSignature, _ := SignCallback(conflicting, key)
	if _, err = service.HandleCallback(context.Background(), conflicting, conflictingSignature); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("conflicting callback was not detected: %v", err)
	}
	client.deliverable = DeliverableResponse{ContentHash: callback.ContentHash, DeliverableRef: callback.DeliverableRef}
	deliverable, err := service.Deliverable(context.Background(), "execution-1")
	if err != nil || deliverable.ContentHash != callback.ContentHash || client.deliverableCalls[0].FencingToken != attempt.FencingToken {
		t.Fatalf("deliverable verification: response=%#v err=%v", deliverable, err)
	}
	client.deliverable.ContentHash = conflicting.ContentHash
	if _, err = service.Deliverable(context.Background(), "execution-1"); !errors.Is(err, ErrContentConflict) {
		t.Fatalf("deliverable substitution was not rejected: %v", err)
	}
}

func TestCancelMakesSignedLateResultAuditOnly(t *testing.T) {
	service, _, leaser, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.Dispatch(context.Background(), "execution-1"); err != nil {
		t.Fatal(err)
	}
	envelope := client.createCalls[0]
	cancelled, err := service.Cancel(context.Background(), "execution-1")
	if err != nil || cancelled.Status != ExecutionCancelled || len(client.cancelCalls) != 1 || len(leaser.releases) != 1 {
		t.Fatalf("cancel: execution=%#v calls=%d releases=%d err=%v", cancelled, len(client.cancelCalls), len(leaser.releases), err)
	}
	callback := successCallback(envelope, key, clock.Now())
	signature, _ := SignCallback(callback, key)
	result, err := service.HandleCallback(context.Background(), callback, signature)
	if err != nil || result.Outcome != CallbackLate || result.Execution.Status != ExecutionCancelled || result.Execution.ContentHash != "" {
		t.Fatalf("late callback changed cancelled result: %#v err=%v", result, err)
	}
}

func TestExpiredAttemptRejectsOldFenceAndCostCapStopsActiveAttempt(t *testing.T) {
	service, repository, leaser, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	_, first, _, err := service.Dispatch(context.Background(), "execution-1")
	if err != nil {
		t.Fatal(err)
	}
	firstEnvelope := client.createCalls[0]
	clock.value = clock.Now().Add(11 * time.Minute)
	_, second, replay, err := service.Dispatch(context.Background(), "execution-1")
	if err != nil || replay || second.Number != 2 || second.FencingToken <= first.FencingToken {
		t.Fatalf("expired lease did not advance fence: first=%#v second=%#v replay=%v err=%v", first, second, replay, err)
	}
	late := successCallback(firstEnvelope, key, clock.Now())
	lateSignature, _ := SignCallback(late, key)
	result, err := service.HandleCallback(context.Background(), late, lateSignature)
	if err != nil || result.Outcome != CallbackStaleFence {
		t.Fatalf("old fence callback was accepted: %#v err=%v", result, err)
	}
	client.statusResponse = StatusResponse{Status: ExecutionRunning, UsedCost: "100"}
	if _, err = service.Poll(context.Background(), "execution-1"); !errors.Is(err, ErrCostCapExceeded) {
		t.Fatalf("cost cap did not stop execution: %v", err)
	}
	stopped, _ := repository.Get(context.Background(), "execution-1")
	if stopped.Status != ExecutionCostStopped || len(client.cancelCalls) != 1 || len(leaser.releases) < 1 {
		t.Fatalf("cost stop incomplete: execution=%#v cancelCalls=%d releases=%d", stopped, len(client.cancelCalls), len(leaser.releases))
	}
}

func TestCallbackAfterLeaseExpiryIsAuditOnlyWithoutReplacementAttempt(t *testing.T) {
	service, repository, _, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.Dispatch(context.Background(), "execution-1"); err != nil {
		t.Fatal(err)
	}
	clock.value = clock.Now().Add(11 * time.Minute)
	callback := successCallback(client.createCalls[0], key, clock.Now())
	signature, _ := SignCallback(callback, key)
	result, err := service.HandleCallback(context.Background(), callback, signature)
	if err != nil || result.Outcome != CallbackStaleFence {
		t.Fatalf("expired lease callback result=%#v err=%v", result, err)
	}
	current, _ := repository.Get(context.Background(), "execution-1")
	if current.Status != ExecutionRunning || current.ContentHash != "" {
		t.Fatalf("expired callback changed business result: %#v", current)
	}
}

func TestCallbackRejectsBadSignatureTimestampAndNonce(t *testing.T) {
	service, _, _, client, key, clock := executionFixture(t)
	if _, _, err := service.Create(context.Background(), validOverviewSpec()); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := service.Dispatch(context.Background(), "execution-1"); err != nil {
		t.Fatal(err)
	}
	callback := successCallback(client.createCalls[0], key, clock.Now())
	if _, err := service.HandleCallback(context.Background(), callback, "hmac-sha256="+string(make([]byte, 64))); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("bad signature accepted: %v", err)
	}
	callback.Timestamp = clock.Now().Add(-DefaultCallbackClockSkew - time.Second)
	signature, _ := SignCallback(callback, key)
	if _, err := service.HandleCallback(context.Background(), callback, signature); !errors.Is(err, ErrInvalidCallback) {
		t.Fatalf("stale callback accepted: %v", err)
	}
	callback.Timestamp = clock.Now()
	callback.Nonce = "wrong-once-nonce"
	signature, _ = SignCallback(callback, key)
	if _, err := service.HandleCallback(context.Background(), callback, signature); !errors.Is(err, ErrCallbackReplay) {
		t.Fatalf("wrong callback nonce accepted: %v", err)
	}
}

func executionFixture(t *testing.T) (*Service, *MemoryRepository, *leaserStub, *clientStub, []byte, *testClock) {
	t.Helper()
	clock := &testClock{value: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)}
	key := []byte("callback-signing-key-32-bytes!!!")
	repository := NewMemoryRepository()
	repository.now = clock.Now
	leaser := &leaserStub{now: clock.Now, leases: make(map[string]agent.CapacityLease)}
	client := &clientStub{createAccepted: true}
	verifier, err := NewCallbackVerifier(keyProviderStub{key: key}, DefaultCallbackClockSkew)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = clock.Now
	service, err := NewService(repository, leaser, client, verifier, Config{
		CallbackBaseURL: "https://engine.example/v1/agent-callbacks",
		NonceKeyVersion: "callback-nonce-v1",
		NonceSecret:     []byte("callback-nonce-secret-32-bytes!!"),
		LeaseTTL:        10 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	service.now = clock.Now
	return service, repository, leaser, client, key, clock
}

func validOverviewSpec() Spec {
	return Spec{
		LogicalExecutionID: "execution-1",
		Stage:              StageOverview,
		TaskID:             "task-1",
		TaskSpecHash:       "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		InputRef:           "brief://task-1/spec-1",
		InputHash:          "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		AgentID:            "agent-1",
		AgentEndpoint:      "https://agent.example",
		ResponsibilityCode: "overview_candidate",
		CostCap:            "100",
		ToolPolicy:         ToolPolicy{Mode: ToolPolicyReadOnly, AllowedTools: []string{"search", "read"}},
		Deadline:           time.Date(2026, 8, 21, 13, 0, 0, 0, time.UTC),
		IdempotencyKey:     "execution-1-v1",
		Overview:           &OverviewBinding{MatchRevision: 1, AllocationID: "allocation-1", QuoteHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
	}
}

func successCallback(envelope Envelope, _ []byte, timestamp time.Time) Callback {
	return Callback{
		ProtocolVersion:    ProtocolVersion,
		LogicalExecutionID: envelope.LogicalExecutionID,
		AttemptID:          envelope.AttemptID,
		AgentID:            "agent-1",
		FencingToken:       envelope.FencingToken,
		Status:             CallbackSucceeded,
		UsedCost:           "50",
		ContentHash:        "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DeliverableRef:     "agent-artifact://overview/result-1",
		Timestamp:          timestamp,
		Nonce:              envelope.CallbackNonce,
		KeyVersion:         "agent-callback-key-v1",
	}
}

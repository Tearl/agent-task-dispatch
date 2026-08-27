package selection

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/auth"
)

const testPrivateKey = "00000000000000000000000000000000000000000000000000000000000a11ce"

type capacityStub struct {
	mu       sync.Mutex
	next     int64
	leases   map[string]agent.CapacityLease
	released map[string]bool
}

func newCapacityStub() *capacityStub {
	return &capacityStub{leases: make(map[string]agent.CapacityLease), released: make(map[string]bool)}
}

func (stub *capacityStub) ReserveCapacity(_ context.Context, agentID, reservationID string, ttl time.Duration) (agent.CapacityLease, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if value, ok := stub.leases[reservationID]; ok {
		if value.AgentID != agentID {
			return agent.CapacityLease{}, agent.ErrInvalidInput
		}
		return value, nil
	}
	stub.next++
	value := agent.CapacityLease{ReservationID: reservationID, AgentID: agentID, FencingToken: stub.next, ExpiresAt: testNow.Add(ttl)}
	stub.leases[reservationID] = value
	return value, nil
}

func (stub *capacityStub) ReleaseCapacity(_ context.Context, reservationID string, fencingToken int64) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	value, ok := stub.leases[reservationID]
	if !ok {
		return agent.ErrNotFound
	}
	if value.FencingToken != fencingToken {
		return agent.ErrStaleVersion
	}
	stub.released[reservationID] = true
	return nil
}

type chainStub struct {
	mu     sync.Mutex
	result ChainResult
	err    error
}

func (stub *chainStub) VerifySelection(_ context.Context, transactionHash string) (ChainResult, error) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	value := stub.result
	if value.TransactionHash == "" {
		value.TransactionHash = transactionHash
	}
	return value, stub.err
}

var testNow = time.Date(2026, 8, 22, 5, 0, 0, 0, time.UTC)

func TestReserveCreatesSignedNetIntentAndReplaysWithoutNewCapacity(t *testing.T) {
	service, repository, capacity, _ := newTestService(t)
	request := Request{BatchID: digest("batch"), SlotID: digest("slot-1")}
	repository.SetEligibility("publisher-1", eligibility("task-1", request.BatchID, request.SlotID, "agent-1"))

	first, replay, err := service.Reserve(context.Background(), publisherSession(), "select-1", "task-1", request)
	if err != nil || replay {
		t.Fatalf("reserve: replay=%v err=%v", replay, err)
	}
	if first.Reservation.FormalPayable != "90" || first.Reservation.Proof.OverviewCredit != "10" || first.Reservation.Proof.Deadline != uint64(testNow.Add(10*time.Minute).Unix()) || len(first.PlatformSignature) != 132 {
		t.Fatalf("invalid intent: %#v", first)
	}
	second, replay, err := service.Reserve(context.Background(), publisherSession(), "select-1", "task-1", request)
	if err != nil || !replay || second != first || len(capacity.leases) != 1 {
		t.Fatalf("reserve replay changed result: replay=%v err=%v first=%#v second=%#v leases=%d", replay, err, first, second, len(capacity.leases))
	}
}

func TestDeletedTaskCannotReplayReadOrReconcileLiveSelection(t *testing.T) {
	service, repository, _, chain := newTestService(t)
	request := Request{BatchID: digest("deleted-batch"), SlotID: digest("deleted-slot")}
	eligible := eligibility("task-deleted", request.BatchID, request.SlotID, "agent-1")
	repository.SetEligibility("publisher-1", eligible)
	intent, _, err := service.Reserve(context.Background(), publisherSession(), "select-deleted", eligible.TaskID, request)
	if err != nil {
		t.Fatal(err)
	}
	repository.ClearEligibility("publisher-1", eligible)
	if _, replay, reserveErr := service.Reserve(context.Background(), publisherSession(), "select-deleted", eligible.TaskID, request); !errors.Is(reserveErr, ErrInvalidState) || replay {
		t.Fatalf("deleted task replayed a selection: replay=%v err=%v", replay, reserveErr)
	}
	if _, getErr := service.Get(context.Background(), publisherSession(), eligible.TaskID, intent.Reservation.ID); !errors.Is(getErr, ErrInvalidState) {
		t.Fatalf("deleted task exposed a live selection signature: %v", getErr)
	}
	chain.result = ChainResult{Status: ChainConfirmed, Proof: intent.Reservation.Proof, FormalPayable: intent.Reservation.FormalPayable, WorkNonce: 1}
	if _, _, reconcileErr := service.Reconcile(context.Background(), publisherSession(), eligible.TaskID, intent.Reservation.ID, ReconcileRequest{TransactionHash: transaction("deleted")}); !errors.Is(reconcileErr, ErrInvalidState) {
		t.Fatalf("deleted task reconciled a selection: %v", reconcileErr)
	}
}

func TestConcurrentSelectionsCreateOneReservationAndReleaseLoser(t *testing.T) {
	service, repository, capacity, _ := newTestService(t)
	batch := digest("batch")
	firstSlot, secondSlot := digest("slot-1"), digest("slot-2")
	repository.SetEligibility("publisher-1", eligibility("task-1", batch, firstSlot, "agent-1"))
	repository.SetEligibility("publisher-1", eligibility("task-1", batch, secondSlot, "agent-2"))

	type result struct {
		intent Intent
		err    error
	}
	results := make(chan result, 2)
	for index, slot := range []string{firstSlot, secondSlot} {
		go func(index int, slot string) {
			intent, _, err := service.Reserve(context.Background(), publisherSession(), "select-"+string(rune('a'+index)), "task-1", Request{BatchID: batch, SlotID: slot})
			results <- result{intent, err}
		}(index, slot)
	}
	var successes, conflicts int
	for range 2 {
		value := <-results
		if value.err == nil {
			successes++
		} else if errors.Is(value.err, ErrInvalidState) {
			conflicts++
		} else {
			t.Fatalf("unexpected race result: %v", value.err)
		}
	}
	if successes != 1 || conflicts != 1 || len(capacity.released) != 1 {
		t.Fatalf("selection race: successes=%d conflicts=%d releases=%d", successes, conflicts, len(capacity.released))
	}
}

func TestConfirmedSelectionMustMatchProofAndCreatesAssignmentOnce(t *testing.T) {
	service, repository, capacity, chain := newTestService(t)
	request := Request{BatchID: digest("batch"), SlotID: digest("slot")}
	repository.SetEligibility("publisher-1", eligibility("task-1", request.BatchID, request.SlotID, "agent-1"))
	intent, _, err := service.Reserve(context.Background(), publisherSession(), "select-confirm", "task-1", request)
	if err != nil {
		t.Fatal(err)
	}
	txHash := transaction("confirmed")
	chain.result = ChainResult{Status: ChainConfirmed, TransactionHash: txHash, Proof: intent.Reservation.Proof, FormalPayable: "90", WorkNonce: 1}

	confirmed, assignment, err := service.Reconcile(context.Background(), publisherSession(), "task-1", intent.Reservation.ID, ReconcileRequest{TransactionHash: txHash})
	if err != nil || assignment == nil || confirmed.Status != StatusConfirmed || assignment.ID != intent.Reservation.Proof.AssignmentID || !capacity.released[intent.Reservation.ID] {
		t.Fatalf("confirmation failed: reservation=%#v assignment=%#v err=%v", confirmed, assignment, err)
	}
	terminal, err := service.Get(context.Background(), publisherSession(), "task-1", intent.Reservation.ID)
	if err != nil || terminal.PlatformSignature != "" {
		t.Fatalf("terminal reservation exposed a reusable signature: %#v err=%v", terminal, err)
	}
	replayed, replayedAssignment, err := service.Reconcile(context.Background(), publisherSession(), "task-1", intent.Reservation.ID, ReconcileRequest{TransactionHash: txHash})
	if err != nil || replayed.Status != StatusConfirmed || replayedAssignment == nil || *replayedAssignment != *assignment {
		t.Fatalf("confirmation replay failed: %#v %#v %v", replayed, replayedAssignment, err)
	}
}

func TestUnknownSubmittedTransactionIsPersistedBeforeProjectionCatchesUp(t *testing.T) {
	service, repository, _, chain := newTestService(t)
	request := Request{BatchID: digest("batch"), SlotID: digest("slot")}
	repository.SetEligibility("publisher-1", eligibility("task-1", request.BatchID, request.SlotID, "agent-1"))
	intent, _, err := service.Reserve(context.Background(), publisherSession(), "select-pending", "task-1", request)
	if err != nil {
		t.Fatal(err)
	}
	chain.err = ErrDependencyPending
	txHash := transaction("pending")
	if _, _, err = service.Reconcile(context.Background(), publisherSession(), "task-1", intent.Reservation.ID, ReconcileRequest{TransactionHash: txHash}); !errors.Is(err, ErrDependencyPending) {
		t.Fatalf("expected pending, got %v", err)
	}
	stored, err := repository.Get(context.Background(), "publisher-1", intent.Reservation.ID)
	if err != nil || stored.Status != StatusSubmitted || stored.TransactionHash != txHash {
		t.Fatalf("submitted transaction was not retained: %#v %v", stored, err)
	}
}

func TestProofMismatchDoesNotCreateAssignmentOrReleaseCapacity(t *testing.T) {
	service, repository, capacity, chain := newTestService(t)
	request := Request{BatchID: digest("batch"), SlotID: digest("slot")}
	repository.SetEligibility("publisher-1", eligibility("task-1", request.BatchID, request.SlotID, "agent-1"))
	intent, _, _ := service.Reserve(context.Background(), publisherSession(), "select-mismatch", "task-1", request)
	wrong := intent.Reservation.Proof
	wrong.AllocationID = bytes32ID("wrong")
	chain.result = ChainResult{Status: ChainConfirmed, Proof: wrong, FormalPayable: "90", WorkNonce: 1}

	_, _, err := service.Reconcile(context.Background(), publisherSession(), "task-1", intent.Reservation.ID, ReconcileRequest{TransactionHash: transaction("wrong")})
	if !errors.Is(err, ErrProofMismatch) || capacity.released[intent.Reservation.ID] {
		t.Fatalf("proof mismatch advanced state: err=%v released=%v", err, capacity.released[intent.Reservation.ID])
	}
}

func TestReservationReadAndReconcileBindPublisherAndTask(t *testing.T) {
	service, repository, _, chain := newTestService(t)
	request := Request{BatchID: digest("batch"), SlotID: digest("slot")}
	repository.SetEligibility("publisher-1", eligibility("task-1", request.BatchID, request.SlotID, "agent-1"))
	intent, _, err := service.Reserve(context.Background(), publisherSession(), "select-boundary", "task-1", request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Get(context.Background(), publisherSession(), "task-2", intent.Reservation.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-task read returned %v", err)
	}
	chain.result = ChainResult{Status: ChainConfirmed, Proof: intent.Reservation.Proof, FormalPayable: "90", WorkNonce: 1}
	if _, _, err = service.Reconcile(context.Background(), publisherSession(), "task-2", intent.Reservation.ID, ReconcileRequest{TransactionHash: transaction("cross-task")}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-task reconciliation returned %v", err)
	}
	unauthorized := auth.Session{UserID: "publisher-1", Wallet: publisherSession().Wallet}
	if _, _, err = service.Reconcile(context.Background(), unauthorized, "task-1", intent.Reservation.ID, ReconcileRequest{TransactionHash: transaction("unauthorized")}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized reconciliation returned %v", err)
	}
}

func TestFailedAndExpiredReservationsReleaseCapacity(t *testing.T) {
	service, repository, capacity, chain := newTestService(t)
	firstRequest := Request{BatchID: digest("batch-1"), SlotID: digest("slot-1")}
	repository.SetEligibility("publisher-1", eligibility("task-1", firstRequest.BatchID, firstRequest.SlotID, "agent-1"))
	first, _, _ := service.Reserve(context.Background(), publisherSession(), "select-failed", "task-1", firstRequest)
	chain.result = ChainResult{Status: ChainFailed, FailureReasonCode: "execution_reverted"}
	failed, assignment, err := service.Reconcile(context.Background(), publisherSession(), "task-1", first.Reservation.ID, ReconcileRequest{TransactionHash: transaction("failed")})
	if err != nil || assignment != nil || failed.Status != StatusFailed || !capacity.released[first.Reservation.ID] {
		t.Fatalf("failed release: %#v %v", failed, err)
	}

	secondRequest := Request{BatchID: digest("batch-2"), SlotID: digest("slot-2")}
	repository.SetEligibility("publisher-1", eligibility("task-2", secondRequest.BatchID, secondRequest.SlotID, "agent-2"))
	second, _, _ := service.Reserve(context.Background(), publisherSession(), "select-expire", "task-2", secondRequest)
	repository.now = func() time.Time { return testNow.Add(11 * time.Minute) }
	expired, changed, err := service.Expire(context.Background(), second.Reservation.ID)
	if err != nil || !changed || expired.Status != StatusExpired || !capacity.released[second.Reservation.ID] {
		t.Fatalf("expire release: %#v changed=%v err=%v", expired, changed, err)
	}
}

func newTestService(t *testing.T) (*Service, *MemoryRepository, *capacityStub, *chainStub) {
	t.Helper()
	repository := NewMemoryRepository()
	repository.now = func() time.Time { return testNow }
	capacity := newCapacityStub()
	chain := &chainStub{result: ChainResult{Status: ChainPending}}
	signer, err := NewEIP712Signer(testPrivateKey, "31337", "0x0000000000000000000000000000000000001234")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(repository, capacity, signer, chain, Config{ChainID: "31337", ContractAddress: "0x0000000000000000000000000000000000001234", ReservationTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return testNow }
	return service, repository, capacity, chain
}

func eligibility(taskID, batchID, slotID, agentID string) Eligibility {
	return Eligibility{TaskID: taskID, TaskDeadline: testNow.Add(time.Hour), SnapshotID: digest("snapshot-" + taskID), TaskSpecHash: digest("spec-" + taskID), MatchRevision: 1, PolicyHash: digest("policy"), BatchID: batchID, SlotID: slotID, AgentID: agentID, ProviderID: "provider-" + agentID, AgentController: "0x000000000000000000000000000000000000beef", Payout: "0x000000000000000000000000000000000000f00d", PriceVersion: 1, QuoteHash: digest("quote-" + agentID), AllocationID: digest("allocation-" + slotID), OverviewPrice: "10", FormalGrossPrice: "100"}
}

func publisherSession() auth.Session {
	return auth.Session{UserID: "publisher-1", Wallet: "0x000000000000000000000000000000000000cafe", Roles: []string{"publisher"}}
}

func digest(value string) string      { return stableDigest(value) }
func transaction(value string) string { return bytes32ID("transaction", value) }

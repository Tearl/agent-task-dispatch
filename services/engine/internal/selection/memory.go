package selection

import (
	"context"
	"sync"
	"time"

	"github.com/example/agent-platform/engine/internal/persistence"
)

type memoryRecord struct {
	reservation Reservation
	key         string
	requestHash string
}

type MemoryRepository struct {
	mu           sync.Mutex
	now          func() time.Time
	eligibility  map[string]Eligibility
	records      map[string]memoryRecord
	requestIndex map[string]string
	activeTasks  map[string]string
	assignments  map[string]Assignment
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		now: time.Now, eligibility: make(map[string]Eligibility), records: make(map[string]memoryRecord),
		requestIndex: make(map[string]string), activeTasks: make(map[string]string), assignments: make(map[string]Assignment),
	}
}

func (repository *MemoryRepository) SetEligibility(publisherID string, value Eligibility) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.eligibility[eligibilityKey(publisherID, value.TaskID, value.BatchID, value.SlotID)] = value
}

func (repository *MemoryRepository) ClearEligibility(publisherID string, value Eligibility) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	delete(repository.eligibility, eligibilityKey(publisherID, value.TaskID, value.BatchID, value.SlotID))
}

func (repository *MemoryRepository) Replay(_ context.Context, publisherID, key, requestHash string) (Reservation, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	id, ok := repository.requestIndex[publisherID+"\x00"+key]
	if !ok {
		return Reservation{}, false, nil
	}
	record := repository.records[id]
	if record.requestHash != requestHash {
		return Reservation{}, false, persistence.ErrIdempotencyConflict
	}
	return record.reservation, true, nil
}

func (repository *MemoryRepository) Eligibility(_ context.Context, publisherID, taskID, batchID, slotID string) (Eligibility, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	value, ok := repository.eligibility[eligibilityKey(publisherID, taskID, batchID, slotID)]
	if !ok {
		return Eligibility{}, ErrInvalidState
	}
	return value, nil
}

func (repository *MemoryRepository) Prepare(_ context.Context, mutation Mutation, draft Reservation) (Reservation, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	eligible, ok := repository.eligibility[eligibilityKey(mutation.PublisherID, draft.TaskID, draft.BatchID, draft.SlotID)]
	if !ok {
		return Reservation{}, false, ErrInvalidState
	}
	requestKey := mutation.PublisherID + "\x00" + mutation.IdempotencyKey
	if id, ok := repository.requestIndex[requestKey]; ok {
		record := repository.records[id]
		if record.requestHash != mutation.RequestHash {
			return Reservation{}, false, persistence.ErrIdempotencyConflict
		}
		return record.reservation, true, nil
	}
	if !reservationMatchesEligibility(draft, eligible) {
		return Reservation{}, false, ErrContentConflict
	}
	if _, exists := repository.activeTasks[draft.TaskID]; exists {
		return Reservation{}, false, ErrInvalidState
	}
	if _, exists := repository.records[draft.ID]; exists {
		return Reservation{}, false, ErrContentConflict
	}
	now := mutation.Now.UTC()
	draft.CreatedAt, draft.UpdatedAt, draft.Status = now, now, StatusReserved
	repository.records[draft.ID] = memoryRecord{reservation: draft, key: mutation.IdempotencyKey, requestHash: mutation.RequestHash}
	repository.requestIndex[requestKey] = draft.ID
	repository.activeTasks[draft.TaskID] = draft.ID
	return draft, false, nil
}

func (repository *MemoryRepository) Get(_ context.Context, publisherID, reservationID string) (Reservation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, ok := repository.records[reservationID]
	if !ok || record.reservation.PublisherID != publisherID {
		return Reservation{}, ErrNotFound
	}
	return record.reservation, nil
}

func (repository *MemoryRepository) RecordSubmitted(_ context.Context, reservationID, transactionHash string) (Reservation, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, ok := repository.records[reservationID]
	if !ok {
		return Reservation{}, ErrNotFound
	}
	value := record.reservation
	if value.Status == StatusSubmitted && value.TransactionHash == transactionHash {
		return value, nil
	}
	if value.Status != StatusReserved || value.TransactionHash != "" && value.TransactionHash != transactionHash {
		return Reservation{}, ErrInvalidState
	}
	value.Status, value.TransactionHash, value.UpdatedAt = StatusSubmitted, transactionHash, repository.now().UTC()
	record.reservation = value
	repository.records[reservationID] = record
	return value, nil
}

func (repository *MemoryRepository) Confirm(_ context.Context, reservationID string, result ChainResult) (Reservation, Assignment, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, ok := repository.records[reservationID]
	if !ok {
		return Reservation{}, Assignment{}, false, ErrNotFound
	}
	value := record.reservation
	if value.Status == StatusConfirmed {
		assignment := repository.assignments[value.Proof.AssignmentID]
		if assignment.TransactionHash != result.TransactionHash {
			return Reservation{}, Assignment{}, false, ErrContentConflict
		}
		return value, assignment, true, nil
	}
	if (value.Status != StatusReserved && value.Status != StatusSubmitted) || value.TransactionHash != "" && value.TransactionHash != result.TransactionHash || !sameProof(value.Proof, result.Proof) || result.FormalPayable != value.FormalPayable || result.WorkNonce != 1 {
		return Reservation{}, Assignment{}, false, ErrProofMismatch
	}
	if repository.now().UTC().After(time.Unix(int64(value.Proof.Deadline), 0)) {
		return Reservation{}, Assignment{}, false, ErrInvalidState
	}
	if _, exists := repository.assignments[value.Proof.AssignmentID]; exists {
		return Reservation{}, Assignment{}, false, ErrContentConflict
	}
	now := repository.now().UTC()
	assignment := Assignment{ID: value.Proof.AssignmentID, TaskID: value.TaskID, ReservationID: value.ID, AgentID: value.AgentID, ProviderID: value.ProviderID, FormalPayable: value.FormalPayable, OverviewCredit: value.Proof.OverviewCredit, WorkNonce: 1, TransactionHash: result.TransactionHash, ConfirmedAt: now}
	value.Status, value.TransactionHash, value.UpdatedAt = StatusConfirmed, result.TransactionHash, now
	record.reservation = value
	repository.records[reservationID] = record
	repository.assignments[assignment.ID] = assignment
	return value, assignment, false, nil
}

func (repository *MemoryRepository) Fail(_ context.Context, reservationID, transactionHash, reason string) (Reservation, bool, error) {
	return repository.terminate(reservationID, transactionHash, reason, StatusFailed, false)
}

func (repository *MemoryRepository) Expire(_ context.Context, reservationID string) (Reservation, bool, error) {
	return repository.terminate(reservationID, "", "reservation_expired", StatusExpired, true)
}

func (repository *MemoryRepository) terminate(reservationID, transactionHash, reason, status string, requireExpiry bool) (Reservation, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	record, ok := repository.records[reservationID]
	if !ok {
		return Reservation{}, false, ErrNotFound
	}
	value := record.reservation
	if value.Status == status {
		return value, false, nil
	}
	if value.Status != StatusReserved && value.Status != StatusSubmitted {
		return Reservation{}, false, ErrInvalidState
	}
	if value.TransactionHash != "" && transactionHash != "" && value.TransactionHash != transactionHash {
		return Reservation{}, false, ErrContentConflict
	}
	if requireExpiry && repository.now().UTC().Before(time.Unix(int64(value.Proof.Deadline), 0)) {
		return Reservation{}, false, ErrInvalidState
	}
	value.Status, value.FailureReasonCode, value.UpdatedAt = status, reason, repository.now().UTC()
	if transactionHash != "" {
		value.TransactionHash = transactionHash
	}
	record.reservation = value
	repository.records[reservationID] = record
	delete(repository.activeTasks, value.TaskID)
	return value, true, nil
}

func eligibilityKey(publisherID, taskID, batchID, slotID string) string {
	return publisherID + "\x00" + taskID + "\x00" + batchID + "\x00" + slotID
}

func reservationMatchesEligibility(value Reservation, eligible Eligibility) bool {
	return value.TaskID == eligible.TaskID && value.BatchID == eligible.BatchID && value.SlotID == eligible.SlotID && value.SnapshotID == eligible.SnapshotID && value.AgentID == eligible.AgentID && value.ProviderID == eligible.ProviderID && value.Proof.AgentController == eligible.AgentController && value.Proof.Payout == eligible.Payout && value.Proof.MatchRevision == eligible.MatchRevision && value.Proof.PriceVersion == eligible.PriceVersion && value.Proof.OverviewPrice == eligible.OverviewPrice && value.Proof.FormalGrossPrice == eligible.FormalGrossPrice && value.Proof.OverviewCredit == eligible.OverviewPrice && value.Proof.QuoteHash == digestToBytes32(eligible.QuoteHash) && value.Proof.TaskSpecHash == digestToBytes32(eligible.TaskSpecHash) && value.Proof.PolicyHash == digestToBytes32(eligible.PolicyHash) && value.Proof.AllocationID == digestToBytes32(eligible.AllocationID) && value.Proof.OverviewID == digestToBytes32(eligible.SlotID)
}

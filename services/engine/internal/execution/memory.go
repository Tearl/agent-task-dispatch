package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
)

type callbackRecord struct {
	payloadHash string
	result      CallbackResult
}

type MemoryRepository struct {
	mu          sync.Mutex
	now         func() time.Time
	executions  map[string]Execution
	attempts    map[string][]Attempt
	idempotency map[string]string
	callbacks   map[string]callbackRecord
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		now:         func() time.Time { return time.Now().UTC() },
		executions:  make(map[string]Execution),
		attempts:    make(map[string][]Attempt),
		idempotency: make(map[string]string),
		callbacks:   make(map[string]callbackRecord),
	}
}

func (repository *MemoryRepository) GetOrCreate(_ context.Context, spec Spec) (Execution, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existingID, ok := repository.idempotency[spec.IdempotencyKey]; ok && existingID != spec.LogicalExecutionID {
		return Execution{}, false, ErrContentConflict
	}
	if existing, ok := repository.executions[spec.LogicalExecutionID]; ok {
		if !reflect.DeepEqual(existing.Spec, spec) {
			return Execution{}, false, ErrContentConflict
		}
		return cloneExecution(existing), true, nil
	}
	now := repository.now()
	created := Execution{Spec: spec, Status: ExecutionPending, UsedCost: "0", CreatedAt: now, UpdatedAt: now}
	repository.executions[spec.LogicalExecutionID] = cloneExecution(created)
	repository.idempotency[spec.IdempotencyKey] = spec.LogicalExecutionID
	return cloneExecution(created), false, nil
}

func (repository *MemoryRepository) Get(_ context.Context, logicalExecutionID string) (Execution, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	value, ok := repository.executions[logicalExecutionID]
	if !ok {
		return Execution{}, ErrNotFound
	}
	return cloneExecution(value), nil
}

func (repository *MemoryRepository) CurrentAttempt(_ context.Context, logicalExecutionID string) (Attempt, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	items := repository.attempts[logicalExecutionID]
	if len(items) == 0 {
		return Attempt{}, ErrNotFound
	}
	return items[len(items)-1], nil
}

func (repository *MemoryRepository) PrepareAttempt(_ context.Context, logicalExecutionID string, ttl time.Duration) (Execution, Attempt, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	execution, ok := repository.executions[logicalExecutionID]
	if !ok {
		return Execution{}, Attempt{}, false, ErrNotFound
	}
	if execution.Status == ExecutionSucceeded || execution.Status == ExecutionCancelled || execution.Status == ExecutionCancelRequested || execution.Status == ExecutionCostStopped {
		return Execution{}, Attempt{}, false, ErrInvalidState
	}
	now := repository.now()
	items := repository.attempts[logicalExecutionID]
	if len(items) > 0 {
		current := items[len(items)-1]
		if current.Status == AttemptPrepared || current.Status == AttemptActive && current.LeaseExpiresAt.After(now) {
			return cloneExecution(execution), current, true, nil
		}
		if current.Status == AttemptActive && !current.LeaseExpiresAt.After(now) {
			terminal := now
			current.Status = AttemptExpired
			current.TerminalAt = &terminal
			current.UpdatedAt = now
			items[len(items)-1] = current
			repository.attempts[logicalExecutionID] = items
		}
	}
	number := len(items) + 1
	attemptID := fmt.Sprintf("%s:attempt:%d", logicalExecutionID, number)
	attempt := Attempt{LogicalExecutionID: logicalExecutionID, Number: number, AttemptID: attemptID, ReservationID: attemptID, Status: AttemptPrepared, CreatedAt: now, UpdatedAt: now}
	repository.attempts[logicalExecutionID] = append(items, attempt)
	execution.Status = ExecutionPending
	execution.CurrentAttempt = number
	execution.UpdatedAt = now
	repository.executions[logicalExecutionID] = execution
	_ = ttl
	return cloneExecution(execution), attempt, false, nil
}

func (repository *MemoryRepository) ActivateAttempt(_ context.Context, logicalExecutionID string, number int, lease agent.CapacityLease, nonceHash, nonceKeyVersion string) (Execution, Attempt, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	execution, attempt, index, err := repository.lockedAttempt(logicalExecutionID, number)
	if err != nil {
		return Execution{}, Attempt{}, err
	}
	if lease.AgentID != execution.Spec.AgentID || lease.ReservationID != attempt.ReservationID || lease.FencingToken < 1 || !lease.ExpiresAt.After(repository.now()) || !validDigest(nonceHash) || nonceKeyVersion == "" {
		return Execution{}, Attempt{}, ErrInvalidInput
	}
	if attempt.Status == AttemptActive {
		if attempt.FencingToken != lease.FencingToken || attempt.CallbackNonceHash != nonceHash || attempt.NonceKeyVersion != nonceKeyVersion {
			return Execution{}, Attempt{}, ErrStaleFence
		}
		return cloneExecution(execution), attempt, nil
	}
	if attempt.Status != AttemptPrepared {
		return Execution{}, Attempt{}, ErrInvalidState
	}
	now := repository.now()
	attempt.Status = AttemptActive
	attempt.FencingToken = lease.FencingToken
	attempt.LeaseExpiresAt = lease.ExpiresAt
	attempt.CallbackNonceHash = nonceHash
	attempt.NonceKeyVersion = nonceKeyVersion
	attempt.UpdatedAt = now
	repository.attempts[logicalExecutionID][index] = attempt
	execution.Status = ExecutionRunning
	execution.UpdatedAt = now
	repository.executions[logicalExecutionID] = execution
	return cloneExecution(execution), attempt, nil
}

func (repository *MemoryRepository) RecordDispatch(_ context.Context, logicalExecutionID string, number int) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	_, attempt, index, err := repository.lockedAttempt(logicalExecutionID, number)
	if err != nil {
		return err
	}
	if attempt.Status != AttemptActive {
		return ErrInvalidState
	}
	attempt.DispatchCount++
	attempt.UpdatedAt = repository.now()
	repository.attempts[logicalExecutionID][index] = attempt
	return nil
}

func (repository *MemoryRepository) FailAttempt(_ context.Context, logicalExecutionID string, number int, fencingToken int64, _ string) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	execution, attempt, index, err := repository.lockedAttempt(logicalExecutionID, number)
	if err != nil {
		return err
	}
	if attempt.FencingToken != fencingToken {
		return ErrStaleFence
	}
	if attempt.Status == AttemptFailed {
		return nil
	}
	if attempt.Status != AttemptActive {
		return ErrInvalidState
	}
	now := repository.now()
	attempt.Status = AttemptFailed
	attempt.UpdatedAt = now
	attempt.TerminalAt = &now
	repository.attempts[logicalExecutionID][index] = attempt
	execution.Status = ExecutionFailed
	execution.UpdatedAt = now
	repository.executions[logicalExecutionID] = execution
	return nil
}

func (repository *MemoryRepository) RequestCancel(_ context.Context, logicalExecutionID string) (Execution, Attempt, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	execution, ok := repository.executions[logicalExecutionID]
	if !ok {
		return Execution{}, Attempt{}, false, ErrNotFound
	}
	if execution.Status == ExecutionSucceeded || execution.Status == ExecutionCostStopped {
		return Execution{}, Attempt{}, false, ErrInvalidState
	}
	items := repository.attempts[logicalExecutionID]
	var attempt Attempt
	if len(items) > 0 {
		attempt = items[len(items)-1]
	}
	if execution.Status == ExecutionCancelled {
		return cloneExecution(execution), attempt, true, nil
	}
	replay := execution.Status == ExecutionCancelRequested
	execution.Status = ExecutionCancelRequested
	execution.UpdatedAt = repository.now()
	repository.executions[logicalExecutionID] = execution
	return cloneExecution(execution), attempt, replay, nil
}

func (repository *MemoryRepository) CompleteCancel(_ context.Context, logicalExecutionID string, number int, fencingToken int64) (Execution, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	execution, ok := repository.executions[logicalExecutionID]
	if !ok {
		return Execution{}, ErrNotFound
	}
	if execution.Status == ExecutionCancelled {
		return cloneExecution(execution), nil
	}
	if execution.Status != ExecutionCancelRequested {
		return Execution{}, ErrInvalidState
	}
	now := repository.now()
	if number > 0 {
		_, attempt, index, err := repository.lockedAttempt(logicalExecutionID, number)
		if err != nil {
			return Execution{}, err
		}
		if execution.CurrentAttempt != number || attempt.FencingToken != fencingToken {
			return Execution{}, ErrStaleFence
		}
		if attempt.Status != AttemptActive && attempt.Status != AttemptPrepared && attempt.Status != AttemptCancelled {
			return Execution{}, ErrInvalidState
		}
		attempt.Status = AttemptCancelled
		attempt.UpdatedAt = now
		attempt.TerminalAt = &now
		repository.attempts[logicalExecutionID][index] = attempt
	}
	execution.Status = ExecutionCancelled
	execution.UpdatedAt = now
	execution.CancelledAt = &now
	repository.executions[logicalExecutionID] = execution
	return cloneExecution(execution), nil
}

func (repository *MemoryRepository) RecordUsage(_ context.Context, logicalExecutionID string, number int, fencingToken int64, usedCost string) (Execution, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	execution, attempt, index, err := repository.lockedAttempt(logicalExecutionID, number)
	if err != nil {
		return Execution{}, false, err
	}
	if invalidMoney(usedCost) {
		return Execution{}, false, ErrInvalidInput
	}
	if execution.CurrentAttempt != number || attempt.FencingToken != fencingToken || attempt.Status != AttemptActive {
		return Execution{}, false, ErrStaleFence
	}
	if compareMoney(usedCost, execution.UsedCost) < 0 {
		return Execution{}, false, ErrInvalidInput
	}
	now := repository.now()
	execution.UsedCost = usedCost
	execution.UpdatedAt = now
	shouldStop := compareMoney(usedCost, execution.Spec.CostCap) >= 0
	if shouldStop {
		execution.UsedCost = execution.Spec.CostCap
		execution.Status = ExecutionCostStopped
		attempt.Status = AttemptFailed
		attempt.UpdatedAt = now
		attempt.TerminalAt = &now
		repository.attempts[logicalExecutionID][index] = attempt
	}
	repository.executions[logicalExecutionID] = execution
	return cloneExecution(execution), shouldStop, nil
}

func (repository *MemoryRepository) ApplyCallback(_ context.Context, verified VerifiedCallback) (CallbackResult, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if previous, ok := repository.callbacks[verified.NonceHash]; ok {
		if previous.payloadHash != verified.PayloadHash {
			return CallbackResult{}, ErrContentConflict
		}
		result := previous.result
		result.Replay = true
		return cloneCallbackResult(result), nil
	}
	callback := verified.Callback
	execution, ok := repository.executions[callback.LogicalExecutionID]
	if !ok {
		return CallbackResult{}, ErrNotFound
	}
	items := repository.attempts[callback.LogicalExecutionID]
	attemptIndex := -1
	for index := range items {
		if items[index].AttemptID == callback.AttemptID {
			attemptIndex = index
			break
		}
	}
	if attemptIndex < 0 || callback.AgentID != execution.Spec.AgentID {
		return CallbackResult{}, ErrInvalidCallback
	}
	attempt := items[attemptIndex]
	if attempt.CallbackNonceHash != verified.NonceHash {
		return CallbackResult{}, ErrCallbackReplay
	}
	result := CallbackResult{Execution: cloneExecution(execution), Outcome: CallbackAccepted}
	if attempt.FencingToken != callback.FencingToken || attemptIndex != len(items)-1 || !attempt.LeaseExpiresAt.After(repository.now()) {
		result.Outcome = CallbackStaleFence
		repository.callbacks[verified.NonceHash] = callbackRecord{payloadHash: verified.PayloadHash, result: cloneCallbackResult(result)}
		return result, nil
	}
	if execution.Status == ExecutionCancelRequested || execution.Status == ExecutionCancelled || execution.Status == ExecutionCostStopped || attempt.Status != AttemptActive {
		result.Outcome = CallbackLate
		repository.callbacks[verified.NonceHash] = callbackRecord{payloadHash: verified.PayloadHash, result: cloneCallbackResult(result)}
		return result, nil
	}
	if compareMoney(callback.UsedCost, execution.UsedCost) < 0 {
		return CallbackResult{}, ErrInvalidCallback
	}
	now := repository.now()
	execution.UsedCost = callback.UsedCost
	execution.UpdatedAt = now
	attempt.UpdatedAt = now
	attempt.TerminalAt = &now
	if compareMoney(callback.UsedCost, execution.Spec.CostCap) > 0 {
		execution.UsedCost = execution.Spec.CostCap
		execution.Status = ExecutionCostStopped
		attempt.Status = AttemptFailed
		result.Outcome = CallbackCostStop
		result.ShouldCancel = true
	} else if callback.Status == CallbackSucceeded {
		if execution.ContentHash != "" && execution.ContentHash != callback.ContentHash {
			return CallbackResult{}, ErrContentConflict
		}
		execution.Status = ExecutionSucceeded
		execution.ContentHash = callback.ContentHash
		execution.DeliverableRef = callback.DeliverableRef
		attempt.Status = AttemptCompleted
	} else {
		execution.Status = ExecutionFailed
		attempt.Status = AttemptFailed
	}
	repository.executions[callback.LogicalExecutionID] = execution
	items[attemptIndex] = attempt
	repository.attempts[callback.LogicalExecutionID] = items
	result.Execution = cloneExecution(execution)
	repository.callbacks[verified.NonceHash] = callbackRecord{payloadHash: verified.PayloadHash, result: cloneCallbackResult(result)}
	return result, nil
}

func (repository *MemoryRepository) lockedAttempt(logicalExecutionID string, number int) (Execution, Attempt, int, error) {
	execution, ok := repository.executions[logicalExecutionID]
	if !ok {
		return Execution{}, Attempt{}, -1, ErrNotFound
	}
	items := repository.attempts[logicalExecutionID]
	if number < 1 || number > len(items) || execution.CurrentAttempt != number {
		return Execution{}, Attempt{}, -1, ErrStaleFence
	}
	return execution, items[number-1], number - 1, nil
}

func cloneExecution(source Execution) Execution {
	encoded, err := json.Marshal(source)
	if err != nil {
		panic(fmt.Sprintf("clone execution: %v", err))
	}
	var result Execution
	if err = json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("clone execution: %v", err))
	}
	return result
}

func cloneCallbackResult(source CallbackResult) CallbackResult {
	encoded, err := json.Marshal(source)
	if err != nil {
		panic(fmt.Sprintf("clone callback result: %v", err))
	}
	var result CallbackResult
	if err = json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Sprintf("clone callback result: %v", err))
	}
	return result
}

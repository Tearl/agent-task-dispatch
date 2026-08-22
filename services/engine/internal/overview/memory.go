package overview

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu      sync.Mutex
	now     func() time.Time
	batches map[string]Batch
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{now: func() time.Time { return time.Now().UTC() }, batches: make(map[string]Batch)}
}

func (repository *MemoryRepository) GetOrCreate(_ context.Context, draft Batch) (Batch, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, ok := repository.batches[draft.ID]; ok {
		if !sameBatchPlan(existing, draft) {
			return Batch{}, false, ErrContentConflict
		}
		return cloneBatch(existing), true, nil
	}
	for _, batch := range repository.batches {
		if batch.SnapshotID == draft.SnapshotID {
			return Batch{}, false, ErrContentConflict
		}
	}
	repository.batches[draft.ID] = cloneBatch(draft)
	return cloneBatch(draft), false, nil
}

func (repository *MemoryRepository) Get(_ context.Context, batchID string) (Batch, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	batch, ok := repository.batches[batchID]
	if !ok {
		return Batch{}, ErrNotFound
	}
	return cloneBatch(batch), nil
}

func (repository *MemoryRepository) RecordDispatched(_ context.Context, batchID, slotID string) (Batch, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	batch, slot, index, err := repository.slot(batchID, slotID)
	if err != nil {
		return Batch{}, err
	}
	if batch.Status == BatchObsolete {
		return Batch{}, ErrObsolete
	}
	if slot.Status == SlotDispatched {
		return cloneBatch(batch), nil
	}
	if slot.Status != SlotPlanned {
		return Batch{}, ErrInvalidState
	}
	now := repository.now()
	slot.Status = SlotDispatched
	slot.UpdatedAt = now
	batch.Slots[index] = slot
	batch.UpdatedAt = now
	repository.batches[batchID] = batch
	return cloneBatch(batch), nil
}

func (repository *MemoryRepository) RecordValidation(_ context.Context, batchID, slotID string, validation Validation, contentHash, deliverableRef string) (Batch, Slot, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	batch, slot, index, err := repository.slot(batchID, slotID)
	if err != nil {
		return Batch{}, Slot{}, false, err
	}
	if batch.Status == BatchObsolete {
		return Batch{}, Slot{}, false, ErrObsolete
	}
	if slot.Status == SlotValid || slot.Status == SlotInvalid {
		if slot.ContentHash != contentHash || slot.DeliverableRef != deliverableRef {
			return Batch{}, Slot{}, false, ErrContentConflict
		}
		return cloneBatch(batch), slot, true, nil
	}
	if slot.Status != SlotDispatched {
		return Batch{}, Slot{}, false, ErrInvalidState
	}
	if validation.Valid {
		for otherIndex, other := range batch.Slots {
			if otherIndex != index && other.Status == SlotValid && other.ContentHash == contentHash {
				validation = Validation{Valid: false, Codes: []string{"duplicate_content"}}
				break
			}
		}
	}
	now := repository.now()
	slot.Validation = cloneValidation(validation)
	slot.ContentHash = contentHash
	slot.DeliverableRef = deliverableRef
	slot.UpdatedAt = now
	if validation.Valid {
		slot.Status = SlotValid
	} else {
		slot.Status = SlotInvalid
	}
	batch.Slots[index] = slot
	batch.UpdatedAt = now
	repository.finish(&batch)
	repository.batches[batchID] = batch
	return cloneBatch(batch), slot, false, nil
}

func (repository *MemoryRepository) RecordBilling(_ context.Context, batchID, slotID, status string) (Batch, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	batch, slot, index, err := repository.slot(batchID, slotID)
	if err != nil {
		return Batch{}, err
	}
	if status != BillingCaptured && status != BillingReleased {
		return Batch{}, ErrInvalidInput
	}
	if slot.BillingStatus == status {
		return cloneBatch(batch), nil
	}
	releaseAllowed := slot.Status == SlotInvalid || slot.Status == SlotFailed || slot.Status == SlotObsolete || batch.Status == BatchObsolete && slot.Status == SlotValid
	if slot.BillingStatus != BillingAuthorized || status == BillingCaptured && slot.Status != SlotValid || status == BillingReleased && !releaseAllowed {
		return Batch{}, ErrInvalidState
	}
	now := repository.now()
	slot.BillingStatus = status
	slot.UpdatedAt = now
	batch.Slots[index] = slot
	batch.UpdatedAt = now
	repository.finish(&batch)
	repository.batches[batchID] = batch
	return cloneBatch(batch), nil
}

func (repository *MemoryRepository) AddReplacement(_ context.Context, batchID string, replacement Slot) (Batch, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	batch, ok := repository.batches[batchID]
	if !ok {
		return Batch{}, false, ErrNotFound
	}
	if batch.Status == BatchObsolete {
		return Batch{}, false, ErrObsolete
	}
	for _, slot := range batch.Slots {
		if slot.Replacement {
			if !sameSlotPlan(slot, replacement) {
				return Batch{}, false, ErrContentConflict
			}
			return cloneBatch(batch), true, nil
		}
	}
	if batch.ReplacementUsed || batch.ReplacementExhausted || !replacement.Replacement || replacement.Ordinal != len(batch.Slots)+1 || replacement.BatchID != batchID {
		return Batch{}, false, ErrInvalidState
	}
	batch.ReplacementUsed = true
	batch.Slots = append(batch.Slots, replacement)
	batch.UpdatedAt = repository.now()
	repository.batches[batchID] = batch
	return cloneBatch(batch), false, nil
}

func (repository *MemoryRepository) ExhaustReplacement(_ context.Context, batchID string) (Batch, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	batch, ok := repository.batches[batchID]
	if !ok {
		return Batch{}, ErrNotFound
	}
	if batch.ReplacementUsed || batch.ReplacementExhausted {
		return cloneBatch(batch), nil
	}
	batch.ReplacementExhausted = true
	batch.UpdatedAt = repository.now()
	repository.finish(&batch)
	repository.batches[batchID] = batch
	return cloneBatch(batch), nil
}

func (repository *MemoryRepository) MarkObsoleteBefore(_ context.Context, taskID string, revision int) ([]Batch, error) {
	if taskID == "" || revision < 1 {
		return nil, ErrInvalidInput
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	changed := make([]Batch, 0)
	now := repository.now()
	for id, batch := range repository.batches {
		if batch.TaskID != taskID || batch.MatchRevision >= revision || batch.Status == BatchObsolete {
			continue
		}
		batch.Status = BatchObsolete
		batch.UpdatedAt = now
		for index, slot := range batch.Slots {
			if slot.Status != SlotValid && slot.Status != SlotInvalid && slot.Status != SlotFailed {
				slot.Status = SlotObsolete
				slot.UpdatedAt = now
				batch.Slots[index] = slot
			}
		}
		repository.batches[id] = batch
		changed = append(changed, cloneBatch(batch))
	}
	return changed, nil
}

func (repository *MemoryRepository) slot(batchID, slotID string) (Batch, Slot, int, error) {
	batch, ok := repository.batches[batchID]
	if !ok {
		return Batch{}, Slot{}, -1, ErrNotFound
	}
	for index, slot := range batch.Slots {
		if slot.ID == slotID {
			return batch, slot, index, nil
		}
	}
	return Batch{}, Slot{}, -1, ErrNotFound
}

func (repository *MemoryRepository) finish(batch *Batch) {
	if batch.Status == BatchObsolete {
		return
	}
	hasInvalid := false
	for _, slot := range batch.Slots {
		if slot.Status == SlotPlanned || slot.Status == SlotDispatched || slot.BillingStatus == BillingAuthorized {
			return
		}
		hasInvalid = hasInvalid || slot.Status == SlotInvalid || slot.Status == SlotFailed
	}
	if !hasInvalid || batch.ReplacementUsed || batch.ReplacementExhausted {
		batch.Status = BatchCompleted
	}
}

func sameBatchPlan(left, right Batch) bool {
	if left.ID != right.ID || left.SnapshotID != right.SnapshotID || left.TaskID != right.TaskID || left.TaskSpecHash != right.TaskSpecHash || left.MatchRevision != right.MatchRevision || left.AlgorithmVersion != right.AlgorithmVersion || left.BriefRef != right.BriefRef || left.BriefHash != right.BriefHash || !left.Deadline.Equal(right.Deadline) || len(left.Slots) != len(right.Slots) {
		return false
	}
	for index := range left.Slots {
		leftSlot, rightSlot := left.Slots[index], right.Slots[index]
		leftSlot.Status, rightSlot.Status = "", ""
		leftSlot.BillingStatus, rightSlot.BillingStatus = "", ""
		leftSlot.Validation, rightSlot.Validation = Validation{}, Validation{}
		leftSlot.ContentHash, rightSlot.ContentHash = "", ""
		leftSlot.DeliverableRef, rightSlot.DeliverableRef = "", ""
		leftSlot.CreatedAt, rightSlot.CreatedAt = time.Time{}, time.Time{}
		leftSlot.UpdatedAt, rightSlot.UpdatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(leftSlot, rightSlot) {
			return false
		}
	}
	return true
}

func sameSlotPlan(left, right Slot) bool {
	return left.ID == right.ID && left.BatchID == right.BatchID && left.Ordinal == right.Ordinal && left.SourcePosition == right.SourcePosition && left.Replacement == right.Replacement && left.AgentID == right.AgentID && left.ProviderID == right.ProviderID && left.PriceVersion == right.PriceVersion && left.QuoteHash == right.QuoteHash && left.OverviewPrice == right.OverviewPrice && left.ExternalCostCap == right.ExternalCostCap && left.AllocationID == right.AllocationID && left.LogicalExecutionID == right.LogicalExecutionID
}

func cloneBatch(source Batch) Batch {
	copy := source
	copy.Slots = slices.Clone(source.Slots)
	for index := range copy.Slots {
		copy.Slots[index].Validation = cloneValidation(copy.Slots[index].Validation)
	}
	return copy
}

func cloneValidation(source Validation) Validation {
	return Validation{Valid: source.Valid, Codes: slices.Clone(source.Codes)}
}

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/example/agent-platform/engine/internal/overview"
)

type Store struct{ db *sql.DB }

var errNoChange = errors.New("overview mutation replay")

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (store *Store) GetOrCreate(ctx context.Context, draft overview.Batch) (overview.Batch, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return overview.Batch{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lock(ctx, tx, "overview-batch:"+draft.ID, "overview-snapshot:"+draft.SnapshotID); err != nil {
		return overview.Batch{}, false, err
	}
	existing, err := loadBatchWhere(ctx, tx, `batch_id=$1 OR snapshot_id=$2 ORDER BY batch_id=$1 DESC LIMIT 1`, draft.ID, draft.SnapshotID)
	if err == nil {
		if !samePlan(existing, draft) {
			return overview.Batch{}, false, overview.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return overview.Batch{}, false, err
		}
		return existing, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return overview.Batch{}, false, err
	}
	var databaseNow time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
		return overview.Batch{}, false, err
	}
	if !draft.Deadline.After(databaseNow) {
		return overview.Batch{}, false, overview.ErrInvalidInput
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO overview_batches (batch_id,snapshot_id,task_id,task_spec_hash,match_revision,algorithm_version,orchestration_version,replacement_version,brief_ref,brief_hash,deadline,status,replacement_used,replacement_exhausted,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'running',false,false,$12,$12)`, draft.ID, draft.SnapshotID, draft.TaskID, draft.TaskSpecHash, draft.MatchRevision, draft.AlgorithmVersion, overview.OrchestrationVersion, overview.ReplacementVersion, draft.BriefRef, draft.BriefHash, draft.Deadline, databaseNow)
	if err != nil {
		return overview.Batch{}, false, fmt.Errorf("insert overview batch: %w", err)
	}
	created := draft
	created.Status = overview.BatchRunning
	created.ReplacementUsed = false
	created.ReplacementExhausted = false
	created.CreatedAt = databaseNow
	created.UpdatedAt = databaseNow
	for index := range created.Slots {
		created.Slots[index].CreatedAt = databaseNow
		created.Slots[index].UpdatedAt = databaseNow
		if err = insertSlot(ctx, tx, created.Slots[index]); err != nil {
			return overview.Batch{}, false, err
		}
	}
	if err = insertEvent(ctx, tx, created.ID, "", "overview.batch_created", map[string]any{"snapshotId": created.SnapshotID, "matchRevision": created.MatchRevision, "slotCount": len(created.Slots)}, databaseNow); err != nil {
		return overview.Batch{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return overview.Batch{}, false, err
	}
	return created, false, nil
}

func (store *Store) Get(ctx context.Context, batchID string) (overview.Batch, error) {
	batch, err := loadBatchWhere(ctx, store.db, `batch_id=$1`, batchID)
	if errors.Is(err, sql.ErrNoRows) {
		return overview.Batch{}, overview.ErrNotFound
	}
	return batch, err
}

func (store *Store) RecordDispatched(ctx context.Context, batchID, slotID string) (overview.Batch, error) {
	return store.mutateSlot(ctx, batchID, slotID, func(tx *sql.Tx, batch overview.Batch, slot overview.Slot, now time.Time) error {
		if batch.Status == overview.BatchObsolete {
			return overview.ErrObsolete
		}
		if slot.Status == overview.SlotDispatched {
			return errNoChange
		}
		if slot.Status != overview.SlotPlanned {
			return overview.ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `UPDATE overview_slots SET status='dispatched',updated_at=$1 WHERE slot_id=$2`, now, slotID); err != nil {
			return err
		}
		return insertEvent(ctx, tx, batchID, slotID, "overview.slot_dispatched", map[string]any{"logicalExecutionId": slot.LogicalExecutionID}, now)
	})
}

func (store *Store) RecordValidation(ctx context.Context, batchID, slotID string, validation overview.Validation, contentHash, deliverableRef string) (overview.Batch, overview.Slot, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	batch, _, err := lockBatch(ctx, tx, batchID)
	if err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	if batch.Status == overview.BatchObsolete {
		return overview.Batch{}, overview.Slot{}, false, overview.ErrObsolete
	}
	slot, err := scanSlot(tx.QueryRowContext(ctx, slotSelect+` WHERE slot_id=$1 AND batch_id=$2 FOR UPDATE`, slotID, batchID))
	if errors.Is(err, sql.ErrNoRows) {
		return overview.Batch{}, overview.Slot{}, false, overview.ErrNotFound
	}
	if err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	if slot.Status == overview.SlotValid || slot.Status == overview.SlotInvalid {
		if slot.ContentHash != contentHash || slot.DeliverableRef != deliverableRef {
			return overview.Batch{}, overview.Slot{}, false, overview.ErrContentConflict
		}
		if err = tx.Commit(); err != nil {
			return overview.Batch{}, overview.Slot{}, false, err
		}
		return batch, slot, true, nil
	}
	if slot.Status != overview.SlotDispatched {
		return overview.Batch{}, overview.Slot{}, false, overview.ErrInvalidState
	}
	if validation.Valid {
		var duplicate bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM overview_slots WHERE batch_id=$1 AND slot_id<>$2 AND status='valid' AND content_hash=$3)`, batchID, slotID, contentHash).Scan(&duplicate); err != nil {
			return overview.Batch{}, overview.Slot{}, false, err
		}
		if duplicate {
			validation = overview.Validation{Valid: false, Codes: []string{"duplicate_content"}}
		}
	}
	codes, err := marshalValidationCodes(validation.Codes)
	if err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	status := overview.SlotInvalid
	if validation.Valid {
		status = overview.SlotValid
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE overview_slots SET status=$1,validation_codes=$2,content_hash=$3,deliverable_ref=$4,updated_at=$5 WHERE slot_id=$6`, status, string(codes), nullable(contentHash), nullable(deliverableRef), now, slotID); err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	if err = insertEvent(ctx, tx, batchID, slotID, "overview.slot_"+status, map[string]any{"validationCodes": validation.Codes, "contentHash": contentHash}, now); err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE overview_batches SET updated_at=$1 WHERE batch_id=$2`, now, batchID); err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	batch, err = loadBatchWhere(ctx, tx, `batch_id=$1`, batchID)
	if err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	slot, err = findSlot(batch, slotID)
	if err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return overview.Batch{}, overview.Slot{}, false, err
	}
	return batch, slot, false, nil
}

func (store *Store) RecordBilling(ctx context.Context, batchID, slotID, status string) (overview.Batch, error) {
	return store.mutateSlot(ctx, batchID, slotID, func(tx *sql.Tx, batch overview.Batch, slot overview.Slot, now time.Time) error {
		if status != overview.BillingCaptured && status != overview.BillingReleased {
			return overview.ErrInvalidInput
		}
		if slot.BillingStatus == status {
			return errNoChange
		}
		releaseAllowed := slot.Status == overview.SlotInvalid || slot.Status == overview.SlotFailed || slot.Status == overview.SlotObsolete || batch.Status == overview.BatchObsolete && slot.Status == overview.SlotValid
		if slot.BillingStatus != overview.BillingAuthorized || status == overview.BillingCaptured && slot.Status != overview.SlotValid || status == overview.BillingReleased && !releaseAllowed {
			return overview.ErrInvalidState
		}
		if _, err := tx.ExecContext(ctx, `UPDATE overview_slots SET billing_status=$1,updated_at=$2 WHERE slot_id=$3`, status, now, slotID); err != nil {
			return err
		}
		if err := insertEvent(ctx, tx, batchID, slotID, "overview.billing_"+status, map[string]any{"allocationId": slot.AllocationID}, now); err != nil {
			return err
		}
		return finishBatch(ctx, tx, batchID, now)
	})
}

func (store *Store) AddReplacement(ctx context.Context, batchID string, replacement overview.Slot) (overview.Batch, bool, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return overview.Batch{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	batch, now, err := lockBatch(ctx, tx, batchID)
	if err != nil {
		return overview.Batch{}, false, err
	}
	if batch.Status == overview.BatchObsolete {
		return overview.Batch{}, false, overview.ErrObsolete
	}
	for _, slot := range batch.Slots {
		if slot.Replacement {
			if !sameSlotPlan(slot, replacement) {
				return overview.Batch{}, false, overview.ErrContentConflict
			}
			if err = tx.Commit(); err != nil {
				return overview.Batch{}, false, err
			}
			return batch, true, nil
		}
	}
	if batch.ReplacementUsed || batch.ReplacementExhausted || !replacement.Replacement || replacement.Ordinal != len(batch.Slots)+1 || replacement.BatchID != batchID {
		return overview.Batch{}, false, overview.ErrInvalidState
	}
	replacement.CreatedAt = now
	replacement.UpdatedAt = now
	if err = insertSlot(ctx, tx, replacement); err != nil {
		return overview.Batch{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE overview_batches SET replacement_used=true,updated_at=$1 WHERE batch_id=$2`, now, batchID); err != nil {
		return overview.Batch{}, false, err
	}
	if err = insertEvent(ctx, tx, batchID, replacement.ID, "overview.replacement_added", map[string]any{"agentId": replacement.AgentID, "sourcePosition": replacement.SourcePosition}, now); err != nil {
		return overview.Batch{}, false, err
	}
	batch, err = loadBatchWhere(ctx, tx, `batch_id=$1`, batchID)
	if err != nil {
		return overview.Batch{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return overview.Batch{}, false, err
	}
	return batch, false, nil
}

func (store *Store) ExhaustReplacement(ctx context.Context, batchID string) (overview.Batch, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return overview.Batch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	batch, now, err := lockBatch(ctx, tx, batchID)
	if err != nil {
		return overview.Batch{}, err
	}
	if !batch.ReplacementUsed && !batch.ReplacementExhausted {
		if _, err = tx.ExecContext(ctx, `UPDATE overview_batches SET replacement_exhausted=true,updated_at=$1 WHERE batch_id=$2`, now, batchID); err != nil {
			return overview.Batch{}, err
		}
		if err = insertEvent(ctx, tx, batchID, "", "overview.replacement_exhausted", map[string]any{}, now); err != nil {
			return overview.Batch{}, err
		}
	}
	if err = finishBatch(ctx, tx, batchID, now); err != nil {
		return overview.Batch{}, err
	}
	batch, err = loadBatchWhere(ctx, tx, `batch_id=$1`, batchID)
	if err != nil {
		return overview.Batch{}, err
	}
	if err = tx.Commit(); err != nil {
		return overview.Batch{}, err
	}
	return batch, nil
}

func (store *Store) MarkObsoleteBefore(ctx context.Context, taskID string, revision int) ([]overview.Batch, error) {
	if taskID == "" || revision < 1 {
		return nil, overview.ErrInvalidInput
	}
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT batch_id FROM overview_batches WHERE task_id=$1 AND match_revision<$2 AND status<>'obsolete' ORDER BY match_revision,batch_id FOR UPDATE`, taskID, revision)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	changed := make([]overview.Batch, 0, len(ids))
	for _, id := range ids {
		if _, err = tx.ExecContext(ctx, `UPDATE overview_batches SET status='obsolete',updated_at=$1 WHERE batch_id=$2`, now, id); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE overview_slots SET status='obsolete',updated_at=$1 WHERE batch_id=$2 AND status IN ('planned','dispatched')`, now, id); err != nil {
			return nil, err
		}
		if err = insertEvent(ctx, tx, id, "", "overview.batch_obsolete", map[string]any{"supersedingRevision": revision}, now); err != nil {
			return nil, err
		}
		batch, loadErr := loadBatchWhere(ctx, tx, `batch_id=$1`, id)
		if loadErr != nil {
			return nil, loadErr
		}
		changed = append(changed, batch)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return changed, nil
}

func (store *Store) mutateSlot(ctx context.Context, batchID, slotID string, mutate func(*sql.Tx, overview.Batch, overview.Slot, time.Time) error) (overview.Batch, error) {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return overview.Batch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	batch, now, err := lockBatch(ctx, tx, batchID)
	if err != nil {
		return overview.Batch{}, err
	}
	slot, err := scanSlot(tx.QueryRowContext(ctx, slotSelect+` WHERE slot_id=$1 AND batch_id=$2 FOR UPDATE`, slotID, batchID))
	if errors.Is(err, sql.ErrNoRows) {
		return overview.Batch{}, overview.ErrNotFound
	}
	if err != nil {
		return overview.Batch{}, err
	}
	if err = mutate(tx, batch, slot, now); errors.Is(err, errNoChange) {
		if commitErr := tx.Commit(); commitErr != nil {
			return overview.Batch{}, commitErr
		}
		return batch, nil
	} else if err != nil {
		return overview.Batch{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE overview_batches SET updated_at=GREATEST(updated_at,$1) WHERE batch_id=$2`, now, batchID); err != nil {
		return overview.Batch{}, err
	}
	batch, err = loadBatchWhere(ctx, tx, `batch_id=$1`, batchID)
	if err != nil {
		return overview.Batch{}, err
	}
	if err = tx.Commit(); err != nil {
		return overview.Batch{}, err
	}
	return batch, nil
}

func lockBatch(ctx context.Context, tx *sql.Tx, batchID string) (overview.Batch, time.Time, error) {
	var now time.Time
	batch, err := scanBatch(tx.QueryRowContext(ctx, batchSelect+`,clock_timestamp() FROM overview_batches WHERE batch_id=$1 FOR UPDATE`, batchID), &now)
	if errors.Is(err, sql.ErrNoRows) {
		return overview.Batch{}, now, overview.ErrNotFound
	}
	if err != nil {
		return overview.Batch{}, now, err
	}
	batch.Slots, err = loadSlots(ctx, tx, batchID)
	return batch, now, err
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadBatchWhere(ctx context.Context, query queryer, where string, arguments ...any) (overview.Batch, error) {
	batch, err := scanBatch(query.QueryRowContext(ctx, batchSelect+` FROM overview_batches WHERE `+where, arguments...))
	if err != nil {
		return overview.Batch{}, err
	}
	batch.Slots, err = loadSlots(ctx, query, batch.ID)
	return batch, err
}

func loadSlots(ctx context.Context, query queryer, batchID string) ([]overview.Slot, error) {
	rows, err := query.QueryContext(ctx, slotSelect+` WHERE batch_id=$1 ORDER BY ordinal`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slots []overview.Slot
	for rows.Next() {
		slot, scanErr := scanSlot(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		slots = append(slots, slot)
	}
	return slots, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanBatch(row scanner, additional ...any) (value overview.Batch, err error) {
	destinations := []any{&value.ID, &value.SnapshotID, &value.TaskID, &value.TaskSpecHash, &value.MatchRevision, &value.AlgorithmVersion, &value.BriefRef, &value.BriefHash, &value.Deadline, &value.Status, &value.ReplacementUsed, &value.ReplacementExhausted, &value.CreatedAt, &value.UpdatedAt}
	destinations = append(destinations, additional...)
	err = row.Scan(destinations...)
	return value, err
}

func scanSlot(row scanner) (value overview.Slot, err error) {
	var codes []byte
	var contentHash, deliverableRef sql.NullString
	err = row.Scan(&value.ID, &value.BatchID, &value.Ordinal, &value.SourcePosition, &value.Replacement, &value.AgentID, &value.ProviderID, &value.PriceVersion, &value.QuoteHash, &value.OverviewPrice, &value.ExternalCostCap, &value.AllocationID, &value.LogicalExecutionID, &value.Status, &value.BillingStatus, &codes, &contentHash, &deliverableRef, &value.CreatedAt, &value.UpdatedAt)
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(codes, &value.Validation.Codes); err != nil {
		return value, err
	}
	value.Validation.Valid = value.Status == overview.SlotValid
	if contentHash.Valid {
		value.ContentHash = contentHash.String
	}
	if deliverableRef.Valid {
		value.DeliverableRef = deliverableRef.String
	}
	return value, nil
}

func insertSlot(ctx context.Context, tx *sql.Tx, slot overview.Slot) error {
	codes, err := marshalValidationCodes(slot.Validation.Codes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO overview_slots (slot_id,batch_id,ordinal,source_position,replacement,agent_id,provider_id,price_version,quote_hash,overview_price,external_cost_cap,allocation_id,logical_execution_id,status,billing_status,validation_codes,content_hash,deliverable_ref,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19)`, slot.ID, slot.BatchID, slot.Ordinal, slot.SourcePosition, slot.Replacement, slot.AgentID, slot.ProviderID, slot.PriceVersion, slot.QuoteHash, slot.OverviewPrice, slot.ExternalCostCap, slot.AllocationID, slot.LogicalExecutionID, slot.Status, slot.BillingStatus, string(codes), nullable(slot.ContentHash), nullable(slot.DeliverableRef), slot.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert overview slot: %w", err)
	}
	return nil
}

func marshalValidationCodes(codes []string) ([]byte, error) {
	if codes == nil {
		codes = []string{}
	}
	return json.Marshal(codes)
}

func finishBatch(ctx context.Context, tx *sql.Tx, batchID string, now time.Time) error {
	var pending, invalid bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM overview_slots WHERE batch_id=$1 AND (status IN ('planned','dispatched') OR billing_status='authorized')),EXISTS(SELECT 1 FROM overview_slots WHERE batch_id=$1 AND status IN ('invalid','failed'))`, batchID).Scan(&pending, &invalid); err != nil {
		return err
	}
	if pending {
		return nil
	}
	var used, exhausted bool
	if err := tx.QueryRowContext(ctx, `SELECT replacement_used,replacement_exhausted FROM overview_batches WHERE batch_id=$1`, batchID).Scan(&used, &exhausted); err != nil {
		return err
	}
	if !invalid || used || exhausted {
		_, err := tx.ExecContext(ctx, `UPDATE overview_batches SET status='completed',updated_at=$1 WHERE batch_id=$2 AND status='running'`, now, batchID)
		return err
	}
	return nil
}

func insertEvent(ctx context.Context, tx *sql.Tx, batchID, slotID, eventType string, payload any, now time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID := digest("overview-event:" + batchID + ":" + slotID + ":" + eventType)
	_, err = tx.ExecContext(ctx, `INSERT INTO overview_events (event_id,batch_id,slot_id,event_type,payload,occurred_at) VALUES ($1,$2,$3,$4,$5,$6)`, eventID, batchID, nullable(slotID), eventType, string(encoded), now)
	return err
}

func lock(ctx context.Context, tx *sql.Tx, identities ...string) error {
	sort.Strings(identities)
	for _, identity := range identities {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, identity); err != nil {
			return err
		}
	}
	return nil
}

func samePlan(left, right overview.Batch) bool {
	if left.ID != right.ID || left.SnapshotID != right.SnapshotID || left.TaskID != right.TaskID || left.TaskSpecHash != right.TaskSpecHash || left.MatchRevision != right.MatchRevision || left.AlgorithmVersion != right.AlgorithmVersion || left.BriefRef != right.BriefRef || left.BriefHash != right.BriefHash || !left.Deadline.Equal(right.Deadline) || len(left.Slots) != len(right.Slots) {
		return false
	}
	for index := range left.Slots {
		if !sameSlotPlan(left.Slots[index], right.Slots[index]) {
			return false
		}
	}
	return true
}

func sameSlotPlan(left, right overview.Slot) bool {
	return left.ID == right.ID && left.BatchID == right.BatchID && left.Ordinal == right.Ordinal && left.SourcePosition == right.SourcePosition && left.Replacement == right.Replacement && left.AgentID == right.AgentID && left.ProviderID == right.ProviderID && left.PriceVersion == right.PriceVersion && left.QuoteHash == right.QuoteHash && left.OverviewPrice == right.OverviewPrice && left.ExternalCostCap == right.ExternalCostCap && left.AllocationID == right.AllocationID && left.LogicalExecutionID == right.LogicalExecutionID
}

func findSlot(batch overview.Batch, slotID string) (overview.Slot, error) {
	for _, slot := range batch.Slots {
		if slot.ID == slotID {
			return slot, nil
		}
	}
	return overview.Slot{}, overview.ErrNotFound
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

const batchSelect = `SELECT batch_id,snapshot_id,task_id,task_spec_hash,match_revision,algorithm_version,brief_ref,brief_hash,deadline,status,replacement_used,replacement_exhausted,created_at,updated_at`
const slotSelect = `SELECT slot_id,batch_id,ordinal,source_position,replacement,agent_id,provider_id,price_version,quote_hash,overview_price::text,external_cost_cap::text,allocation_id,logical_execution_id,status,billing_status,validation_codes,content_hash,deliverable_ref,created_at,updated_at FROM overview_slots`

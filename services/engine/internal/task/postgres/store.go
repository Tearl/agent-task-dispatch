package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/example/agent-platform/engine/internal/persistence"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	"github.com/lib/pq"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Store{db: db}, nil
}

func (s *Store) Create(ctx context.Context, mutation enginetask.Mutation, input enginetask.DraftInput, id string) (result enginetask.Task, replay bool, err error) {
	body, replay, err := s.execute(ctx, mutation, "tasks.create:"+mutation.ActorID, func(tx *sql.Tx) (any, error) {
		databaseNow, err := databaseTime(ctx, tx)
		if err != nil {
			return nil, err
		}
		if !input.Deadline.After(databaseNow) {
			return nil, enginetask.ErrInvalidInput
		}
		change := mutation
		change.Now = databaseNow
		criteria, err := json.Marshal(input.AcceptanceCriteria)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,status,title,description,expert_type,tags,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,draft_acceptance,created_at,updated_at) VALUES ($1,$2,'draft',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)`, id, mutation.ActorID, input.Title, input.Description, input.ExpertType, pq.Array(nonNil(input.Tags)), input.Language, input.OverviewBudget, input.FormalBudget, input.ExternalCostCap, input.Deadline, pq.Array(nonNil(input.Inputs)), pq.Array(nonNil(input.AllowedTools)), pq.Array(nonNil(input.Exclusions)), input.DeliveryFormat, string(criteria), databaseNow)
		if err != nil {
			return nil, fmt.Errorf("create task draft: %w", err)
		}
		created, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordChange(ctx, tx, change, created, "task.draft_created"); err != nil {
			return nil, err
		}
		return created, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) UpdateDraft(ctx context.Context, mutation enginetask.Mutation, id string, input enginetask.UpdateDraftInput) (result enginetask.Task, replay bool, err error) {
	body, replay, err := s.execute(ctx, mutation, "tasks.draft:"+mutation.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, mutation.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if current.Status != enginetask.StatusDraft {
			return nil, enginetask.ErrInvalidState
		}
		databaseNow, err := databaseTime(ctx, tx)
		if err != nil {
			return nil, err
		}
		if !input.Deadline.After(databaseNow) {
			return nil, enginetask.ErrInvalidInput
		}
		change := mutation
		change.Now = databaseNow
		criteria, err := json.Marshal(input.AcceptanceCriteria)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET title=$1,description=$2,expert_type=$3,tags=$4,language=$5,overview_budget=$6,formal_budget=$7,external_cost_cap=$8,deadline=$9,inputs=$10,allowed_tools=$11,exclusions=$12,delivery_format=$13,draft_acceptance=$14,aggregate_version=aggregate_version+1,updated_at=$15 WHERE task_id=$16`, input.Title, input.Description, input.ExpertType, pq.Array(nonNil(input.Tags)), input.Language, input.OverviewBudget, input.FormalBudget, input.ExternalCostCap, input.Deadline, pq.Array(nonNil(input.Inputs)), pq.Array(nonNil(input.AllowedTools)), pq.Array(nonNil(input.Exclusions)), input.DeliveryFormat, string(criteria), databaseNow, id)
		if err != nil {
			return nil, fmt.Errorf("update task draft: %w", err)
		}
		updated, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordChange(ctx, tx, change, updated, "task.draft_updated"); err != nil {
			return nil, err
		}
		return updated, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) Publish(ctx context.Context, mutation enginetask.Mutation, id string, input enginetask.PublishInput) (result enginetask.Publication, replay bool, err error) {
	body, replay, err := s.execute(ctx, mutation, "tasks.publish:"+mutation.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, mutation.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		if current.Status != enginetask.StatusDraft || current.CurrentSpecVersion != nil || current.CurrentAcceptanceVersion != nil {
			return nil, enginetask.ErrInvalidState
		}
		databaseNow, err := databaseTime(ctx, tx)
		if err != nil {
			return nil, err
		}
		if !current.Deadline.After(databaseNow) {
			return nil, enginetask.ErrInvalidInput
		}
		if !enginetask.ValidFormalBudget(current.FormalBudget) {
			return nil, enginetask.ErrInvalidInput
		}
		change := mutation
		change.Now = databaseNow
		version := 1
		spec, acceptance, err := enginetask.PublicationVersions(current, version, databaseNow)
		if err != nil {
			return nil, err
		}
		criteria, err := json.Marshal(acceptance.Criteria)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO task_spec_versions (task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,tags,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, id, version, spec.TaskAggregateVersion, spec.ContentHash, spec.Title, spec.Description, spec.ExpertType, pq.Array(nonNil(spec.Tags)), spec.Language, spec.OverviewBudget, spec.FormalBudget, spec.ExternalCostCap, spec.Deadline, pq.Array(nonNil(spec.Inputs)), pq.Array(nonNil(spec.AllowedTools)), pq.Array(nonNil(spec.Exclusions)), spec.DeliveryFormat, databaseNow)
		if err != nil {
			return nil, fmt.Errorf("freeze task spec: %w", err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO acceptance_versions (task_id,version_no,task_aggregate_version,content_hash,criteria,total_weight,created_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, version, acceptance.TaskAggregateVersion, acceptance.ContentHash, string(criteria), acceptance.TotalWeight, databaseNow)
		if err != nil {
			return nil, fmt.Errorf("freeze task acceptance: %w", err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='pending_escrow',current_spec_version=$1,current_acceptance_version=$1,published_at=$2,aggregate_version=aggregate_version+1,updated_at=$2 WHERE task_id=$3`, version, databaseNow, id)
		if err != nil {
			return nil, fmt.Errorf("publish task: %w", err)
		}
		published, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1`, id))
		if err != nil {
			return nil, err
		}
		if err = recordPublicationChange(ctx, tx, change, published, spec, acceptance); err != nil {
			return nil, err
		}
		payload, err := json.Marshal(map[string]any{"eventType": "task.published", "taskId": id, "taskAggregateVersion": published.AggregateVersion, "specVersion": version, "specContentHash": spec.ContentHash, "acceptanceVersion": version, "acceptanceContentHash": acceptance.ContentHash})
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox_messages (message_id,dedupe_key,topic,payload,available_at) VALUES ($1,$2,'task-events',$3,$4)`, mutation.EventID+"_outbox", fmt.Sprintf("task:%s:published:%d", id, published.AggregateVersion), string(payload), databaseNow); err != nil {
			return nil, fmt.Errorf("enqueue task publication: %w", err)
		}
		return enginetask.Publication{Task: published, Spec: spec, Acceptance: acceptance}, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) RequestDelete(ctx context.Context, mutation enginetask.Mutation, id string, input enginetask.DeleteInput) (result enginetask.DeleteResult, replay bool, err error) {
	body, replay, err := s.execute(ctx, mutation, "tasks.delete:"+mutation.ActorID+":"+id, func(tx *sql.Tx) (any, error) {
		current, err := loadOwned(ctx, tx, mutation.ActorID, id, input.ExpectedVersion)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{"draft": true, "pending_escrow": true, "escrowed": true, "matching": true, "overview_generating": true, "awaiting_selection": true, "funding_configuration_invalid": true, "funding_refund_pending": true}
		if !allowed[current.Status] {
			return nil, enginetask.ErrInvalidState
		}
		databaseNow, err := databaseTime(ctx, tx)
		if err != nil {
			return nil, err
		}
		var intentID, chainID, contract, chainTaskID, wallet, fundingStatus string
		fundingErr := tx.QueryRowContext(ctx, `SELECT intent_id,chain_id,contract_address,chain_task_id,publisher_wallet,status FROM task_funding_intents WHERE task_id=$1`, id).Scan(&intentID, &chainID, &contract, &chainTaskID, &wallet, &fundingStatus)
		if fundingErr != nil && !errors.Is(fundingErr, sql.ErrNoRows) {
			return nil, fundingErr
		}
		fundedStatuses := map[string]bool{"escrowed": true, "matching": true, "overview_generating": true, "awaiting_selection": true, "funding_refund_pending": true}
		funded := fundedStatuses[current.Status] || fundingStatus == "confirmed"
		if !funded && fundingStatus == "orphaned" {
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_funding_canonicalizations WHERE intent_id=$1)`, intentID).Scan(&funded); err != nil {
				return nil, err
			}
		}
		if fundingStatus == "submitted" && current.Status == "pending_escrow" {
			return nil, enginetask.ErrInvalidState
		}
		change := mutation
		change.Now = databaseNow
		if funded {
			if chainID == "" || contract == "" || chainTaskID == "" || wallet == "" {
				return nil, enginetask.ErrInvalidState
			}
			_, err = tx.ExecContext(ctx, `UPDATE tasks SET deletion_requested_at=COALESCE(deletion_requested_at,$1),aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2`, databaseNow, id)
			if err != nil {
				return nil, err
			}
			updated, loadErr := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1`, id))
			if loadErr != nil {
				return nil, loadErr
			}
			if err = recordChange(ctx, tx, change, updated, "task.deletion_requested"); err != nil {
				return nil, err
			}
			return enginetask.DeleteResult{TaskID: id, Status: updated.Status, RefundRequired: true, ChainID: chainID, ContractAddress: contract, ChainTaskID: chainTaskID, PublisherWallet: wallet}, nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET status='cancelled',deletion_requested_at=COALESCE(deletion_requested_at,$1),deleted_at=$1,aggregate_version=aggregate_version+1,updated_at=$1 WHERE task_id=$2`, databaseNow, id)
		if err != nil {
			return nil, err
		}
		updated, loadErr := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1`, id))
		if loadErr != nil {
			return nil, loadErr
		}
		if err = recordChange(ctx, tx, change, updated, "task.deleted"); err != nil {
			return nil, err
		}
		return enginetask.DeleteResult{TaskID: id, Status: updated.Status}, nil
	})
	if err != nil {
		return result, false, err
	}
	err = json.Unmarshal(body, &result)
	return result, replay, err
}

func (s *Store) Get(ctx context.Context, publisherID, id string) (enginetask.Task, error) {
	result, err := scanTask(s.db.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1 AND publisher_id=$2 AND deleted_at IS NULL`, id, publisherID))
	if errors.Is(err, sql.ErrNoRows) {
		return result, enginetask.ErrNotFound
	}
	return result, err
}

func (s *Store) GetForActions(ctx context.Context, publisherID, id string) (enginetask.Task, time.Time, error) {
	var databaseNow time.Time
	result, err := scanTaskWithTime(s.db.QueryRowContext(ctx, taskSelectWithTime+` WHERE task_id=$1 AND publisher_id=$2 AND deleted_at IS NULL`, id, publisherID), &databaseNow)
	if errors.Is(err, sql.ErrNoRows) {
		return result, databaseNow, enginetask.ErrNotFound
	}
	return result, databaseNow, err
}

type work func(*sql.Tx) (any, error)

func (s *Store) execute(ctx context.Context, mutation enginetask.Mutation, scope string, fn work) (json.RawMessage, bool, error) {
	if mutation.IdempotencyKey == "" || mutation.RequestHash == "" {
		return nil, false, enginetask.ErrInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope+":"+mutation.IdempotencyKey); err != nil {
		return nil, false, err
	}
	var previousHash string
	var previous []byte
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_body FROM idempotency_records WHERE scope=$1 AND idempotency_key=$2`, scope, mutation.IdempotencyKey).Scan(&previousHash, &previous)
	if err == nil {
		if previousHash != mutation.RequestHash {
			return nil, false, persistence.ErrIdempotencyConflict
		}
		if err = tx.Commit(); err != nil {
			return nil, false, err
		}
		return previous, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	value, err := fn(tx)
	if err != nil {
		return nil, false, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_records (scope,idempotency_key,request_hash,response_body) VALUES ($1,$2,$3,$4)`, scope, mutation.IdempotencyKey, mutation.RequestHash, body); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return body, false, nil
}

func loadOwned(ctx context.Context, tx *sql.Tx, publisherID, id string, version int64) (enginetask.Task, error) {
	current, err := scanTask(tx.QueryRowContext(ctx, taskSelect+` WHERE task_id=$1 FOR UPDATE`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return current, enginetask.ErrNotFound
	}
	if err != nil {
		return current, err
	}
	if current.PublisherID != publisherID {
		return current, enginetask.ErrNotFound
	}
	if current.AggregateVersion != version {
		return current, enginetask.ErrStaleVersion
	}
	return current, nil
}

func recordChange(ctx context.Context, tx *sql.Tx, mutation enginetask.Mutation, value enginetask.Task, eventType string) error {
	payload, err := json.Marshal(map[string]any{"status": value.Status, "aggregateVersion": value.AggregateVersion})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO domain_events (event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at) VALUES ($1,'task',$2,$3,$4,$5,$6)`, mutation.EventID, value.ID, value.AggregateVersion, eventType, string(payload), mutation.Now); err != nil {
		return fmt.Errorf("record task domain event: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,$3,'task',$4,$5,$6)`, mutation.EventID+"_audit", mutation.ActorID, eventType, value.ID, string(payload), mutation.Now); err != nil {
		return fmt.Errorf("record task audit event: %w", err)
	}
	return nil
}

func recordPublicationChange(ctx context.Context, tx *sql.Tx, mutation enginetask.Mutation, value enginetask.Task, spec enginetask.SpecVersion, acceptance enginetask.AcceptanceVersion) error {
	payload, err := json.Marshal(map[string]any{"previousStatus": enginetask.StatusDraft, "status": value.Status, "aggregateVersion": value.AggregateVersion, "specVersion": spec.Version, "specContentHash": spec.ContentHash, "acceptanceVersion": acceptance.Version, "acceptanceContentHash": acceptance.ContentHash})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO domain_events (event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at) VALUES ($1,'task',$2,$3,'task.published',$4,$5)`, mutation.EventID, value.ID, value.AggregateVersion, string(payload), mutation.Now); err != nil {
		return fmt.Errorf("record task publication event: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,'task.published','task',$3,$4,$5)`, mutation.EventID+"_audit", mutation.ActorID, value.ID, string(payload), mutation.Now); err != nil {
		return fmt.Errorf("record task publication audit: %w", err)
	}
	return nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func databaseTime(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	return now, nil
}

const taskColumns = `task_id,publisher_id,status,title,description,expert_type,tags,language,overview_budget::text,formal_budget::text,external_cost_cap::text,deadline,inputs,allowed_tools,exclusions,delivery_format,draft_acceptance,aggregate_version,current_spec_version,current_acceptance_version,published_at,created_at,updated_at`
const taskSelect = `SELECT ` + taskColumns + ` FROM tasks`
const taskSelectWithTime = `SELECT ` + taskColumns + `,clock_timestamp() FROM tasks`

type scanner interface{ Scan(...any) error }

func scanTask(row scanner) (value enginetask.Task, err error) {
	return scanTaskWithTime(row, nil)
}

func scanTaskWithTime(row scanner, databaseNow *time.Time) (value enginetask.Task, err error) {
	var tags, inputs, tools, exclusions pq.StringArray
	var criteria []byte
	var specVersion, acceptanceVersion sql.NullInt64
	var publishedAt sql.NullTime
	destinations := []any{&value.ID, &value.PublisherID, &value.Status, &value.Title, &value.Description, &value.ExpertType, &tags, &value.Language, &value.OverviewBudget, &value.FormalBudget, &value.ExternalCostCap, &value.Deadline, &inputs, &tools, &exclusions, &value.DeliveryFormat, &criteria, &value.AggregateVersion, &specVersion, &acceptanceVersion, &publishedAt, &value.CreatedAt, &value.UpdatedAt}
	if databaseNow != nil {
		destinations = append(destinations, databaseNow)
	}
	err = row.Scan(destinations...)
	if err != nil {
		return value, err
	}
	value.Inputs = []string(inputs)
	value.Tags = []string(tags)
	value.AllowedTools = []string(tools)
	value.Exclusions = []string(exclusions)
	if err = json.Unmarshal(criteria, &value.AcceptanceCriteria); err != nil {
		return value, fmt.Errorf("decode acceptance criteria: %w", err)
	}
	if specVersion.Valid {
		version := int(specVersion.Int64)
		value.CurrentSpecVersion = &version
	}
	if acceptanceVersion.Valid {
		version := int(acceptanceVersion.Int64)
		value.CurrentAcceptanceVersion = &version
	}
	if publishedAt.Valid {
		value.PublishedAt = &publishedAt.Time
	}
	return value, nil
}

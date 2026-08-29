package workflow

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/matching"
	"github.com/lib/pq"
)

const vectorVersion = "matching-vector-v1"

type TaskInput struct {
	ID              string
	PublisherID     string
	Status          string
	SpecHash        string
	Title           string
	Description     string
	ExpertType      string
	Tags            []string
	Language        string
	OverviewBudget  string
	FormalBudget    string
	ExternalCostCap string
	Deadline        time.Time
	Inputs          []string
	AllowedTools    []string
	DeliveryFormat  string
}

type Store struct{ db *sql.DB }

type MatchingOperationLock struct {
	conn *sql.Conn
	key  string
}

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &Store{db: db}, nil
}

func (store *Store) LockMatchingOperation(ctx context.Context, publisherID, operationID string) (*MatchingOperationLock, error) {
	conn, err := store.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	key := "matching-operation:" + publisherID + ":" + operationID
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, key); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &MatchingOperationLock{conn: conn, key: key}, nil
}

func (lock *MatchingOperationLock) Close() {
	if lock == nil || lock.conn == nil {
		return
	}
	_, _ = lock.conn.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, lock.key)
	_ = lock.conn.Close()
	lock.conn = nil
}

func (lock *MatchingOperationLock) Completed(ctx context.Context, publisherID, operationID, taskID string) (StartMatchingResult, bool, error) {
	var storedTaskID string
	var payload []byte
	err := lock.conn.QueryRowContext(ctx, `SELECT task_id,response_body FROM matching_run_operations WHERE publisher_id=$1 AND operation_id=$2`, publisherID, operationID).Scan(&storedTaskID, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return StartMatchingResult{}, false, nil
	}
	if err != nil {
		return StartMatchingResult{}, false, err
	}
	if storedTaskID != taskID {
		return StartMatchingResult{}, false, ErrInvalidInput
	}
	if payload == nil {
		return StartMatchingResult{}, false, nil
	}
	var result StartMatchingResult
	if err = json.Unmarshal(payload, &result); err != nil {
		return StartMatchingResult{}, false, err
	}
	return result, true, nil
}

func (lock *MatchingOperationLock) PendingEvaluation(ctx context.Context, publisherID, operationID, taskID string) (time.Time, bool, error) {
	var storedTaskID string
	var evaluatedAt time.Time
	err := lock.conn.QueryRowContext(ctx, `SELECT task_id,evaluated_at FROM matching_run_operations WHERE publisher_id=$1 AND operation_id=$2 AND response_body IS NULL`, publisherID, operationID).Scan(&storedTaskID, &evaluatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	if storedTaskID != taskID {
		return time.Time{}, false, ErrInvalidInput
	}
	return evaluatedAt, true, nil
}

func (lock *MatchingOperationLock) Begin(ctx context.Context, publisherID, operationID, taskID string, evaluatedAt time.Time) (time.Time, StartMatchingResult, bool, error) {
	if _, err := lock.conn.ExecContext(ctx, `INSERT INTO matching_run_operations(publisher_id,operation_id,task_id,evaluated_at,created_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT DO NOTHING`, publisherID, operationID, taskID, evaluatedAt); err != nil {
		return time.Time{}, StartMatchingResult{}, false, err
	}
	var storedTaskID string
	var storedTime time.Time
	var payload []byte
	err := lock.conn.QueryRowContext(ctx, `SELECT task_id,evaluated_at,COALESCE(response_body,'null'::jsonb) FROM matching_run_operations WHERE publisher_id=$1 AND operation_id=$2`, publisherID, operationID).Scan(&storedTaskID, &storedTime, &payload)
	if err != nil {
		return time.Time{}, StartMatchingResult{}, false, err
	}
	if storedTaskID != taskID {
		return time.Time{}, StartMatchingResult{}, false, ErrInvalidInput
	}
	if string(payload) == "null" {
		return storedTime, StartMatchingResult{}, false, nil
	}
	var result StartMatchingResult
	if err = json.Unmarshal(payload, &result); err != nil {
		return time.Time{}, StartMatchingResult{}, false, err
	}
	return storedTime, result, true, nil
}

func (lock *MatchingOperationLock) FrozenDraft(ctx context.Context, publisherID, operationID, taskID string) (matching.SnapshotDraft, bool, error) {
	var storedTaskID string
	var payload []byte
	err := lock.conn.QueryRowContext(ctx, `SELECT task_id,snapshot_draft FROM matching_run_operations WHERE publisher_id=$1 AND operation_id=$2`, publisherID, operationID).Scan(&storedTaskID, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return matching.SnapshotDraft{}, false, ErrNotFound
	}
	if err != nil {
		return matching.SnapshotDraft{}, false, err
	}
	if storedTaskID != taskID {
		return matching.SnapshotDraft{}, false, ErrInvalidInput
	}
	if payload == nil {
		return matching.SnapshotDraft{}, false, nil
	}
	var draft matching.SnapshotDraft
	if err = json.Unmarshal(payload, &draft); err != nil {
		return matching.SnapshotDraft{}, false, err
	}
	return draft, true, nil
}

func (lock *MatchingOperationLock) FreezeDraft(ctx context.Context, publisherID, operationID, taskID string, draft matching.SnapshotDraft) (matching.SnapshotDraft, error) {
	payload, err := json.Marshal(draft)
	if err != nil {
		return matching.SnapshotDraft{}, err
	}
	if _, err = lock.conn.ExecContext(ctx, `UPDATE matching_run_operations SET snapshot_draft=$1 WHERE publisher_id=$2 AND operation_id=$3 AND task_id=$4 AND response_body IS NULL AND snapshot_draft IS NULL`, payload, publisherID, operationID, taskID); err != nil {
		return matching.SnapshotDraft{}, err
	}
	stored, exists, err := lock.FrozenDraft(ctx, publisherID, operationID, taskID)
	if err != nil {
		return matching.SnapshotDraft{}, err
	}
	if !exists {
		return matching.SnapshotDraft{}, ErrInvalidInput
	}
	return stored, nil
}

func (store *Store) Task(ctx context.Context, publisherID, taskID string) (TaskInput, error) {
	return store.taskAtSpec(ctx, publisherID, taskID, "")
}

func (store *Store) taskAtSpec(ctx context.Context, publisherID, taskID, specHash string) (TaskInput, error) {
	var value TaskInput
	var tags, inputs, allowed pq.StringArray
	err := store.db.QueryRowContext(ctx, `SELECT task.task_id,task.publisher_id,task.status,spec.content_hash,spec.title,spec.description,spec.expert_type,spec.tags,spec.language,spec.overview_budget::text,spec.formal_budget::text,spec.external_cost_cap::text,spec.deadline,spec.inputs,spec.allowed_tools,spec.delivery_format
FROM tasks task JOIN task_spec_versions spec ON spec.task_id=task.task_id AND (($3='' AND spec.version_no=task.current_spec_version) OR ($3<>'' AND spec.content_hash=$3))
WHERE task.task_id=$1 AND task.publisher_id=$2`, taskID, publisherID, specHash).Scan(&value.ID, &value.PublisherID, &value.Status, &value.SpecHash, &value.Title, &value.Description, &value.ExpertType, &tags, &value.Language, &value.OverviewBudget, &value.FormalBudget, &value.ExternalCostCap, &value.Deadline, &inputs, &allowed, &value.DeliveryFormat)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	value.Tags, value.Inputs, value.AllowedTools = []string(tags), []string(inputs), []string(allowed)
	return value, err
}

func (store *Store) Candidates(ctx context.Context) ([]matching.Candidate, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT agent.agent_id,agent.owner_id,agent.status,agent.approval_status,agent.health,agent.health_checked_at,agent.health_valid_until,agent.max_concurrency,(SELECT count(*)::integer FROM agent_capacity_leases lease WHERE lease.agent_id=agent.agent_id AND lease.released_at IS NULL AND lease.expires_at>statement_timestamp()),agent.category,agent.languages,agent.tags,agent.capabilities,agent.risk_status,agent.payout_address,agent.matching_vector_version,agent.estimated_duration_seconds,price.overview_price::text,price.formal_package_gross_price::text,price.external_cost_cap::text,price.version_no,agent.matching_exposure_count,agent.matching_effective_samples,agent.reputation_quality,agent.reputation_speed,agent.reputation_reliability,agent.reputation_communication,agent.reputation_compliance
FROM agents agent JOIN agent_price_versions price ON price.agent_id=agent.agent_id AND price.version_no=agent.current_price_version
ORDER BY agent.agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []matching.Candidate{}
	for rows.Next() {
		var candidate matching.Candidate
		var languages, tags pq.StringArray
		var capabilities string
		var duration int64
		var vector sql.NullString
		var reputation [5]sql.NullInt64
		if err = rows.Scan(&candidate.AgentID, &candidate.ProviderID, &candidate.Status, &candidate.ApprovalStatus, &candidate.Health, &candidate.HealthCheckedAt, &candidate.HealthValidUntil, &candidate.MaxConcurrency, &candidate.ActiveCapacity, &candidate.Category, &languages, &tags, &capabilities, &candidate.RiskStatus, &candidate.PayoutAddress, &vector, &duration, &candidate.OverviewPrice, &candidate.FormalPrice, &candidate.ExternalCostCap, &candidate.PriceVersion, &candidate.ExposureCount, &candidate.EffectiveSamples, &reputation[0], &reputation[1], &reputation[2], &reputation[3], &reputation[4]); err != nil {
			return nil, err
		}
		candidate.Languages = []string(languages)
		candidate.Tags = []string(tags)
		candidate.Capabilities = parseCapabilities(capabilities)
		candidate.EstimatedDuration = time.Duration(duration) * time.Second
		candidate.ProtocolVersion = execution.ProtocolVersion
		if vector.Valid {
			candidate.VectorVersion = vector.String
		}
		candidate.ReputationAvailable = reputation[0].Valid && reputation[1].Valid && reputation[2].Valid && reputation[3].Valid && reputation[4].Valid
		if candidate.ReputationAvailable {
			candidate.Reputation = matching.Reputation{Quality: int(reputation[0].Int64), Speed: int(reputation[1].Int64), Reliability: int(reputation[2].Int64), Communication: int(reputation[3].Int64), Compliance: int(reputation[4].Int64)}
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (store *Store) ResolveTarget(ctx context.Context, agentID string, priceVersion int) (target TargetRecord, err error) {
	err = store.db.QueryRowContext(ctx, `SELECT agent.agent_id,agent.owner_id,agent.endpoint_url,price.version_no,price.overview_price::text,price.external_cost_cap::text
FROM agents agent JOIN agent_price_versions price ON price.agent_id=agent.agent_id
WHERE agent.agent_id=$1 AND price.version_no=$2 AND agent.status='active'`, agentID, priceVersion).Scan(&target.AgentID, &target.ProviderID, &target.Endpoint, &target.PriceVersion, &target.OverviewPrice, &target.ExternalCostCap)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return target, err
}

func (store *Store) ListExecutions(ctx context.Context, publisherID, taskID string) ([]ExecutionView, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT execution.logical_execution_id,execution.stage,execution.agent_id,execution.status,execution.current_attempt,execution.used_cost::text,execution.cost_cap::text,COALESCE(execution.content_hash,''),COALESCE(execution.deliverable_ref,''),execution.deadline,execution.created_at,execution.updated_at
FROM logical_executions execution JOIN tasks task ON task.task_id=execution.task_id
WHERE execution.task_id=$1 AND task.publisher_id=$2 ORDER BY execution.created_at,execution.logical_execution_id`, taskID, publisherID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []ExecutionView{}
	for rows.Next() {
		var value ExecutionView
		if err = rows.Scan(&value.LogicalExecutionID, &value.Stage, &value.AgentID, &value.Status, &value.CurrentAttempt, &value.UsedCost, &value.CostCap, &value.ContentHash, &value.DeliverableRef, &value.Deadline, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (store *Store) BatchOwned(ctx context.Context, publisherID, taskID, batchID string) error {
	var exists bool
	if err := store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM overview_batches batch JOIN tasks task ON task.task_id=batch.task_id WHERE batch.batch_id=$1 AND batch.task_id=$2 AND task.publisher_id=$3)`, batchID, taskID, publisherID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (store *Store) OverviewFundingReady(ctx context.Context, publisherID, taskID, snapshotID string) (bool, error) {
	var ready bool
	err := store.db.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1
FROM tasks task
JOIN fund_accounts formal ON formal.task_id=task.task_id AND formal.reference_id=task.task_id AND formal.account_type='formal_escrow'
JOIN fund_accounts discovery ON discovery.task_id=task.task_id AND discovery.reference_id=task.task_id
    AND discovery.account_type='discovery_pool' AND discovery.asset_key=formal.asset_key AND discovery.state='open'
WHERE task.task_id=$1 AND task.publisher_id=$2
AND discovery.balance-COALESCE((
    SELECT sum(allocation.reserve_amount)
    FROM fund_allocations allocation
    WHERE allocation.account_id=discovery.account_id AND allocation.status='authorized'
),0) >= COALESCE((
    SELECT sum(candidate.overview_price::numeric+candidate.external_cost_cap::numeric)
    FROM match_snapshot_candidates candidate
    WHERE candidate.snapshot_id=$3 AND candidate.final_position IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM fund_allocations existing
          WHERE existing.snapshot_id=candidate.snapshot_id AND existing.agent_id=candidate.agent_id
            AND existing.price_version=candidate.price_version
      )
),0)
)`, taskID, publisherID, snapshotID).Scan(&ready)
	return ready, err
}

func (store *Store) TransitionTask(ctx context.Context, publisherID, taskID, target, eventType string) error {
	return store.transitionTask(ctx, publisherID, taskID, target, eventType, "", nil)
}

func (store *Store) TransitionTaskAndRecordMatchingOperation(ctx context.Context, publisherID, operationID, taskID, target, eventType string, response StartMatchingResult) error {
	return store.transitionTask(ctx, publisherID, taskID, target, eventType, operationID, &response)
}

func (store *Store) transitionTask(ctx context.Context, publisherID, taskID, target, eventType, operationID string, response *StartMatchingResult) error {
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var status string
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT status,aggregate_version FROM tasks WHERE task_id=$1 AND publisher_id=$2 FOR UPDATE`, taskID, publisherID).Scan(&status, &version); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if status != target {
		if !workflowTransitionAllowed(status, target) {
			return ErrInvalidInput
		}
		version++
		if result, updateErr := tx.ExecContext(ctx, `UPDATE tasks SET status=$1,aggregate_version=$2,updated_at=$3 WHERE task_id=$4 AND publisher_id=$5`, target, version, now, taskID, publisherID); updateErr != nil {
			return updateErr
		} else if changed, _ := result.RowsAffected(); changed != 1 {
			return ErrNotFound
		}
		payload, marshalErr := json.Marshal(map[string]any{"previousStatus": status, "status": target, "aggregateVersion": version})
		if marshalErr != nil {
			return marshalErr
		}
		eventID := workflowEventID(eventType, taskID, fmt.Sprintf("%d", version))
		if _, err = tx.ExecContext(ctx, `INSERT INTO domain_events (event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at) VALUES ($1,'task',$2,$3,$4,$5,$6)`, eventID, taskID, version, eventType, string(payload), now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,$3,'task',$4,$5,$6)`, eventID+"_audit", publisherID, eventType, taskID, string(payload), now); err != nil {
			return err
		}
	}
	if response != nil {
		payload, marshalErr := json.Marshal(response)
		if marshalErr != nil {
			return marshalErr
		}
		if result, updateErr := tx.ExecContext(ctx, `UPDATE matching_run_operations SET response_body=$1,completed_at=$2 WHERE publisher_id=$3 AND operation_id=$4 AND task_id=$5 AND response_body IS NULL`, payload, now, publisherID, operationID, taskID); updateErr != nil {
			return updateErr
		} else if changed, _ := result.RowsAffected(); changed != 1 {
			return ErrInvalidInput
		}
	}
	return tx.Commit()
}

func (store *Store) InputAuthorized(ctx context.Context, agentID, inputRef string) (TaskInput, error) {
	var taskID, publisherID, taskSpecHash string
	err := store.db.QueryRowContext(ctx, `SELECT execution.task_id,task.publisher_id,execution.task_spec_hash FROM logical_executions execution JOIN tasks task ON task.task_id=execution.task_id
WHERE execution.agent_id=$1 AND execution.input_ref=$2 AND execution.stage='overview' AND execution.deadline>clock_timestamp() AND execution.status IN ('pending','running','succeeded')
ORDER BY execution.created_at DESC LIMIT 1`, agentID, inputRef).Scan(&taskID, &publisherID, &taskSpecHash)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskInput{}, ErrForbidden
	}
	if err != nil {
		return TaskInput{}, err
	}
	return store.taskAtSpec(ctx, publisherID, taskID, taskSpecHash)
}

func (store *Store) ArtifactAgent(ctx context.Context, deliverableRef string) (string, string, error) {
	var agentID, endpoint string
	err := store.db.QueryRowContext(ctx, `SELECT agent_id,agent_endpoint FROM logical_executions WHERE deliverable_ref=$1 AND stage='overview' AND status='succeeded'`, deliverableRef).Scan(&agentID, &endpoint)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return agentID, endpoint, err
}

func (store *Store) ToolEvidence(ctx context.Context, logicalExecutionID string) (bool, error) {
	var succeeded bool
	err := store.db.QueryRowContext(ctx, `SELECT status='succeeded' AND stage='overview' FROM logical_executions WHERE logical_execution_id=$1`, logicalExecutionID).Scan(&succeeded)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return succeeded, err
}

func parseCapabilities(value string) []string {
	var decoded []string
	if json.Unmarshal([]byte(value), &decoded) == nil && len(decoded) > 0 {
		return decoded
	}
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		if trimmed := strings.TrimSpace(field); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func workflowEventID(parts ...string) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write([]byte(fmt.Sprintf("%d:%s", len(part), part)))
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func workflowTransitionAllowed(current, target string) bool {
	switch target {
	case "matching":
		return current == "escrowed" || current == "overview_generating" || current == "awaiting_selection"
	case "overview_generating":
		return current == "matching"
	case "awaiting_selection":
		return current == "overview_generating"
	default:
		return false
	}
}

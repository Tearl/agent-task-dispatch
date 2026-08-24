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

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &Store{db: db}, nil
}

func (store *Store) Task(ctx context.Context, publisherID, taskID string) (TaskInput, error) {
	return store.taskAtSpec(ctx, publisherID, taskID, "")
}

func (store *Store) taskAtSpec(ctx context.Context, publisherID, taskID, specHash string) (TaskInput, error) {
	var value TaskInput
	var inputs, allowed pq.StringArray
	err := store.db.QueryRowContext(ctx, `SELECT task.task_id,task.publisher_id,task.status,spec.content_hash,spec.title,spec.description,spec.expert_type,spec.language,spec.overview_budget::text,spec.formal_budget::text,spec.external_cost_cap::text,spec.deadline,spec.inputs,spec.allowed_tools,spec.delivery_format
FROM tasks task JOIN task_spec_versions spec ON spec.task_id=task.task_id AND (($3='' AND spec.version_no=task.current_spec_version) OR ($3<>'' AND spec.content_hash=$3))
WHERE task.task_id=$1 AND task.publisher_id=$2`, taskID, publisherID, specHash).Scan(&value.ID, &value.PublisherID, &value.Status, &value.SpecHash, &value.Title, &value.Description, &value.ExpertType, &value.Language, &value.OverviewBudget, &value.FormalBudget, &value.ExternalCostCap, &value.Deadline, &inputs, &allowed, &value.DeliveryFormat)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	value.Inputs, value.AllowedTools = []string(inputs), []string(allowed)
	return value, err
}

func (store *Store) Candidates(ctx context.Context) ([]matching.Candidate, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT agent.agent_id,agent.owner_id,agent.status,agent.health,agent.health_checked_at,agent.health_valid_until,agent.max_concurrency,(SELECT count(*)::integer FROM agent_capacity_leases lease WHERE lease.agent_id=agent.agent_id AND lease.released_at IS NULL AND lease.expires_at>statement_timestamp()),agent.category,agent.languages,agent.tags,agent.capabilities,agent.payout_address,agent.estimated_duration_seconds,price.overview_price::text,price.formal_package_gross_price::text,price.external_cost_cap::text,price.version_no
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
		if err = rows.Scan(&candidate.AgentID, &candidate.ProviderID, &candidate.Status, &candidate.Health, &candidate.HealthCheckedAt, &candidate.HealthValidUntil, &candidate.MaxConcurrency, &candidate.ActiveCapacity, &candidate.Category, &languages, &tags, &capabilities, &candidate.PayoutAddress, &duration, &candidate.OverviewPrice, &candidate.FormalPrice, &candidate.ExternalCostCap, &candidate.PriceVersion); err != nil {
			return nil, err
		}
		candidate.Languages = []string(languages)
		candidate.Tags = []string(tags)
		candidate.Capabilities = parseCapabilities(capabilities)
		candidate.EstimatedDuration = time.Duration(duration) * time.Second
		// Activation is the current persisted approval/risk gate. Dedicated
		// approval and risk projections can replace these mappings without
		// changing the matching contract.
		candidate.ApprovalStatus = "approved"
		candidate.RiskStatus = "eligible"
		candidate.ProtocolVersion = execution.ProtocolVersion
		candidate.VectorVersion = vectorVersion
		candidate.Reputation = matching.Reputation{Quality: 80, Speed: 80, Reliability: 80, Communication: 80, Compliance: 80}
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

func (store *Store) TransitionTask(ctx context.Context, publisherID, taskID, target, eventType string) error {
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
	if status == target {
		return tx.Commit()
	}
	if !workflowTransitionAllowed(status, target) {
		return ErrInvalidInput
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	version++
	if result, updateErr := tx.ExecContext(ctx, `UPDATE tasks SET status=$1,aggregate_version=$2,updated_at=$3 WHERE task_id=$4 AND publisher_id=$5`, target, version, now, taskID, publisherID); updateErr != nil {
		return updateErr
	} else if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrNotFound
	}
	payload, err := json.Marshal(map[string]any{"previousStatus": status, "status": target, "aggregateVersion": version})
	if err != nil {
		return err
	}
	eventID := workflowEventID(eventType, taskID, fmt.Sprintf("%d", version))
	if _, err = tx.ExecContext(ctx, `INSERT INTO domain_events (event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at) VALUES ($1,'task',$2,$3,$4,$5,$6)`, eventID, taskID, version, eventType, string(payload), now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events (event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at) VALUES ($1,$2,$3,'task',$4,$5,$6)`, eventID+"_audit", publisherID, eventType, taskID, string(payload), now); err != nil {
		return err
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

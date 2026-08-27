package orchestration

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

	"github.com/lib/pq"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalidInput
	}
	return &Store{db: db}, nil
}

func (store *Store) Task(ctx context.Context, publisherID, taskID string) (Task, error) {
	var value Task
	var deliveryFormat string
	var tools pq.StringArray
	err := store.db.QueryRowContext(ctx, `SELECT task.task_id,task.publisher_id,task.status,spec.content_hash,spec.title,spec.description,spec.expert_type,spec.language,spec.delivery_format,spec.allowed_tools,spec.deadline FROM tasks task JOIN task_spec_versions spec ON spec.task_id=task.task_id AND spec.version_no=task.current_spec_version WHERE task.task_id=$1 AND task.publisher_id=$2`, taskID, publisherID).Scan(&value.ID, &value.PublisherID, &value.Status, &value.SpecHash, &value.Title, &value.Description, &value.Category, &value.Language, &deliveryFormat, &tools, &value.Deadline)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	value.Deliverables = []string{deliveryFormat}
	value.AllowedTools = []string(tools)
	return value, err
}

func (store *Store) Agents(ctx context.Context) ([]Agent, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT agent_id,category,tags,capabilities FROM agents WHERE status='active' AND health='healthy' AND health_valid_until>statement_timestamp() ORDER BY agent_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []Agent{}
	for rows.Next() {
		var value Agent
		var tags pq.StringArray
		var raw string
		if err = rows.Scan(&value.AgentID, &value.Category, &tags, &raw); err != nil {
			return nil, err
		}
		value.Tags = []string(tags)
		value.Capabilities = parseCapabilities(raw)
		values = append(values, value)
	}
	return values, rows.Err()
}

func (store *Store) Save(ctx context.Context, publisherID, operationID, inputHash string, task Task, draft Draft) (Plan, bool, error) {
	now := time.Now().UTC()
	id := digest("task-orchestration-plan", task.ID, task.SpecHash, inputHash)
	rationale, _ := json.Marshal(draft.Rationale)
	steps, _ := json.Marshal(draft.Steps)
	result, err := store.db.ExecContext(ctx, `INSERT INTO task_orchestration_plans(plan_id,task_id,publisher_id,task_spec_hash,operation_id,input_hash,mode,summary,rationale,confidence,steps,model_version,graph_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT DO NOTHING`, id, task.ID, publisherID, task.SpecHash, operationID, inputHash, draft.Mode, draft.Summary, string(rationale), draft.Confidence, string(steps), draft.Model, draft.GraphVersion, now)
	if err != nil {
		return Plan{}, false, err
	}
	inserted, _ := result.RowsAffected()
	plan, err := store.read(ctx, `SELECT plan_id,task_id,task_spec_hash,mode,summary,rationale,confidence,steps,model_version,graph_version,created_at FROM task_orchestration_plans WHERE task_id=$1 AND publisher_id=$2 AND (operation_id=$3 OR input_hash=$4) ORDER BY (operation_id=$3) DESC LIMIT 1`, task.ID, publisherID, operationID, inputHash)
	return plan, inserted == 0, err
}

func (store *Store) Latest(ctx context.Context, publisherID, taskID string) (Plan, error) {
	return store.read(ctx, `SELECT plan_id,task_id,task_spec_hash,mode,summary,rationale,confidence,steps,model_version,graph_version,created_at FROM task_orchestration_plans WHERE task_id=$1 AND publisher_id=$2 ORDER BY created_at DESC LIMIT 1`, taskID, publisherID)
}
func (store *Store) ByOperation(ctx context.Context, publisherID, taskID, operationID string) (Plan, error) {
	return store.read(ctx, `SELECT plan_id,task_id,task_spec_hash,mode,summary,rationale,confidence,steps,model_version,graph_version,created_at FROM task_orchestration_plans WHERE task_id=$1 AND publisher_id=$2 AND operation_id=$3`, taskID, publisherID, operationID)
}
func (store *Store) read(ctx context.Context, query string, args ...any) (Plan, error) {
	var p Plan
	var rationale, steps []byte
	err := store.db.QueryRowContext(ctx, query, args...).Scan(&p.ID, &p.TaskID, &p.TaskSpecHash, &p.Mode, &p.Summary, &rationale, &p.Confidence, &steps, &p.Model, &p.GraphVersion, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if json.Unmarshal(rationale, &p.Rationale) != nil || json.Unmarshal(steps, &p.Steps) != nil {
		return Plan{}, ErrInvalidInput
	}
	return p, nil
}
func parseCapabilities(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) == nil {
		return values
	}
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
}
func digest(parts ...string) string {
	sum := sha256.New()
	for _, part := range parts {
		_, _ = sum.Write([]byte(fmt.Sprintf("%d:%s", len(part), part)))
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil))
}

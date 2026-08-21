//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	"github.com/example/agent-platform/engine/internal/execution"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresLogicalExecutionFencingCallbackReplayAndImmutability(t *testing.T) {
	baseURL := os.Getenv("ENGINE_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("ENGINE_TEST_POSTGRES_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("engine_t203_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", executionSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedExecutionDependencies(t, ctx, db)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	spec := integrationSpec()
	created, replay, err := store.GetOrCreate(ctx, spec)
	if err != nil || replay || created.Status != execution.ExecutionPending {
		t.Fatalf("create: execution=%#v replay=%v err=%v", created, replay, err)
	}
	replayed, replay, err := store.GetOrCreate(ctx, spec)
	if err != nil || !replay || !reflect.DeepEqual(replayed.Spec, created.Spec) {
		t.Fatalf("logical replay: execution=%#v replay=%v err=%v", replayed, replay, err)
	}
	changed := spec
	changed.CostCap = "101"
	if _, _, err = store.GetOrCreate(ctx, changed); !errors.Is(err, execution.ErrContentConflict) {
		t.Fatalf("changed idempotent content accepted: %v", err)
	}

	_, prepared, replay, err := store.PrepareAttempt(ctx, spec.LogicalExecutionID, 5*time.Minute)
	if err != nil || replay || prepared.Number != 1 {
		t.Fatalf("prepare: attempt=%#v replay=%v err=%v", prepared, replay, err)
	}
	lease := agent.CapacityLease{AgentID: spec.AgentID, ReservationID: prepared.ReservationID, FencingToken: 9, ExpiresAt: time.Now().UTC().Add(5 * time.Minute)}
	_, active, err := store.ActivateAttempt(ctx, spec.LogicalExecutionID, prepared.Number, lease, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "nonce-v1")
	if err != nil || active.FencingToken != 9 {
		t.Fatalf("activate: attempt=%#v err=%v", active, err)
	}
	if err = store.RecordDispatch(ctx, spec.LogicalExecutionID, active.Number); err != nil {
		t.Fatal(err)
	}
	callback := execution.Callback{ProtocolVersion: execution.ProtocolVersion, LogicalExecutionID: spec.LogicalExecutionID, AttemptID: active.AttemptID, AgentID: spec.AgentID, FencingToken: active.FencingToken, Status: execution.CallbackSucceeded, UsedCost: "40", ContentHash: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", DeliverableRef: "agent-artifact://result", Timestamp: time.Now().UTC(), Nonce: "not-persisted", KeyVersion: "agent-key-v1"}
	verified := execution.VerifiedCallback{Callback: callback, NonceHash: active.CallbackNonceHash, PayloadHash: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}
	result, err := store.ApplyCallback(ctx, verified)
	if err != nil || result.Outcome != execution.CallbackAccepted || result.Execution.Status != execution.ExecutionSucceeded {
		t.Fatalf("callback: result=%#v err=%v", result, err)
	}
	result, err = store.ApplyCallback(ctx, verified)
	if err != nil || !result.Replay || result.Execution.Status != execution.ExecutionSucceeded {
		t.Fatalf("callback replay: result=%#v err=%v", result, err)
	}
	verified.PayloadHash = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if _, err = store.ApplyCallback(ctx, verified); !errors.Is(err, execution.ErrContentConflict) {
		t.Fatalf("conflicting callback replay accepted: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE execution_attempts SET lease_expires_at=lease_expires_at+interval '1 hour' WHERE attempt_id=$1`, active.AttemptID); err == nil {
		t.Fatal("database allowed fencing lease mutation")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM execution_callback_events WHERE logical_execution_id=$1`, spec.LogicalExecutionID); err == nil {
		t.Fatal("database allowed callback evidence deletion")
	}
	var auditCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE resource_type='logical_execution' AND resource_id=$1 AND action='execution.callback.accepted'`, spec.LogicalExecutionID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("callback audit evidence count=%d err=%v", auditCount, err)
	}
}

func seedExecutionDependencies(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ('publisher'),('provider')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO agents (agent_id,owner_id,name,category,capabilities,languages,estimated_duration_seconds,controller_address,payout_address,max_concurrency,created_at,updated_at) VALUES ('agent-execution','provider','Agent','research','Research',ARRAY['zh-CN'],60,'0x1111111111111111111111111111111111111111','0x2222222222222222222222222222222222222222',1,$1,$1)`, now); err != nil {
		t.Fatal(err)
	}
	criteria := `[{"id":"quality","title":"Quality","description":"Complete","weight":100}]`
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,delivery_format,draft_acceptance,created_at,updated_at) VALUES ('task-execution','publisher','Title','Description','research','zh-CN',100,1000,100,$1,'markdown',$2,$3,$3)`, now.Add(time.Hour), criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO task_spec_versions (task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES ('task-execution',1,2,'sha256:1111111111111111111111111111111111111111111111111111111111111111','Title','Description','research','zh-CN',100,1000,100,$1,'{}','{}','{}','markdown',$2)`, now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
}

func integrationSpec() execution.Spec {
	return execution.Spec{LogicalExecutionID: "execution-pg-1", Stage: execution.StageOverview, TaskID: "task-execution", TaskSpecHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111", AgentID: "agent-execution", AgentEndpoint: "https://agent.example", ResponsibilityCode: "overview_candidate", CostCap: "100", ToolPolicy: execution.ToolPolicy{Mode: execution.ToolPolicyReadOnly, AllowedTools: []string{"read"}}, Deadline: time.Now().UTC().Add(30 * time.Minute), IdempotencyKey: "execution-pg-idem-1", Overview: &execution.OverviewBinding{MatchRevision: 1, AllocationID: "allocation-1", QuoteHash: "sha256:2222222222222222222222222222222222222222222222222222222222222222"}}
}

func executionSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	return databaseURL + separator + "search_path=" + url.QueryEscape(schema)
}

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/persistence"
	"github.com/lib/pq"
)

func TestPostgresMigrationsIdempotencyAndAtomicity(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t101_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()

	db, err := sql.Open("postgres", withSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	defer db.Close()
	assertScalar(t, db, `SHOW default_transaction_isolation`, "repeatable read")
	migrationErrors := make(chan error, 4)
	startMigrations := make(chan struct{})
	for range 4 {
		go func() {
			<-startMigrations
			migrationErrors <- ApplyMigrations(ctx, db)
		}()
	}
	close(startMigrations)
	for range 4 {
		if migrationErr := <-migrationErrors; migrationErr != nil {
			t.Fatalf("concurrent migration: %v", migrationErr)
		}
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE test_aggregates (aggregate_id text PRIMARY KEY, aggregate_version bigint NOT NULL, value text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO test_aggregates VALUES ('task-1', 1, 'original')`); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	request := persistence.Request{Scope: "tasks.update", Key: "key-1", RequestHash: "sha256:one"}
	response := json.RawMessage(`{"b": 2, "a": 1}`)
	var calls atomic.Int32
	work := func(ctx context.Context, tx persistence.Transaction) (persistence.Outcome, error) {
		calls.Add(1)
		affected, err := tx.Exec(ctx, `UPDATE test_aggregates SET aggregate_version = 2, value = 'updated' WHERE aggregate_id = 'task-1' AND aggregate_version = 1`)
		if err != nil {
			return persistence.Outcome{}, err
		}
		if affected != 1 {
			return persistence.Outcome{}, errors.New("stale aggregate version")
		}
		now := time.Now().UTC()
		return persistence.Outcome{
			Response:     response,
			DomainEvents: []persistence.DomainEvent{{ID: "event-1", AggregateType: "task", AggregateID: "task-1", AggregateVersion: 2, EventType: "task.updated", Payload: json.RawMessage(`{}`), OccurredAt: now}},
			AuditEvents:  []persistence.AuditEvent{{ID: "audit-1", ActorID: "user-1", Action: "task.update", ResourceType: "task", ResourceID: "task-1", Metadata: json.RawMessage(`{}`), OccurredAt: now}},
			Outbox:       []persistence.OutboxMessage{{ID: "message-1", DedupeKey: "task-1:2", Topic: "task-events", Payload: json.RawMessage(`{}`), AvailableAt: now}},
		}, nil
	}

	results := make([]persistence.Outcome, 2)
	replays := make([]bool, 2)
	errorsFound := make([]error, 2)
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results[index], replays[index], errorsFound[index] = store.Execute(ctx, request, work)
		}(index)
	}
	group.Wait()
	for _, executeErr := range errorsFound {
		if executeErr != nil {
			t.Fatal(executeErr)
		}
	}
	if calls.Load() != 1 || replays[0] == replays[1] {
		t.Fatalf("expected one execution and one replay: calls=%d replays=%v", calls.Load(), replays)
	}
	for _, result := range results {
		if string(result.Response) != string(response) {
			t.Fatalf("response bytes changed: %q", result.Response)
		}
	}
	if _, _, err = store.Execute(ctx, persistence.Request{Scope: request.Scope, Key: request.Key, RequestHash: "sha256:different"}, work); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("expected idempotency conflict, got %v", err)
	}

	rollbackErr := errors.New("force rollback")
	_, _, err = store.Execute(ctx, persistence.Request{Scope: "tasks.update", Key: "rollback", RequestHash: "sha256:rollback"}, func(ctx context.Context, tx persistence.Transaction) (persistence.Outcome, error) {
		if _, execErr := tx.Exec(ctx, `UPDATE test_aggregates SET value = 'must-rollback' WHERE aggregate_id = 'task-1'`); execErr != nil {
			return persistence.Outcome{}, execErr
		}
		now := time.Now().UTC()
		return persistence.Outcome{Response: json.RawMessage(`{}`), DomainEvents: []persistence.DomainEvent{{ID: "event-rollback", AggregateType: "task", AggregateID: "task-1", AggregateVersion: 3, EventType: "task.updated", Payload: json.RawMessage(`{}`), OccurredAt: now}}}, rollbackErr
	})
	if !errors.Is(err, rollbackErr) {
		t.Fatalf("expected rollback error, got %v", err)
	}
	assertScalar(t, db, `SELECT value FROM test_aggregates WHERE aggregate_id = 'task-1'`, "updated")
	assertScalar(t, db, `SELECT count(*)::text FROM idempotency_records`, "1")
	assertScalar(t, db, `SELECT count(*)::text FROM domain_events`, "1")
	assertScalar(t, db, `SELECT count(*)::text FROM audit_events`, "1")
	assertScalar(t, db, `SELECT count(*)::text FROM outbox_messages`, "1")

	db.SetMaxOpenConns(1)
	func() {
		defer func() { _ = recover() }()
		_, _, _ = store.Execute(ctx, persistence.Request{Scope: "panic", Key: "panic", RequestHash: "sha256:panic"}, func(context.Context, persistence.Transaction) (persistence.Outcome, error) { panic("test panic") })
	}()
	if err = db.PingContext(ctx); err != nil {
		t.Fatalf("panic leaked transaction connection or advisory lock: %v", err)
	}
}

func TestEscrowV3MigrationPreservesConfirmedLegacyFunding(t *testing.T) {
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
	schema := fmt.Sprintf("engine_v3_history_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", withSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	names, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	for _, name := range names {
		if name >= "migrations/000028_escrow_v3.up.sql" {
			break
		}
		contents, readErr := migrationFiles.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err = db.ExecContext(ctx, string(contents)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO users(user_id) VALUES('legacy-publisher')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, taskID := range []string{"legacy-confirmed", "legacy-pending"} {
		if _, err = db.ExecContext(ctx, `INSERT INTO tasks(task_id,publisher_id,status,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,draft_acceptance,created_at,updated_at) VALUES($1,'legacy-publisher','draft','Legacy','Legacy','research','en',10,90,5,$2,'{}','{}','{}','json','[{"id":"quality","title":"Quality","description":"Accurate","weight":100}]',$3,$3)`, taskID, now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO task_spec_versions(task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES($1,1,2,$2,'Legacy','Legacy','research','en',10,90,5,$3,'{}','{}','{}','json',$4)`, taskID, "sha256:"+strings.Repeat("1", 64), now.Add(time.Hour), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO acceptance_versions(task_id,version_no,task_aggregate_version,content_hash,criteria,total_weight,created_at) VALUES($1,1,2,$2,'[{"id":"quality","title":"Quality","description":"Accurate","weight":100}]',100,$3)`, taskID, "sha256:"+strings.Repeat("2", 64), now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `UPDATE tasks SET status='pending_escrow',current_spec_version=1,current_acceptance_version=1,published_at=$2,aggregate_version=2,updated_at=$2 WHERE task_id=$1`, taskID, now); err != nil {
			t.Fatal(err)
		}
	}
	for index, value := range []struct{ taskID, state string }{{"legacy-confirmed", "confirmed"}, {"legacy-pending", "submitted"}} {
		if _, err = db.ExecContext(ctx, `INSERT INTO task_funding_intents(intent_id,task_id,publisher_id,publisher_wallet,idempotency_key,request_hash,chain_id,contract_address,chain_task_id,overview_amount,formal_amount,external_cost_amount,total_amount,status,transaction_hash,aggregate_version,created_at,updated_at) VALUES($1,$2,'legacy-publisher','0x1111111111111111111111111111111111111111',$3,$4,31337,'0x2222222222222222222222222222222222222222',$5,10,90,5,105,$6,$7,1,$8,$8)`, "sha256:"+strings.Repeat(fmt.Sprintf("%x", index+3), 64), value.taskID, "legacy-key-"+value.taskID, "sha256:"+strings.Repeat(fmt.Sprintf("%x", index+5), 64), "0x"+strings.Repeat(fmt.Sprintf("%x", index+7), 64), value.state, "0x"+strings.Repeat(fmt.Sprintf("%x", index+9), 64), now); err != nil {
			t.Fatal(err)
		}
	}
	v3, err := migrationFiles.ReadFile("migrations/000028_escrow_v3.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, string(v3)); err != nil {
		t.Fatal(err)
	}
	var confirmedStatus, confirmedTotal string
	var confirmedReason sql.NullString
	if err = db.QueryRowContext(ctx, `SELECT status,total_amount::text,failure_reason_code FROM task_funding_intents WHERE task_id='legacy-confirmed'`).Scan(&confirmedStatus, &confirmedTotal, &confirmedReason); err != nil || confirmedStatus != "confirmed" || confirmedTotal != "105" || confirmedReason.Valid {
		t.Fatalf("confirmed legacy history changed: status=%s total=%s reason=%v err=%v", confirmedStatus, confirmedTotal, confirmedReason, err)
	}
	var pendingStatus, pendingTotal, taskStatus string
	if err = db.QueryRowContext(ctx, `SELECT intent.status,intent.total_amount::text,task.status FROM task_funding_intents intent JOIN tasks task ON task.task_id=intent.task_id WHERE intent.task_id='legacy-pending'`).Scan(&pendingStatus, &pendingTotal, &taskStatus); err != nil || pendingStatus != "failed" || pendingTotal != "105" || taskStatus != "funding_configuration_invalid" {
		t.Fatalf("legacy pending isolation: intent=%s total=%s task=%s err=%v", pendingStatus, pendingTotal, taskStatus, err)
	}
	assertScalar(t, db, `SELECT count(*)::text FROM task_funding_attempts WHERE intent_id=(SELECT intent_id FROM task_funding_intents WHERE task_id='legacy-confirmed') AND state='canonical_confirmed'`, "1")
	assertScalar(t, db, `SELECT count(*)::text FROM task_funding_intent_events WHERE intent_id=(SELECT intent_id FROM task_funding_intents WHERE task_id='legacy-pending') AND state='failed' AND reason_code='escrow_v3_migration_required'`, "1")
	assertScalar(t, db, `SELECT count(*)::text FROM domain_events WHERE aggregate_id='legacy-pending' AND event_type='task.funding_configuration_invalid'`, "1")
	assertScalar(t, db, `SELECT count(*)::text FROM audit_events WHERE resource_id='legacy-pending' AND action='task.funding_configuration_invalid'`, "1")
}

func withSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		query.Set("binary_parameters", "yes")
		query.Set("options", "-c default_transaction_isolation=repeatable\\ read")
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	return databaseURL + separator + "search_path=" + url.QueryEscape(schema) + "&binary_parameters=yes&options=" + url.QueryEscape("-c default_transaction_isolation=repeatable\\ read")
}

func assertScalar(t *testing.T, db *sql.DB, query, expected string) {
	t.Helper()
	var actual string
	if err := db.QueryRow(query).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q: expected %q, got %q", query, expected, actual)
	}
}

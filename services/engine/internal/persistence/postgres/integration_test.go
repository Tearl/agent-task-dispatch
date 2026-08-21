//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
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

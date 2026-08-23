//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/outbox"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresOutboxClaimsOnlyRegisteredTopicsAndFencesLeases(t *testing.T) {
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
	schema := fmt.Sprintf("engine_outbox_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", outboxSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO outbox_messages (message_id,dedupe_key,topic,payload,available_at) VALUES
('formal-1','execution-1','agent.execution.formal.requested','{}',clock_timestamp()),
('event-1','task-1','task-events','{}',clock_timestamp())`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := store.Claim(ctx, "worker-a", []string{"agent.execution.formal.requested"}, 10, time.Minute)
	if err != nil || len(messages) != 1 || messages[0].ID != "formal-1" || messages[0].Attempts != 1 {
		t.Fatalf("first claim: messages=%#v err=%v", messages, err)
	}
	if replay, claimErr := store.Claim(ctx, "worker-b", []string{"agent.execution.formal.requested"}, 10, time.Minute); claimErr != nil || len(replay) != 0 {
		t.Fatalf("active lease was stolen: messages=%#v err=%v", replay, claimErr)
	}
	if err = store.Complete(ctx, "worker-b", "formal-1"); !errors.Is(err, outbox.ErrLeaseLost) {
		t.Fatalf("wrong worker completed lease: %v", err)
	}
	if err = store.Retry(ctx, "worker-a", "formal-1", "agent_unavailable", time.Now().UTC().Add(-time.Second), false); err != nil {
		t.Fatal(err)
	}
	messages, err = store.Claim(ctx, "worker-b", []string{"agent.execution.formal.requested"}, 10, time.Minute)
	if err != nil || len(messages) != 1 || messages[0].Attempts != 2 {
		t.Fatalf("retry claim: messages=%#v err=%v", messages, err)
	}
	if err = store.Retry(ctx, "worker-b", "formal-1", "invalid_execution_spec", time.Now().UTC(), true); err != nil {
		t.Fatal(err)
	}
	if messages, err = store.Claim(ctx, "worker-c", []string{"agent.execution.formal.requested"}, 10, time.Minute); err != nil || len(messages) != 0 {
		t.Fatalf("dead letter was reclaimed: messages=%#v err=%v", messages, err)
	}
	var dead, taskPending bool
	if err = db.QueryRowContext(ctx, `SELECT dead_lettered_at IS NOT NULL FROM outbox_messages WHERE message_id='formal-1'`).Scan(&dead); err != nil || !dead {
		t.Fatalf("dead-letter state: dead=%v err=%v", dead, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT published_at IS NULL AND dead_lettered_at IS NULL FROM outbox_messages WHERE message_id='event-1'`).Scan(&taskPending); err != nil || !taskPending {
		t.Fatalf("unregistered topic changed: pending=%v err=%v", taskPending, err)
	}
}

func outboxSearchPath(databaseURL, schema string) string {
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

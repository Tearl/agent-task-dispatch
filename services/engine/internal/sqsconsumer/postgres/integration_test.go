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

	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/example/agent-platform/engine/internal/sqsconsumer"
	"github.com/lib/pq"
)

func TestProcessedMessageLedgerDetectsReplayAndIdentityConflict(t *testing.T) {
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
	schema := fmt.Sprintf("engine_sqs_ledger_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", ledgerSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	value := sqsconsumer.Consumption{ConsumerName: "formal-execution-v1", MessageID: "message-1", Topic: "agent.execution.formal.requested", DedupeKey: "logical-1", EnvelopeHash: "sha256:" + strings.Repeat("a", 64), BrokerMessageID: "broker-1"}
	if _, found, lookupErr := store.Lookup(ctx, value.ConsumerName, value.MessageID); lookupErr != nil || found {
		t.Fatalf("unexpected initial lookup: found=%v err=%v", found, lookupErr)
	}
	if replay, completeErr := store.Complete(ctx, value); completeErr != nil || replay {
		t.Fatalf("unexpected first completion: replay=%v err=%v", replay, completeErr)
	}
	value.BrokerMessageID = "broker-redelivery"
	if replay, completeErr := store.Complete(ctx, value); completeErr != nil || !replay {
		t.Fatalf("same envelope was not replayed: replay=%v err=%v", replay, completeErr)
	}
	value.EnvelopeHash = "sha256:" + strings.Repeat("b", 64)
	if _, completeErr := store.Complete(ctx, value); !errors.Is(completeErr, sqsconsumer.ErrLedgerConflict) {
		t.Fatalf("identity conflict was accepted: %v", completeErr)
	}
}

func ledgerSearchPath(databaseURL, schema string) string {
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

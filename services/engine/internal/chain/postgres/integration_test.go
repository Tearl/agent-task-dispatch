//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresProjectionReplayRewindAndReconciliationEvidence(t *testing.T) {
	baseURL := os.Getenv("ENGINE_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("ENGINE_TEST_POSTGRES_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("engine_t402_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", projectionSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	store, _ := NewStore(db)
	scope := chainprojection.Scope{ChainID: "31337", Contract: "0x0000000000000000000000000000000000001234", StartBlock: 1, Confirmations: 2, MaxReorgDepth: 10}
	block1 := chainprojection.Block{Number: 1, Hash: integrationHash(1), ParentHash: integrationHash(0), Timestamp: time.Unix(1, 0).UTC()}
	if err = store.ApplyBlock(ctx, scope, block1, nil); err != nil {
		t.Fatal(err)
	}
	activeStoredScope := chainprojection.Scope{ChainID: scope.ChainID, Contract: "0x000000000000000000000000000000000000abcd", StartBlock: 10, Confirmations: 5, MaxReorgDepth: 20}
	resolver := "0x0000000000000000000000000000000000009999"
	if err = store.RegisterDeployment(ctx, Deployment{ChainID: scope.ChainID, Contract: scope.Contract, Asset: "evm:31337/native", DisputeResolver: resolver}); err != nil {
		t.Fatal(err)
	}
	if err = store.RegisterDeployment(ctx, Deployment{ChainID: activeStoredScope.ChainID, Contract: activeStoredScope.Contract, Asset: "evm:31337/erc20:0x0000000000000000000000000000000000005678", DisputeResolver: resolver, ActiveForNewTasks: true}); err != nil {
		t.Fatal(err)
	}
	if err = store.ApplyBlock(ctx, activeStoredScope, chainprojection.Block{Number: 10, Hash: integrationHash(10), ParentHash: integrationHash(9), Timestamp: time.Unix(10, 0).UTC()}, nil); err != nil {
		t.Fatal(err)
	}
	activeScope := activeStoredScope
	activeScope.Contract = "0x000000000000000000000000000000000000AbCd"
	persisted, err := store.PersistedProjectionScopes(ctx, activeScope)
	if err != nil || len(persisted) != 1 || persisted[0].Contract != scope.Contract || persisted[0].Confirmations != activeScope.Confirmations || persisted[0].MaxReorgDepth != activeScope.MaxReorgDepth {
		t.Fatalf("persisted deployment scopes=%#v err=%v", persisted, err)
	}
	txHash := integrationHash(402)
	block2 := chainprojection.Block{Number: 2, Hash: integrationHash(2), ParentHash: block1.Hash, Timestamp: time.Unix(2, 0).UTC(), Transactions: []chainprojection.Transaction{{Hash: txHash, To: scope.Contract, Input: chainprojection.SelectionCallSelector(), Status: chainprojection.TxFailed}}}
	if err = store.ApplyBlock(ctx, scope, block2, nil); err != nil {
		t.Fatal(err)
	}
	result, found, err := store.SelectionResult(ctx, scope, txHash)
	if err != nil || !found || result.Status != "failed" {
		t.Fatalf("failed result: %#v found=%v err=%v", result, found, err)
	}
	if err = store.Rewind(ctx, scope, 1, "chain_reorganization"); err != nil {
		t.Fatal(err)
	}
	if _, found, err = store.SelectionResult(ctx, scope, txHash); err != nil || found {
		t.Fatalf("orphaned result stayed authoritative: found=%v err=%v", found, err)
	}
	if err = store.RecordReconciliation(ctx, chainprojection.ReconciliationRun{ID: "sha256:" + fmt.Sprintf("%064x", 402), Scope: scope, SafeHeight: 1, Status: "difference_detected", Differences: []chainprojection.Difference{{Category: "assignment", ResourceID: "task", ExpectedValue: "a", ObservedValue: "b", Severity: "critical"}}, StartedAt: time.Unix(3, 0).UTC(), FinishedAt: time.Unix(3, 0).UTC()}); err != nil {
		t.Fatal(err)
	}
	var orphaned, differences int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM chain_block_states WHERE state='orphaned'`).Scan(&orphaned); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM chain_reconciliation_differences`).Scan(&differences); err != nil {
		t.Fatal(err)
	}
	if orphaned != 1 || differences != 1 {
		t.Fatalf("audit evidence: orphaned=%d differences=%d", orphaned, differences)
	}
}

func projectionSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return databaseURL + "?search_path=" + url.QueryEscape(schema)
}

func integrationHash(value int) string { return "0x" + fmt.Sprintf("%064x", value) }

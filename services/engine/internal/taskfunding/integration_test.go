//go:build integration

package taskfunding

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	chainpostgres "github.com/example/agent-platform/engine/internal/chain/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	taskpostgres "github.com/example/agent-platform/engine/internal/task/postgres"
	"github.com/lib/pq"
)

func TestFundingIntentProjectsCanonicalDepositAndReversesReorg(t *testing.T) {
	baseURL := os.Getenv("ENGINE_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("ENGINE_TEST_POSTGRES_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("engine_funding_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", fundingSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	const userID = "publisher-funding"
	if _, err = db.ExecContext(ctx, `INSERT INTO users(user_id) VALUES($1)`, userID); err != nil {
		t.Fatal(err)
	}
	contract := "0x0000000000000000000000000000000000001234"
	service, err := NewService(db, Config{ChainID: "31337", ContractAddress: contract, Asset: "evm:31337/native"})
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{UserID: userID, Wallet: "0x1111111111111111111111111111111111111111", Roles: []string{"publisher"}}
	taskStore, err := taskpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	taskService, err := enginetask.NewService(taskStore)
	if err != nil {
		t.Fatal(err)
	}
	draft := enginetask.DraftInput{Title: "Funded task", Description: "Description", ExpertType: "research", Language: "en", OverviewBudget: "10", FormalBudget: "90", ExternalCostCap: "5", Deadline: time.Now().UTC().Add(time.Hour), Inputs: []string{"input"}, AllowedTools: []string{"read"}, Exclusions: []string{"write"}, DeliveryFormat: "json", AcceptanceCriteria: []enginetask.AcceptanceCriterion{{ID: "quality", Title: "Quality", Description: "Accurate", Weight: 100}}}
	task, replay, err := taskService.Create(ctx, session, "create-task", draft)
	if err != nil || replay {
		t.Fatalf("create: %#v replay=%v err=%v", task, replay, err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-task", task.ID, enginetask.PublishInput{ExpectedVersion: task.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	intent, replay, err := service.Prepare(ctx, session, "funding-key", task.ID)
	if err != nil || replay || intent.TotalAmount != "105" || intent.Status != "prepared" {
		t.Fatalf("prepare: %#v replay=%v err=%v", intent, replay, err)
	}
	txHash := "0x" + strings.Repeat("3", 64)
	intent, err = service.Submit(ctx, session, task.ID, intent.ID, SubmitInput{TransactionHash: txHash, ExpectedVersion: intent.AggregateVersion})
	if err != nil || intent.Status != "submitted" {
		t.Fatalf("submit: %#v err=%v", intent, err)
	}
	store, err := chainpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	scope := chainprojection.Scope{ChainID: "31337", Contract: contract, StartBlock: 1, Confirmations: 1, MaxReorgDepth: 16}
	block := chainprojection.Block{Number: 1, Hash: "0x" + strings.Repeat("1", 64), ParentHash: "0x" + strings.Repeat("0", 64), Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: txHash, To: contract, Status: chainprojection.TxSucceeded, Input: "0x"}}}
	event := chainprojection.Event{ID: "sha256:" + strings.Repeat("2", 64), Type: chainprojection.EventTaskCreated, TaskID: intent.ChainTaskID, BlockNumber: 1, BlockHash: block.Hash, TransactionHash: txHash, LogIndex: 0, Payload: map[string]any{"publisher": session.Wallet, "amount": "105"}}
	if err = store.ApplyBlock(ctx, scope, block, []chainprojection.Event{event}); err != nil {
		t.Fatal(err)
	}
	var status, discovery, formal string
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT balance::text FROM fund_accounts WHERE task_id=$1 AND account_type='discovery_pool'`, task.ID).Scan(&discovery); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT balance::text FROM fund_accounts WHERE task_id=$1 AND account_type='formal_escrow'`, task.ID).Scan(&formal); err != nil {
		t.Fatal(err)
	}
	if status != "escrowed" || discovery != "15" || formal != "90" {
		t.Fatalf("projection status=%s discovery=%s formal=%s", status, discovery, formal)
	}
	if err = store.Rewind(ctx, scope, 0, "test_reorg"); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil || status != "pending_escrow" {
		t.Fatalf("rewind status=%s err=%v", status, err)
	}
}

func fundingSearchPath(raw, schema string) string {
	parsed, _ := url.Parse(raw)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

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
	assetAddress := "0x0000000000000000000000000000000000005678"
	store, err := chainpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	scope := chainprojection.Scope{ChainID: "31337", Contract: contract, StartBlock: 1, Confirmations: 1, MaxReorgDepth: 16}
	service, err := NewService(db, Config{ChainID: "31337", ContractAddress: contract, AssetAddress: assetAddress, Asset: "evm:31337/erc20:" + assetAddress}, func(ctx context.Context, transactionHash string) error {
		return store.ReconcileFundingAttempt(ctx, scope, transactionHash)
	})
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
	if err != nil || replay || intent.TotalAmount != "90" || intent.FormalBudget != "90" || intent.Status != "prepared" {
		t.Fatalf("prepare: %#v replay=%v err=%v", intent, replay, err)
	}
	txHash := "0x" + strings.Repeat("3", 64)
	intent, err = service.Submit(ctx, session, task.ID, intent.ID, SubmitInput{TransactionHash: txHash, ExpectedVersion: intent.AggregateVersion})
	if err != nil || intent.Status != "submitted" {
		t.Fatalf("submit: %#v err=%v", intent, err)
	}
	block := chainprojection.Block{Number: 1, Hash: "0x" + strings.Repeat("1", 64), ParentHash: "0x" + strings.Repeat("0", 64), Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: txHash, To: contract, Status: chainprojection.TxSucceeded, Input: "0x"}}}
	event := chainprojection.Event{ID: "sha256:" + strings.Repeat("2", 64), Type: chainprojection.EventTaskCreated, TaskID: intent.ChainTaskID, BlockNumber: 1, BlockHash: block.Hash, TransactionHash: txHash, LogIndex: 0, Payload: map[string]any{"publisher": session.Wallet, "amount": "90"}}
	if err = store.ApplyBlock(ctx, scope, block, []chainprojection.Event{event}); err != nil {
		t.Fatal(err)
	}
	var status, formal string
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT balance::text FROM fund_accounts WHERE task_id=$1 AND account_type='formal_escrow'`, task.ID).Scan(&formal); err != nil {
		t.Fatal(err)
	}
	if status != "escrowed" || formal != "90" {
		t.Fatalf("projection status=%s formal=%s", status, formal)
	}
	var discoveryCount int
	var discoveryBalance string
	if err = db.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(balance),0)::text FROM fund_accounts WHERE task_id=$1 AND account_type='discovery_pool'`, task.ID).Scan(&discoveryCount, &discoveryBalance); err != nil || discoveryCount != 0 || discoveryBalance != "0" {
		t.Fatalf("formal-only funding must not credit discovery count=%d balance=%s err=%v", discoveryCount, discoveryBalance, err)
	}
	if err = store.Rewind(ctx, scope, 0, "test_reorg"); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil || status != "pending_escrow" {
		t.Fatalf("rewind status=%s err=%v", status, err)
	}
	orphanedTask, err := taskService.Get(ctx, session, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	deletion, replay, err := taskService.RequestDelete(ctx, session, "delete-orphaned-funding", task.ID, enginetask.DeleteInput{ExpectedVersion: orphanedTask.AggregateVersion})
	if err != nil || replay || !deletion.RefundRequired || deletion.Status != "pending_escrow" {
		t.Fatalf("orphaned funding deletion must remain recoverable: deletion=%#v replay=%v err=%v", deletion, replay, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil || status != "pending_escrow" {
		t.Fatalf("orphaned funding deletion hard-deleted task status=%s err=%v", status, err)
	}
	if err = store.ApplyBlock(ctx, scope, block, []chainprojection.Event{event}); err != nil {
		t.Fatalf("recanonicalization after deletion request must advance: %v", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil || status != "escrowed" {
		t.Fatalf("recanonical status=%s err=%v", status, err)
	}
	var epochs, activeEpochs int
	if err = db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE orphaned_at IS NULL) FROM task_funding_canonicalizations WHERE intent_id=$1`, intent.ID).Scan(&epochs, &activeEpochs); err != nil || epochs != 2 || activeEpochs != 1 {
		t.Fatalf("canonicalization epochs=%d active=%d err=%v", epochs, activeEpochs, err)
	}

	retainedDraft := draft
	retainedDraft.Title = "Retained occurrence task"
	retainedTask, _, err := taskService.Create(ctx, session, "create-retained-task", retainedDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-retained-task", retainedTask.ID, enginetask.PublishInput{ExpectedVersion: retainedTask.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	retainedIntent, _, err := service.Prepare(ctx, session, "retained-funding", retainedTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	retainedHash := "0x" + strings.Repeat("4", 64)
	block2 := chainprojection.Block{Number: 2, Hash: "0x" + strings.Repeat("5", 64), ParentHash: block.Hash, Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: retainedHash, To: contract, Status: chainprojection.TxSucceeded, Input: "0x"}}}
	retainedEvent := chainprojection.Event{ID: "sha256:" + strings.Repeat("6", 64), Type: chainprojection.EventTaskCreated, TaskID: retainedIntent.ChainTaskID, BlockNumber: 2, BlockHash: block2.Hash, TransactionHash: retainedHash, LogIndex: 0, Payload: map[string]any{"publisher": session.Wallet, "amount": "90"}}
	if err = store.ApplyBlock(ctx, scope, block2, []chainprojection.Event{retainedEvent}); err != nil {
		t.Fatal(err)
	}
	retainedIntent, err = service.Submit(ctx, session, retainedTask.ID, retainedIntent.ID, SubmitInput{TransactionHash: retainedHash, ExpectedVersion: retainedIntent.AggregateVersion})
	if err != nil || retainedIntent.Status != "confirmed" || len(retainedIntent.Attempts) != 1 || retainedIntent.Attempts[0].State != "canonical_confirmed" {
		t.Fatalf("retained occurrence reconciliation: intent=%#v err=%v", retainedIntent, err)
	}

	retryDraft := draft
	retryDraft.Title = "Failed replacement task"
	retryTask, _, err := taskService.Create(ctx, session, "create-retry-task", retryDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-retry-task", retryTask.ID, enginetask.PublishInput{ExpectedVersion: retryTask.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	retryIntent, _, err := service.Prepare(ctx, session, "retry-funding", retryTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedHash := "0x" + strings.Repeat("7", 64)
	retryIntent, err = service.Submit(ctx, session, retryTask.ID, retryIntent.ID, SubmitInput{TransactionHash: failedHash, ExpectedVersion: retryIntent.AggregateVersion})
	if err != nil {
		t.Fatal(err)
	}
	block3 := chainprojection.Block{Number: 3, Hash: "0x" + strings.Repeat("8", 64), ParentHash: block2.Hash, Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: failedHash, To: contract, Status: chainprojection.TxFailed, Input: "0x"}}}
	if err = store.ApplyBlock(ctx, scope, block3, nil); err != nil {
		t.Fatal(err)
	}
	retryIntent, err = service.Get(ctx, session, retryTask.ID)
	if err != nil || retryIntent.Status != "failed" || len(retryIntent.Attempts) != 1 || retryIntent.Attempts[0].State != "observed_failed" {
		t.Fatalf("failed attempt projection: intent=%#v err=%v", retryIntent, err)
	}
	replacementHash := "0x" + strings.Repeat("9", 64)
	retryIntent, err = service.Submit(ctx, session, retryTask.ID, retryIntent.ID, SubmitInput{TransactionHash: replacementHash, ExpectedVersion: retryIntent.AggregateVersion})
	if err != nil || retryIntent.Status != "submitted" || len(retryIntent.Attempts) != 2 || retryIntent.Attempts[0].State != "superseded" {
		t.Fatalf("replacement attempt: intent=%#v err=%v", retryIntent, err)
	}

	lateDraft := draft
	lateDraft.Title = "Late confirmation task"
	lateDraft.Deadline = time.Now().UTC().Add(2 * time.Second)
	lateTask, _, err := taskService.Create(ctx, session, "create-late-task", lateDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-late-task", lateTask.ID, enginetask.PublishInput{ExpectedVersion: lateTask.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	lateIntent, _, err := service.Prepare(ctx, session, "late-funding", lateTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	lateHash := "0x" + strings.Repeat("a", 64)
	lateIntent, err = service.Submit(ctx, session, lateTask.ID, lateIntent.ID, SubmitInput{TransactionHash: lateHash, ExpectedVersion: lateIntent.AggregateVersion})
	if err != nil {
		t.Fatal(err)
	}
	if delay := time.Until(lateDraft.Deadline.Add(100 * time.Millisecond)); delay > 0 {
		time.Sleep(delay)
	}
	block4 := chainprojection.Block{Number: 4, Hash: "0x" + strings.Repeat("b", 64), ParentHash: block3.Hash, Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: lateHash, To: contract, Status: chainprojection.TxSucceeded, Input: "0x"}}}
	lateEvent := chainprojection.Event{ID: "sha256:" + strings.Repeat("c", 64), Type: chainprojection.EventTaskCreated, TaskID: lateIntent.ChainTaskID, BlockNumber: 4, BlockHash: block4.Hash, TransactionHash: lateHash, LogIndex: 0, Payload: map[string]any{"publisher": session.Wallet, "amount": "90"}}
	if err = store.ApplyBlock(ctx, scope, block4, []chainprojection.Event{lateEvent}); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, lateTask.ID).Scan(&status); err != nil || status != "funding_refund_pending" {
		t.Fatalf("late confirmation status=%s err=%v", status, err)
	}

	retainedFailureDraft := draft
	retainedFailureDraft.Title = "Retained failed occurrence task"
	retainedFailureTask, _, err := taskService.Create(ctx, session, "create-retained-failure-task", retainedFailureDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-retained-failure-task", retainedFailureTask.ID, enginetask.PublishInput{ExpectedVersion: retainedFailureTask.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	retainedFailureIntent, _, err := service.Prepare(ctx, session, "retained-failure-funding", retainedFailureTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	retainedFailureHash := "0x" + strings.Repeat("d", 64)
	block5 := chainprojection.Block{Number: 5, Hash: "0x" + strings.Repeat("e", 64), ParentHash: block4.Hash, Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: retainedFailureHash, To: contract, Status: chainprojection.TxFailed, Input: "0x"}}}
	if err = store.ApplyBlock(ctx, scope, block5, nil); err != nil {
		t.Fatal(err)
	}
	retainedFailureIntent, err = service.Submit(ctx, session, retainedFailureTask.ID, retainedFailureIntent.ID, SubmitInput{TransactionHash: retainedFailureHash, ExpectedVersion: retainedFailureIntent.AggregateVersion})
	if err != nil || retainedFailureIntent.Status != "failed" || len(retainedFailureIntent.Attempts) != 1 || retainedFailureIntent.Attempts[0].State != "observed_failed" {
		t.Fatalf("retained failed occurrence reconciliation: intent=%#v err=%v", retainedFailureIntent, err)
	}
	failedVersion := retainedFailureIntent.AggregateVersion
	var failedEventsBefore int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM task_funding_intent_events WHERE intent_id=$1 AND state='failed'`, retainedFailureIntent.ID).Scan(&failedEventsBefore); err != nil {
		t.Fatal(err)
	}
	retainedFailureIntent, err = service.Submit(ctx, session, retainedFailureTask.ID, retainedFailureIntent.ID, SubmitInput{TransactionHash: retainedFailureHash, ExpectedVersion: failedVersion})
	var failedEventsAfter int
	if countErr := db.QueryRowContext(ctx, `SELECT count(*) FROM task_funding_intent_events WHERE intent_id=$1 AND state='failed'`, retainedFailureIntent.ID).Scan(&failedEventsAfter); countErr != nil {
		t.Fatal(countErr)
	}
	if err != nil || retainedFailureIntent.AggregateVersion != failedVersion || failedEventsAfter != failedEventsBefore {
		t.Fatalf("failed occurrence replay changed state: intent=%#v events=%d/%d err=%v", retainedFailureIntent, failedEventsBefore, failedEventsAfter, err)
	}

	// A funding occurrence can be reorganized after the task has already
	// progressed. Failed occurrences must also be rewound rather than remaining
	// authoritative forever.
	if _, err = db.ExecContext(ctx, `UPDATE tasks SET status='matching' WHERE task_id=$1`, task.ID); err != nil {
		t.Fatal(err)
	}
	if err = store.Rewind(ctx, scope, 0, "test_full_reorg"); err != nil {
		t.Fatal(err)
	}
	retainedFailureIntent, err = service.Get(ctx, session, retainedFailureTask.ID)
	if err != nil || retainedFailureIntent.Status != "orphaned" || len(retainedFailureIntent.Attempts) != 1 || retainedFailureIntent.Attempts[0].State != "canonical_orphaned" {
		t.Fatalf("failed occurrence rewind: intent=%#v err=%v", retainedFailureIntent, err)
	}
	orphanedFailureTask, err := taskService.Get(ctx, session, retainedFailureTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	failedDeletion, replay, err := taskService.RequestDelete(ctx, session, "delete-orphaned-failure", retainedFailureTask.ID, enginetask.DeleteInput{ExpectedVersion: orphanedFailureTask.AggregateVersion})
	if err != nil || replay || failedDeletion.RefundRequired || failedDeletion.Status != "cancelled" {
		t.Fatalf("orphaned failed attempt must delete without refund: deletion=%#v replay=%v err=%v", failedDeletion, replay, err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil || status != "chain_reorg_pending" {
		t.Fatalf("progressed funding rewind status=%s err=%v", status, err)
	}
	if err = store.ApplyBlock(ctx, scope, block, []chainprojection.Event{event}); err != nil {
		t.Fatalf("recanonicalized progressed funding must not stall projection: %v", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, task.ID).Scan(&status); err != nil || status != "chain_reorg_pending" {
		t.Fatalf("recanonicalized progressed task escaped quarantine status=%s err=%v", status, err)
	}

	legacyIntentID := "sha256:" + strings.Repeat("f", 64)
	legacyContract := "0x0000000000000000000000000000000000004321"
	legacyHash := "0x" + strings.Repeat("6", 64)
	legacyDraft := draft
	legacyDraft.Title = "Legacy confirmed reorg task"
	legacyTask, _, err := taskService.Create(ctx, session, "create-legacy-reorg-task", legacyDraft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-legacy-reorg-task", legacyTask.ID, enginetask.PublishInput{ExpectedVersion: legacyTask.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO task_funding_intents(intent_id,idempotency_key,request_hash,task_id,publisher_id,publisher_wallet,chain_id,contract_address,chain_task_id,overview_amount,formal_amount,external_cost_amount,total_amount,status,transaction_hash,aggregate_version,created_at,updated_at)
	VALUES($1,'legacy-reorg',$8,$2,$3,$4,31337,$5,$6,0,90,0,90,'confirmed',$7,1,clock_timestamp(),clock_timestamp())`, legacyIntentID, legacyTask.ID, userID, session.Wallet, legacyContract, "0x"+strings.Repeat("f", 64), legacyHash, "sha256:"+strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE task_funding_intents SET status='orphaned',aggregate_version=2 WHERE intent_id=$1`, legacyIntentID); err != nil {
		t.Fatalf("legacy confirmed intent must permit orphaning after a reorg: %v", err)
	}
	legacyAttemptID := "sha256:" + strings.Repeat("a", 64)
	if _, err = db.ExecContext(ctx, `INSERT INTO task_funding_attempts(attempt_id,intent_id,chain_id,contract_address,transaction_hash,state,created_at,updated_at) VALUES($1,$2,31337,$3,$4,'canonical_orphaned',clock_timestamp(),clock_timestamp())`, legacyAttemptID, legacyIntentID, legacyContract, legacyHash); err != nil {
		t.Fatal(err)
	}
	if err = store.RegisterDeployment(ctx, chainpostgres.Deployment{ChainID: "31337", Contract: legacyContract, Asset: "evm:31337/native", DisputeResolver: "0x0000000000000000000000000000000000009999"}); err != nil {
		t.Fatal(err)
	}
	legacyScope := chainprojection.Scope{ChainID: "31337", Contract: legacyContract, StartBlock: 1, Confirmations: 1, MaxReorgDepth: 16}
	legacyBlock := chainprojection.Block{Number: 1, Hash: "0x" + strings.Repeat("c", 64), ParentHash: "0x" + strings.Repeat("0", 64), Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: legacyHash, To: legacyContract, Status: chainprojection.TxSucceeded, Input: "0x"}}}
	legacyEvent := chainprojection.Event{ID: "sha256:" + strings.Repeat("d", 64), Type: chainprojection.EventTaskCreated, TaskID: "0x" + strings.Repeat("f", 64), BlockNumber: 1, BlockHash: legacyBlock.Hash, TransactionHash: legacyHash, Payload: map[string]any{"publisher": session.Wallet, "amount": "90"}}
	if err = store.ApplyBlock(ctx, legacyScope, legacyBlock, []chainprojection.Event{legacyEvent}); err != nil {
		t.Fatalf("legacy funding recanonicalization failed: %v", err)
	}
	var legacyStatus, legacyAsset string
	if err = db.QueryRowContext(ctx, `SELECT intent.status,account.asset_key FROM task_funding_intents intent JOIN fund_accounts account ON account.task_id=intent.task_id AND account.account_type='formal_escrow' WHERE intent.intent_id=$1`, legacyIntentID).Scan(&legacyStatus, &legacyAsset); err != nil || legacyStatus != "confirmed" || legacyAsset != "evm:31337/native" {
		t.Fatalf("legacy funding recanonicalization status=%s asset=%s err=%v", legacyStatus, legacyAsset, err)
	}
}

func fundingSearchPath(raw, schema string) string {
	parsed, _ := url.Parse(raw)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

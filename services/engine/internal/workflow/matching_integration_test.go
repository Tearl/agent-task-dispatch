//go:build integration

package workflow

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
	"github.com/example/agent-platform/engine/internal/execution"
	"github.com/example/agent-platform/engine/internal/funds"
	fundspostgres "github.com/example/agent-platform/engine/internal/funds/postgres"
	"github.com/example/agent-platform/engine/internal/matching"
	matchingpostgres "github.com/example/agent-platform/engine/internal/matching/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	taskpostgres "github.com/example/agent-platform/engine/internal/task/postgres"
	"github.com/lib/pq"
)

func TestCandidatesLoadCurrentPostgresMatchingAuthority(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t201_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()

	db, err := sql.Open("postgres", workflowSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if _, err = db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ('publisher-t201'),('provider-t201')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO agents (
agent_id,owner_id,name,category,tags,capabilities,languages,estimated_duration_seconds,
controller_address,payout_address,status,health,health_checked_at,health_valid_until,
max_concurrency,active_capacity,activated_at,created_at,updated_at,approval_status,risk_status,
matching_vector_version,reputation_quality,reputation_speed,reputation_reliability,
reputation_communication,reputation_compliance,matching_exposure_count,matching_effective_samples
) VALUES (
'agent-t201','provider-t201','T-201 Agent','research',ARRAY['analysis'],'["summarize"]',ARRAY['zh-CN'],600,
'0x1111111111111111111111111111111111111111','0x2222222222222222222222222222222222222222',
'active','healthy',$1,$2,2,0,$1,$1,$1,'approved','eligible','matching-vector-v1',
91,82,73,64,55,123,45
)`, now.Add(-time.Minute), now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO agent_price_versions (
agent_id,version_no,overview_price,formal_package_gross_price,additional_version_price,external_cost_cap,created_at
) VALUES ('agent-t201',1,10,100,20,5,$1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agents SET current_price_version=1 WHERE agent_id='agent-t201'`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO agent_capacity_leases (
reservation_id,agent_id,fencing_token,expires_at,created_at
) VALUES ('lease-t201','agent-t201',1,$1,$2)`, now.Add(10*time.Minute), now); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := store.Candidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one PostgreSQL candidate, got %d", len(candidates))
	}
	candidate := candidates[0]
	if candidate.AgentID != "agent-t201" || candidate.ProviderID != "provider-t201" ||
		candidate.ApprovalStatus != "approved" || candidate.RiskStatus != "eligible" ||
		candidate.VectorVersion != "matching-vector-v1" || candidate.ProtocolVersion != execution.ProtocolVersion ||
		candidate.ActiveCapacity != 1 || candidate.MaxConcurrency != 2 || candidate.PriceVersion != 1 ||
		candidate.OverviewPrice != "10" || candidate.FormalPrice != "100" || candidate.ExternalCostCap != "5" ||
		candidate.ExposureCount != 123 || candidate.EffectiveSamples != 45 || !candidate.ReputationAvailable ||
		candidate.Reputation != (matching.Reputation{Quality: 91, Speed: 82, Reliability: 73, Communication: 64, Compliance: 55}) {
		t.Fatalf("PostgreSQL matching authority was not preserved: %#v", candidate)
	}

	request := matching.Request{
		TaskID: "task-t201", PublisherID: "publisher-t201", Category: "research", Language: "zh-CN",
		RequiredCapabilities: []string{"summarize"}, RequiredProtocolVersion: execution.ProtocolVersion,
		RequiredVectorVersion: "matching-vector-v1", OverviewBudget: "10", FormalBudget: "100",
		ExternalCostCap: "5", Deadline: now.Add(time.Hour), Now: now,
	}
	eligible, excluded := matching.HardFilter(request, candidates)
	if len(eligible) != 1 || len(excluded) != 0 {
		t.Fatalf("authoritative PostgreSQL candidate should pass every hard gate: eligible=%#v excluded=%#v", eligible, excluded)
	}

	// A completed matching trigger is recorded independently from its exact
	// evaluation time. A lost HTTP response can therefore be replayed without
	// creating a fresh matching revision.
	taskStore, err := taskpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	taskService, err := enginetask.NewService(taskStore)
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{UserID: "publisher-t201", Wallet: "0x3333333333333333333333333333333333333333", Roles: []string{"publisher"}}
	draft := enginetask.DraftInput{Title: "Idempotent matching", Description: "Verify lost response replay", ExpertType: "research", Language: "zh-CN", OverviewBudget: "10", FormalBudget: "100", ExternalCostCap: "5", Deadline: now.Add(time.Hour), Inputs: []string{"input"}, AllowedTools: []string{"read"}, Exclusions: []string{"write"}, DeliveryFormat: "json", AcceptanceCriteria: []enginetask.AcceptanceCriterion{{ID: "quality", Title: "Quality", Description: "Accurate", Weight: 100}}}
	workflowTask, _, err := taskService.Create(ctx, session, "create-matching-operation-task", draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = taskService.Publish(ctx, session, "publish-matching-operation-task", workflowTask.ID, enginetask.PublishInput{ExpectedVersion: workflowTask.AggregateVersion}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE tasks SET status='escrowed' WHERE task_id=$1`, workflowTask.ID); err != nil {
		t.Fatal(err)
	}
	operation, err := store.LockMatchingOperation(ctx, session.UserID, "matching-operation-1")
	if err != nil {
		t.Fatal(err)
	}
	response := StartMatchingResult{SnapshotID: "sha256:" + strings.Repeat("9", 64), MatchRevision: 1, Qualified: 1, Selected: 1}
	if _, _, completed, beginErr := operation.Begin(ctx, session.UserID, "matching-operation-1", workflowTask.ID, now); beginErr != nil || completed {
		operation.Close()
		t.Fatalf("begin matching operation completed=%v err=%v", completed, beginErr)
	}
	if err = store.TransitionTaskAndRecordMatchingOperation(ctx, session.UserID, "matching-operation-1", workflowTask.ID, "matching", "task.matching_started", response); err != nil {
		operation.Close()
		t.Fatal(err)
	}
	operation.Close()
	operation, err = store.LockMatchingOperation(ctx, session.UserID, "matching-operation-1")
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Close()
	_, replayResult, exists, err := operation.Begin(ctx, session.UserID, "matching-operation-1", workflowTask.ID, now.Add(time.Minute))
	if err != nil || !exists || replayResult != response {
		t.Fatalf("persisted matching operation replay result=%#v exists=%v err=%v", replayResult, exists, err)
	}
	var operationCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM matching_run_operations WHERE publisher_id=$1 AND operation_id=$2`, session.UserID, "matching-operation-1").Scan(&operationCount); err != nil || operationCount != 1 {
		t.Fatalf("matching operation count=%d err=%v", operationCount, err)
	}

	// Freeze the authoritative draft before the snapshot transaction. If the
	// process crashes after the snapshot commits but before the operation is
	// completed, retry must replay this exact draft and not increment exposure.
	crashOperation, err := store.LockMatchingOperation(ctx, session.UserID, "matching-operation-crash")
	if err != nil {
		t.Fatal(err)
	}
	evaluatedAt, _, completed, err := crashOperation.Begin(ctx, session.UserID, "matching-operation-crash", workflowTask.ID, now)
	if err != nil || completed {
		crashOperation.Close()
		t.Fatalf("begin crash operation completed=%v err=%v", completed, err)
	}
	input, err := store.Task(ctx, session.UserID, workflowTask.ID)
	if err != nil {
		crashOperation.Close()
		t.Fatal(err)
	}
	scored := matching.ScoredCandidate{
		Candidate: candidates[0],
		Recall:    map[string]matching.RecallEvidence{},
		Score:     matching.ScoreBreakdown{TaskMatch: 50, Reputation: 25, PriceTime: 10, Availability: 5, RuleScore: 90, RankingScore: 90},
	}
	snapshotDraft := matching.SnapshotDraft{
		Key: matching.SnapshotKey{
			TaskID: workflowTask.ID, TaskSpecHash: input.SpecHash,
			AlgorithmVersion:   matching.FairShuffleAlgorithmVersion,
			EffectiveInputHash: "sha256:" + strings.Repeat("8", 64),
		},
		RuleVersion: "matching-rules-v1", ModelVersion: "ranking-model-disabled-v1",
		Result: matching.Result{Scored: []matching.ScoredCandidate{scored}, Qualified: []matching.ScoredCandidate{scored}, Degradations: []matching.Degradation{}},
	}
	snapshotDraft, err = crashOperation.FreezeDraft(ctx, session.UserID, "matching-operation-crash", workflowTask.ID, snapshotDraft)
	if err != nil {
		crashOperation.Close()
		t.Fatal(err)
	}
	crashOperation.Close()

	snapshotStore, err := matchingpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	snapshotService, err := matching.NewSnapshotService(snapshotStore, matching.DefaultShufflePolicy("shuffle-key-v1", []byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	createdSnapshot, replay, err := snapshotService.CreateRevision(ctx, snapshotDraft)
	if err != nil || replay {
		t.Fatalf("commit snapshot before simulated crash replay=%v err=%v", replay, err)
	}
	asset := "evm:84532/erc20:0x1111111111111111111111111111111111111111"
	formalAccountID := "sha256:" + strings.Repeat("6", 64)
	discoveryAccountID := "sha256:" + strings.Repeat("7", 64)
	if _, err = db.ExecContext(ctx, `INSERT INTO fund_accounts(account_id,account_class,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance,created_at,updated_at)
VALUES($1,'business','formal_escrow',$2,$2,$3,$4,$4,'task-funding-v3','open',100,clock_timestamp(),clock_timestamp())`, formalAccountID, workflowTask.ID, asset, session.UserID); err != nil {
		t.Fatal(err)
	}
	if ready, readyErr := store.OverviewFundingReady(ctx, session.UserID, workflowTask.ID, createdSnapshot.ID); readyErr != nil || ready {
		t.Fatalf("formal-only funding must not start overview: ready=%v err=%v", ready, readyErr)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO fund_accounts(account_id,account_class,account_type,task_id,reference_id,asset_key,principal_owner_id,residual_recipient_id,refund_policy_version,state,balance,created_at,updated_at)
VALUES($1,'business','discovery_pool',$2,$2,$3,$4,$4,'refund-v1','open',14,clock_timestamp(),clock_timestamp())`, discoveryAccountID, workflowTask.ID, asset, session.UserID); err != nil {
		t.Fatal(err)
	}
	if ready, readyErr := store.OverviewFundingReady(ctx, session.UserID, workflowTask.ID, createdSnapshot.ID); readyErr != nil || ready {
		t.Fatalf("underfunded discovery pool must not start overview: ready=%v err=%v", ready, readyErr)
	}
	fundsStore, err := fundspostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	fundsService, err := funds.NewService(fundsStore, asset)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = fundsService.RecordFunding(ctx, funds.FundingRequest{IdempotencyKey: "top-up-discovery-t201", AccountID: discoveryAccountID, Amount: "1", ExternalRef: "testnet:top-up"}); err != nil {
		t.Fatal(err)
	}
	if ready, readyErr := store.OverviewFundingReady(ctx, session.UserID, workflowTask.ID, createdSnapshot.ID); readyErr != nil || !ready {
		t.Fatalf("independently funded discovery pool should enable overview: ready=%v err=%v", ready, readyErr)
	}
	var statusAfterFundingGate string
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id=$1`, workflowTask.ID).Scan(&statusAfterFundingGate); err != nil || statusAfterFundingGate != "matching" {
		t.Fatalf("overview funding preflight changed task state: status=%s err=%v", statusAfterFundingGate, err)
	}
	var exposureAfterCommit int
	if err = db.QueryRowContext(ctx, `SELECT matching_exposure_count FROM agents WHERE agent_id='agent-t201'`).Scan(&exposureAfterCommit); err != nil {
		t.Fatal(err)
	}

	retryOperation, err := store.LockMatchingOperation(ctx, session.UserID, "matching-operation-crash")
	if err != nil {
		t.Fatal(err)
	}
	defer retryOperation.Close()
	retryEvaluatedAt, _, completed, err := retryOperation.Begin(ctx, session.UserID, "matching-operation-crash", workflowTask.ID, now.Add(2*time.Hour))
	if err != nil || completed || !retryEvaluatedAt.Equal(evaluatedAt) {
		t.Fatalf("retry changed frozen evaluation time: first=%s retry=%s completed=%v err=%v", evaluatedAt, retryEvaluatedAt, completed, err)
	}
	frozenDraft, exists, err := retryOperation.FrozenDraft(ctx, session.UserID, "matching-operation-crash", workflowTask.ID)
	if err != nil || !exists {
		t.Fatalf("read frozen draft exists=%v err=%v", exists, err)
	}
	replayedSnapshot, replay, err := snapshotService.CreateRevision(ctx, frozenDraft)
	if err != nil || !replay || replayedSnapshot.ID != createdSnapshot.ID || replayedSnapshot.MatchRevision != createdSnapshot.MatchRevision {
		t.Fatalf("snapshot crash recovery snapshot=%#v replay=%v err=%v", replayedSnapshot, replay, err)
	}
	var exposureAfterRetry int
	if err = db.QueryRowContext(ctx, `SELECT matching_exposure_count FROM agents WHERE agent_id='agent-t201'`).Scan(&exposureAfterRetry); err != nil || exposureAfterRetry != exposureAfterCommit {
		t.Fatalf("matching exposure duplicated: after commit=%d after retry=%d err=%v", exposureAfterCommit, exposureAfterRetry, err)
	}
}

func workflowSearchPath(databaseURL, schema string) string {
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

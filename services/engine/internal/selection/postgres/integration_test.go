//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	agentpostgres "github.com/example/agent-platform/engine/internal/agent/postgres"
	"github.com/example/agent-platform/engine/internal/auth"
	chainprojection "github.com/example/agent-platform/engine/internal/chain"
	chainpostgres "github.com/example/agent-platform/engine/internal/chain/postgres"
	"github.com/example/agent-platform/engine/internal/execution"
	executionpostgres "github.com/example/agent-platform/engine/internal/execution/postgres"
	"github.com/example/agent-platform/engine/internal/funds"
	fundspostgres "github.com/example/agent-platform/engine/internal/funds/postgres"
	"github.com/example/agent-platform/engine/internal/overview"
	overviewpostgres "github.com/example/agent-platform/engine/internal/overview/postgres"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/example/agent-platform/engine/internal/selection"
	"github.com/lib/pq"
)

type integrationChain struct{ result selection.ChainResult }

func (chain *integrationChain) VerifySelection(context.Context, string) (selection.ChainResult, error) {
	return chain.result, nil
}

func TestPostgresSelectionReservationConfirmationAndReplay(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t205_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", selectionSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedSelectionDependencies(t, ctx, db)

	fundStore, _ := fundspostgres.NewStore(db)
	fundService, _ := funds.NewService(fundStore, "eip155:31337/native:18")
	discovery, _, err := fundService.OpenAccount(ctx, funds.OpenAccountRequest{Type: funds.AccountDiscoveryPool, TaskID: "task-selection", ReferenceID: "task-selection", Asset: "eip155:31337/native:18", PrincipalOwnerID: "publisher", ResidualRecipientID: "publisher", RefundPolicyVersion: "refund-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = fundService.RecordFunding(ctx, funds.FundingRequest{IdempotencyKey: "selection-funding", AccountID: discovery.ID, Amount: "100", ExternalRef: "chain:31337/tx:fund/log:0"}); err != nil {
		t.Fatal(err)
	}
	allocation, _, err := fundService.AuthorizeOverview(ctx, funds.OverviewAuthorization{IdempotencyKey: "selection-allocation", TaskID: "task-selection", TaskSpecHash: integrationDigest("spec"), SnapshotID: integrationDigest("snapshot"), MatchRevision: 1, AgentID: "agent-1", PriceVersion: 1, QuoteHash: integrationDigest("quote"), OverviewPrice: "10", ExternalCostCap: "0", Deadline: time.Now().UTC().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	logicalID := integrationDigest("execution")
	if _, _, err = fundService.CaptureOverview(ctx, allocation.ID, funds.OverviewCapture{TaskID: "task-selection", TaskSpecHash: integrationDigest("spec"), MatchRevision: 1, LogicalExecutionID: logicalID, AgentID: "agent-1", QuoteHash: integrationDigest("quote"), ContentHash: integrationDigest("content"), OverviewAmount: "10", UsedCost: "0"}); err != nil {
		t.Fatal(err)
	}

	executionStore, _ := executionpostgres.NewStore(db)
	deadline := time.Now().UTC().Add(time.Hour)
	if _, _, err = executionStore.GetOrCreate(ctx, execution.Spec{LogicalExecutionID: logicalID, Stage: execution.StageOverview, TaskID: "task-selection", TaskSpecHash: integrationDigest("spec"), InputRef: "brief://selection", InputHash: integrationDigest("brief"), AgentID: "agent-1", AgentEndpoint: "https://agent.example", ResponsibilityCode: "overview_candidate", CostCap: "0", ToolPolicy: execution.ToolPolicy{Mode: execution.ToolPolicyReadOnly, AllowedTools: []string{"read"}}, Deadline: deadline, IdempotencyKey: logicalID, Overview: &execution.OverviewBinding{MatchRevision: 1, AllocationID: allocation.ID, QuoteHash: integrationDigest("quote")}}); err != nil {
		t.Fatal(err)
	}
	overviewStore, _ := overviewpostgres.NewStore(db)
	batchID, slotID := integrationDigest("batch"), integrationDigest("slot")
	batch := overview.Batch{ID: batchID, SnapshotID: integrationDigest("snapshot"), TaskID: "task-selection", TaskSpecHash: integrationDigest("spec"), MatchRevision: 1, AlgorithmVersion: "fair-shuffle-v1", BriefRef: "brief://selection", BriefHash: integrationDigest("brief"), Deadline: deadline, Status: overview.BatchRunning, Slots: []overview.Slot{{ID: slotID, BatchID: batchID, Ordinal: 1, SourcePosition: 1, AgentID: "agent-1", ProviderID: "provider-1", PriceVersion: 1, QuoteHash: integrationDigest("quote"), OverviewPrice: "10", ExternalCostCap: "0", AllocationID: allocation.ID, LogicalExecutionID: logicalID, Status: overview.SlotPlanned, BillingStatus: overview.BillingAuthorized}}}
	if _, _, err = overviewStore.GetOrCreate(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if _, err = overviewStore.RecordDispatched(ctx, batchID, slotID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = overviewStore.RecordValidation(ctx, batchID, slotID, overview.Validation{Valid: true}, integrationDigest("content"), "artifact://selection"); err != nil {
		t.Fatal(err)
	}
	if _, err = overviewStore.RecordBilling(ctx, batchID, slotID, overview.BillingCaptured); err != nil {
		t.Fatal(err)
	}

	agentStore, _ := agentpostgres.NewStore(db)
	agentService, _ := agent.NewService(agentStore)
	selectionStore, _ := NewStore(db)
	signer, _ := selection.NewEIP712Signer(testPrivateKeyIntegration, "31337", "0x0000000000000000000000000000000000001234")
	chain := &integrationChain{}
	service, err := selection.NewService(selectionStore, agentService, signer, chain, selection.Config{ChainID: "31337", ContractAddress: "0x0000000000000000000000000000000000001234", ReservationTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{UserID: "publisher", Wallet: "0x000000000000000000000000000000000000cafe", Roles: []string{"publisher"}}
	if _, err = db.ExecContext(ctx, `UPDATE task_funding_intents SET asset_address=NULL,platform_task_key=NULL,task_spec_hash=NULL,funding_deadline=NULL WHERE task_id='task-selection'`); err != nil {
		t.Fatal(err)
	}
	if _, _, legacyErr := service.Reserve(ctx, session, "legacy-selection-key", "task-selection", selection.Request{BatchID: batchID, SlotID: slotID}); !errors.Is(legacyErr, selection.ErrInvalidState) {
		t.Fatalf("legacy confirmed funding must fail closed, got %v", legacyErr)
	}
	if _, err = db.ExecContext(ctx, `UPDATE task_funding_intents SET asset_address='0x0000000000000000000000000000000000009999',platform_task_key=$1,task_spec_hash=$2,funding_deadline=$3 WHERE task_id='task-selection'`, "0x"+fmt.Sprintf("%064x", 1), "0x"+fmt.Sprintf("%064x", 2), deadline.Unix()); err != nil {
		t.Fatal(err)
	}
	intent, replay, err := service.Reserve(ctx, session, "selection-key", "task-selection", selection.Request{BatchID: batchID, SlotID: slotID})
	if err != nil || replay || intent.Reservation.FormalPayable != "90" {
		t.Fatalf("reserve: %#v replay=%v err=%v", intent, replay, err)
	}
	if replayed, isReplay, replayErr := service.Reserve(ctx, session, "selection-key", "task-selection", selection.Request{BatchID: batchID, SlotID: slotID}); replayErr != nil || !isReplay || replayed != intent {
		t.Fatalf("reserve replay: %#v replay=%v err=%v", replayed, isReplay, replayErr)
	}
	if _, err = db.ExecContext(ctx, `UPDATE tasks SET deletion_requested_at=clock_timestamp() WHERE task_id='task-selection'`); err != nil {
		t.Fatal(err)
	}
	if _, isReplay, replayErr := service.Reserve(ctx, session, "selection-key", "task-selection", selection.Request{BatchID: batchID, SlotID: slotID}); !errors.Is(replayErr, selection.ErrInvalidState) || isReplay {
		t.Fatalf("deletion-pending task replayed selection: replay=%v err=%v", isReplay, replayErr)
	}
	if _, readErr := service.Get(ctx, session, "task-selection", intent.Reservation.ID); !errors.Is(readErr, selection.ErrInvalidState) {
		t.Fatalf("deletion-pending task exposed selection signature: %v", readErr)
	}
	if _, err = db.ExecContext(ctx, `UPDATE tasks SET deletion_requested_at=NULL WHERE task_id='task-selection'`); err != nil {
		t.Fatal(err)
	}
	txHash := "0x" + fmt.Sprintf("%064x", 205)
	chain.result = selection.ChainResult{Status: selection.ChainConfirmed, TransactionHash: txHash, BlockNumber: 12, LogIndex: 3, Proof: intent.Reservation.Proof, FormalPayable: "90", WorkNonce: 1}
	confirmed, assignment, err := service.Reconcile(ctx, session, "task-selection", intent.Reservation.ID, selection.ReconcileRequest{TransactionHash: txHash})
	if err != nil || assignment == nil || confirmed.Status != selection.StatusConfirmed || assignment.WorkNonce != 1 {
		t.Fatalf("confirm: %#v %#v err=%v", confirmed, assignment, err)
	}
	var taskStatus string
	var activeCapacity int
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id='task-selection'`).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT active_capacity FROM agents WHERE agent_id='agent-1'`).Scan(&activeCapacity); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "assigned" || activeCapacity != 0 {
		t.Fatalf("projection mismatch: task=%s capacity=%d", taskStatus, activeCapacity)
	}
	if _, err = db.ExecContext(ctx, `UPDATE assignments SET work_nonce=2 WHERE assignment_id=$1`, assignment.ID); err == nil {
		t.Fatal("database allowed assignment mutation")
	}
	chainStore, _ := chainpostgres.NewStore(db)
	scope := chainprojection.Scope{ChainID: "31337", Contract: "0x0000000000000000000000000000000000001234", StartBlock: 1, Confirmations: 2, MaxReorgDepth: 10}
	blockHash, parentHash := "0x"+fmt.Sprintf("%064x", 1205), "0x"+fmt.Sprintf("%064x", 1204)
	block := chainprojection.Block{Number: 1, Hash: blockHash, ParentHash: parentHash, Timestamp: time.Now().UTC(), Transactions: []chainprojection.Transaction{{Hash: txHash, To: scope.Contract, Input: chainprojection.SelectionCallSelector(), Status: chainprojection.TxSucceeded}}}
	event := chainprojection.Event{ID: integrationDigest("selection-chain-event"), Type: chainprojection.EventSelection, BlockNumber: 1, BlockHash: blockHash, TransactionHash: txHash, LogIndex: 3, TaskID: intent.Reservation.Proof.TaskID, AssignmentID: assignment.ID, Payload: map[string]any{"formalPayable": "90"}, Selection: &chain.result}
	if err = chainStore.ApplyBlock(ctx, scope, block, []chainprojection.Event{event}); err != nil {
		t.Fatal(err)
	}
	if err = chainStore.Rewind(ctx, scope, 0, "chain_reorganization"); err != nil {
		t.Fatal(err)
	}
	var reservationStatus string
	if err = db.QueryRowContext(ctx, `SELECT status FROM selection_reservations WHERE reservation_id=$1`, intent.Reservation.ID).Scan(&reservationStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT status FROM tasks WHERE task_id='task-selection'`).Scan(&taskStatus); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM active_assignments`).Scan(&activeCapacity); err != nil {
		t.Fatal(err)
	}
	if reservationStatus != selection.StatusOrphaned || taskStatus != "chain_reorg_pending" || activeCapacity != 0 {
		t.Fatalf("reorg quarantine mismatch: reservation=%s task=%s activeAssignments=%d", reservationStatus, taskStatus, activeCapacity)
	}
}

const testPrivateKeyIntegration = "00000000000000000000000000000000000000000000000000000000000a11ce"

func seedSelectionDependencies(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SET CONSTRAINTS ALL DEFERRED`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ('publisher'),('provider-1')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agents (agent_id,owner_id,name,category,capabilities,languages,estimated_duration_seconds,controller_address,payout_address,status,health,health_checked_at,health_valid_until,max_concurrency,active_capacity,aggregate_version,activated_at,current_price_version,created_at,updated_at) VALUES ('agent-1','provider-1','Agent','research','Research',ARRAY['zh-CN'],60,'0x000000000000000000000000000000000000beef','0x000000000000000000000000000000000000f00d','active','healthy',$1,$2,1,0,2,$1,1,$1,$1)`, now, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_price_versions (agent_id,version_no,overview_price,formal_package_gross_price,additional_version_price,external_cost_cap,created_at) VALUES ('agent-1',1,10,100,20,0,$1)`, now); err != nil {
		t.Fatal(err)
	}
	criteria := `[ {"id":"quality","title":"Quality","description":"Complete","weight":100} ]`
	if _, err = tx.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,status,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,delivery_format,draft_acceptance,aggregate_version,current_spec_version,current_acceptance_version,published_at,created_at,updated_at) VALUES ('task-selection','publisher','awaiting_selection','Title','Description','research','zh-CN',100,100,0,$1,'markdown',$2,2,1,1,$3,$3,$3)`, now.Add(time.Hour), criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_spec_versions (task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES ('task-selection',1,2,$1,'Title','Description','research','zh-CN',100,100,0,$2,'{}',ARRAY['read'],'{}','markdown',$3)`, integrationDigest("spec"), now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO acceptance_versions (task_id,version_no,task_aggregate_version,content_hash,criteria,total_weight,created_at) VALUES ('task-selection',1,2,$1,$2,100,$3)`, integrationDigest("acceptance"), criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO task_funding_intents(intent_id,task_id,publisher_id,publisher_wallet,idempotency_key,request_hash,chain_id,contract_address,chain_task_id,overview_amount,formal_amount,external_cost_amount,total_amount,status,aggregate_version,created_at,updated_at,asset_address,platform_task_key,task_spec_hash,funding_deadline)
VALUES($1,'task-selection','publisher','0x000000000000000000000000000000000000cafe','selection-funding',$2,31337,'0x0000000000000000000000000000000000001234',$3,0,100,0,100,'confirmed',1,$4,$4,'0x0000000000000000000000000000000000009999',$5,$6,$7)`, integrationDigest("funding-intent"), integrationDigest("funding-request"), selection.TaskChainID("task-selection"), now, "0x"+fmt.Sprintf("%064x", 1), "0x"+fmt.Sprintf("%064x", 2), now.Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO match_snapshots (snapshot_id,task_id,task_spec_hash,match_revision,effective_input_hash,algorithm_version,rule_version,model_version,seed_digest,seed_key_version,policy_hash,exploration_triggered,degradations,snapshot_body,created_at,sealed_at) VALUES ($1,'task-selection',$2,1,$3,'fair-shuffle-v1','rules-v1','disabled',$4,'seed-v1',$5,false,'[]','{}',$6,NULL)`, integrationDigest("snapshot"), integrationDigest("spec"), integrationDigest("input"), integrationDigest("seed"), integrationDigest("policy"), now); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO match_snapshot_candidates (snapshot_id,candidate_index,agent_id,provider_id,price_version,overview_price,formal_price,external_cost_cap,evaluation_status,exclusion_reasons,recall_evidence,task_match_score,reputation_score,price_time_score,availability_score,rule_score,model_delta,ranking_score,qualified,qualification_reasons,selection_weight,probability_numerator,probability_denominator,random_draw,final_position,exploration) VALUES ($1,1,'agent-1','provider-1',1,'10','100','0','scored','[]','{}',50,20,10,5,85,0,85,true,'[]',26,1,1,0,1,false)`, integrationDigest("snapshot")); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE match_snapshots SET sealed_at=created_at WHERE snapshot_id=$1`, integrationDigest("snapshot")); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func selectionSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return databaseURL + "?search_path=" + url.QueryEscape(schema)
}

func integrationDigest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}

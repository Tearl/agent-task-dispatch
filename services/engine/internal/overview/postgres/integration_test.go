//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/execution"
	executionpostgres "github.com/example/agent-platform/engine/internal/execution/postgres"
	"github.com/example/agent-platform/engine/internal/funds"
	fundspostgres "github.com/example/agent-platform/engine/internal/funds/postgres"
	"github.com/example/agent-platform/engine/internal/overview"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresOverviewAllocationValidationReplacementAndImmutability(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t204_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", overviewSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedOverviewDependencies(t, ctx, db)
	executionStore, err := executionpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	fundStore, err := fundspostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	fundService, err := funds.NewService(fundStore, "eip155:31337/native:18")
	if err != nil {
		t.Fatal(err)
	}
	discovery, _, err := fundService.OpenAccount(ctx, funds.OpenAccountRequest{Type: funds.AccountDiscoveryPool, TaskID: "task-overview", ReferenceID: "task-overview", Asset: "eip155:31337/native:18", PrincipalOwnerID: "publisher", ResidualRecipientID: "publisher", RefundPolicyVersion: "refund-v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = fundService.RecordFunding(ctx, funds.FundingRequest{IdempotencyKey: "fund-overview", AccountID: discovery.ID, Amount: "1000", ExternalRef: "chain:31337/tx:overview/log:0"}); err != nil {
		t.Fatal(err)
	}
	allocationGateway, err := funds.NewOverviewGateway(fundService)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	batch := overview.Batch{ID: pgDigest("batch"), SnapshotID: pgDigest("snapshot"), TaskID: "task-overview", TaskSpecHash: pgDigest("spec"), MatchRevision: 1, AlgorithmVersion: "fair-shuffle-v1", BriefRef: "brief://sanitized", BriefHash: pgDigest("brief"), Deadline: now.Add(time.Hour), Status: overview.BatchRunning, CreatedAt: now, UpdatedAt: now}
	for index := 1; index <= 3; index++ {
		slot := postgresSlot(batch.ID, index, false)
		bindPostgresAllocation(t, ctx, allocationGateway, batch, &slot)
		createPostgresExecution(t, ctx, executionStore, batch, slot)
		batch.Slots = append(batch.Slots, slot)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	created, replay, err := store.GetOrCreate(ctx, batch)
	if err != nil || replay || len(created.Slots) != 3 {
		t.Fatalf("create batch: batch=%#v replay=%v err=%v", created, replay, err)
	}
	replayed, replay, err := store.GetOrCreate(ctx, batch)
	if err != nil || !replay || replayed.ID != created.ID {
		t.Fatalf("replay batch: batch=%#v replay=%v err=%v", replayed, replay, err)
	}
	for _, slot := range created.Slots[:2] {
		if _, err = store.RecordDispatched(ctx, created.ID, slot.ID); err != nil {
			t.Fatal(err)
		}
	}
	contentHash := pgDigest("same-content")
	created, first, replay, err := store.RecordValidation(ctx, created.ID, created.Slots[0].ID, overview.Validation{Valid: true}, contentHash, "artifact://first")
	if err != nil || replay || first.Status != overview.SlotValid {
		t.Fatalf("valid result: slot=%#v replay=%v err=%v", first, replay, err)
	}
	if _, err = allocationGateway.CaptureOverview(ctx, first.AllocationID, overview.BillingClaim{TaskID: batch.TaskID, TaskSpecHash: batch.TaskSpecHash, MatchRevision: batch.MatchRevision, LogicalExecutionID: first.LogicalExecutionID, AgentID: first.AgentID, QuoteHash: first.QuoteHash, ContentHash: contentHash, Amount: first.OverviewPrice, UsedCost: "0"}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RecordBilling(ctx, created.ID, first.ID, overview.BillingCaptured); err != nil {
		t.Fatal(err)
	}
	created, duplicate, _, err := store.RecordValidation(ctx, created.ID, created.Slots[1].ID, overview.Validation{Valid: true}, contentHash, "artifact://duplicate")
	if err != nil || duplicate.Status != overview.SlotInvalid || len(duplicate.Validation.Codes) != 1 || duplicate.Validation.Codes[0] != "duplicate_content" {
		t.Fatalf("duplicate result: slot=%#v err=%v", duplicate, err)
	}
	if _, err = allocationGateway.ReleaseOverview(ctx, duplicate.AllocationID, "duplicate_content"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RecordBilling(ctx, created.ID, duplicate.ID, overview.BillingReleased); err != nil {
		t.Fatal(err)
	}
	third := created.Slots[2]
	if _, err = store.RecordDispatched(ctx, created.ID, third.ID); err != nil {
		t.Fatal(err)
	}
	created, third, _, err = store.RecordValidation(ctx, created.ID, third.ID, overview.Validation{Valid: true}, pgDigest("third-content"), "artifact://third")
	if err != nil || third.Status != overview.SlotValid {
		t.Fatalf("third valid result: slot=%#v err=%v", third, err)
	}
	if _, err = store.RecordBilling(ctx, created.ID, third.ID, overview.BillingReleased); err == nil {
		t.Fatal("database allowed a valid result to be released before batch obsolescence")
	}
	replacement := postgresSlot(batch.ID, 4, true)
	bindPostgresAllocation(t, ctx, allocationGateway, batch, &replacement)
	createPostgresExecution(t, ctx, executionStore, batch, replacement)
	created, replay, err = store.AddReplacement(ctx, batch.ID, replacement)
	if err != nil || replay || !created.ReplacementUsed || len(created.Slots) != 4 {
		t.Fatalf("replacement: batch=%#v replay=%v err=%v", created, replay, err)
	}
	changed, err := store.MarkObsoleteBefore(ctx, batch.TaskID, batch.MatchRevision+1)
	if err != nil || len(changed) != 1 {
		t.Fatalf("obsolete batch: changed=%d err=%v", len(changed), err)
	}
	if _, err = allocationGateway.ReleaseOverview(ctx, third.AllocationID, "matching_revision_obsolete"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RecordBilling(ctx, created.ID, third.ID, overview.BillingReleased); err != nil {
		t.Fatalf("valid uncaptured result could not be released after obsolescence: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE overview_slots SET allocation_id='tampered' WHERE slot_id=$1`, first.ID); err == nil {
		t.Fatal("database allowed allocation identity mutation")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM overview_events WHERE batch_id=$1`, batch.ID); err == nil {
		t.Fatal("database allowed overview event deletion")
	}
}

func seedOverviewDependencies(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ('publisher'),('provider-1'),('provider-2'),('provider-3'),('provider-4')`); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 4; index++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO agents (agent_id,owner_id,name,category,capabilities,languages,estimated_duration_seconds,controller_address,payout_address,max_concurrency,created_at,updated_at) VALUES ($1,$2,$3,'research','Research',ARRAY['zh-CN'],60,'0x1111111111111111111111111111111111111111','0x2222222222222222222222222222222222222222',1,$4,$4)`, fmt.Sprintf("agent-%d", index), fmt.Sprintf("provider-%d", index), fmt.Sprintf("Agent %d", index), now); err != nil {
			t.Fatal(err)
		}
	}
	criteria := `[{"id":"quality","title":"Quality","description":"Complete","weight":100}]`
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,delivery_format,draft_acceptance,created_at,updated_at) VALUES ('task-overview','publisher','Title','Description','research','zh-CN',100,1000,100,$1,'markdown',$2,$3,$3)`, now.Add(time.Hour), criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO task_spec_versions (task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES ('task-overview',1,2,$1,'Title','Description','research','zh-CN',100,1000,100,$2,'{}','{}','{}','markdown',$3)`, pgDigest("spec"), now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO match_snapshots (snapshot_id,task_id,task_spec_hash,match_revision,effective_input_hash,algorithm_version,rule_version,model_version,seed_digest,seed_key_version,policy_hash,exploration_triggered,degradations,snapshot_body,created_at,sealed_at) VALUES ($1,'task-overview',$2,1,$3,'fair-shuffle-v1','rules-v1','disabled',$4,'seed-v1',$5,false,'[]','{}',$6,NULL)`, pgDigest("snapshot"), pgDigest("spec"), pgDigest("input"), pgDigest("seed"), pgDigest("policy"), now); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 4; index++ {
		var finalPosition any
		var numerator, denominator, draw any
		if index <= 3 {
			finalPosition, numerator, denominator, draw = index, 1, 4, index
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO match_snapshot_candidates (snapshot_id,candidate_index,agent_id,provider_id,price_version,overview_price,formal_price,external_cost_cap,evaluation_status,exclusion_reasons,recall_evidence,task_match_score,reputation_score,price_time_score,availability_score,rule_score,model_delta,ranking_score,qualified,qualification_reasons,selection_weight,probability_numerator,probability_denominator,random_draw,final_position,exploration) VALUES ($1,$2,$3,$4,1,'10','100','5','scored','[]','{}',50,20,10,5,85,0,85,true,'[]',26,$5,$6,$7,$8,false)`, pgDigest("snapshot"), index, fmt.Sprintf("agent-%d", index), fmt.Sprintf("provider-%d", index), numerator, denominator, draw, finalPosition); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE match_snapshots SET sealed_at=created_at WHERE snapshot_id=$1`, pgDigest("snapshot")); err != nil {
		t.Fatal(err)
	}
}

func postgresSlot(batchID string, ordinal int, replacement bool) overview.Slot {
	index := ordinal
	if replacement {
		index = 4
	}
	return overview.Slot{ID: pgDigest(fmt.Sprintf("slot-%d", ordinal)), BatchID: batchID, Ordinal: ordinal, SourcePosition: ordinal, Replacement: replacement, AgentID: fmt.Sprintf("agent-%d", index), ProviderID: fmt.Sprintf("provider-%d", index), PriceVersion: 1, QuoteHash: pgDigest(fmt.Sprintf("quote-%d", index)), OverviewPrice: "10", ExternalCostCap: "5", AllocationID: fmt.Sprintf("allocation-%d", ordinal), LogicalExecutionID: pgDigest(fmt.Sprintf("execution-%d", ordinal)), Status: overview.SlotPlanned, BillingStatus: overview.BillingAuthorized, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
}

func createPostgresExecution(t *testing.T, ctx context.Context, store *executionpostgres.Store, batch overview.Batch, slot overview.Slot) {
	t.Helper()
	_, _, err := store.GetOrCreate(ctx, execution.Spec{LogicalExecutionID: slot.LogicalExecutionID, Stage: execution.StageOverview, TaskID: batch.TaskID, TaskSpecHash: batch.TaskSpecHash, InputRef: batch.BriefRef, InputHash: batch.BriefHash, AgentID: slot.AgentID, AgentEndpoint: "https://agent.example", ResponsibilityCode: "overview_candidate", CostCap: slot.ExternalCostCap, ToolPolicy: execution.ToolPolicy{Mode: execution.ToolPolicyReadOnly, AllowedTools: []string{"read"}}, Deadline: batch.Deadline, IdempotencyKey: slot.LogicalExecutionID, Overview: &execution.OverviewBinding{MatchRevision: batch.MatchRevision, AllocationID: slot.AllocationID, QuoteHash: slot.QuoteHash}})
	if err != nil {
		t.Fatal(err)
	}
}

func bindPostgresAllocation(t *testing.T, ctx context.Context, gateway *funds.OverviewGateway, batch overview.Batch, slot *overview.Slot) {
	t.Helper()
	allocation, _, err := gateway.AuthorizeOverview(ctx, overview.AllocationRequest{IdempotencyKey: pgDigest("allocation:" + slot.AgentID), TaskID: batch.TaskID, TaskSpecHash: batch.TaskSpecHash, SnapshotID: batch.SnapshotID, MatchRevision: batch.MatchRevision, AgentID: slot.AgentID, PriceVersion: slot.PriceVersion, QuoteHash: slot.QuoteHash, OverviewPrice: slot.OverviewPrice, ExternalCostCap: slot.ExternalCostCap, Deadline: batch.Deadline})
	if err != nil {
		t.Fatal(err)
	}
	slot.AllocationID = allocation.ID
}

func overviewSearchPath(databaseURL, schema string) string {
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

func pgDigest(value string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value)))
}

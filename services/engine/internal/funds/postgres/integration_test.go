//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/funds"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresFundsIsolationBalanceReplayAndReversal(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t401_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", fundsSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedFundsDependencies(t, ctx, db)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := funds.NewService(store, "eip155:31337/native:18")
	if err != nil {
		t.Fatal(err)
	}

	accountTypes := []string{funds.AccountDiscoveryPool, funds.AccountFormalEscrow, funds.AccountChangeOrder, funds.AccountDisputeFee}
	accounts := make(map[string]funds.Account)
	for _, accountType := range accountTypes {
		reference := "task-funds"
		if accountType == funds.AccountChangeOrder {
			reference = "change-1"
		}
		if accountType == funds.AccountDisputeFee {
			reference = "dispute-1"
		}
		account, replay, openErr := service.OpenAccount(ctx, funds.OpenAccountRequest{Type: accountType, TaskID: "task-funds", ReferenceID: reference, Asset: "eip155:31337/native:18", PrincipalOwnerID: "publisher", ResidualRecipientID: "publisher", RefundPolicyVersion: "refund-v1"})
		if openErr != nil || replay {
			t.Fatalf("open %s: replay=%v err=%v", accountType, replay, openErr)
		}
		accounts[accountType] = account
	}
	if len(accounts) != 4 {
		t.Fatal("business accounts were not isolated")
	}
	funding, replay, err := service.RecordFunding(ctx, funds.FundingRequest{IdempotencyKey: "fund-discovery", AccountID: accounts[funds.AccountDiscoveryPool].ID, Amount: "100", ExternalRef: "chain:31337/tx:1/log:0"})
	if err != nil || replay {
		t.Fatalf("fund discovery: replay=%v err=%v", replay, err)
	}
	formalFunding, _, err := service.RecordFunding(ctx, funds.FundingRequest{IdempotencyKey: "fund-formal", AccountID: accounts[funds.AccountFormalEscrow].ID, Amount: "200", ExternalRef: "chain:31337/tx:2/log:0"})
	if err != nil {
		t.Fatal(err)
	}

	request := postgresAuthorization("allocation-1", "agent-1", "20", "10")
	allocation, replay, err := service.AuthorizeOverview(ctx, request)
	if err != nil || replay || allocation.AccountID != accounts[funds.AccountDiscoveryPool].ID {
		t.Fatalf("authorize: %#v replay=%v err=%v", allocation, replay, err)
	}
	claim := funds.OverviewCapture{TaskID: "task-funds", TaskSpecHash: pgFundsDigest("spec"), MatchRevision: 1, LogicalExecutionID: pgFundsDigest("execution-1"), AgentID: "agent-1", QuoteHash: request.QuoteHash, ContentHash: pgFundsDigest("content-1"), OverviewAmount: "20", UsedCost: "7"}
	allocation, replay, err = service.CaptureOverview(ctx, allocation.ID, claim)
	if err != nil || replay || allocation.Status != funds.AllocationCaptured {
		t.Fatalf("capture: %#v replay=%v err=%v", allocation, replay, err)
	}
	if _, replay, err = service.CaptureOverview(ctx, allocation.ID, claim); err != nil || !replay {
		t.Fatalf("capture replay: replay=%v err=%v", replay, err)
	}
	discovery, _ := store.GetAccount(ctx, accounts[funds.AccountDiscoveryPool].ID)
	formal, _ := store.GetAccount(ctx, accounts[funds.AccountFormalEscrow].ID)
	if discovery.Balance != "73" || formal.Balance != "200" {
		t.Fatalf("cross-account effect: discovery=%s formal=%s", discovery.Balance, formal.Balance)
	}

	journal, err := store.GetJournal(ctx, allocation.CaptureJournalID)
	if err != nil || journal.SourceRef != claim.LogicalExecutionID || pgEntrySum(journal.Entries, funds.EntryDebit) != pgEntrySum(journal.Entries, funds.EntryCredit) {
		t.Fatalf("capture journal: %#v err=%v", journal, err)
	}
	reversal, replay, err := service.ReverseJournal(ctx, funds.ReverseRequest{IdempotencyKey: "reverse-discovery-funding", JournalID: funding.ID, ReasonCode: "chain_event_reverted"})
	if err == nil || replay || reversal.ID != "" {
		t.Fatalf("reversal bypassed captured/reserved balance: %#v replay=%v err=%v", reversal, replay, err)
	}
	reversal, replay, err = service.ReverseJournal(ctx, funds.ReverseRequest{IdempotencyKey: "reverse-formal-funding", JournalID: formalFunding.ID, ReasonCode: "chain_event_reverted"})
	if err != nil || replay || reversal.ReversalOf != formalFunding.ID {
		t.Fatalf("balanced reversal failed: %#v replay=%v err=%v", reversal, replay, err)
	}
	formal, _ = store.GetAccount(ctx, accounts[funds.AccountFormalEscrow].ID)
	if formal.Balance != "0" {
		t.Fatalf("formal reversal balance=%s", formal.Balance)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	unbalancedID := pgFundsDigest("unbalanced")
	_, err = tx.ExecContext(ctx, `INSERT INTO fund_journals (journal_id,idempotency_key,request_hash,ledger_version,journal_type,task_id,source_ref,reason_code,created_at) VALUES ($1,'unbalanced',$2,'double-entry-v1','funding','task-funds','test:unbalanced','escrow_funded',now())`, unbalancedID, pgFundsDigest("unbalanced-request"))
	if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO fund_entries (journal_id,entry_index,account_id,account_type,direction,amount,asset_key,created_at) VALUES ($1,1,$2,'discovery_pool','credit',1,'eip155:31337/native:18',now())`, unbalancedID, discovery.ID)
	}
	if err == nil {
		err = tx.Commit()
	} else {
		_ = tx.Rollback()
	}
	if err == nil {
		t.Fatal("database committed an unbalanced journal")
	}
	if _, err = db.ExecContext(ctx, `UPDATE fund_journals SET reason_code='tampered' WHERE journal_id=$1`, funding.ID); err == nil {
		t.Fatal("database allowed journal mutation")
	}
}

func seedFundsDependencies(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ('publisher'),('provider-1'),('provider-2')`); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO agents (agent_id,owner_id,name,category,capabilities,languages,estimated_duration_seconds,controller_address,payout_address,max_concurrency,created_at,updated_at) VALUES ($1,$2,$3,'research','Research',ARRAY['zh-CN'],60,'0x1111111111111111111111111111111111111111','0x2222222222222222222222222222222222222222',1,$4,$4)`, fmt.Sprintf("agent-%d", index), fmt.Sprintf("provider-%d", index), fmt.Sprintf("Agent %d", index), now); err != nil {
			t.Fatal(err)
		}
	}
	criteria := `[{"id":"quality","title":"Quality","description":"Complete","weight":100}]`
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,delivery_format,draft_acceptance,created_at,updated_at) VALUES ('task-funds','publisher','Title','Description','research','zh-CN',100,1000,100,$1,'markdown',$2,$3,$3)`, now.Add(time.Hour), criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO task_spec_versions (task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES ('task-funds',1,2,$1,'Title','Description','research','zh-CN',100,1000,100,$2,'{}','{}','{}','markdown',$3)`, pgFundsDigest("spec"), now.Add(time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO match_snapshots (snapshot_id,task_id,task_spec_hash,match_revision,effective_input_hash,algorithm_version,rule_version,model_version,seed_digest,seed_key_version,policy_hash,exploration_triggered,degradations,snapshot_body,created_at,sealed_at) VALUES ($1,'task-funds',$2,1,$3,'fair-shuffle-v1','rules-v1','disabled',$4,'seed-v1',$5,false,'[]','{}',$6,NULL)`, pgFundsDigest("snapshot"), pgFundsDigest("spec"), pgFundsDigest("input"), pgFundsDigest("seed"), pgFundsDigest("policy"), now); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 2; index++ {
		if _, err := db.ExecContext(ctx, `INSERT INTO match_snapshot_candidates (snapshot_id,candidate_index,agent_id,provider_id,price_version,overview_price,formal_price,external_cost_cap,evaluation_status,exclusion_reasons,recall_evidence,task_match_score,reputation_score,price_time_score,availability_score,rule_score,model_delta,ranking_score,qualified,qualification_reasons,selection_weight,probability_numerator,probability_denominator,random_draw,final_position,exploration) VALUES ($1,$2,$3,$4,1,'20','100','10','scored','[]','{}',50,20,10,5,85,0,85,true,'[]',26,1,2,$2,$2,false)`, pgFundsDigest("snapshot"), index, fmt.Sprintf("agent-%d", index), fmt.Sprintf("provider-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE match_snapshots SET sealed_at=created_at WHERE snapshot_id=$1`, pgFundsDigest("snapshot")); err != nil {
		t.Fatal(err)
	}
}

func postgresAuthorization(key, agent, price, cap string) funds.OverviewAuthorization {
	return funds.OverviewAuthorization{IdempotencyKey: key, TaskID: "task-funds", TaskSpecHash: pgFundsDigest("spec"), SnapshotID: pgFundsDigest("snapshot"), MatchRevision: 1, AgentID: agent, PriceVersion: 1, QuoteHash: pgFundsDigest("quote:" + agent), OverviewPrice: price, ExternalCostCap: cap, Deadline: time.Now().UTC().Add(time.Hour)}
}

func fundsSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return databaseURL + " search_path=" + schema
}

func pgFundsDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}
func pgEntrySum(entries []funds.Entry, direction string) string {
	total := "0"
	for _, entry := range entries {
		if entry.Direction == direction {
			total = addMoney(total, entry.Amount)
		}
	}
	return total
}

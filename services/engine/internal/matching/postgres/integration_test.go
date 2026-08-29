//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/matching"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresSnapshotRevisionReplayAndImmutability(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t202_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()

	db, err := sql.Open("postgres", matchingSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	seedPublishedTask(t, ctx, db)
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := matching.NewSnapshotService(store, matching.DefaultShufflePolicy("shuffle-key-v1", []byte("0123456789abcdef0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	draft := postgresSnapshotDraft("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	first, replay, err := service.CreateRevision(ctx, draft)
	if err != nil || replay || first.MatchRevision != 1 {
		t.Fatalf("create snapshot: snapshot=%#v replay=%v err=%v", first, replay, err)
	}
	replayed, replay, err := service.CreateRevision(ctx, draft)
	if err != nil || !replay || replayed.ID != first.ID {
		t.Fatalf("replay snapshot: snapshot=%#v replay=%v err=%v", replayed, replay, err)
	}
	draft.Key.EffectiveInputHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	second, replay, err := service.CreateRevision(ctx, draft)
	if err != nil || replay || second.MatchRevision != 2 {
		t.Fatalf("create second revision: snapshot=%#v replay=%v err=%v", second, replay, err)
	}
	assertCount(t, db, `SELECT count(*) FROM match_snapshots`, 2)
	assertCount(t, db, `SELECT count(*) FROM match_snapshot_candidates`, 8)
	if _, err = db.ExecContext(ctx, `UPDATE match_snapshots SET model_version='tampered' WHERE snapshot_id=$1`, first.ID); err == nil {
		t.Fatal("sealed snapshot update unexpectedly succeeded")
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO match_snapshot_candidates (snapshot_id,candidate_index,agent_id,provider_id,price_version,overview_price,formal_price,external_cost_cap,evaluation_status,exclusion_reasons,recall_evidence,qualified,qualification_reasons,exploration) VALUES ($1,99,'late','provider',0,'0','0','0','excluded','[]','{}',false,'[]',false)`, first.ID); err == nil {
		t.Fatal("sealed snapshot candidate insert unexpectedly succeeded")
	}

	concurrent := postgresSnapshotDraft("sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	const callers = 12
	var wait sync.WaitGroup
	ids := make(chan string, callers)
	created := make(chan bool, callers)
	errorsFound := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, wasReplay, createErr := service.CreateRevision(ctx, concurrent)
			if createErr != nil {
				errorsFound <- createErr
				return
			}
			ids <- value.ID
			created <- !wasReplay
		}()
	}
	wait.Wait()
	close(ids)
	close(created)
	close(errorsFound)
	for createErr := range errorsFound {
		t.Fatal(createErr)
	}
	unique := map[string]struct{}{}
	for id := range ids {
		unique[id] = struct{}{}
	}
	createdCount := 0
	for value := range created {
		if value {
			createdCount++
		}
	}
	if len(unique) != 1 || createdCount != 1 {
		t.Fatalf("concurrent snapshot identity diverged: ids=%v creators=%d", unique, createdCount)
	}
	assertCount(t, db, `SELECT count(*) FROM match_snapshots`, 3)
}

func seedPublishedTask(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ('publisher')`); err != nil {
		t.Fatal(err)
	}
	criteria := `[{"id":"quality","title":"Quality","description":"Complete","weight":100}]`
	if _, err := db.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,status,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,delivery_format,draft_acceptance,created_at,updated_at) VALUES ('task-snapshot','publisher','draft','Title','Description','research','zh-CN',100,1000,50,$1,'markdown',$2,$3,$3)`, now.Add(24*time.Hour), criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO task_spec_versions (task_id,version_no,task_aggregate_version,content_hash,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,inputs,allowed_tools,exclusions,delivery_format,created_at) VALUES ('task-snapshot',1,2,'sha256:1111111111111111111111111111111111111111111111111111111111111111','Title','Description','research','zh-CN',100,1000,50,$1,'{}','{}','{}','markdown',$2)`, now.Add(24*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO acceptance_versions (task_id,version_no,task_aggregate_version,content_hash,criteria,total_weight,created_at) VALUES ('task-snapshot',1,2,'sha256:2222222222222222222222222222222222222222222222222222222222222222',$1,100,$2)`, criteria, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tasks SET status='pending_escrow',current_spec_version=1,current_acceptance_version=1,published_at=$1,aggregate_version=2,updated_at=$1 WHERE task_id='task-snapshot'`, now); err != nil {
		t.Fatal(err)
	}
}

func postgresSnapshotDraft(effectiveInputHash string) matching.SnapshotDraft {
	scored := make([]matching.ScoredCandidate, 0, 4)
	for index := range 4 {
		score := 90 - index
		scored = append(scored, matching.ScoredCandidate{
			Candidate: matching.Candidate{AgentID: fmt.Sprintf("agent-%d", index), ProviderID: fmt.Sprintf("provider-%d", index), PriceVersion: 1, OverviewPrice: "10", FormalPrice: "100", ExternalCostCap: "0", ExposureCount: 200, EffectiveSamples: 100},
			Recall:    map[string]matching.RecallEvidence{},
			Score:     matching.ScoreBreakdown{TaskMatch: 50, Reputation: 25, PriceTime: 10, Availability: 5, RuleScore: score, RankingScore: score},
		})
	}
	return matching.SnapshotDraft{
		Key:         matching.SnapshotKey{TaskID: "task-snapshot", TaskSpecHash: "sha256:1111111111111111111111111111111111111111111111111111111111111111", AlgorithmVersion: matching.FairShuffleAlgorithmVersion, EffectiveInputHash: effectiveInputHash},
		RuleVersion: "matching-rules-v1", ModelVersion: "disabled",
		Result: matching.Result{Scored: scored, Qualified: append([]matching.ScoredCandidate{}, scored...), Degradations: []matching.Degradation{}},
	}
}

func matchingSearchPath(databaseURL, schema string) string {
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

func assertCount(t *testing.T, db *sql.DB, query string, expected int) {
	t.Helper()
	var actual int
	if err := db.QueryRow(query).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q: expected %d, got %d", query, expected, actual)
	}
}

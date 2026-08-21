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
	"sync"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/persistence"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	enginetask "github.com/example/agent-platform/engine/internal/task"
	"github.com/lib/pq"
)

func TestPostgresTaskPublicationOwnershipHashesConcurrencyAndRedelivery(t *testing.T) {
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
	schema := fmt.Sprintf("engine_t105_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", withSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"publisher-a", "publisher-b"} {
		if _, err = db.ExecContext(ctx, `INSERT INTO users(user_id) VALUES ($1)`, userID); err != nil {
			t.Fatal(err)
		}
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	service, err := enginetask.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	owner := auth.Session{UserID: "publisher-a", Roles: []string{"publisher"}}
	other := auth.Session{UserID: "publisher-b", Roles: []string{"publisher"}}
	draft := validDraft()
	expiredInput := validDraft()
	expiredInput.Deadline = time.Now().UTC().Add(-time.Hour)
	if _, _, err = service.Create(ctx, owner, "expired-create", expiredInput); !errors.Is(err, enginetask.ErrInvalidInput) {
		t.Fatalf("expired create: %v", err)
	}
	updateTarget, _, err := service.Create(ctx, owner, "update-target", validDraft())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.UpdateDraft(ctx, owner, "expired-update", updateTarget.ID, enginetask.UpdateDraftInput{DraftInput: expiredInput, ExpectedVersion: 1}); !errors.Is(err, enginetask.ErrInvalidInput) {
		t.Fatalf("expired update: %v", err)
	}
	const expiredTaskID = "task_expired_publication"
	criteria := `[{"id":"quality","title":"Quality","description":"Accurate","weight":100}]`
	if _, err = db.ExecContext(ctx, `INSERT INTO tasks (task_id,publisher_id,title,description,expert_type,language,overview_budget,formal_budget,external_cost_cap,deadline,delivery_format,draft_acceptance,created_at,updated_at) VALUES ($1,$2,'Expired','Expired draft','research','en',1,10,0,now()-interval '1 hour','markdown',$3,now()-interval '2 hours',now()-interval '2 hours')`, expiredTaskID, owner.UserID, criteria); err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.Publish(ctx, owner, "expired-publish", expiredTaskID, enginetask.PublishInput{ExpectedVersion: 1}); !errors.Is(err, enginetask.ErrInvalidInput) {
		t.Fatalf("expired publish: %v", err)
	}
	assertCount(t, db, `SELECT count(*) FROM task_spec_versions WHERE task_id=$1`, expiredTaskID, 0)
	created, replay, err := service.Create(ctx, owner, "create", draft)
	if err != nil || replay || created.Status != enginetask.StatusDraft || created.AggregateVersion != 1 {
		t.Fatalf("create: value=%#v replay=%v err=%v", created, replay, err)
	}
	createdReplay, replay, err := service.Create(ctx, owner, "create", draft)
	if err != nil || !replay || createdReplay.ID != created.ID {
		t.Fatalf("create replay: value=%#v replay=%v err=%v", createdReplay, replay, err)
	}
	changedDraft := draft
	changedDraft.Title = "Different"
	if _, _, err = service.Create(ctx, owner, "create", changedDraft); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("create key reuse: %v", err)
	}
	if _, _, err = service.UpdateDraft(ctx, other, "steal", created.ID, enginetask.UpdateDraftInput{DraftInput: draft, ExpectedVersion: 1}); !errors.Is(err, enginetask.ErrNotFound) {
		t.Fatalf("other publisher update: %v", err)
	}
	draft.Description = "Updated immutable source content"
	updated, replay, err := service.UpdateDraft(ctx, owner, "update", created.ID, enginetask.UpdateDraftInput{DraftInput: draft, ExpectedVersion: 1})
	if err != nil || replay || updated.AggregateVersion != 2 || updated.Description != draft.Description {
		t.Fatalf("update: value=%#v replay=%v err=%v", updated, replay, err)
	}
	if _, _, err = service.UpdateDraft(ctx, owner, "stale", created.ID, enginetask.UpdateDraftInput{DraftInput: draft, ExpectedVersion: 1}); !errors.Is(err, enginetask.ErrStaleVersion) {
		t.Fatalf("stale update: %v", err)
	}

	publications := make([]enginetask.Publication, 2)
	replays := make([]bool, 2)
	errorsFound := make([]error, 2)
	var group sync.WaitGroup
	for index := range publications {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			publications[index], replays[index], errorsFound[index] = service.Publish(ctx, owner, "publish", created.ID, enginetask.PublishInput{ExpectedVersion: 2})
		}(index)
	}
	group.Wait()
	for _, publishErr := range errorsFound {
		if publishErr != nil {
			t.Fatal(publishErr)
		}
	}
	if replays[0] == replays[1] || publications[0].Spec.ContentHash != publications[1].Spec.ContentHash || publications[0].Acceptance.ContentHash != publications[1].Acceptance.ContentHash {
		t.Fatalf("expected one stable execution and replay: replays=%v publications=%#v", replays, publications)
	}
	publication := publications[0]
	if publication.Task.Status != enginetask.StatusPendingEscrow || publication.Task.AggregateVersion != 3 || publication.Spec.Version != 1 || publication.Acceptance.Version != 1 || publication.Acceptance.TotalWeight != 100 || publication.Spec.ContentHash == publication.Acceptance.ContentHash {
		t.Fatalf("invalid publication: %#v", publication)
	}
	if _, _, err = service.UpdateDraft(ctx, owner, "after-publish", created.ID, enginetask.UpdateDraftInput{DraftInput: draft, ExpectedVersion: 3}); !errors.Is(err, enginetask.ErrInvalidState) {
		t.Fatalf("published draft update: %v", err)
	}
	if _, _, err = service.Publish(ctx, owner, "publish-again", created.ID, enginetask.PublishInput{ExpectedVersion: 3}); !errors.Is(err, enginetask.ErrInvalidState) {
		t.Fatalf("second publication: %v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE task_spec_versions SET description='tampered' WHERE task_id=$1 AND version_no=1`, created.ID); err == nil {
		t.Fatal("immutable spec version accepted update")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM acceptance_versions WHERE task_id=$1 AND version_no=1`, created.ID); err == nil {
		t.Fatal("immutable acceptance version accepted delete")
	}
	if _, err = db.ExecContext(ctx, `UPDATE tasks SET description='tampered' WHERE task_id=$1`, created.ID); err == nil {
		t.Fatal("published task accepted content overwrite")
	}
	assertCount(t, db, `SELECT count(*) FROM task_spec_versions WHERE task_id=$1`, created.ID, 1)
	assertCount(t, db, `SELECT count(*) FROM acceptance_versions WHERE task_id=$1`, created.ID, 1)
	assertCount(t, db, `SELECT count(*) FROM domain_events WHERE aggregate_type='task' AND aggregate_id=$1`, created.ID, 3)
	assertCount(t, db, `SELECT count(*) FROM audit_events WHERE resource_type='task' AND resource_id=$1`, created.ID, 3)
	assertCount(t, db, `SELECT count(*) FROM outbox_messages WHERE dedupe_key=$1 AND topic='task-events'`, fmt.Sprintf("task:%s:published:3", created.ID), 1)
	var specHash, acceptanceHash string
	if err = db.QueryRowContext(ctx, `SELECT content_hash FROM task_spec_versions WHERE task_id=$1 AND version_no=1`, created.ID).Scan(&specHash); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT content_hash FROM acceptance_versions WHERE task_id=$1 AND version_no=1`, created.ID).Scan(&acceptanceHash); err != nil {
		t.Fatal(err)
	}
	if specHash != publication.Spec.ContentHash || acceptanceHash != publication.Acceptance.ContentHash {
		t.Fatalf("stored hashes differ from response: %s/%s %s/%s", specHash, publication.Spec.ContentHash, acceptanceHash, publication.Acceptance.ContentHash)
	}
}

func validDraft() enginetask.DraftInput {
	return enginetask.DraftInput{Title: "Research", Description: "Research the market", ExpertType: "research", Language: "en", OverviewBudget: "100", FormalBudget: "1000", ExternalCostCap: "50", Deadline: time.Now().UTC().Add(24 * time.Hour), Inputs: []string{"market.csv"}, AllowedTools: []string{"search"}, Exclusions: []string{"PII"}, DeliveryFormat: "markdown", AcceptanceCriteria: []enginetask.AcceptanceCriterion{{ID: "accuracy", Title: "Accuracy", Description: "Claims are accurate", Weight: 60}, {ID: "coverage", Title: "Coverage", Description: "All markets covered", Weight: 40}}}
}

func withSearchPath(databaseURL, schema string) string {
	parsed, err := url.Parse(databaseURL)
	if err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		query.Set("binary_parameters", "yes")
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	separator := "?"
	if strings.Contains(databaseURL, "?") {
		separator = "&"
	}
	return databaseURL + separator + "search_path=" + url.QueryEscape(schema) + "&binary_parameters=yes"
}

func assertCount(t *testing.T, db *sql.DB, query string, argument any, expected int) {
	t.Helper()
	var actual int
	if err := db.QueryRow(query, argument).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q: expected %d, got %d", query, expected, actual)
	}
}

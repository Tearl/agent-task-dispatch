//go:build integration

package postgres

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/agent-platform/engine/internal/agent"
	agentpostgres "github.com/example/agent-platform/engine/internal/agent/postgres"
	"github.com/example/agent-platform/engine/internal/auth"
	"github.com/example/agent-platform/engine/internal/credential"
	"github.com/example/agent-platform/engine/internal/persistence"
	persistencepostgres "github.com/example/agent-platform/engine/internal/persistence/postgres"
	"github.com/lib/pq"
)

func TestPostgresCredentialRotationIsOwnerOnlyEncryptedImmutableAndIdempotent(t *testing.T) {
	baseURL := os.Getenv("ENGINE_TEST_POSTGRES_URL")
	if baseURL == "" {
		t.Skip("ENGINE_TEST_POSTGRES_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adminDB, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	schema := fmt.Sprintf("engine_t104_%d", time.Now().UnixNano())
	if _, err = adminDB.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = adminDB.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
	}()
	db, err := sql.Open("postgres", credentialSearchPath(baseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = persistencepostgres.ApplyMigrations(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, userID := range []string{"owner-a", "owner-b", "admin", "arbitrator"} {
		if _, err = db.ExecContext(ctx, `INSERT INTO users (user_id) VALUES ($1)`, userID); err != nil {
			t.Fatal(err)
		}
	}
	agentStore, err := agentpostgres.NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	agentService, err := agent.NewService(agentStore)
	if err != nil {
		t.Fatal(err)
	}
	ownerA := auth.Session{UserID: "owner-a", Roles: []string{"agent_provider"}}
	ownerB := auth.Session{UserID: "owner-b", Roles: []string{"agent_provider"}}
	created, _, err := agentService.Create(ctx, ownerA, "create-agent", credentialAgentInput())
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	encryptor, err := credential.NewAESGCMEncryptor(bytes.Repeat([]byte{0x71}, 32), bytes.Repeat([]byte{0x72}, 32), "integration-key-v1")
	if err != nil {
		t.Fatal(err)
	}
	service, err := credential.NewService(store, encryptor)
	if err != nil {
		t.Fatal(err)
	}
	firstSecret := "sk_live_first_secret_never_persist_plaintext"
	firstInput := credential.RotateInput{CredentialType: credential.TypeAPIKey, Label: "production", Secret: firstSecret, ExpectedVersion: 1}
	metadata, replay, err := service.Rotate(ctx, ownerA, "rotate-1", created.ID, firstInput)
	if err != nil || replay || metadata.Version != 1 || metadata.AgentAggregateVersion != 2 || metadata.Fingerprint == "" {
		t.Fatalf("first rotation: metadata=%#v replay=%v err=%v", metadata, replay, err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{firstSecret, "ciphertext", "nonce", "secretDigest", "keyReference", "encryptionAlgorithm"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("credential response exposed %q: %s", forbidden, encoded)
		}
	}
	replayed, replay, err := service.Rotate(ctx, ownerA, "rotate-1", created.ID, firstInput)
	if err != nil || !replay || replayed != metadata {
		t.Fatalf("rotation replay: metadata=%#v replay=%v err=%v", replayed, replay, err)
	}
	rotatedKEK, err := credential.NewAESGCMEncryptor(bytes.Repeat([]byte{0x73}, 32), bytes.Repeat([]byte{0x72}, 32), "integration-key-v2")
	if err != nil {
		t.Fatal(err)
	}
	serviceAfterKEKRotation, err := credential.NewService(store, rotatedKEK)
	if err != nil {
		t.Fatal(err)
	}
	replayedAfterKEKRotation, replay, err := serviceAfterKEKRotation.Rotate(ctx, ownerA, "rotate-1", created.ID, firstInput)
	if err != nil || !replay || replayedAfterKEKRotation != metadata {
		t.Fatalf("rotation replay after KEK change: metadata=%#v replay=%v err=%v", replayedAfterKEKRotation, replay, err)
	}
	different := firstInput
	different.Secret = "different-secret"
	if _, _, err = service.Rotate(ctx, ownerA, "rotate-1", created.ID, different); !errors.Is(err, persistence.ErrIdempotencyConflict) {
		t.Fatalf("same idempotency key accepted different secret: %v", err)
	}
	if _, _, err = service.Rotate(ctx, ownerB, "foreign", created.ID, firstInput); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("other owner rotation: %v", err)
	}
	for _, session := range []auth.Session{{UserID: "admin", Roles: []string{"admin"}}, {UserID: "arbitrator", Roles: []string{"arbitrator"}}, {UserID: "admin", Roles: []string{"admin", "agent_provider"}}, {UserID: "arbitrator", Roles: []string{"arbitrator", "agent_provider"}}} {
		if _, _, err = service.Rotate(ctx, session, "forbidden-"+session.UserID, created.ID, firstInput); !errors.Is(err, credential.ErrForbidden) {
			t.Fatalf("%s rotation: %v", session.UserID, err)
		}
	}
	secondSecret := "bearer_second_secret_never_persist_plaintext"
	secondInput := credential.RotateInput{CredentialType: credential.TypeBearerToken, Label: "production", Secret: secondSecret, ExpectedVersion: 2}
	second, _, err := service.Rotate(ctx, ownerA, "rotate-2", created.ID, secondInput)
	if err != nil || second.Version != 2 || second.AgentAggregateVersion != 3 || second.Fingerprint == metadata.Fingerprint {
		t.Fatalf("second rotation: metadata=%#v err=%v", second, err)
	}

	rows, err := db.QueryContext(ctx, `SELECT ciphertext,nonce,wrapped_data_key,key_nonce,encryption_algorithm,key_wrap_algorithm,key_reference FROM agent_credential_versions WHERE agent_id=$1 ORDER BY version_no`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var ciphertext, nonce, wrappedDataKey, keyNonce []byte
		var algorithm, keyWrapAlgorithm, keyReference string
		if err = rows.Scan(&ciphertext, &nonce, &wrappedDataKey, &keyNonce, &algorithm, &keyWrapAlgorithm, &keyReference); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(ciphertext, []byte(firstSecret)) || bytes.Contains(ciphertext, []byte(secondSecret)) || len(nonce) != 12 || len(wrappedDataKey) != 48 || len(keyNonce) != 12 || algorithm != credential.AlgorithmAES256GCM || keyWrapAlgorithm != credential.AlgorithmAES256GCM || keyReference != "integration-key-v1" {
			t.Fatalf("invalid stored envelope: ciphertext=%x nonce=%x wrappedKey=%x keyNonce=%x algorithm=%q wrap=%q key=%q", ciphertext, nonce, wrappedDataKey, keyNonce, algorithm, keyWrapAlgorithm, keyReference)
		}
		count++
	}
	if err = rows.Err(); err != nil || count != 2 {
		t.Fatalf("credential versions: count=%d err=%v", count, err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE agent_credential_versions SET label='changed' WHERE agent_id=$1 AND version_no=1`, created.ID); err == nil {
		t.Fatal("database allowed credential history update")
	}
	if _, err = db.ExecContext(ctx, `DELETE FROM agent_credential_versions WHERE agent_id=$1 AND version_no=1`, created.ID); err == nil {
		t.Fatal("database allowed credential history delete")
	}
	assertNoSecret(t, db, firstSecret)
	assertNoSecret(t, db, secondSecret)
	assertCredentialCount(t, db, `SELECT count(*) FROM outbox_messages`, nil, 0)
	assertCredentialCount(t, db, `SELECT count(*) FROM domain_events WHERE aggregate_id=$1 AND event_type='agent.credential_rotated'`, created.ID, 2)
	assertCredentialCount(t, db, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='agent.credential_rotated'`, created.ID, 2)
	assertCredentialCount(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='agent_credential_versions' AND (column_name LIKE '%plain%' OR column_name IN ('secret','api_key','token','credential_value'))`, nil, 0)

	retired, _, err := agentService.Transition(ctx, ownerA, "retire", created.ID, agent.LifecycleInput{Status: agent.StatusRetired, ExpectedVersion: 3})
	if err != nil || retired.AggregateVersion != 4 {
		t.Fatalf("retire agent: %#v %v", retired, err)
	}
	retiredInput := firstInput
	retiredInput.ExpectedVersion = 4
	if _, _, err = service.Rotate(ctx, ownerA, "retired-rotation", created.ID, retiredInput); !errors.Is(err, credential.ErrInvalidState) {
		t.Fatalf("retired credential rotation: %v", err)
	}
}

func assertNoSecret(t *testing.T, db *sql.DB, secret string) {
	t.Helper()
	for _, query := range []string{
		`SELECT count(*) FROM idempotency_records WHERE convert_from(response_body,'UTF8') LIKE '%' || $1 || '%' OR request_hash LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM domain_events WHERE payload::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM audit_events WHERE metadata::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM outbox_messages WHERE payload::text LIKE '%' || $1 || '%'`,
	} {
		assertCredentialCount(t, db, query, secret, 0)
	}
}

func assertCredentialCount(t *testing.T, db *sql.DB, query string, argument any, expected int) {
	t.Helper()
	var actual int
	var err error
	if argument == nil {
		err = db.QueryRow(query).Scan(&actual)
	} else {
		err = db.QueryRow(query, argument).Scan(&actual)
	}
	if err != nil {
		t.Fatal(err)
	}
	if actual != expected {
		t.Fatalf("query %q: expected %d, got %d", query, expected, actual)
	}
}

func credentialAgentInput() agent.CreateInput {
	return agent.CreateInput{Name: "Credential Agent", Category: "research", Tags: []string{"secure"}, Capabilities: "Secure calls", Languages: []string{"en"}, EstimatedDurationSeconds: 60, AuthorBio: "Owner", EndpointURL: "https://agent.example", ControllerAddress: "0x1111111111111111111111111111111111111111", PayoutAddress: "0x2222222222222222222222222222222222222222", MaxConcurrency: 1}
}

func credentialSearchPath(databaseURL, schema string) string {
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

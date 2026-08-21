package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestFoundationMigrationContainsRequiredConstraints(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000001_foundation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"PRIMARY KEY (scope, idempotency_key)", "response_body bytea NOT NULL", "UNIQUE (aggregate_type, aggregate_id, aggregate_version)", "dedupe_key text NOT NULL UNIQUE", "WHERE published_at IS NULL"} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
}

func TestFoundationDownMigrationPreservesImmutableHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000001_foundation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToUpper(string(contents)), "DROP TABLE") {
		t.Fatal("foundation rollback must preserve immutable history")
	}
}

func TestMigrationFilesAreOrderedAndTransactionManagedByRunner(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000001_foundation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "BEGIN;") || strings.Contains(upper, "COMMIT;") {
		t.Fatal("embedded migrations must not manage transactions; ApplyMigrations owns the transaction")
	}
}

func TestAuthenticationMigrationEnforcesHashedSessionsAndRoleBounds(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000002_auth.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"token_hash text NOT NULL UNIQUE", "consumed_at timestamptz", "CHECK (expires_at > issued_at)", "role IN ('publisher', 'agent_provider', 'admin', 'arbitrator')", "WHERE revoked_at IS NULL", "auth_rate_limit_buckets", "auth_rate_limit_buckets_expiry_idx"} {
		if !strings.Contains(sql, required) {
			t.Errorf("authentication migration missing %q", required)
		}
	}
	if strings.Contains(sql, "signature") || strings.Contains(sql, "private_key") {
		t.Fatal("authentication schema must not persist signatures or private keys")
	}
}

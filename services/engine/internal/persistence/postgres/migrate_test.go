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

func TestAgentMigrationEnforcesLifecyclePriceAndCapacityInvariants(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000003_agents.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"activated_at timestamptz",
		"overview_price >= 0",
		"overview_price <= formal_package_gross_price",
		"included_versions = 3",
		"max_versions = 5",
		"UNIQUE (agent_id, fencing_token)",
		"activated agent addresses are immutable",
		"agent price versions are immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("agent migration missing %q", required)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"task_bond", "agent_bond", "task_deposit", "agent_deposit"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("agent schema must not define task-level bond/deposit field %q", forbidden)
		}
	}
}

func TestAgentDownMigrationPreservesImmutableHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000003_agents.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("agent rollback must preserve immutable price and audit history")
	}
}

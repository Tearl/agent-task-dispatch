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

func TestAgentEndpointMigrationIsAdditiveAndBounded(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000006_agent_endpoint.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"ADD COLUMN IF NOT EXISTS endpoint_url", "NOT NULL DEFAULT ''", "length(endpoint_url) <= 2048"} {
		if !strings.Contains(sql, required) {
			t.Errorf("Agent endpoint migration missing %q", required)
		}
	}
}

func TestAgentCredentialMigrationStoresOnlyEncryptedImmutableVersions(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000004_agent_credentials.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"ciphertext bytea NOT NULL",
		"nonce bytea NOT NULL",
		"wrapped_data_key bytea NOT NULL",
		"key_nonce bytea NOT NULL",
		"encryption_algorithm text NOT NULL",
		"key_wrap_algorithm text NOT NULL",
		"key_reference text NOT NULL",
		"fingerprint text NOT NULL",
		"agent credential versions are immutable",
		"agents_current_credential_fk",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("agent credential migration missing %q", required)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"plaintext", "api_key text", "secret text", "credential_value"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("credential schema contains plaintext-shaped field %q", forbidden)
		}
	}
}

func TestAgentCredentialDownMigrationPreservesCiphertextHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000004_agent_credentials.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("credential rollback must preserve encrypted history")
	}
}

func TestTaskMigrationFreezesHashedSpecAndWeightedAcceptanceVersions(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000005_tasks.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"content_hash text NOT NULL",
		"task_spec_versions_immutable",
		"acceptance_versions_immutable",
		"total_weight integer NOT NULL CHECK (total_weight = 100)",
		"tasks_current_spec_fk",
		"tasks_current_acceptance_fk",
		"published task content is immutable",
		"valid_task_acceptance_criteria",
		"current_spec_version IS NOT DISTINCT FROM current_acceptance_version",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("task migration missing %q", required)
		}
	}
}

func TestTaskDownMigrationPreservesImmutablePublicationHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000005_tasks.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("task rollback must preserve immutable publication history")
	}
}

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

func TestOutboxWorkerMigrationAddsLeasingRetryAndDeadLetterState(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000019_outbox_worker.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"locked_by text",
		"locked_until timestamptz",
		"last_error text",
		"dead_lettered_at timestamptz",
		"outbox_messages_lock_pair",
		"outbox_messages_last_error_safe",
		"outbox_messages_dead_letter_time",
		"outbox_messages_claim_idx",
		"published_at IS NULL AND dead_lettered_at IS NULL",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("outbox worker migration missing %q", required)
		}
	}
}

func TestOutboxWorkerRollbackPreservesDeadLetterEvidence(t *testing.T) {
	contents, err := os.ReadFile("migrations/000019_outbox_worker.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(contents))
	for _, forbidden := range []string{"drop column", "drop table", "delete from", "truncate"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("outbox rollback destroys operational evidence with %q", forbidden)
		}
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

func TestFormalDeliveryMigrationFreezesScopeAndSerializesVersions(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000014_formal_delivery.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"formal_scope_snapshots",
		"formal_versions_one_active_per_package_uidx",
		"UNIQUE (package_id, package_aggregate_version)",
		"included_versions = 3",
		"maximum_versions = 5",
		"formal scope snapshots are immutable",
		"formal version command is immutable",
		"formal_billing_results",
		"charge_amount = 0",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("formal delivery migration missing %q", required)
		}
	}
	if !strings.Contains(sql, "parent_version = version_no - 1") || !strings.Contains(sql, "feedback_set_id ~") || !strings.Contains(sql, "work_nonce bigint NOT NULL") {
		t.Fatal("formal revisions must bind the parent, feedback and work nonce")
	}
}

func TestFormalDeliveryRollbackPreservesHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000014_formal_delivery.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(contents))
	for _, forbidden := range []string{"drop table", "truncate", "delete from"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("formal delivery rollback must preserve immutable history: %q", forbidden)
		}
	}
}

func TestFormalFeedbackProofMigrationIsAppendOnlyAndNonceGated(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000015_formal_feedback_proofs.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"formal_feedback_sets", "formal_feedback_items", "formal_feedback_responses", "formal_version_changes", "formal_delivery_proofs", "feedback_aggregate_version", "formal_versions_feedback_set_uidx", "work_nonce_advanced", "immutable"} {
		if !strings.Contains(sql, required) {
			t.Errorf("formal feedback migration missing %q", required)
		}
	}
	down, err := os.ReadFile("migrations/000015_formal_feedback_proofs.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(down))
	for _, forbidden := range []string{"drop table", "truncate", "delete from"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("formal proof rollback destroys history: %q", forbidden)
		}
	}
}

func TestFormalChangeOrderMigrationEnforcesFundingScopeAndVersionBounds(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000016_formal_change_orders.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"formal_change_orders", "target_version IN (4,5)", "agent_absorbed", "platform_incident", "change_order_escrow", "formal_versions_change_order_check", "formal_versions_change_order_uidx", "formal change order history is immutable", "consumed change order is immutable"} {
		if !strings.Contains(sql, required) {
			t.Errorf("formal change order migration missing %q", required)
		}
	}
	down, err := os.ReadFile("migrations/000016_formal_change_orders.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"drop table", "truncate", "delete from"} {
		if strings.Contains(strings.ToLower(string(down)), forbidden) {
			t.Fatalf("change order rollback destroys history: %q", forbidden)
		}
	}
}

func TestFormalAcceptanceMigrationIsAppendOnlyAndSettlementBounded(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000017_formal_acceptance.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"formal_acceptance_intents", "formal_acceptance_states", "intent_recorded", "pending_confirmation", "confirmed", "orphaned", "change_order_release", "formal_change_order_settlements", "formal acceptance history is immutable", "authorized funding boundary"} {
		if !strings.Contains(sql, required) {
			t.Errorf("formal acceptance migration missing %q", required)
		}
	}
	down, err := os.ReadFile("migrations/000017_formal_acceptance.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"drop table", "truncate", "delete from"} {
		if strings.Contains(strings.ToLower(string(down)), forbidden) {
			t.Fatalf("formal acceptance rollback destroys history: %q", forbidden)
		}
	}
}

func TestDisputeMigrationPreservesWORMEvidenceAndAuditedRepairs(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000018_disputes_admin.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{"dispute_cases", "dispute_events", "dispute_worm_receipts", "retention_mode='COMPLIANCE'", "dispute_evidence_manifest", "dispute_evidence_access_grants", "15 minutes", "dispute_conflict_declarations", "dispute_review_fee_authorizations", "dispute_admin_operations", "dispute_allocation", "dispute evidence and audit history is immutable", "dispute_frozen", "dispute_allocation_finalized"} {
		if !strings.Contains(sql, required) {
			t.Errorf("dispute migration missing %q", required)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"private_key", "signature text", "credential_value", "plaintext"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("dispute schema persists forbidden material %q", forbidden)
		}
	}
	down, err := os.ReadFile("migrations/000018_disputes_admin.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(down))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("dispute rollback must preserve cases, evidence, decisions and audits")
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

func TestMatchingMigrationEnforcesRevisionIdentityAndImmutableSnapshots(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000007_matching_snapshots.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"UNIQUE (task_id, match_revision)",
		"UNIQUE (task_id, task_spec_hash, algorithm_version, effective_input_hash)",
		"seed_digest text NOT NULL",
		"seed_key_version text NOT NULL",
		"policy_hash text NOT NULL",
		"probability_numerator integer",
		"probability_denominator integer",
		"CHECK (NOT exploration OR final_position = 3)",
		"matching snapshots are immutable",
		"sealed matching snapshot candidates are immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("matching migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(sql), "seed_secret") {
		t.Fatal("matching schema must never persist shuffle seed secrets")
	}
}

func TestMatchingDownMigrationPreservesSnapshotHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000007_matching_snapshots.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("matching rollback must preserve immutable snapshot history")
	}
}

func TestExecutionMigrationSeparatesLogicalWorkAttemptsAndCallbackEvidence(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000008_agent_executions.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"task_spec_versions_task_version_hash_uidx",
		"logical_executions",
		"input_ref text NOT NULL",
		"input_hash text NOT NULL",
		"idempotency_key text NOT NULL UNIQUE",
		"execution_attempts",
		"fencing_token bigint",
		"callback_nonce_hash text UNIQUE",
		"execution_callback_events",
		"nonce_hash text NOT NULL UNIQUE",
		"logical execution specification is immutable",
		"terminal logical execution is immutable",
		"execution attempt identity and fencing are immutable",
		"terminal execution attempt is immutable",
		"execution callback events are immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("execution migration missing %q", required)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"signature text", "callback_nonce text", "api_key", "private_key"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("execution schema persists secret-shaped field %q", forbidden)
		}
	}
}

func TestExecutionDownMigrationPreservesCallbackEvidence(t *testing.T) {
	contents, err := os.ReadFile("migrations/000008_agent_executions.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("execution rollback must preserve callback evidence")
	}
}

func TestOverviewMigrationEnforcesAllocationResultAndReplacementUniqueness(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000009_overview_orchestration.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"snapshot_id text NOT NULL UNIQUE",
		"orchestration_version = 'overview-orchestration-v1'",
		"replacement_version = 'overview-replacement-v1'",
		"allocation_id text NOT NULL UNIQUE",
		"logical_execution_id text NOT NULL UNIQUE",
		"overview_slots_valid_content_unique",
		"overview replacement decision is monotonic",
		"overview validation evidence is immutable",
		"valid overview billing can only be released after batch obsolescence",
		"overview events are immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("overview migration missing %q", required)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"brief_body", "access_token", "api_key", "signature text"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("overview schema persists sensitive field %q", forbidden)
		}
	}
}

func TestOverviewDownMigrationPreservesBillingAndValidationEvidence(t *testing.T) {
	contents, err := os.ReadFile("migrations/000009_overview_orchestration.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("overview rollback must preserve billing and validation evidence")
	}
}

func TestFundsMigrationEnforcesIsolationBalanceAndImmutableHistory(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000010_funds_ledger.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"'discovery_pool','formal_escrow','change_order_escrow','dispute_fee_pool'",
		"UNIQUE (account_type, reference_id, asset_key)",
		"reserve_amount = overview_price + external_cost_cap",
		"fund journal must contain one-asset balanced double entries",
		"overview capture journal crossed its allocation boundary",
		"fund reversal must exactly invert the original journal",
		"DEFERRABLE INITIALLY DEFERRED",
		"fund balance can only change through ledger entries",
		"fund journals and entries are immutable",
		"reversal_of text UNIQUE",
		"terminal fund allocation evidence is immutable",
		"fund_allocation_events_immutable",
		"overview_slots_fund_allocation_fk",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("funds migration missing %q", required)
		}
	}
}

func TestFundsDownMigrationPreservesSettlementEvidence(t *testing.T) {
	contents, err := os.ReadFile("migrations/000010_funds_ledger.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("funds rollback must preserve accounts, allocations, journals, and entries")
	}
}

func TestSelectionMigrationEnforcesProofAssignmentAndReservationInvariants(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000011_selection_reservations.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"selection_version = 'selection-reservation-v1'",
		"UNIQUE (publisher_id, idempotency_key)",
		"selection_reservations_active_task_uidx",
		"assignment_id text NOT NULL UNIQUE",
		"allocation_id text NOT NULL UNIQUE",
		"selection_nonce text NOT NULL UNIQUE",
		"formal_payable = formal_gross_price - overview_credit",
		"work_nonce bigint NOT NULL CHECK (work_nonce = 1)",
		"selection reservation identity is immutable",
		"selection history is immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("selection migration missing %q", required)
		}
	}
	lower := strings.ToLower(sql)
	for _, forbidden := range []string{"private_key", "proof_signature", "platform_signature", "signature text"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("selection schema persists secret/signature-shaped field %q", forbidden)
		}
	}
}

func TestSelectionDownMigrationPreservesAssignmentHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000011_selection_reservations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("selection rollback must preserve reservations, assignments, and events")
	}
}

func TestChainProjectionMigrationPreservesReorgAndReconciliationEvidence(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000012_chain_projection.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"authoritative-chain-projection-v1",
		"chain_projection_cursors",
		"chain_canonical_blocks",
		"chain_block_states",
		"chain_event_states",
		"selection_call boolean NOT NULL",
		"input_hash text NOT NULL",
		"chain_reconciliation_runs",
		"chain_reconciliation_differences",
		"assignment_states",
		"active_assignments",
		"chain_reorg_pending",
		"chain projection history is immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("chain projection migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"transaction_input text", "signature text", "private_key"} {
		if strings.Contains(strings.ToLower(sql), forbidden) {
			t.Errorf("chain projection persists unnecessary sensitive field %q", forbidden)
		}
	}
}

func TestChainProjectionDownMigrationPreservesAuditHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000012_chain_projection.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("chain projection rollback must preserve blocks, events, states, and reconciliation evidence")
	}
}

func TestSettlementProjectionMigrationSeparatesEarningsAndAddsReversibleJournals(t *testing.T) {
	contents, err := migrationFiles.ReadFile("migrations/000013_settlement_projection.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, required := range []string{
		"earnings_accrued",
		"earnings_withdrawn",
		"yield_eligibility_changed",
		"settlement_release",
		"settlement_refund",
		"earnings_withdrawal",
		"formal_agent_receivable",
		"chain_agent_earnings_positions",
		"chain_yield_positions",
		"chain_task_settlement_positions",
		"chain_canonical_blocks",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("settlement projection migration missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(sql), "private_key") {
		t.Fatal("settlement projection must not persist signing material")
	}
}

func TestSettlementProjectionDownMigrationPreservesJournalHistory(t *testing.T) {
	contents, err := os.ReadFile("migrations/000013_settlement_projection.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(string(contents))
	if strings.Contains(upper, "DROP TABLE") || strings.Contains(upper, "DELETE FROM") {
		t.Fatal("settlement rollback must preserve chain events and journal history")
	}
}

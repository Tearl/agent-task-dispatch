CREATE TABLE dispute_cases (
    case_id text PRIMARY KEY CHECK (case_id ~ '^sha256:[0-9a-f]{64}$'),
    task_id text NOT NULL REFERENCES tasks(task_id),
    assignment_id text NOT NULL REFERENCES assignments(assignment_id),
    delivery_unit_id text NOT NULL REFERENCES formal_packages(package_id),
    policy_version text NOT NULL CHECK (policy_version='platform-dispute-v1'),
    publisher_id text NOT NULL REFERENCES users(user_id),
    agent_provider_id text NOT NULL REFERENCES users(user_id),
    created_at timestamptz NOT NULL,
    UNIQUE (assignment_id,delivery_unit_id)
);

ALTER TABLE chain_events DROP CONSTRAINT chain_events_event_type_check;
ALTER TABLE chain_events ADD CONSTRAINT chain_events_event_type_check CHECK (event_type IN (
    'task_created','selection_confirmed','work_nonce_advanced','funds_released',
    'funds_refunded','earnings_accrued','earnings_withdrawn','yield_eligibility_changed',
    'dispute_opened','dispute_resolved','dispute_frozen','dispute_allocation_finalized'
));

ALTER TABLE fund_journals DROP CONSTRAINT fund_journals_journal_type_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_journal_type_check CHECK (journal_type IN (
    'funding','overview_capture','settlement_release','settlement_refund','earnings_withdrawal',
    'change_order_release','change_order_residual','dispute_allocation','reversal'
));
ALTER TABLE fund_journals DROP CONSTRAINT fund_journals_shape_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_shape_check CHECK (
    (journal_type='funding' AND allocation_id IS NULL AND reversal_of IS NULL)
 OR (journal_type='overview_capture' AND allocation_id IS NOT NULL AND reversal_of IS NULL)
 OR (journal_type IN ('settlement_release','settlement_refund','earnings_withdrawal','change_order_release','change_order_residual','dispute_allocation') AND allocation_id IS NULL AND reversal_of IS NULL)
 OR (journal_type='reversal' AND allocation_id IS NULL AND reversal_of IS NOT NULL)
);

CREATE TABLE dispute_case_projections (
    case_id text PRIMARY KEY REFERENCES dispute_cases(case_id),
    state text NOT NULL CHECK (state IN ('soft_lock_pending','frozen','evidence','decided','review_pending','final','orphaned')),
    aggregate_version bigint NOT NULL CHECK (aggregate_version>0),
    view_body jsonb NOT NULL CHECK (jsonb_typeof(view_body)='object'),
    updated_at timestamptz NOT NULL
);

CREATE TABLE dispute_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    case_id text REFERENCES dispute_cases(case_id),
    event_type text NOT NULL CHECK (event_type IN ('open','claim','freeze_submit','freeze_confirm','evidence','access','assign','decision','settlement','review','finalize','admin')),
    actor_id text NOT NULL REFERENCES users(user_id),
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL
);

CREATE TABLE dispute_requests (
    actor_id text NOT NULL REFERENCES users(user_id),
    idempotency_key text NOT NULL CHECK (idempotency_key<>''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    operation text NOT NULL,
    case_id text,
    response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body)='object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (actor_id,idempotency_key)
);

CREATE TABLE dispute_evidence_manifest (
    evidence_id text PRIMARY KEY CHECK (evidence_id ~ '^sha256:[0-9a-f]{64}$'),
    case_id text NOT NULL REFERENCES dispute_cases(case_id),
    claim_id text NOT NULL CHECK (claim_id ~ '^sha256:[0-9a-f]{64}$'),
    category text NOT NULL CHECK (category IN ('specification','overview','acceptance','formal_versions','feedback','change_orders','executions','usage','messages','callbacks','fees','policy')),
    object_key text NOT NULL UNIQUE CHECK (object_key<>''),
    ciphertext_digest text NOT NULL UNIQUE CHECK (ciphertext_digest ~ '^sha256:[0-9a-f]{64}$'),
    envelope_key_reference text NOT NULL CHECK (envelope_key_reference<>''),
    object_version_id text NOT NULL CHECK (object_version_id<>''),
    retention_mode text NOT NULL CHECK (retention_mode='COMPLIANCE'),
    retain_until timestamptz NOT NULL,
    submitted_by text NOT NULL REFERENCES users(user_id),
    created_at timestamptz NOT NULL,
    CHECK (retain_until>created_at)
);

CREATE TABLE dispute_worm_receipts (
    object_key text PRIMARY KEY CHECK (object_key<>''),
    ciphertext_digest text NOT NULL UNIQUE CHECK (ciphertext_digest ~ '^sha256:[0-9a-f]{64}$'),
    envelope_key_reference text NOT NULL CHECK (envelope_key_reference<>''),
    object_version_id text NOT NULL CHECK (object_version_id<>''),
    retention_mode text NOT NULL CHECK (retention_mode='COMPLIANCE'),
    retain_until timestamptz NOT NULL,
    storage_attestation text NOT NULL CHECK (storage_attestation ~ '^sha256:[0-9a-f]{64}$'),
    verified_at timestamptz NOT NULL,
    CHECK (retain_until>verified_at)
);

CREATE TABLE dispute_evidence_access_grants (
    grant_id text PRIMARY KEY CHECK (grant_id ~ '^sha256:[0-9a-f]{64}$'),
    case_id text NOT NULL REFERENCES dispute_cases(case_id),
    evidence_id text NOT NULL REFERENCES dispute_evidence_manifest(evidence_id),
    principal_id text NOT NULL REFERENCES users(user_id),
    purpose text NOT NULL CHECK (purpose ~ '^[a-z0-9][a-z0-9_-]{1,99}$'),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (expires_at>created_at AND expires_at<=created_at+interval '15 minutes')
);

-- Populated by the compliance identity service. Assignment commands only read
-- these append-only declarations and never trust conflict lists from clients.
CREATE TABLE dispute_conflict_declarations (
    declaration_id text PRIMARY KEY CHECK (declaration_id ~ '^sha256:[0-9a-f]{64}$'),
    case_id text NOT NULL REFERENCES dispute_cases(case_id),
    subject_user_id text NOT NULL REFERENCES users(user_id),
    conflict_code text NOT NULL CHECK (conflict_code ~ '^[a-z0-9][a-z0-9_-]{1,99}$'),
    attestation_hash text NOT NULL CHECK (attestation_hash ~ '^sha256:[0-9a-f]{64}$'),
    recorded_at timestamptz NOT NULL,
    UNIQUE (case_id,subject_user_id,conflict_code)
);

CREATE TABLE dispute_review_fee_authorizations (
    authorization_id text PRIMARY KEY CHECK (authorization_id ~ '^sha256:[0-9a-f]{64}$'),
    case_id text NOT NULL REFERENCES dispute_cases(case_id),
    assignee_id text NOT NULL REFERENCES users(user_id),
    payment_reference_hash text NOT NULL CHECK (payment_reference_hash ~ '^sha256:[0-9a-f]{64}$'),
    authorized_by text NOT NULL REFERENCES users(user_id),
    authorized_at timestamptz NOT NULL,
    UNIQUE (case_id,assignee_id)
);

CREATE TABLE dispute_admin_operations (
    operation_id text PRIMARY KEY CHECK (operation_id ~ '^sha256:[0-9a-f]{64}$'),
    operation_kind text NOT NULL CHECK (operation_kind IN ('dlq_replay','ledger_reversal','reconciliation_repair','state_migration')),
    resource_type text NOT NULL CHECK (resource_type<>''),
    resource_id text NOT NULL CHECK (resource_id<>''),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9][a-z0-9_-]{1,99}$'),
    payload_hash text NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
    actor_id text NOT NULL REFERENCES users(user_id),
    status text NOT NULL CHECK (status IN ('recorded','applied','failed','reversed')),
    created_at timestamptz NOT NULL
);

CREATE OR REPLACE FUNCTION reject_dispute_history_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'dispute evidence and audit history is immutable'; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER dispute_cases_immutable BEFORE UPDATE OR DELETE ON dispute_cases FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_events_immutable BEFORE UPDATE OR DELETE ON dispute_events FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_requests_immutable BEFORE UPDATE OR DELETE ON dispute_requests FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_evidence_manifest_immutable BEFORE UPDATE OR DELETE ON dispute_evidence_manifest FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_worm_receipts_immutable BEFORE UPDATE OR DELETE ON dispute_worm_receipts FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_evidence_access_grants_immutable BEFORE UPDATE OR DELETE ON dispute_evidence_access_grants FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_conflict_declarations_immutable BEFORE UPDATE OR DELETE ON dispute_conflict_declarations FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_review_fee_authorizations_immutable BEFORE UPDATE OR DELETE ON dispute_review_fee_authorizations FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();
CREATE TRIGGER dispute_admin_operations_immutable BEFORE UPDATE OR DELETE ON dispute_admin_operations FOR EACH ROW EXECUTE FUNCTION reject_dispute_history_mutation();

CREATE OR REPLACE FUNCTION protect_dispute_projection_identity() RETURNS trigger AS $$
BEGIN
  IF TG_OP='DELETE' OR NEW.case_id IS DISTINCT FROM OLD.case_id OR NEW.aggregate_version<=OLD.aggregate_version THEN
    RAISE EXCEPTION 'invalid dispute projection transition';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER dispute_projection_monotonic BEFORE UPDATE OR DELETE ON dispute_case_projections FOR EACH ROW EXECUTE FUNCTION protect_dispute_projection_identity();

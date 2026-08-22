ALTER TABLE formal_versions ADD COLUMN feedback_aggregate_version bigint;
ALTER TABLE formal_versions ADD CONSTRAINT formal_versions_feedback_aggregate_check
    CHECK ((version_no = 1 AND feedback_aggregate_version IS NULL)
        OR (version_no > 1 AND feedback_aggregate_version > 0));

CREATE TABLE formal_feedback_sets (
    feedback_set_id text PRIMARY KEY CHECK (feedback_set_id ~ '^sha256:[0-9a-f]{64}$'),
    feedback_version text NOT NULL CHECK (feedback_version = 'formal-feedback-v1'),
    package_id text NOT NULL REFERENCES formal_packages(package_id),
    parent_version integer NOT NULL,
    parent_content_hash text NOT NULL CHECK (parent_content_hash ~ '^sha256:[0-9a-f]{64}$'),
    scope_id text NOT NULL REFERENCES formal_scope_snapshots(scope_id),
    scope_hash text NOT NULL CHECK (scope_hash ~ '^sha256:[0-9a-f]{64}$'),
    feedback_digest text NOT NULL CHECK (feedback_digest ~ '^sha256:[0-9a-f]{64}$'),
    package_aggregate_version bigint NOT NULL CHECK (package_aggregate_version > 0),
    created_at timestamptz NOT NULL,
    UNIQUE (package_id, parent_version),
    UNIQUE (package_id, package_aggregate_version),
    FOREIGN KEY (package_id, parent_version) REFERENCES formal_versions(package_id, version_no)
);

CREATE TABLE formal_feedback_items (
    feedback_set_id text NOT NULL REFERENCES formal_feedback_sets(feedback_set_id),
    feedback_item_id text NOT NULL UNIQUE CHECK (feedback_item_id ~ '^sha256:[0-9a-f]{64}$'),
    ordinal integer NOT NULL CHECK (ordinal > 0),
    criterion_id text NOT NULL CHECK (criterion_id <> ''),
    category text NOT NULL CHECK (category IN ('defect','omission','security','runtime','clarification')),
    priority text NOT NULL CHECK (priority IN ('low','medium','high','blocker')),
    target text NOT NULL CHECK (target <> ''),
    description text NOT NULL CHECK (description <> ''),
    expected_outcome text NOT NULL CHECK (expected_outcome <> ''),
    scope_claim text NOT NULL CHECK (scope_claim IN ('in_scope','out_of_scope','uncertain')),
    PRIMARY KEY (feedback_set_id, ordinal)
);

CREATE TABLE formal_feedback_requests (
    publisher_id text NOT NULL REFERENCES users(user_id),
    idempotency_key text NOT NULL CHECK (idempotency_key <> ''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    task_id text NOT NULL REFERENCES tasks(task_id),
    feedback_set_id text NOT NULL REFERENCES formal_feedback_sets(feedback_set_id),
    response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body) = 'object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (publisher_id, idempotency_key)
);

ALTER TABLE formal_versions ADD CONSTRAINT formal_versions_feedback_set_fk
    FOREIGN KEY (feedback_set_id) REFERENCES formal_feedback_sets(feedback_set_id);
CREATE UNIQUE INDEX formal_versions_feedback_set_uidx ON formal_versions(feedback_set_id) WHERE feedback_set_id IS NOT NULL;

CREATE TABLE formal_feedback_responses (
    package_id text NOT NULL,
    version_no integer NOT NULL,
    feedback_item_id text NOT NULL REFERENCES formal_feedback_items(feedback_item_id),
    disposition text NOT NULL CHECK (disposition IN ('resolved','not_reproduced','declined')),
    summary text NOT NULL CHECK (summary <> ''),
    PRIMARY KEY (package_id, version_no, feedback_item_id),
    FOREIGN KEY (package_id, version_no) REFERENCES formal_versions(package_id, version_no)
);

CREATE TABLE formal_version_changes (
    package_id text NOT NULL,
    version_no integer NOT NULL,
    ordinal integer NOT NULL CHECK (ordinal > 0),
    path text NOT NULL CHECK (path <> ''),
    change_kind text NOT NULL CHECK (change_kind IN ('added','modified','deleted')),
    before_hash text CHECK (before_hash IS NULL OR before_hash ~ '^sha256:[0-9a-f]{64}$'),
    after_hash text CHECK (after_hash IS NULL OR after_hash ~ '^sha256:[0-9a-f]{64}$'),
    PRIMARY KEY (package_id, version_no, ordinal),
    FOREIGN KEY (package_id, version_no) REFERENCES formal_versions(package_id, version_no),
    CHECK (before_hash IS NOT NULL OR after_hash IS NOT NULL)
);

CREATE TABLE formal_delivery_proofs (
    package_id text NOT NULL,
    version_no integer NOT NULL,
    proof_version text NOT NULL CHECK (proof_version = 'formal-proof-v1'),
    proof_body jsonb NOT NULL CHECK (jsonb_typeof(proof_body) = 'object'),
    payload_hash text NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
    proof_digest text NOT NULL UNIQUE CHECK (proof_digest ~ '^sha256:[0-9a-f]{64}$'),
    signature text NOT NULL CHECK (signature ~ '^0x[0-9a-f]{130}$'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (package_id, version_no),
    FOREIGN KEY (package_id, version_no) REFERENCES formal_versions(package_id, version_no)
);

CREATE TRIGGER formal_feedback_sets_immutable BEFORE UPDATE OR DELETE ON formal_feedback_sets
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_feedback_items_immutable BEFORE UPDATE OR DELETE ON formal_feedback_items
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_feedback_requests_immutable BEFORE UPDATE OR DELETE ON formal_feedback_requests
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_feedback_responses_immutable BEFORE UPDATE OR DELETE ON formal_feedback_responses
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_version_changes_immutable BEFORE UPDATE OR DELETE ON formal_version_changes
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_delivery_proofs_immutable BEFORE UPDATE OR DELETE ON formal_delivery_proofs
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();

CREATE OR REPLACE FUNCTION enforce_formal_version_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'formal version history is immutable'; END IF;
    IF NEW.package_id IS DISTINCT FROM OLD.package_id OR NEW.version_no IS DISTINCT FROM OLD.version_no
       OR NEW.package_aggregate_version IS DISTINCT FROM OLD.package_aggregate_version
       OR NEW.scope_id IS DISTINCT FROM OLD.scope_id OR NEW.scope_hash IS DISTINCT FROM OLD.scope_hash
       OR NEW.work_nonce IS DISTINCT FROM OLD.work_nonce OR NEW.parent_version IS DISTINCT FROM OLD.parent_version
       OR NEW.parent_content_hash IS DISTINCT FROM OLD.parent_content_hash OR NEW.feedback_set_id IS DISTINCT FROM OLD.feedback_set_id
       OR NEW.feedback_digest IS DISTINCT FROM OLD.feedback_digest OR NEW.feedback_aggregate_version IS DISTINCT FROM OLD.feedback_aggregate_version
       OR NEW.logical_execution_id IS DISTINCT FROM OLD.logical_execution_id OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.used_cost < OLD.used_cost THEN RAISE EXCEPTION 'formal version command is immutable'; END IF;
    IF OLD.result_hash IS NOT NULL AND NEW.result_hash IS DISTINCT FROM OLD.result_hash THEN RAISE EXCEPTION 'formal version result identity is immutable'; END IF;
    IF OLD.status IN ('review','failed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'terminal formal version is immutable'; END IF;
    IF NOT (NEW.status = OLD.status OR (OLD.status = 'allocated' AND NEW.status IN ('generating','review','failed'))
        OR (OLD.status = 'generating' AND NEW.status IN ('review','failed'))) THEN RAISE EXCEPTION 'invalid formal version state transition'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE chain_events ADD CONSTRAINT chain_events_work_nonce_payload_check
    CHECK (event_type <> 'work_nonce_advanced' OR (work_nonce > 1 AND task_chain_id IS NOT NULL AND assignment_chain_id IS NOT NULL)) NOT VALID;

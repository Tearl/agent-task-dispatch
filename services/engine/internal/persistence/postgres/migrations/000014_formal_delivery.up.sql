CREATE TABLE IF NOT EXISTS formal_scope_snapshots (
    scope_id text PRIMARY KEY CHECK (scope_id ~ '^sha256:[0-9a-f]{64}$'),
    package_id text NOT NULL CHECK (package_id ~ '^sha256:[0-9a-f]{64}$'),
    scope_revision integer NOT NULL CHECK (scope_revision > 0),
    scope_version text NOT NULL CHECK (scope_version = 'formal-scope-v1'),
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[0-9a-f]{64}$'),
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    selected_overview_id text NOT NULL REFERENCES overview_slots(slot_id),
    overview_content_hash text NOT NULL CHECK (overview_content_hash ~ '^sha256:[0-9a-f]{64}$'),
    overview_ref text NOT NULL CHECK (overview_ref <> ''),
    input_snapshot text[] NOT NULL,
    acceptance_hash text NOT NULL CHECK (acceptance_hash ~ '^sha256:[0-9a-f]{64}$'),
    acceptance_criteria jsonb NOT NULL CHECK (valid_task_acceptance_criteria(acceptance_criteria)),
    output_constraints jsonb NOT NULL CHECK (jsonb_typeof(output_constraints) = 'object'),
    allowed_tools text[] NOT NULL,
    external_cost_cap numeric(78,0) NOT NULL CHECK (external_cost_cap >= 0),
    exclusions text[] NOT NULL,
    scope_body jsonb NOT NULL CHECK (jsonb_typeof(scope_body) = 'object'),
    created_at timestamptz NOT NULL,
    UNIQUE (package_id, scope_revision),
    UNIQUE (package_id, content_hash)
);

CREATE TABLE IF NOT EXISTS formal_packages (
    package_id text PRIMARY KEY CHECK (package_id ~ '^sha256:[0-9a-f]{64}$'),
    protocol_version text NOT NULL CHECK (protocol_version = 'formal-delivery-v1'),
    task_id text NOT NULL REFERENCES tasks(task_id),
    assignment_id text NOT NULL UNIQUE REFERENCES assignments(assignment_id),
    delivery_unit text NOT NULL CHECK (delivery_unit <> ''),
    package_kind text NOT NULL CHECK (package_kind = 'standard'),
    scope_id text NOT NULL UNIQUE REFERENCES formal_scope_snapshots(scope_id),
    scope_revision integer NOT NULL CHECK (scope_revision = 1),
    agent_id text NOT NULL REFERENCES agents(agent_id),
    provider_id text NOT NULL REFERENCES users(user_id),
    publisher_id text NOT NULL REFERENCES users(user_id),
    included_versions integer NOT NULL CHECK (included_versions = 3),
    maximum_versions integer NOT NULL CHECK (maximum_versions = 5),
    allocated_version integer NOT NULL DEFAULT 0 CHECK (allocated_version BETWEEN 0 AND 5),
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    status text NOT NULL CHECK (status IN ('active')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (task_id, delivery_unit, package_kind, scope_revision)
);
ALTER TABLE formal_scope_snapshots ADD CONSTRAINT formal_scope_snapshots_package_fk
    FOREIGN KEY (package_id) REFERENCES formal_packages(package_id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS formal_versions (
    package_id text NOT NULL REFERENCES formal_packages(package_id),
    version_no integer NOT NULL CHECK (version_no BETWEEN 1 AND 5),
    package_aggregate_version bigint NOT NULL CHECK (package_aggregate_version > 1),
    scope_id text NOT NULL REFERENCES formal_scope_snapshots(scope_id),
    scope_hash text NOT NULL CHECK (scope_hash ~ '^sha256:[0-9a-f]{64}$'),
    work_nonce bigint NOT NULL CHECK (work_nonce > 0),
    parent_version integer,
    parent_content_hash text,
    feedback_set_id text,
    feedback_digest text,
    logical_execution_id text NOT NULL UNIQUE CHECK (logical_execution_id ~ '^sha256:[0-9a-f]{64}$'),
    status text NOT NULL CHECK (status IN ('allocated','generating','review','failed')),
    content_hash text,
    deliverable_ref text,
    used_cost numeric(78,0) NOT NULL DEFAULT 0 CHECK (used_cost >= 0),
    failure_reason_code text CHECK (failure_reason_code IS NULL OR failure_reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    result_hash text CHECK (result_hash IS NULL OR result_hash ~ '^sha256:[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (package_id, version_no),
    UNIQUE (package_id, package_aggregate_version),
    CHECK ((version_no = 1 AND parent_version IS NULL AND parent_content_hash IS NULL AND feedback_set_id IS NULL AND feedback_digest IS NULL)
        OR (version_no > 1 AND parent_version = version_no - 1
            AND parent_content_hash ~ '^sha256:[0-9a-f]{64}$'
            AND feedback_set_id ~ '^sha256:[0-9a-f]{64}$'
            AND feedback_digest ~ '^sha256:[0-9a-f]{64}$')),
    CHECK ((status = 'review' AND content_hash ~ '^sha256:[0-9a-f]{64}$' AND deliverable_ref IS NOT NULL AND failure_reason_code IS NULL AND result_hash IS NOT NULL)
        OR (status = 'failed' AND content_hash IS NULL AND deliverable_ref IS NULL AND failure_reason_code IS NOT NULL AND result_hash IS NOT NULL)
        OR (status IN ('allocated','generating') AND content_hash IS NULL AND deliverable_ref IS NULL AND failure_reason_code IS NULL AND result_hash IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS formal_versions_one_active_per_package_uidx
    ON formal_versions (package_id) WHERE status IN ('allocated','generating');
CREATE UNIQUE INDEX IF NOT EXISTS formal_versions_content_uidx
    ON formal_versions (package_id, content_hash) WHERE content_hash IS NOT NULL;

CREATE TABLE IF NOT EXISTS formal_start_requests (
    publisher_id text NOT NULL REFERENCES users(user_id),
    idempotency_key text NOT NULL CHECK (idempotency_key <> ''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    task_id text NOT NULL REFERENCES tasks(task_id),
    package_id text NOT NULL,
    version_no integer NOT NULL,
    response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body) = 'object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (publisher_id, idempotency_key),
    FOREIGN KEY (package_id, version_no) REFERENCES formal_versions(package_id, version_no)
);

CREATE TABLE IF NOT EXISTS formal_version_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    package_id text NOT NULL,
    version_no integer NOT NULL,
    event_sequence bigint NOT NULL,
    event_type text NOT NULL CHECK (event_type IN ('allocated','generating','review','failed')),
    reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at timestamptz NOT NULL,
    UNIQUE (package_id, version_no, event_sequence),
    FOREIGN KEY (package_id, version_no) REFERENCES formal_versions(package_id, version_no)
);

CREATE TABLE IF NOT EXISTS formal_billing_results (
    package_id text NOT NULL,
    version_no integer NOT NULL,
    billing_key text NOT NULL UNIQUE CHECK (billing_key ~ '^sha256:[0-9a-f]{64}$'),
    billing_status text NOT NULL CHECK (billing_status = 'included'),
    charge_amount numeric(78,0) NOT NULL CHECK (charge_amount = 0),
    used_cost numeric(78,0) NOT NULL CHECK (used_cost >= 0),
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[0-9a-f]{64}$'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (package_id, version_no),
    FOREIGN KEY (package_id, version_no) REFERENCES formal_versions(package_id, version_no)
);

CREATE OR REPLACE FUNCTION reject_formal_scope_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'formal scope snapshots are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_formal_package_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'formal package history is immutable'; END IF;
    IF NEW.package_id IS DISTINCT FROM OLD.package_id OR NEW.protocol_version IS DISTINCT FROM OLD.protocol_version
       OR NEW.task_id IS DISTINCT FROM OLD.task_id OR NEW.assignment_id IS DISTINCT FROM OLD.assignment_id
       OR NEW.delivery_unit IS DISTINCT FROM OLD.delivery_unit OR NEW.package_kind IS DISTINCT FROM OLD.package_kind
       OR NEW.scope_id IS DISTINCT FROM OLD.scope_id OR NEW.scope_revision IS DISTINCT FROM OLD.scope_revision
       OR NEW.agent_id IS DISTINCT FROM OLD.agent_id OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.publisher_id IS DISTINCT FROM OLD.publisher_id OR NEW.included_versions IS DISTINCT FROM OLD.included_versions
       OR NEW.maximum_versions IS DISTINCT FROM OLD.maximum_versions OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.allocated_version < OLD.allocated_version OR NEW.aggregate_version < OLD.aggregate_version THEN
        RAISE EXCEPTION 'formal package identity is immutable and counters are monotonic';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_formal_version_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'formal version history is immutable'; END IF;
    IF NEW.package_id IS DISTINCT FROM OLD.package_id OR NEW.version_no IS DISTINCT FROM OLD.version_no
       OR NEW.package_aggregate_version IS DISTINCT FROM OLD.package_aggregate_version
       OR NEW.scope_id IS DISTINCT FROM OLD.scope_id OR NEW.scope_hash IS DISTINCT FROM OLD.scope_hash
       OR NEW.work_nonce IS DISTINCT FROM OLD.work_nonce OR NEW.parent_version IS DISTINCT FROM OLD.parent_version
       OR NEW.parent_content_hash IS DISTINCT FROM OLD.parent_content_hash OR NEW.feedback_set_id IS DISTINCT FROM OLD.feedback_set_id
       OR NEW.feedback_digest IS DISTINCT FROM OLD.feedback_digest OR NEW.logical_execution_id IS DISTINCT FROM OLD.logical_execution_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.used_cost < OLD.used_cost THEN
        RAISE EXCEPTION 'formal version command is immutable';
    END IF;
    IF OLD.result_hash IS NOT NULL AND NEW.result_hash IS DISTINCT FROM OLD.result_hash THEN
        RAISE EXCEPTION 'formal version result identity is immutable';
    END IF;
    IF OLD.status IN ('review','failed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'terminal formal version is immutable'; END IF;
    IF NOT (NEW.status = OLD.status OR (OLD.status = 'allocated' AND NEW.status IN ('generating','review','failed'))
        OR (OLD.status = 'generating' AND NEW.status IN ('review','failed'))) THEN
        RAISE EXCEPTION 'invalid formal version state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_formal_append_only_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'formal delivery audit history is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER formal_scope_snapshots_immutable BEFORE UPDATE OR DELETE ON formal_scope_snapshots
    FOR EACH ROW EXECUTE FUNCTION reject_formal_scope_mutation();
CREATE TRIGGER formal_packages_transition BEFORE UPDATE OR DELETE ON formal_packages
    FOR EACH ROW EXECUTE FUNCTION enforce_formal_package_transition();
CREATE TRIGGER formal_versions_transition BEFORE UPDATE OR DELETE ON formal_versions
    FOR EACH ROW EXECUTE FUNCTION enforce_formal_version_transition();
CREATE TRIGGER formal_start_requests_immutable BEFORE UPDATE OR DELETE ON formal_start_requests
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_version_events_immutable BEFORE UPDATE OR DELETE ON formal_version_events
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_billing_results_immutable BEFORE UPDATE OR DELETE ON formal_billing_results
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();

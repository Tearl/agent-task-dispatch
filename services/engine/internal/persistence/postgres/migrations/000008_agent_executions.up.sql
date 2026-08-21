-- Include version_no so acceptance-only revisions may legitimately reuse an
-- identical task-spec hash without making this migration fail.
CREATE UNIQUE INDEX IF NOT EXISTS task_spec_versions_task_version_hash_uidx
    ON task_spec_versions (task_id, version_no, content_hash);

CREATE TABLE IF NOT EXISTS logical_executions (
    logical_execution_id text PRIMARY KEY CHECK (logical_execution_id <> ''),
    idempotency_key text NOT NULL UNIQUE CHECK (idempotency_key <> ''),
    spec_hash text NOT NULL CHECK (spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    protocol_version text NOT NULL CHECK (protocol_version = 'agent-execution-v1'),
    stage text NOT NULL CHECK (stage IN ('overview','formal')),
    task_id text NOT NULL REFERENCES tasks(task_id),
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    task_spec_version integer NOT NULL CHECK (task_spec_version > 0),
    agent_id text NOT NULL REFERENCES agents(agent_id),
    agent_endpoint text NOT NULL CHECK (agent_endpoint <> ''),
    responsibility_code text NOT NULL CHECK (responsibility_code <> ''),
    cost_cap numeric(78,0) NOT NULL CHECK (cost_cap >= 0),
    tool_policy jsonb NOT NULL CHECK (jsonb_typeof(tool_policy) = 'object'),
    deadline timestamptz NOT NULL,
    overview_binding jsonb,
    formal_binding jsonb,
    spec_body jsonb NOT NULL CHECK (jsonb_typeof(spec_body) = 'object'),
    status text NOT NULL CHECK (status IN ('pending','running','cancel_requested','cancelled','succeeded','failed','cost_stopped')),
    current_attempt integer NOT NULL DEFAULT 0 CHECK (current_attempt >= 0),
    used_cost numeric(78,0) NOT NULL DEFAULT 0 CHECK (used_cost >= 0 AND used_cost <= cost_cap),
    content_hash text CHECK (content_hash IS NULL OR content_hash ~ '^sha256:[0-9a-f]{64}$'),
    deliverable_ref text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    cancelled_at timestamptz,
    CHECK ((stage = 'overview' AND overview_binding IS NOT NULL AND formal_binding IS NULL)
        OR (stage = 'formal' AND formal_binding IS NOT NULL AND overview_binding IS NULL)),
    CHECK ((status = 'succeeded' AND content_hash IS NOT NULL AND deliverable_ref IS NOT NULL)
        OR (status <> 'succeeded' AND content_hash IS NULL AND deliverable_ref IS NULL)),
    CHECK ((status = 'cancelled' AND cancelled_at IS NOT NULL) OR (status <> 'cancelled' AND cancelled_at IS NULL)),
    CONSTRAINT logical_executions_task_spec_fk FOREIGN KEY (task_id, task_spec_version, task_spec_hash)
        REFERENCES task_spec_versions(task_id, version_no, content_hash)
);
CREATE INDEX IF NOT EXISTS logical_executions_task_idx ON logical_executions (task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS logical_executions_agent_active_idx ON logical_executions (agent_id, updated_at)
    WHERE status IN ('pending','running','cancel_requested');

CREATE TABLE IF NOT EXISTS execution_attempts (
    logical_execution_id text NOT NULL REFERENCES logical_executions(logical_execution_id),
    attempt_no integer NOT NULL CHECK (attempt_no > 0),
    attempt_id text NOT NULL UNIQUE CHECK (attempt_id <> ''),
    reservation_id text NOT NULL UNIQUE CHECK (reservation_id <> ''),
    status text NOT NULL CHECK (status IN ('prepared','active','completed','failed','expired','cancelled')),
    fencing_token bigint,
    lease_expires_at timestamptz,
    callback_nonce_hash text UNIQUE CHECK (callback_nonce_hash IS NULL OR callback_nonce_hash ~ '^sha256:[0-9a-f]{64}$'),
    nonce_key_version text,
    dispatch_count integer NOT NULL DEFAULT 0 CHECK (dispatch_count >= 0),
    failure_reason text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    terminal_at timestamptz,
    PRIMARY KEY (logical_execution_id, attempt_no),
    CHECK ((status = 'prepared' AND fencing_token IS NULL AND lease_expires_at IS NULL AND callback_nonce_hash IS NULL AND nonce_key_version IS NULL)
        OR (status = 'cancelled' AND ((fencing_token IS NULL AND lease_expires_at IS NULL AND callback_nonce_hash IS NULL AND nonce_key_version IS NULL)
            OR (fencing_token > 0 AND lease_expires_at IS NOT NULL AND callback_nonce_hash IS NOT NULL AND nonce_key_version IS NOT NULL)))
        OR (status NOT IN ('prepared','cancelled') AND fencing_token > 0 AND lease_expires_at IS NOT NULL AND callback_nonce_hash IS NOT NULL AND nonce_key_version IS NOT NULL)),
    CHECK ((status IN ('completed','failed','expired','cancelled') AND terminal_at IS NOT NULL)
        OR (status IN ('prepared','active') AND terminal_at IS NULL))
);

CREATE TABLE IF NOT EXISTS execution_callback_events (
    callback_event_id text PRIMARY KEY CHECK (callback_event_id ~ '^sha256:[0-9a-f]{64}$'),
    logical_execution_id text NOT NULL REFERENCES logical_executions(logical_execution_id),
    attempt_id text NOT NULL REFERENCES execution_attempts(attempt_id),
    agent_id text NOT NULL,
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    nonce_hash text NOT NULL UNIQUE CHECK (nonce_hash ~ '^sha256:[0-9a-f]{64}$'),
    payload_hash text NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
    callback_status text NOT NULL CHECK (callback_status IN ('succeeded','failed')),
    used_cost numeric(78,0) NOT NULL CHECK (used_cost >= 0),
    content_hash text,
    deliverable_ref text,
    outcome text NOT NULL CHECK (outcome IN ('accepted','late','stale_fence','cost_stop')),
    result_body jsonb NOT NULL CHECK (jsonb_typeof(result_body) = 'object'),
    callback_timestamp timestamptz NOT NULL,
    received_at timestamptz NOT NULL,
    CHECK ((callback_status = 'succeeded' AND content_hash IS NOT NULL AND deliverable_ref IS NOT NULL)
        OR (callback_status = 'failed' AND content_hash IS NULL AND deliverable_ref IS NULL))
);
CREATE INDEX IF NOT EXISTS execution_callback_events_execution_idx
    ON execution_callback_events (logical_execution_id, received_at);

CREATE OR REPLACE FUNCTION enforce_logical_execution_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'logical execution history is immutable';
    END IF;
    IF OLD.status IN ('cancelled','succeeded','cost_stopped') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal logical execution is immutable';
    END IF;
    IF NEW.logical_execution_id IS DISTINCT FROM OLD.logical_execution_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.spec_hash IS DISTINCT FROM OLD.spec_hash
       OR NEW.protocol_version IS DISTINCT FROM OLD.protocol_version
       OR NEW.stage IS DISTINCT FROM OLD.stage
       OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.task_spec_hash IS DISTINCT FROM OLD.task_spec_hash
       OR NEW.task_spec_version IS DISTINCT FROM OLD.task_spec_version
       OR NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.agent_endpoint IS DISTINCT FROM OLD.agent_endpoint
       OR NEW.responsibility_code IS DISTINCT FROM OLD.responsibility_code
       OR NEW.cost_cap IS DISTINCT FROM OLD.cost_cap
       OR NEW.tool_policy IS DISTINCT FROM OLD.tool_policy
       OR NEW.deadline IS DISTINCT FROM OLD.deadline
       OR NEW.overview_binding IS DISTINCT FROM OLD.overview_binding
       OR NEW.formal_binding IS DISTINCT FROM OLD.formal_binding
       OR NEW.spec_body IS DISTINCT FROM OLD.spec_body
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'logical execution specification is immutable';
    END IF;
    IF NEW.current_attempt < OLD.current_attempt OR NEW.used_cost < OLD.used_cost THEN
        RAISE EXCEPTION 'logical execution counters are monotonic';
    END IF;
    IF NOT (
        NEW.status = OLD.status
        OR (OLD.status IN ('pending','failed') AND NEW.status IN ('pending','running','cancel_requested','cancelled'))
        OR (OLD.status = 'running' AND NEW.status IN ('pending','running','failed','succeeded','cancel_requested','cost_stopped'))
        OR (OLD.status = 'cancel_requested' AND NEW.status = 'cancelled')
    ) THEN
        RAISE EXCEPTION 'invalid logical execution state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_execution_attempt_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'execution attempt history is immutable';
    END IF;
    IF OLD.status IN ('completed','failed','expired','cancelled') AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'terminal execution attempt is immutable';
    END IF;
    IF NEW.logical_execution_id IS DISTINCT FROM OLD.logical_execution_id
       OR NEW.attempt_no IS DISTINCT FROM OLD.attempt_no
       OR NEW.attempt_id IS DISTINCT FROM OLD.attempt_id
       OR NEW.reservation_id IS DISTINCT FROM OLD.reservation_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR (OLD.fencing_token IS NOT NULL AND NEW.fencing_token IS DISTINCT FROM OLD.fencing_token)
       OR (OLD.lease_expires_at IS NOT NULL AND NEW.lease_expires_at IS DISTINCT FROM OLD.lease_expires_at)
       OR (OLD.callback_nonce_hash IS NOT NULL AND NEW.callback_nonce_hash IS DISTINCT FROM OLD.callback_nonce_hash)
       OR (OLD.nonce_key_version IS NOT NULL AND NEW.nonce_key_version IS DISTINCT FROM OLD.nonce_key_version)
       OR NEW.dispatch_count < OLD.dispatch_count THEN
        RAISE EXCEPTION 'execution attempt identity and fencing are immutable';
    END IF;
    IF NOT (NEW.status = OLD.status OR (OLD.status = 'prepared' AND NEW.status IN ('active','cancelled'))
        OR (OLD.status = 'active' AND NEW.status IN ('completed','failed','expired','cancelled'))) THEN
        RAISE EXCEPTION 'invalid execution attempt state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_execution_callback_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'execution callback events are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS logical_executions_transition ON logical_executions;
CREATE TRIGGER logical_executions_transition BEFORE UPDATE OR DELETE ON logical_executions
    FOR EACH ROW EXECUTE FUNCTION enforce_logical_execution_transition();
DROP TRIGGER IF EXISTS execution_attempts_transition ON execution_attempts;
CREATE TRIGGER execution_attempts_transition BEFORE UPDATE OR DELETE ON execution_attempts
    FOR EACH ROW EXECUTE FUNCTION enforce_execution_attempt_transition();
DROP TRIGGER IF EXISTS execution_callback_events_immutable ON execution_callback_events;
CREATE TRIGGER execution_callback_events_immutable BEFORE UPDATE OR DELETE ON execution_callback_events
    FOR EACH ROW EXECUTE FUNCTION reject_execution_callback_mutation();

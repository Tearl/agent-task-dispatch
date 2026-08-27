CREATE TABLE IF NOT EXISTS execution_poll_reconciliations (
    reconciliation_event_id text PRIMARY KEY CHECK (reconciliation_event_id ~ '^sha256:[0-9a-f]{64}$'),
    logical_execution_id text NOT NULL REFERENCES logical_executions(logical_execution_id),
    attempt_id text NOT NULL UNIQUE REFERENCES execution_attempts(attempt_id),
    agent_id text NOT NULL,
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    payload_hash text NOT NULL CHECK (payload_hash ~ '^sha256:[0-9a-f]{64}$'),
    observed_status text NOT NULL CHECK (observed_status IN ('succeeded','failed','cancelled')),
    used_cost numeric(78,0) NOT NULL CHECK (used_cost >= 0),
    content_hash text,
    deliverable_ref text,
    outcome text NOT NULL CHECK (outcome IN ('accepted','already_terminal','late','stale_fence','cost_stop')),
    result_body jsonb NOT NULL CHECK (jsonb_typeof(result_body) = 'object'),
    observed_at timestamptz NOT NULL,
    CHECK ((observed_status = 'succeeded' AND content_hash IS NOT NULL AND deliverable_ref IS NOT NULL)
        OR (observed_status IN ('failed','cancelled') AND content_hash IS NULL AND deliverable_ref IS NULL))
);
CREATE INDEX IF NOT EXISTS execution_poll_reconciliations_execution_idx
    ON execution_poll_reconciliations (logical_execution_id, observed_at);

CREATE OR REPLACE FUNCTION reject_execution_poll_reconciliation_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'execution poll reconciliation evidence is immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS execution_poll_reconciliations_immutable ON execution_poll_reconciliations;
CREATE TRIGGER execution_poll_reconciliations_immutable BEFORE UPDATE OR DELETE ON execution_poll_reconciliations
    FOR EACH ROW EXECUTE FUNCTION reject_execution_poll_reconciliation_mutation();

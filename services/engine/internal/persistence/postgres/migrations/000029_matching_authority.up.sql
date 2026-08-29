ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS approval_status text NOT NULL DEFAULT 'pending'
        CHECK (approval_status IN ('pending','approved','revoked')),
    ADD COLUMN IF NOT EXISTS risk_status text NOT NULL DEFAULT 'pending'
        CHECK (risk_status IN ('pending','eligible','blocked')),
    ADD COLUMN IF NOT EXISTS matching_vector_version text
        CHECK (matching_vector_version IS NULL OR btrim(matching_vector_version) <> ''),
    ADD COLUMN IF NOT EXISTS reputation_quality smallint
        CHECK (reputation_quality BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS reputation_speed smallint
        CHECK (reputation_speed BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS reputation_reliability smallint
        CHECK (reputation_reliability BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS reputation_communication smallint
        CHECK (reputation_communication BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS reputation_compliance smallint
        CHECK (reputation_compliance BETWEEN 0 AND 100),
    ADD COLUMN IF NOT EXISTS matching_exposure_count integer NOT NULL DEFAULT 0
        CHECK (matching_exposure_count >= 0),
    ADD COLUMN IF NOT EXISTS matching_effective_samples integer NOT NULL DEFAULT 0
        CHECK (matching_effective_samples >= 0);

CREATE INDEX IF NOT EXISTS agents_matching_authority_idx
    ON agents (approval_status, risk_status, matching_vector_version, category, agent_id)
    WHERE status = 'active' AND health = 'healthy';

-- Existing Agents remain fail-closed until an admin records an Engine-owned
-- authority transition. New Agents inherit the same pending policy.
CREATE TABLE IF NOT EXISTS agent_matching_authority_events (
    event_id text PRIMARY KEY,
    agent_id text NOT NULL REFERENCES agents(agent_id),
    actor_id text NOT NULL REFERENCES users(user_id),
    agent_aggregate_version bigint NOT NULL CHECK (agent_aggregate_version > 0),
    approval_status text NOT NULL CHECK (approval_status IN ('pending','approved','revoked')),
    risk_status text NOT NULL CHECK (risk_status IN ('pending','eligible','blocked')),
    matching_vector_version text CHECK (matching_vector_version IS NULL OR btrim(matching_vector_version) <> ''),
    reputation_quality smallint CHECK (reputation_quality BETWEEN 0 AND 100),
    reputation_speed smallint CHECK (reputation_speed BETWEEN 0 AND 100),
    reputation_reliability smallint CHECK (reputation_reliability BETWEEN 0 AND 100),
    reputation_communication smallint CHECK (reputation_communication BETWEEN 0 AND 100),
    reputation_compliance smallint CHECK (reputation_compliance BETWEEN 0 AND 100),
    occurred_at timestamptz NOT NULL,
    UNIQUE (agent_id, agent_aggregate_version)
);

CREATE OR REPLACE FUNCTION reject_agent_matching_authority_event_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'Agent matching authority history is immutable';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER agent_matching_authority_events_immutable
    BEFORE UPDATE OR DELETE ON agent_matching_authority_events
    FOR EACH ROW EXECUTE FUNCTION reject_agent_matching_authority_event_mutation();

CREATE TABLE IF NOT EXISTS matching_run_operations (
    publisher_id text NOT NULL REFERENCES users(user_id),
    operation_id text NOT NULL CHECK (operation_id <> ''),
    task_id text NOT NULL REFERENCES tasks(task_id),
    evaluated_at timestamptz NOT NULL,
    snapshot_draft jsonb CHECK (snapshot_draft IS NULL OR jsonb_typeof(snapshot_draft) = 'object'),
    response_body jsonb CHECK (response_body IS NULL OR jsonb_typeof(response_body) = 'object'),
    completed_at timestamptz,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (publisher_id, operation_id),
    CHECK ((response_body IS NULL AND completed_at IS NULL) OR (response_body IS NOT NULL AND completed_at IS NOT NULL))
);

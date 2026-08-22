CREATE UNIQUE INDEX IF NOT EXISTS match_snapshots_identity_uidx
    ON match_snapshots (snapshot_id, task_id, match_revision);

CREATE TABLE IF NOT EXISTS overview_batches (
    batch_id text PRIMARY KEY CHECK (batch_id ~ '^sha256:[0-9a-f]{64}$'),
    snapshot_id text NOT NULL UNIQUE REFERENCES match_snapshots(snapshot_id),
    task_id text NOT NULL,
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    match_revision integer NOT NULL CHECK (match_revision > 0),
    algorithm_version text NOT NULL CHECK (algorithm_version <> ''),
    orchestration_version text NOT NULL CHECK (orchestration_version = 'overview-orchestration-v1'),
    replacement_version text NOT NULL CHECK (replacement_version = 'overview-replacement-v1'),
    brief_ref text NOT NULL CHECK (brief_ref <> ''),
    brief_hash text NOT NULL CHECK (brief_hash ~ '^sha256:[0-9a-f]{64}$'),
    deadline timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('running','completed','obsolete')),
    replacement_used boolean NOT NULL DEFAULT false,
    replacement_exhausted boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (NOT (replacement_used AND replacement_exhausted)),
    CONSTRAINT overview_batches_snapshot_identity_fk
        FOREIGN KEY (snapshot_id, task_id, match_revision)
        REFERENCES match_snapshots(snapshot_id, task_id, match_revision),
    CONSTRAINT overview_batches_task_spec_fk
        FOREIGN KEY (task_id, task_spec_hash)
        REFERENCES task_spec_versions(task_id, content_hash)
);
CREATE INDEX IF NOT EXISTS overview_batches_task_revision_idx
    ON overview_batches (task_id, match_revision DESC);

CREATE TABLE IF NOT EXISTS overview_slots (
    slot_id text PRIMARY KEY CHECK (slot_id ~ '^sha256:[0-9a-f]{64}$'),
    batch_id text NOT NULL REFERENCES overview_batches(batch_id),
    ordinal integer NOT NULL CHECK (ordinal BETWEEN 1 AND 4),
    source_position integer NOT NULL CHECK (source_position > 0),
    replacement boolean NOT NULL,
    agent_id text NOT NULL REFERENCES agents(agent_id),
    provider_id text NOT NULL REFERENCES users(user_id),
    price_version integer NOT NULL CHECK (price_version > 0),
    quote_hash text NOT NULL CHECK (quote_hash ~ '^sha256:[0-9a-f]{64}$'),
    overview_price numeric(78,0) NOT NULL CHECK (overview_price >= 0),
    external_cost_cap numeric(78,0) NOT NULL CHECK (external_cost_cap >= 0),
    allocation_id text NOT NULL UNIQUE CHECK (allocation_id <> ''),
    logical_execution_id text NOT NULL UNIQUE REFERENCES logical_executions(logical_execution_id),
    status text NOT NULL CHECK (status IN ('planned','dispatched','valid','invalid','failed','obsolete')),
    billing_status text NOT NULL CHECK (billing_status IN ('authorized','captured','released')),
    validation_codes jsonb NOT NULL CHECK (jsonb_typeof(validation_codes) = 'array'),
    content_hash text CHECK (content_hash IS NULL OR content_hash ~ '^sha256:[0-9a-f]{64}$'),
    deliverable_ref text,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (batch_id, ordinal),
    UNIQUE (batch_id, agent_id),
    CHECK ((replacement AND ordinal BETWEEN 2 AND 4) OR (NOT replacement AND ordinal BETWEEN 1 AND 3)),
    CHECK ((status = 'valid' AND content_hash IS NOT NULL AND deliverable_ref IS NOT NULL AND validation_codes = '[]'::jsonb)
        OR (status = 'invalid' AND jsonb_array_length(validation_codes) > 0)
        OR (status NOT IN ('valid','invalid') AND validation_codes = '[]'::jsonb AND content_hash IS NULL AND deliverable_ref IS NULL)),
    CHECK ((billing_status = 'captured' AND status = 'valid')
        OR (billing_status = 'released' AND status IN ('valid','invalid','failed','obsolete'))
        OR billing_status = 'authorized')
);
CREATE UNIQUE INDEX IF NOT EXISTS overview_slots_valid_content_unique
    ON overview_slots (batch_id, content_hash) WHERE status = 'valid';

CREATE TABLE IF NOT EXISTS overview_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    batch_id text NOT NULL REFERENCES overview_batches(batch_id),
    slot_id text REFERENCES overview_slots(slot_id),
    event_type text NOT NULL CHECK (event_type <> ''),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at timestamptz NOT NULL
);
CREATE INDEX IF NOT EXISTS overview_events_batch_idx ON overview_events (batch_id, occurred_at, event_id);

CREATE OR REPLACE FUNCTION enforce_overview_batch_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'overview batches are immutable history';
    END IF;
    IF NEW.batch_id IS DISTINCT FROM OLD.batch_id
       OR NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id
       OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.task_spec_hash IS DISTINCT FROM OLD.task_spec_hash
       OR NEW.match_revision IS DISTINCT FROM OLD.match_revision
       OR NEW.algorithm_version IS DISTINCT FROM OLD.algorithm_version
       OR NEW.orchestration_version IS DISTINCT FROM OLD.orchestration_version
       OR NEW.replacement_version IS DISTINCT FROM OLD.replacement_version
       OR NEW.brief_ref IS DISTINCT FROM OLD.brief_ref
       OR NEW.brief_hash IS DISTINCT FROM OLD.brief_hash
       OR NEW.deadline IS DISTINCT FROM OLD.deadline
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'overview batch identity is immutable';
    END IF;
    IF OLD.replacement_used AND NOT NEW.replacement_used
       OR OLD.replacement_exhausted AND NOT NEW.replacement_exhausted THEN
        RAISE EXCEPTION 'overview replacement decision is monotonic';
    END IF;
    IF NOT (NEW.status = OLD.status
        OR (OLD.status = 'running' AND NEW.status IN ('completed','obsolete'))
        OR (OLD.status = 'completed' AND NEW.status = 'obsolete')) THEN
        RAISE EXCEPTION 'invalid overview batch transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_overview_slot_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'overview slots are immutable history';
    END IF;
    IF NEW.slot_id IS DISTINCT FROM OLD.slot_id
       OR NEW.batch_id IS DISTINCT FROM OLD.batch_id
       OR NEW.ordinal IS DISTINCT FROM OLD.ordinal
       OR NEW.source_position IS DISTINCT FROM OLD.source_position
       OR NEW.replacement IS DISTINCT FROM OLD.replacement
       OR NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.price_version IS DISTINCT FROM OLD.price_version
       OR NEW.quote_hash IS DISTINCT FROM OLD.quote_hash
       OR NEW.overview_price IS DISTINCT FROM OLD.overview_price
       OR NEW.external_cost_cap IS DISTINCT FROM OLD.external_cost_cap
       OR NEW.allocation_id IS DISTINCT FROM OLD.allocation_id
       OR NEW.logical_execution_id IS DISTINCT FROM OLD.logical_execution_id
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'overview slot identity is immutable';
    END IF;
    IF OLD.status IN ('valid','invalid','failed','obsolete') AND NEW.status IS DISTINCT FROM OLD.status THEN
        RAISE EXCEPTION 'terminal overview slot is immutable';
    END IF;
    IF NOT (NEW.status = OLD.status
        OR (OLD.status = 'planned' AND NEW.status IN ('dispatched','obsolete'))
        OR (OLD.status = 'dispatched' AND NEW.status IN ('valid','invalid','failed','obsolete'))) THEN
        RAISE EXCEPTION 'invalid overview slot transition';
    END IF;
    IF NOT (NEW.billing_status = OLD.billing_status
        OR (OLD.billing_status = 'authorized' AND NEW.billing_status IN ('captured','released'))) THEN
        RAISE EXCEPTION 'invalid overview billing transition';
    END IF;
    IF NEW.billing_status = 'released' AND NEW.status = 'valid'
       AND NOT EXISTS (SELECT 1 FROM overview_batches WHERE batch_id = NEW.batch_id AND status = 'obsolete') THEN
        RAISE EXCEPTION 'valid overview billing can only be released after batch obsolescence';
    END IF;
    IF OLD.status IN ('valid','invalid','failed','obsolete') AND (NEW.content_hash IS DISTINCT FROM OLD.content_hash
        OR NEW.deliverable_ref IS DISTINCT FROM OLD.deliverable_ref
        OR NEW.validation_codes IS DISTINCT FROM OLD.validation_codes) THEN
        RAISE EXCEPTION 'overview validation evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_overview_event_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'overview events are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS overview_batches_transition ON overview_batches;
CREATE TRIGGER overview_batches_transition BEFORE UPDATE OR DELETE ON overview_batches
    FOR EACH ROW EXECUTE FUNCTION enforce_overview_batch_transition();
DROP TRIGGER IF EXISTS overview_slots_transition ON overview_slots;
CREATE TRIGGER overview_slots_transition BEFORE UPDATE OR DELETE ON overview_slots
    FOR EACH ROW EXECUTE FUNCTION enforce_overview_slot_transition();
DROP TRIGGER IF EXISTS overview_events_immutable ON overview_events;
CREATE TRIGGER overview_events_immutable BEFORE UPDATE OR DELETE ON overview_events
    FOR EACH ROW EXECUTE FUNCTION reject_overview_event_mutation();

CREATE OR REPLACE FUNCTION valid_task_acceptance_criteria(value jsonb) RETURNS boolean AS $$
DECLARE
    criterion jsonb;
    criterion_id text;
    seen_ids text[] := '{}';
    total integer := 0;
BEGIN
    IF jsonb_typeof(value) <> 'array' OR jsonb_array_length(value) = 0 THEN
        RETURN false;
    END IF;
    FOR criterion IN SELECT item FROM jsonb_array_elements(value) AS item LOOP
        IF jsonb_typeof(criterion) <> 'object'
           OR COALESCE(criterion->>'id','') = ''
           OR COALESCE(criterion->>'title','') = ''
           OR COALESCE(criterion->>'description','') = ''
           OR COALESCE(criterion->>'weight','') !~ '^[0-9]+$' THEN
            RETURN false;
        END IF;
        criterion_id := criterion->>'id';
        IF criterion_id = ANY(seen_ids) THEN
            RETURN false;
        END IF;
        seen_ids := array_append(seen_ids, criterion_id);
        IF (criterion->>'weight')::numeric < 1 OR (criterion->>'weight')::numeric > 100 THEN
            RETURN false;
        END IF;
        total := total + (criterion->>'weight')::integer;
    END LOOP;
    RETURN total = 100;
EXCEPTION WHEN OTHERS THEN
    RETURN false;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

CREATE TABLE IF NOT EXISTS tasks (
    task_id text PRIMARY KEY,
    publisher_id text NOT NULL REFERENCES users(user_id),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN (
        'draft','pending_escrow','escrowed','matching','overview_generating',
        'awaiting_selection','assigned','formal_generating','formal_review',
        'revision_requested','change_order_pending','accepted','settlement_pending',
        'settled','cancelled','refund_pending','refunded','dispute_requested',
        'disputed','partially_settled','failed'
    )),
    title text NOT NULL CHECK (title <> ''),
    description text NOT NULL CHECK (description <> ''),
    expert_type text NOT NULL CHECK (expert_type <> ''),
    language text NOT NULL CHECK (language <> ''),
    overview_budget numeric(78,0) NOT NULL CHECK (overview_budget >= 0),
    formal_budget numeric(78,0) NOT NULL CHECK (formal_budget >= 0),
    external_cost_cap numeric(78,0) NOT NULL CHECK (external_cost_cap >= 0),
    deadline timestamptz NOT NULL,
    inputs text[] NOT NULL DEFAULT '{}',
    allowed_tools text[] NOT NULL DEFAULT '{}',
    exclusions text[] NOT NULL DEFAULT '{}',
    delivery_format text NOT NULL CHECK (delivery_format <> ''),
    draft_acceptance jsonb NOT NULL CHECK (valid_task_acceptance_criteria(draft_acceptance)),
    aggregate_version bigint NOT NULL DEFAULT 1 CHECK (aggregate_version > 0),
    current_spec_version integer,
    current_acceptance_version integer,
    published_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (deadline > created_at),
    CHECK (current_spec_version IS NOT DISTINCT FROM current_acceptance_version),
    CHECK ((status = 'draft' AND current_spec_version IS NULL AND current_acceptance_version IS NULL AND published_at IS NULL)
        OR (status <> 'draft' AND current_spec_version IS NOT NULL AND current_acceptance_version IS NOT NULL AND published_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS tasks_publisher_idx ON tasks (publisher_id, created_at DESC);

CREATE TABLE IF NOT EXISTS task_spec_versions (
    task_id text NOT NULL REFERENCES tasks(task_id),
    version_no integer NOT NULL CHECK (version_no > 0),
    task_aggregate_version bigint NOT NULL CHECK (task_aggregate_version > 1),
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[0-9a-f]{64}$'),
    title text NOT NULL CHECK (title <> ''),
    description text NOT NULL CHECK (description <> ''),
    expert_type text NOT NULL CHECK (expert_type <> ''),
    language text NOT NULL CHECK (language <> ''),
    overview_budget numeric(78,0) NOT NULL CHECK (overview_budget >= 0),
    formal_budget numeric(78,0) NOT NULL CHECK (formal_budget >= 0),
    external_cost_cap numeric(78,0) NOT NULL CHECK (external_cost_cap >= 0),
    deadline timestamptz NOT NULL,
    inputs text[] NOT NULL,
    allowed_tools text[] NOT NULL,
    exclusions text[] NOT NULL,
    delivery_format text NOT NULL CHECK (delivery_format <> ''),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (task_id, version_no)
);

CREATE TABLE IF NOT EXISTS acceptance_versions (
    task_id text NOT NULL REFERENCES tasks(task_id),
    version_no integer NOT NULL CHECK (version_no > 0),
    task_aggregate_version bigint NOT NULL CHECK (task_aggregate_version > 1),
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[0-9a-f]{64}$'),
    criteria jsonb NOT NULL CHECK (valid_task_acceptance_criteria(criteria)),
    total_weight integer NOT NULL CHECK (total_weight = 100),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (task_id, version_no)
);

ALTER TABLE tasks ADD CONSTRAINT tasks_current_spec_fk
    FOREIGN KEY (task_id, current_spec_version)
    REFERENCES task_spec_versions(task_id, version_no)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE tasks ADD CONSTRAINT tasks_current_acceptance_fk
    FOREIGN KEY (task_id, current_acceptance_version)
    REFERENCES acceptance_versions(task_id, version_no)
    DEFERRABLE INITIALLY DEFERRED;

CREATE OR REPLACE FUNCTION reject_task_version_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'task specification and acceptance versions are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS task_spec_versions_immutable ON task_spec_versions;
CREATE TRIGGER task_spec_versions_immutable BEFORE UPDATE OR DELETE ON task_spec_versions FOR EACH ROW EXECUTE FUNCTION reject_task_version_mutation();
DROP TRIGGER IF EXISTS acceptance_versions_immutable ON acceptance_versions;
CREATE TRIGGER acceptance_versions_immutable BEFORE UPDATE OR DELETE ON acceptance_versions FOR EACH ROW EXECUTE FUNCTION reject_task_version_mutation();

CREATE OR REPLACE FUNCTION enforce_published_task_content_immutable() RETURNS trigger AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.publisher_id IS DISTINCT FROM OLD.publisher_id OR
        NEW.title IS DISTINCT FROM OLD.title OR
        NEW.description IS DISTINCT FROM OLD.description OR
        NEW.expert_type IS DISTINCT FROM OLD.expert_type OR
        NEW.language IS DISTINCT FROM OLD.language OR
        NEW.overview_budget IS DISTINCT FROM OLD.overview_budget OR
        NEW.formal_budget IS DISTINCT FROM OLD.formal_budget OR
        NEW.external_cost_cap IS DISTINCT FROM OLD.external_cost_cap OR
        NEW.deadline IS DISTINCT FROM OLD.deadline OR
        NEW.inputs IS DISTINCT FROM OLD.inputs OR
        NEW.allowed_tools IS DISTINCT FROM OLD.allowed_tools OR
        NEW.exclusions IS DISTINCT FROM OLD.exclusions OR
        NEW.delivery_format IS DISTINCT FROM OLD.delivery_format OR
        NEW.draft_acceptance IS DISTINCT FROM OLD.draft_acceptance OR
        NEW.published_at IS DISTINCT FROM OLD.published_at
    ) THEN
        RAISE EXCEPTION 'published task content is immutable';
    END IF;
    IF OLD.status <> 'draft' AND (
        NEW.current_spec_version IS NULL OR
        NEW.current_acceptance_version IS NULL OR
        NEW.current_spec_version <> NEW.current_acceptance_version OR
        NEW.current_spec_version < OLD.current_spec_version OR
        NEW.current_acceptance_version < OLD.current_acceptance_version
    ) THEN
        RAISE EXCEPTION 'task version pointers must advance together';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS tasks_published_content_immutable ON tasks;
CREATE TRIGGER tasks_published_content_immutable BEFORE UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION enforce_published_task_content_immutable();

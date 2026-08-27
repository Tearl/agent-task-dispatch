ALTER TABLE tasks
    ADD COLUMN tags text[] NOT NULL DEFAULT '{}'::text[];

ALTER TABLE task_spec_versions
    ADD COLUMN tags text[] NOT NULL DEFAULT '{}'::text[];

CREATE OR REPLACE FUNCTION enforce_published_task_content_immutable() RETURNS trigger AS $$
BEGIN
    IF OLD.status <> 'draft' AND (
        NEW.publisher_id IS DISTINCT FROM OLD.publisher_id OR
        NEW.title IS DISTINCT FROM OLD.title OR
        NEW.description IS DISTINCT FROM OLD.description OR
        NEW.expert_type IS DISTINCT FROM OLD.expert_type OR
        NEW.tags IS DISTINCT FROM OLD.tags OR
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

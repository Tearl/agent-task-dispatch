CREATE TABLE task_orchestration_plans (
    plan_id text PRIMARY KEY CHECK (plan_id ~ '^sha256:[0-9a-f]{64}$'),
    task_id text NOT NULL REFERENCES tasks(task_id),
    publisher_id text NOT NULL REFERENCES users(user_id),
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    operation_id text NOT NULL CHECK (operation_id <> '' AND length(operation_id) <= 200),
    input_hash text NOT NULL CHECK (input_hash ~ '^sha256:[0-9a-f]{64}$'),
    mode text NOT NULL CHECK (mode IN ('single','multi')),
    summary text NOT NULL CHECK (summary <> ''),
    rationale jsonb NOT NULL CHECK (jsonb_typeof(rationale)='array'),
    confidence double precision NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    steps jsonb NOT NULL CHECK (jsonb_typeof(steps)='array' AND jsonb_array_length(steps) > 0),
    model_version text NOT NULL CHECK (model_version <> ''),
    graph_version text NOT NULL CHECK (graph_version <> ''),
    created_at timestamptz NOT NULL,
    UNIQUE (task_id, operation_id),
    UNIQUE (task_id, task_spec_hash, input_hash)
);

CREATE INDEX task_orchestration_plans_latest_idx ON task_orchestration_plans(task_id, created_at DESC);

CREATE OR REPLACE FUNCTION reject_task_orchestration_plan_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'task orchestration plans are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER task_orchestration_plans_immutable BEFORE UPDATE OR DELETE ON task_orchestration_plans
FOR EACH ROW EXECUTE FUNCTION reject_task_orchestration_plan_mutation();

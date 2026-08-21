ALTER TABLE task_spec_versions
    ADD CONSTRAINT task_spec_versions_task_hash_unique UNIQUE (task_id, content_hash);

CREATE TABLE IF NOT EXISTS match_snapshots (
    snapshot_id text PRIMARY KEY CHECK (snapshot_id ~ '^sha256:[0-9a-f]{64}$'),
    task_id text NOT NULL,
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    match_revision integer NOT NULL CHECK (match_revision > 0),
    effective_input_hash text NOT NULL CHECK (effective_input_hash ~ '^sha256:[0-9a-f]{64}$'),
    algorithm_version text NOT NULL CHECK (algorithm_version <> ''),
    rule_version text NOT NULL CHECK (rule_version <> ''),
    model_version text NOT NULL CHECK (model_version <> ''),
    seed_digest text NOT NULL CHECK (seed_digest ~ '^sha256:[0-9a-f]{64}$'),
    seed_key_version text NOT NULL CHECK (seed_key_version <> ''),
    policy_hash text NOT NULL CHECK (policy_hash ~ '^sha256:[0-9a-f]{64}$'),
    exploration_triggered boolean NOT NULL,
    degradations jsonb NOT NULL CHECK (jsonb_typeof(degradations) = 'array'),
    snapshot_body jsonb NOT NULL CHECK (jsonb_typeof(snapshot_body) = 'object'),
    created_at timestamptz NOT NULL,
    sealed_at timestamptz,
    CONSTRAINT match_snapshots_task_spec_fk FOREIGN KEY (task_id, task_spec_hash)
        REFERENCES task_spec_versions(task_id, content_hash),
    UNIQUE (task_id, match_revision),
    UNIQUE (task_id, task_spec_hash, algorithm_version, effective_input_hash),
    CHECK (sealed_at IS NULL OR sealed_at = created_at)
);
CREATE INDEX IF NOT EXISTS match_snapshots_latest_idx
    ON match_snapshots (task_id, task_spec_hash, algorithm_version, match_revision DESC)
    WHERE sealed_at IS NOT NULL;

CREATE TABLE IF NOT EXISTS match_snapshot_candidates (
    snapshot_id text NOT NULL REFERENCES match_snapshots(snapshot_id),
    candidate_index integer NOT NULL CHECK (candidate_index > 0),
    agent_id text NOT NULL,
    provider_id text NOT NULL,
    price_version integer NOT NULL CHECK (price_version >= 0),
    overview_price text NOT NULL,
    formal_price text NOT NULL,
    external_cost_cap text NOT NULL,
    evaluation_status text NOT NULL CHECK (evaluation_status IN ('excluded','scored')),
    exclusion_reasons jsonb NOT NULL CHECK (jsonb_typeof(exclusion_reasons) = 'array'),
    recall_evidence jsonb NOT NULL CHECK (jsonb_typeof(recall_evidence) = 'object'),
    task_match_score integer,
    reputation_score integer,
    price_time_score integer,
    availability_score integer,
    rule_score integer,
    model_delta integer,
    ranking_score integer,
    qualified boolean NOT NULL,
    qualification_reasons jsonb NOT NULL CHECK (jsonb_typeof(qualification_reasons) = 'array'),
    selection_weight integer,
    probability_numerator integer,
    probability_denominator integer,
    random_draw numeric(20,0),
    final_position smallint,
    exploration boolean NOT NULL DEFAULT false,
    PRIMARY KEY (snapshot_id, candidate_index),
    UNIQUE (snapshot_id, agent_id),
    CHECK ((evaluation_status = 'excluded' AND task_match_score IS NULL AND reputation_score IS NULL
            AND price_time_score IS NULL AND availability_score IS NULL AND rule_score IS NULL
            AND model_delta IS NULL AND ranking_score IS NULL AND qualified = false)
        OR (evaluation_status = 'scored' AND task_match_score BETWEEN 0 AND 60
            AND reputation_score BETWEEN 0 AND 25 AND price_time_score BETWEEN 0 AND 10
            AND availability_score BETWEEN 0 AND 5 AND rule_score BETWEEN 0 AND 100
            AND model_delta BETWEEN -5 AND 5 AND ranking_score BETWEEN 0 AND 100)),
    CHECK ((qualified AND selection_weight IS NOT NULL AND selection_weight > 0)
        OR (NOT qualified AND selection_weight IS NULL)),
    CHECK ((final_position IS NULL AND probability_numerator IS NULL AND probability_denominator IS NULL
            AND random_draw IS NULL AND exploration = false)
        OR (qualified AND final_position BETWEEN 1 AND 3 AND probability_numerator > 0
            AND probability_denominator >= probability_numerator AND random_draw >= 0)),
    CHECK (NOT exploration OR final_position = 3)
);
CREATE UNIQUE INDEX IF NOT EXISTS match_snapshot_candidates_position_unique
    ON match_snapshot_candidates (snapshot_id, final_position)
    WHERE final_position IS NOT NULL;

CREATE OR REPLACE FUNCTION enforce_match_snapshot_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'matching snapshots are immutable';
    END IF;
    IF OLD.sealed_at IS NULL AND NEW.sealed_at IS NOT NULL
       AND NEW.sealed_at = OLD.created_at
       AND NEW.snapshot_id = OLD.snapshot_id
       AND NEW.task_id = OLD.task_id
       AND NEW.task_spec_hash = OLD.task_spec_hash
       AND NEW.match_revision = OLD.match_revision
       AND NEW.effective_input_hash = OLD.effective_input_hash
       AND NEW.algorithm_version = OLD.algorithm_version
       AND NEW.rule_version = OLD.rule_version
       AND NEW.model_version = OLD.model_version
       AND NEW.seed_digest = OLD.seed_digest
       AND NEW.seed_key_version = OLD.seed_key_version
       AND NEW.policy_hash = OLD.policy_hash
       AND NEW.exploration_triggered = OLD.exploration_triggered
       AND NEW.degradations = OLD.degradations
       AND NEW.snapshot_body = OLD.snapshot_body
       AND NEW.created_at = OLD.created_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'matching snapshots are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_match_candidate_immutable() RETURNS trigger AS $$
DECLARE
    parent_sealed_at timestamptz;
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'matching snapshot candidates are immutable';
    END IF;
    SELECT sealed_at INTO parent_sealed_at FROM match_snapshots WHERE snapshot_id = NEW.snapshot_id;
    IF parent_sealed_at IS NOT NULL THEN
        RAISE EXCEPTION 'sealed matching snapshot candidates are immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS match_snapshots_immutable ON match_snapshots;
CREATE TRIGGER match_snapshots_immutable BEFORE UPDATE OR DELETE ON match_snapshots
    FOR EACH ROW EXECUTE FUNCTION enforce_match_snapshot_immutable();
DROP TRIGGER IF EXISTS match_snapshot_candidates_immutable ON match_snapshot_candidates;
CREATE TRIGGER match_snapshot_candidates_immutable BEFORE INSERT OR UPDATE OR DELETE ON match_snapshot_candidates
    FOR EACH ROW EXECUTE FUNCTION enforce_match_candidate_immutable();

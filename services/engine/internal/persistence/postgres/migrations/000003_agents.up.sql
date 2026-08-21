CREATE TABLE IF NOT EXISTS agents (
    agent_id text PRIMARY KEY,
    owner_id text NOT NULL REFERENCES users(user_id),
    name text NOT NULL CHECK (name <> ''),
    category text NOT NULL CHECK (category <> ''),
    tags text[] NOT NULL DEFAULT '{}',
    capabilities text NOT NULL CHECK (capabilities <> ''),
    languages text[] NOT NULL CHECK (cardinality(languages) > 0),
    estimated_duration_seconds bigint NOT NULL CHECK (estimated_duration_seconds > 0),
    author_bio text NOT NULL DEFAULT '',
    controller_address text NOT NULL CHECK (controller_address = lower(controller_address)),
    payout_address text NOT NULL CHECK (payout_address = lower(payout_address)),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','paused','retired')),
    health text NOT NULL DEFAULT 'unknown' CHECK (health IN ('unknown','healthy','degraded','unhealthy')),
    health_checked_at timestamptz,
    health_valid_until timestamptz,
    max_concurrency integer NOT NULL CHECK (max_concurrency > 0 AND max_concurrency <= 10000),
    active_capacity integer NOT NULL DEFAULT 0 CHECK (active_capacity >= 0 AND active_capacity <= max_concurrency),
    next_fencing_token bigint NOT NULL DEFAULT 0 CHECK (next_fencing_token >= 0),
    aggregate_version bigint NOT NULL DEFAULT 1 CHECK (aggregate_version > 0),
    activated_at timestamptz,
    current_price_version integer,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (health_valid_until IS NULL OR health_checked_at IS NOT NULL),
    CHECK (status <> 'active' OR activated_at IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS agents_owner_idx ON agents (owner_id, created_at DESC);
CREATE INDEX IF NOT EXISTS agents_matchable_idx ON agents (category, updated_at DESC) WHERE status='active' AND health='healthy';

CREATE TABLE IF NOT EXISTS agent_price_versions (
    agent_id text NOT NULL REFERENCES agents(agent_id),
    version_no integer NOT NULL CHECK (version_no > 0),
    overview_price numeric(78,0) NOT NULL CHECK (overview_price >= 0),
    formal_package_gross_price numeric(78,0) NOT NULL CHECK (formal_package_gross_price >= 0),
    additional_version_price numeric(78,0) NOT NULL CHECK (additional_version_price >= 0),
    external_cost_cap numeric(78,0) NOT NULL CHECK (external_cost_cap >= 0),
    included_versions integer NOT NULL DEFAULT 3 CHECK (included_versions = 3),
    max_versions integer NOT NULL DEFAULT 5 CHECK (max_versions = 5),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (agent_id, version_no),
    CHECK (overview_price <= formal_package_gross_price)
);

ALTER TABLE agents ADD CONSTRAINT agents_current_price_fk
    FOREIGN KEY (agent_id, current_price_version)
    REFERENCES agent_price_versions(agent_id, version_no)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE IF NOT EXISTS agent_capacity_leases (
    reservation_id text PRIMARY KEY,
    agent_id text NOT NULL REFERENCES agents(agent_id),
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    expires_at timestamptz NOT NULL,
    released_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (agent_id, fencing_token),
    CHECK (released_at IS NULL OR released_at >= created_at)
);
CREATE INDEX IF NOT EXISTS agent_capacity_leases_active_idx ON agent_capacity_leases (agent_id, expires_at) WHERE released_at IS NULL;

CREATE OR REPLACE FUNCTION enforce_agent_monotonic_fields() RETURNS trigger AS $$
BEGIN
    IF OLD.status = 'retired' AND NEW IS DISTINCT FROM OLD THEN
        RAISE EXCEPTION 'retired agent is immutable';
    END IF;
    IF OLD.activated_at IS NOT NULL THEN
        IF NEW.activated_at IS DISTINCT FROM OLD.activated_at
           OR NEW.controller_address IS DISTINCT FROM OLD.controller_address
           OR NEW.payout_address IS DISTINCT FROM OLD.payout_address THEN
            RAISE EXCEPTION 'activated agent addresses are immutable';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS agents_monotonic_fields ON agents;
CREATE TRIGGER agents_monotonic_fields BEFORE UPDATE ON agents FOR EACH ROW EXECUTE FUNCTION enforce_agent_monotonic_fields();

CREATE OR REPLACE FUNCTION reject_agent_price_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'agent price versions are immutable';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS agent_price_versions_immutable ON agent_price_versions;
CREATE TRIGGER agent_price_versions_immutable BEFORE UPDATE OR DELETE ON agent_price_versions FOR EACH ROW EXECUTE FUNCTION reject_agent_price_mutation();

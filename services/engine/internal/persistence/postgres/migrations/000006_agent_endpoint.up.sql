ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS endpoint_url text NOT NULL DEFAULT '';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'agents_endpoint_url_length'
    ) THEN
        ALTER TABLE agents ADD CONSTRAINT agents_endpoint_url_length
            CHECK (length(endpoint_url) <= 2048);
    END IF;
END
$$;

-- Existing Agents remain non-matchable after an endpoint change until the
-- Engine records a fresh protocol health check. The application validates new
-- and updated endpoints as absolute HTTPS URLs.

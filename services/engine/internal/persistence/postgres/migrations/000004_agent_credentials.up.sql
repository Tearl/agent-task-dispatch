ALTER TABLE agents ADD COLUMN current_credential_version integer;

CREATE TABLE agent_credential_versions (
    agent_id text NOT NULL REFERENCES agents(agent_id),
    version_no integer NOT NULL CHECK (version_no > 0),
    credential_type text NOT NULL CHECK (credential_type IN ('api_key','bearer_token','oauth_client_secret')),
    label text NOT NULL CHECK (label <> ''),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) > 16),
    nonce bytea NOT NULL CHECK (octet_length(nonce) = 12),
    wrapped_data_key bytea NOT NULL CHECK (octet_length(wrapped_data_key) > 32),
    key_nonce bytea NOT NULL CHECK (octet_length(key_nonce) = 12),
    encryption_algorithm text NOT NULL CHECK (encryption_algorithm = 'AES-256-GCM'),
    key_wrap_algorithm text NOT NULL CHECK (key_wrap_algorithm = 'AES-256-GCM'),
    key_reference text NOT NULL CHECK (key_reference <> ''),
    fingerprint text NOT NULL CHECK (fingerprint ~ '^[0-9a-f]{32}$'),
    created_by text NOT NULL REFERENCES users(user_id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (agent_id, version_no),
    UNIQUE (agent_id, fingerprint)
);

ALTER TABLE agents ADD CONSTRAINT agents_current_credential_fk
    FOREIGN KEY (agent_id, current_credential_version)
    REFERENCES agent_credential_versions(agent_id, version_no)
    DEFERRABLE INITIALLY DEFERRED;

CREATE OR REPLACE FUNCTION reject_agent_credential_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'agent credential versions are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER agent_credential_versions_immutable
    BEFORE UPDATE OR DELETE ON agent_credential_versions
    FOR EACH ROW EXECUTE FUNCTION reject_agent_credential_mutation();

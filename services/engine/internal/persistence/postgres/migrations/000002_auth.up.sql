CREATE TABLE IF NOT EXISTS users (
    user_id text PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wallets (
    wallet_address text PRIMARY KEY CHECK (wallet_address = lower(wallet_address)),
    user_id text NOT NULL REFERENCES users(user_id),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id text NOT NULL REFERENCES users(user_id),
    role text NOT NULL CHECK (role IN ('publisher', 'agent_provider', 'admin', 'arbitrator')),
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role)
);

CREATE TABLE IF NOT EXISTS wallet_nonces (
    nonce text PRIMARY KEY,
    wallet_address text NOT NULL,
    domain text NOT NULL,
    chain_id text NOT NULL,
    purpose text NOT NULL,
    version text NOT NULL,
    message text NOT NULL UNIQUE,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CHECK (expires_at > issued_at),
    CHECK (consumed_at IS NULL OR consumed_at >= issued_at)
);
CREATE INDEX IF NOT EXISTS wallet_nonces_active_idx ON wallet_nonces (wallet_address, expires_at) WHERE consumed_at IS NULL;
CREATE INDEX IF NOT EXISTS wallet_nonces_issued_idx ON wallet_nonces (issued_at);

-- Reconstructable coordination only. Immutable nonce history remains in
-- wallet_nonces; expired rate buckets may be safely removed.
CREATE TABLE IF NOT EXISTS auth_rate_limit_buckets (
    scope text NOT NULL,
    subject text NOT NULL,
    bucket_start timestamptz NOT NULL,
    request_count integer NOT NULL CHECK (request_count > 0),
    expires_at timestamptz NOT NULL,
    PRIMARY KEY (scope, subject, bucket_start)
);
CREATE INDEX IF NOT EXISTS auth_rate_limit_buckets_expiry_idx ON auth_rate_limit_buckets (expires_at);

CREATE TABLE IF NOT EXISTS sessions (
    session_id text PRIMARY KEY,
    token_hash text NOT NULL UNIQUE,
    user_id text NOT NULL REFERENCES users(user_id),
    wallet_address text NOT NULL REFERENCES wallets(wallet_address),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (expires_at > created_at)
);
CREATE INDEX IF NOT EXISTS sessions_active_token_idx ON sessions (token_hash, expires_at) WHERE revoked_at IS NULL;

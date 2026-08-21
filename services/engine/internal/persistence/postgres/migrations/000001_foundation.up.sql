CREATE TABLE IF NOT EXISTS idempotency_records (
    scope text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash text NOT NULL,
    response_body bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (scope, idempotency_key),
    CHECK (scope <> '' AND idempotency_key <> '' AND request_hash <> '')
);

CREATE TABLE IF NOT EXISTS domain_events (
    event_id text PRIMARY KEY,
    aggregate_type text NOT NULL CHECK (aggregate_type <> ''),
    aggregate_id text NOT NULL CHECK (aggregate_id <> ''),
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    event_type text NOT NULL CHECK (event_type <> ''),
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (aggregate_type, aggregate_id, aggregate_version)
);

CREATE TABLE IF NOT EXISTS audit_events (
    event_id text PRIMARY KEY,
    actor_id text,
    action text NOT NULL CHECK (action <> ''),
    resource_type text NOT NULL CHECK (resource_type <> ''),
    resource_id text NOT NULL CHECK (resource_id <> ''),
    metadata jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_events_resource_idx ON audit_events (resource_type, resource_id, occurred_at DESC);

CREATE TABLE IF NOT EXISTS outbox_messages (
    message_id text PRIMARY KEY,
    dedupe_key text NOT NULL UNIQUE CHECK (dedupe_key <> ''),
    topic text NOT NULL CHECK (topic <> ''),
    payload jsonb NOT NULL,
    available_at timestamptz NOT NULL,
    published_at timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (published_at IS NULL OR published_at >= created_at)
);
CREATE INDEX IF NOT EXISTS outbox_messages_pending_idx ON outbox_messages (available_at, message_id) WHERE published_at IS NULL;

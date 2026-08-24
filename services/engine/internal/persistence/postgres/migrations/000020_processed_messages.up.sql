CREATE TABLE IF NOT EXISTS processed_messages (
    consumer_name text NOT NULL CHECK (consumer_name ~ '^[a-z0-9][a-z0-9_.-]{0,127}$'),
    message_id text NOT NULL CHECK (message_id <> '' AND octet_length(message_id) <= 8192),
    topic text NOT NULL CHECK (topic <> '' AND octet_length(topic) <= 512),
    dedupe_key text NOT NULL CHECK (dedupe_key <> '' AND octet_length(dedupe_key) <= 8192),
    envelope_hash text NOT NULL CHECK (envelope_hash ~ '^sha256:[0-9a-f]{64}$'),
    broker_message_id text NOT NULL CHECK (broker_message_id <> '' AND octet_length(broker_message_id) <= 512),
    processed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (consumer_name, message_id)
);

CREATE INDEX IF NOT EXISTS processed_messages_processed_at_idx
    ON processed_messages (processed_at, consumer_name, message_id);

COMMENT ON TABLE processed_messages IS
    'Idempotency evidence for at-least-once message consumers; rows are immutable audit history.';

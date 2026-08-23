ALTER TABLE outbox_messages
    ADD COLUMN IF NOT EXISTS locked_by text,
    ADD COLUMN IF NOT EXISTS locked_until timestamptz,
    ADD COLUMN IF NOT EXISTS last_error text,
    ADD COLUMN IF NOT EXISTS dead_lettered_at timestamptz;

ALTER TABLE outbox_messages DROP CONSTRAINT IF EXISTS outbox_messages_lock_pair;
ALTER TABLE outbox_messages ADD CONSTRAINT outbox_messages_lock_pair CHECK (
    (locked_by IS NULL AND locked_until IS NULL)
    OR (locked_by IS NOT NULL AND locked_by <> '' AND locked_until IS NOT NULL)
);

ALTER TABLE outbox_messages DROP CONSTRAINT IF EXISTS outbox_messages_last_error_safe;
ALTER TABLE outbox_messages ADD CONSTRAINT outbox_messages_last_error_safe CHECK (
    last_error IS NULL OR last_error ~ '^[a-z0-9][a-z0-9_.-]{0,127}$'
);

ALTER TABLE outbox_messages DROP CONSTRAINT IF EXISTS outbox_messages_terminal_state;
ALTER TABLE outbox_messages ADD CONSTRAINT outbox_messages_terminal_state CHECK (
    published_at IS NULL OR dead_lettered_at IS NULL
);

ALTER TABLE outbox_messages DROP CONSTRAINT IF EXISTS outbox_messages_dead_letter_time;
ALTER TABLE outbox_messages ADD CONSTRAINT outbox_messages_dead_letter_time CHECK (
    dead_lettered_at IS NULL OR dead_lettered_at >= created_at
);

CREATE INDEX IF NOT EXISTS outbox_messages_claim_idx
    ON outbox_messages (available_at, message_id)
    WHERE published_at IS NULL AND dead_lettered_at IS NULL;

CREATE INDEX IF NOT EXISTS outbox_messages_dead_letter_idx
    ON outbox_messages (dead_lettered_at, message_id)
    WHERE dead_lettered_at IS NOT NULL;

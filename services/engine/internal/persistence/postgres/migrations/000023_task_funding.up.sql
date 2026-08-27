CREATE TABLE task_funding_intents (
    intent_id text PRIMARY KEY CHECK (intent_id ~ '^sha256:[0-9a-f]{64}$'),
    task_id text NOT NULL UNIQUE REFERENCES tasks(task_id),
    publisher_id text NOT NULL REFERENCES users(user_id),
    publisher_wallet text NOT NULL CHECK (publisher_wallet ~ '^0x[0-9a-f]{40}$'),
    idempotency_key text NOT NULL,
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    chain_id numeric(78,0) NOT NULL CHECK (chain_id > 0),
    contract_address text NOT NULL CHECK (contract_address ~ '^0x[0-9a-f]{40}$'),
    chain_task_id text NOT NULL CHECK (chain_task_id ~ '^0x[0-9a-f]{64}$'),
    overview_amount numeric(78,0) NOT NULL CHECK (overview_amount >= 0),
    formal_amount numeric(78,0) NOT NULL CHECK (formal_amount > 0),
    external_cost_amount numeric(78,0) NOT NULL CHECK (external_cost_amount >= 0),
    total_amount numeric(78,0) NOT NULL CHECK (total_amount = overview_amount + formal_amount + external_cost_amount),
    status text NOT NULL CHECK (status IN ('prepared','submitted','confirmed','orphaned','failed')),
    transaction_hash text CHECK (transaction_hash IS NULL OR transaction_hash ~ '^0x[0-9a-f]{64}$'),
    chain_event_id text REFERENCES chain_events(event_id),
    failure_reason_code text,
    aggregate_version bigint NOT NULL DEFAULT 1 CHECK (aggregate_version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (publisher_id, idempotency_key),
    UNIQUE (chain_id, contract_address, chain_task_id)
);
CREATE INDEX task_funding_intents_chain_idx
    ON task_funding_intents (chain_id, contract_address, chain_task_id, status);

CREATE TABLE task_funding_intent_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    intent_id text NOT NULL REFERENCES task_funding_intents(intent_id),
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    state text NOT NULL CHECK (state IN ('prepared','submitted','confirmed','orphaned','failed')),
    transaction_hash text CHECK (transaction_hash IS NULL OR transaction_hash ~ '^0x[0-9a-f]{64}$'),
    chain_event_id text REFERENCES chain_events(event_id),
    reason_code text,
    occurred_at timestamptz NOT NULL,
    UNIQUE (intent_id, aggregate_version)
);

ALTER TABLE fund_journals DROP CONSTRAINT IF EXISTS fund_journals_journal_type_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_journal_type_check CHECK (journal_type IN (
    'funding','overview_capture','settlement_release','settlement_refund','earnings_withdrawal',
    'change_order_funding','change_order_release','change_order_residual','dispute_allocation','reversal'
));

ALTER TABLE agent_credential_versions DROP CONSTRAINT IF EXISTS agent_credential_versions_credential_type_check;
ALTER TABLE agent_credential_versions ADD CONSTRAINT agent_credential_versions_credential_type_check
    CHECK (credential_type IN ('api_key','bearer_token','oauth_client_secret','protocol_bundle'));

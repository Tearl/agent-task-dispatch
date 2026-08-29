ALTER TABLE task_funding_intents
    ADD COLUMN asset_address text CHECK (asset_address IS NULL OR asset_address ~ '^0x[0-9a-f]{40}$'),
    ADD COLUMN platform_task_key text CHECK (platform_task_key IS NULL OR platform_task_key ~ '^0x[0-9a-f]{64}$'),
    ADD COLUMN task_spec_hash text CHECK (task_spec_hash IS NULL OR task_spec_hash ~ '^0x[0-9a-f]{64}$'),
    ADD COLUMN funding_deadline bigint CHECK (funding_deadline IS NULL OR funding_deadline > 0);

ALTER TABLE task_funding_intents DROP CONSTRAINT IF EXISTS task_funding_intents_check;
ALTER TABLE task_funding_intents DROP CONSTRAINT IF EXISTS task_funding_intents_total_amount_check;

CREATE TEMP TABLE escrow_v3_migrated_intents ON COMMIT DROP AS
SELECT intent_id, aggregate_version + 1 AS aggregate_version, transaction_hash
FROM task_funding_intents
WHERE asset_address IS NULL AND status <> 'confirmed';

UPDATE task_funding_intents
SET status = 'failed',
    failure_reason_code = 'escrow_v3_migration_required',
    aggregate_version = aggregate_version + 1,
    updated_at = now()
WHERE asset_address IS NULL
  AND status <> 'confirmed';

INSERT INTO task_funding_intent_events(event_id,intent_id,aggregate_version,state,transaction_hash,reason_code,occurred_at)
SELECT 'sha256:'||md5('escrow-v3-intent:'||intent_id)||md5('escrow-v3-intent-event:'||intent_id),intent_id,aggregate_version,'failed',transaction_hash,'escrow_v3_migration_required',now()
FROM escrow_v3_migrated_intents;

ALTER TABLE task_funding_intents ADD CONSTRAINT task_funding_intents_total_amount_check
    CHECK (asset_address IS NULL OR total_amount = formal_amount);
ALTER TABLE task_funding_intents ADD CONSTRAINT task_funding_intents_v3_binding_check
    CHECK (
        (asset_address IS NULL AND (status IN ('confirmed','orphaned') OR failure_reason_code = 'escrow_v3_migration_required'))
        OR (asset_address IS NOT NULL AND platform_task_key IS NOT NULL AND task_spec_hash IS NOT NULL AND funding_deadline IS NOT NULL)
    );

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN (
    'draft','pending_escrow','escrowed','matching','overview_generating',
    'awaiting_selection','assigned','formal_generating','formal_review',
    'revision_requested','change_order_pending','accepted','settlement_pending',
    'settled','cancelled','refund_pending','refunded','dispute_requested',
    'disputed','partially_settled','failed','chain_reorg_pending',
    'funding_configuration_invalid','funding_refund_pending'
));

CREATE TEMP TABLE escrow_v3_migrated_tasks ON COMMIT DROP AS
SELECT task.task_id,task.publisher_id,task.aggregate_version + 1 AS aggregate_version
FROM tasks task
WHERE task.status = 'pending_escrow'
  AND (
      task.formal_budget = 0
      OR task.formal_budget > 115792089237316195423570985008687907853269984665640564039457584007913129639935::numeric
      OR EXISTS (SELECT 1 FROM task_funding_intents intent WHERE intent.task_id=task.task_id AND intent.asset_address IS NULL)
  );

UPDATE tasks task
SET status = 'funding_configuration_invalid',
    aggregate_version = aggregate_version + 1,
    updated_at = now()
WHERE task.status = 'pending_escrow'
  AND (
      task.formal_budget = 0
      OR task.formal_budget > 115792089237316195423570985008687907853269984665640564039457584007913129639935::numeric
      OR EXISTS (
          SELECT 1 FROM task_funding_intents intent
          WHERE intent.task_id = task.task_id
            AND intent.asset_address IS NULL
      )
  );

INSERT INTO domain_events(event_id,aggregate_type,aggregate_id,aggregate_version,event_type,payload,occurred_at)
SELECT 'migration:escrow-v3:task:'||task_id,'task',task_id,aggregate_version,'task.funding_configuration_invalid',jsonb_build_object('taskId',task_id,'reasonCode','escrow_v3_migration_required'),now()
FROM escrow_v3_migrated_tasks;
INSERT INTO audit_events(event_id,actor_id,action,resource_type,resource_id,metadata,occurred_at)
SELECT 'migration:escrow-v3:task:'||task_id||':audit',NULL,'task.funding_configuration_invalid','task',task_id,jsonb_build_object('taskId',task_id,'reasonCode','escrow_v3_migration_required'),now()
FROM escrow_v3_migrated_tasks;

CREATE TABLE task_funding_attempts (
    attempt_id text PRIMARY KEY CHECK (attempt_id ~ '^sha256:[0-9a-f]{64}$'),
    intent_id text NOT NULL REFERENCES task_funding_intents(intent_id),
    chain_id numeric(78,0) NOT NULL CHECK (chain_id > 0),
    contract_address text NOT NULL CHECK (contract_address ~ '^0x[0-9a-f]{40}$'),
    transaction_hash text NOT NULL CHECK (transaction_hash ~ '^0x[0-9a-f]{64}$'),
    state text NOT NULL CHECK (state IN ('submitted','observed_failed','canonical_confirmed','superseded','canonical_orphaned')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (chain_id, contract_address, transaction_hash)
);
CREATE INDEX task_funding_attempts_intent_idx
    ON task_funding_attempts (intent_id, created_at, attempt_id);

INSERT INTO task_funding_attempts(attempt_id,intent_id,chain_id,contract_address,transaction_hash,state,created_at,updated_at)
SELECT 'sha256:'||md5(chain_id::text||':'||contract_address||':'||transaction_hash)||md5('legacy-attempt:'||chain_id::text||':'||contract_address||':'||transaction_hash),intent_id,chain_id,contract_address,transaction_hash,
       CASE status WHEN 'confirmed' THEN 'canonical_confirmed' ELSE 'canonical_orphaned' END,created_at,updated_at
FROM task_funding_intents
WHERE asset_address IS NULL AND status IN ('confirmed','orphaned') AND transaction_hash IS NOT NULL;

CREATE TABLE task_funding_attempt_states (
    state_sequence bigserial PRIMARY KEY,
    attempt_id text NOT NULL REFERENCES task_funding_attempts(attempt_id),
    state text NOT NULL CHECK (state IN ('submitted','observed_failed','canonical_confirmed','superseded','canonical_orphaned')),
    chain_event_id text REFERENCES chain_events(event_id),
    chain_id numeric(78,0),
    contract_address text CHECK (contract_address IS NULL OR contract_address ~ '^0x[0-9a-f]{40}$'),
    block_hash text CHECK (block_hash IS NULL OR block_hash ~ '^0x[0-9a-f]{64}$'),
    transaction_hash text CHECK (transaction_hash IS NULL OR transaction_hash ~ '^0x[0-9a-f]{64}$'),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    occurred_at timestamptz NOT NULL,
    CHECK ((block_hash IS NULL AND chain_id IS NULL AND contract_address IS NULL)
        OR (block_hash IS NOT NULL AND chain_id IS NOT NULL AND contract_address IS NOT NULL AND transaction_hash IS NOT NULL))
);

INSERT INTO task_funding_attempt_states(attempt_id,state,transaction_hash,reason_code,occurred_at)
SELECT attempt_id,state,transaction_hash,'escrow_v3_legacy_backfill',updated_at
FROM task_funding_attempts attempt
WHERE EXISTS (SELECT 1 FROM task_funding_intents intent WHERE intent.intent_id=attempt.intent_id AND intent.asset_address IS NULL);

CREATE TABLE escrow_deployments (
    chain_id numeric(78,0) NOT NULL CHECK (chain_id > 0),
    contract_address text NOT NULL CHECK (contract_address ~ '^0x[0-9a-f]{40}$'),
    asset_key text NOT NULL CHECK (asset_key <> ''),
    dispute_resolver_address text NOT NULL CHECK (dispute_resolver_address ~ '^0x[0-9a-f]{40}$'),
    active_for_new_tasks boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY(chain_id,contract_address)
);

CREATE TABLE task_funding_canonicalizations (
    chain_event_id text NOT NULL REFERENCES chain_events(event_id),
    canonicalization_epoch integer NOT NULL CHECK (canonicalization_epoch > 0),
    intent_id text NOT NULL REFERENCES task_funding_intents(intent_id),
    attempt_id text NOT NULL REFERENCES task_funding_attempts(attempt_id),
    journal_id text NOT NULL UNIQUE REFERENCES fund_journals(journal_id),
    reversal_journal_id text UNIQUE REFERENCES fund_journals(journal_id),
    canonical_at timestamptz NOT NULL,
    orphaned_at timestamptz,
    PRIMARY KEY (chain_event_id, canonicalization_epoch),
    CHECK ((orphaned_at IS NULL AND reversal_journal_id IS NULL)
        OR (orphaned_at IS NOT NULL AND reversal_journal_id IS NOT NULL))
);
CREATE UNIQUE INDEX task_funding_one_canonical_effect_uidx
    ON task_funding_canonicalizations (intent_id)
    WHERE orphaned_at IS NULL;

CREATE OR REPLACE FUNCTION reject_funding_attempt_history_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'funding attempt state history is immutable';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER task_funding_attempt_states_immutable
    BEFORE UPDATE OR DELETE ON task_funding_attempt_states
    FOR EACH ROW EXECUTE FUNCTION reject_funding_attempt_history_mutation();

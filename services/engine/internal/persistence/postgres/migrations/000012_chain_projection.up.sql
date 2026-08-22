CREATE TABLE IF NOT EXISTS chain_projection_cursors (
    chain_id numeric(78,0) NOT NULL CHECK (chain_id > 0),
    contract_address text NOT NULL CHECK (contract_address ~ '^0x[0-9a-f]{40}$'),
    block_number bigint NOT NULL CHECK (block_number >= 0),
    block_hash text NOT NULL CHECK (block_hash ~ '^0x[0-9a-f]{64}$'),
    projection_version text NOT NULL CHECK (projection_version = 'authoritative-chain-projection-v1'),
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (chain_id, contract_address)
);

CREATE TABLE IF NOT EXISTS chain_blocks (
    chain_id numeric(78,0) NOT NULL CHECK (chain_id > 0),
    contract_address text NOT NULL CHECK (contract_address ~ '^0x[0-9a-f]{40}$'),
    block_hash text NOT NULL CHECK (block_hash ~ '^0x[0-9a-f]{64}$'),
    block_number bigint NOT NULL CHECK (block_number >= 0),
    parent_hash text NOT NULL CHECK (parent_hash ~ '^0x[0-9a-f]{64}$'),
    block_timestamp timestamptz NOT NULL,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (chain_id, contract_address, block_hash),
    UNIQUE (chain_id, contract_address, block_number, block_hash)
);

CREATE TABLE IF NOT EXISTS chain_block_states (
    state_sequence bigserial PRIMARY KEY,
    chain_id numeric(78,0) NOT NULL,
    contract_address text NOT NULL,
    block_hash text NOT NULL,
    state text NOT NULL CHECK (state IN ('canonical','orphaned')),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    observed_at timestamptz NOT NULL,
    FOREIGN KEY (chain_id, contract_address, block_hash)
        REFERENCES chain_blocks(chain_id, contract_address, block_hash)
);

-- Reconstructable current-chain coordination. Historical blocks and every state
-- transition remain immutable in the two tables above.
CREATE TABLE IF NOT EXISTS chain_canonical_blocks (
    chain_id numeric(78,0) NOT NULL,
    contract_address text NOT NULL,
    block_number bigint NOT NULL CHECK (block_number >= 0),
    block_hash text NOT NULL,
    PRIMARY KEY (chain_id, contract_address, block_number),
    FOREIGN KEY (chain_id, contract_address, block_hash)
        REFERENCES chain_blocks(chain_id, contract_address, block_hash)
);

CREATE TABLE IF NOT EXISTS chain_transactions (
    chain_id numeric(78,0) NOT NULL,
    contract_address text NOT NULL,
    block_hash text NOT NULL,
    transaction_hash text NOT NULL CHECK (transaction_hash ~ '^0x[0-9a-f]{64}$'),
    transaction_status text NOT NULL CHECK (transaction_status IN ('succeeded','failed')),
    input_hash text NOT NULL CHECK (input_hash ~ '^sha256:[0-9a-f]{64}$'),
    selection_call boolean NOT NULL,
    observed_at timestamptz NOT NULL,
    PRIMARY KEY (chain_id, contract_address, block_hash, transaction_hash),
    FOREIGN KEY (chain_id, contract_address, block_hash)
        REFERENCES chain_blocks(chain_id, contract_address, block_hash)
);
CREATE INDEX IF NOT EXISTS chain_transactions_hash_idx
    ON chain_transactions (chain_id, contract_address, transaction_hash);

CREATE TABLE IF NOT EXISTS chain_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    chain_id numeric(78,0) NOT NULL,
    contract_address text NOT NULL,
    block_hash text NOT NULL,
    block_number bigint NOT NULL CHECK (block_number >= 0),
    transaction_hash text NOT NULL,
    log_index integer NOT NULL CHECK (log_index >= 0),
    event_type text NOT NULL CHECK (event_type IN (
        'task_created','selection_confirmed','work_nonce_advanced','funds_released',
        'funds_refunded','dispute_opened','dispute_resolved'
    )),
    task_chain_id text NOT NULL CHECK (task_chain_id ~ '^0x[0-9a-f]{64}$'),
    assignment_chain_id text CHECK (assignment_chain_id IS NULL OR assignment_chain_id ~ '^0x[0-9a-f]{64}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload)='object'),
    selection_proof jsonb,
    formal_payable numeric(78,0),
    work_nonce bigint,
    observed_at timestamptz NOT NULL,
    UNIQUE (chain_id, contract_address, block_hash, transaction_hash, log_index),
    FOREIGN KEY (chain_id, contract_address, block_hash)
        REFERENCES chain_blocks(chain_id, contract_address, block_hash),
    CHECK ((event_type='selection_confirmed' AND selection_proof IS NOT NULL AND formal_payable IS NOT NULL AND work_nonce=1)
        OR (event_type<>'selection_confirmed' AND selection_proof IS NULL AND formal_payable IS NULL))
);

CREATE TABLE IF NOT EXISTS chain_event_states (
    state_sequence bigserial PRIMARY KEY,
    event_id text NOT NULL REFERENCES chain_events(event_id),
    state text NOT NULL CHECK (state IN ('canonical','orphaned')),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    observed_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS chain_reconciliation_runs (
    reconciliation_id text PRIMARY KEY CHECK (reconciliation_id ~ '^sha256:[0-9a-f]{64}$'),
    chain_id numeric(78,0) NOT NULL,
    contract_address text NOT NULL,
    safe_block_number bigint NOT NULL CHECK (safe_block_number >= 0),
    status text NOT NULL CHECK (status IN ('matched','difference_detected')),
    started_at timestamptz NOT NULL,
    finished_at timestamptz NOT NULL,
    CHECK (finished_at >= started_at)
);

CREATE TABLE IF NOT EXISTS chain_reconciliation_differences (
    reconciliation_id text NOT NULL REFERENCES chain_reconciliation_runs(reconciliation_id),
    difference_index integer NOT NULL CHECK (difference_index > 0),
    category text NOT NULL CHECK (category ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    resource_id text NOT NULL CHECK (resource_id <> ''),
    expected_value text NOT NULL,
    observed_value text NOT NULL,
    severity text NOT NULL CHECK (severity IN ('warning','critical')),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (reconciliation_id, difference_index)
);

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check CHECK (status IN (
    'draft','pending_escrow','escrowed','matching','overview_generating',
    'awaiting_selection','assigned','formal_generating','formal_review',
    'revision_requested','change_order_pending','accepted','settlement_pending',
    'settled','cancelled','refund_pending','refunded','dispute_requested',
    'disputed','partially_settled','failed','chain_reorg_pending'
));

ALTER TABLE selection_reservations DROP CONSTRAINT IF EXISTS selection_reservations_status_check;
ALTER TABLE selection_reservations ADD CONSTRAINT selection_reservations_status_check
    CHECK (status IN ('reserved','submitted','confirmed','failed','expired','orphaned'));
ALTER TABLE selection_reservations DROP CONSTRAINT IF EXISTS selection_reservations_terminal_evidence_check;
ALTER TABLE selection_reservations ADD CONSTRAINT selection_reservations_terminal_evidence_check CHECK (
    (status = 'reserved' AND transaction_hash IS NULL AND failure_reason_code IS NULL)
 OR (status = 'submitted' AND transaction_hash IS NOT NULL AND failure_reason_code IS NULL)
 OR (status = 'confirmed' AND transaction_hash IS NOT NULL AND failure_reason_code IS NULL)
 OR (status = 'failed' AND transaction_hash IS NOT NULL AND failure_reason_code IS NOT NULL)
 OR (status = 'expired' AND failure_reason_code = 'reservation_expired')
 OR (status = 'orphaned' AND transaction_hash IS NOT NULL AND failure_reason_code = 'chain_reorganization')
);

ALTER TABLE assignments DROP CONSTRAINT IF EXISTS assignments_task_id_key;

CREATE TABLE IF NOT EXISTS assignment_states (
    state_sequence bigserial PRIMARY KEY,
    assignment_id text NOT NULL REFERENCES assignments(assignment_id),
    state text NOT NULL CHECK (state IN ('confirmed','orphaned')),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    occurred_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS active_assignments (
    task_id text PRIMARY KEY REFERENCES tasks(task_id),
    assignment_id text NOT NULL UNIQUE REFERENCES assignments(assignment_id),
    activated_at timestamptz NOT NULL
);

INSERT INTO assignment_states (assignment_id,state,reason_code,occurred_at)
SELECT assignment_id,'confirmed','projection_initialized',confirmed_at FROM assignments
WHERE NOT EXISTS (SELECT 1 FROM assignment_states state WHERE state.assignment_id=assignments.assignment_id);
INSERT INTO active_assignments (task_id,assignment_id,activated_at)
SELECT task_id,assignment_id,confirmed_at FROM assignments
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION enforce_selection_reservation_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'selection reservations are immutable history'; END IF;
    IF NEW.reservation_id IS DISTINCT FROM OLD.reservation_id
       OR NEW.publisher_id IS DISTINCT FROM OLD.publisher_id
       OR NEW.publisher_wallet IS DISTINCT FROM OLD.publisher_wallet
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
       OR NEW.selection_version IS DISTINCT FROM OLD.selection_version
       OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.batch_id IS DISTINCT FROM OLD.batch_id
       OR NEW.slot_id IS DISTINCT FROM OLD.slot_id
       OR NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id
       OR NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.provider_id IS DISTINCT FROM OLD.provider_id
       OR NEW.chain_id IS DISTINCT FROM OLD.chain_id
       OR NEW.contract_address IS DISTINCT FROM OLD.contract_address
       OR NEW.proof_task_id IS DISTINCT FROM OLD.proof_task_id
       OR NEW.assignment_id IS DISTINCT FROM OLD.assignment_id
       OR NEW.agent_controller IS DISTINCT FROM OLD.agent_controller
       OR NEW.payout_address IS DISTINCT FROM OLD.payout_address
       OR NEW.overview_id IS DISTINCT FROM OLD.overview_id
       OR NEW.allocation_id IS DISTINCT FROM OLD.allocation_id
       OR NEW.proof_allocation_id IS DISTINCT FROM OLD.proof_allocation_id
       OR NEW.quote_hash IS DISTINCT FROM OLD.quote_hash
       OR NEW.task_spec_hash IS DISTINCT FROM OLD.task_spec_hash
       OR NEW.match_revision IS DISTINCT FROM OLD.match_revision
       OR NEW.price_version IS DISTINCT FROM OLD.price_version
       OR NEW.overview_price IS DISTINCT FROM OLD.overview_price
       OR NEW.formal_gross_price IS DISTINCT FROM OLD.formal_gross_price
       OR NEW.overview_credit IS DISTINCT FROM OLD.overview_credit
       OR NEW.formal_payable IS DISTINCT FROM OLD.formal_payable
       OR NEW.policy_hash IS DISTINCT FROM OLD.policy_hash
       OR NEW.selection_nonce IS DISTINCT FROM OLD.selection_nonce
       OR NEW.proof_deadline IS DISTINCT FROM OLD.proof_deadline
       OR NEW.proof_payload_hash IS DISTINCT FROM OLD.proof_payload_hash
       OR NEW.proof_digest IS DISTINCT FROM OLD.proof_digest
       OR NEW.capacity_fencing_token IS DISTINCT FROM OLD.capacity_fencing_token
       OR NEW.capacity_expires_at IS DISTINCT FROM OLD.capacity_expires_at
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'selection reservation identity is immutable';
    END IF;
    IF NOT (NEW.status = OLD.status
       OR (OLD.status = 'reserved' AND NEW.status IN ('submitted','confirmed','failed','expired'))
       OR (OLD.status = 'submitted' AND NEW.status IN ('confirmed','failed','expired'))
       OR (OLD.status = 'confirmed' AND NEW.status = 'orphaned')) THEN
        RAISE EXCEPTION 'invalid selection reservation transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_chain_projection_history_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'chain projection history is immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER chain_blocks_immutable BEFORE UPDATE OR DELETE ON chain_blocks
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER chain_block_states_immutable BEFORE UPDATE OR DELETE ON chain_block_states
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER chain_transactions_immutable BEFORE UPDATE OR DELETE ON chain_transactions
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER chain_events_immutable BEFORE UPDATE OR DELETE ON chain_events
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER chain_event_states_immutable BEFORE UPDATE OR DELETE ON chain_event_states
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER chain_reconciliation_runs_immutable BEFORE UPDATE OR DELETE ON chain_reconciliation_runs
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER chain_reconciliation_differences_immutable BEFORE UPDATE OR DELETE ON chain_reconciliation_differences
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();
CREATE TRIGGER assignment_states_immutable BEFORE UPDATE OR DELETE ON assignment_states
    FOR EACH ROW EXECUTE FUNCTION reject_chain_projection_history_mutation();

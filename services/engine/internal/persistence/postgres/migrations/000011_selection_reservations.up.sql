CREATE UNIQUE INDEX IF NOT EXISTS overview_batches_selection_identity_uidx
    ON overview_batches (batch_id, task_id, match_revision);
CREATE UNIQUE INDEX IF NOT EXISTS overview_slots_selection_identity_uidx
    ON overview_slots (slot_id, batch_id);

CREATE TABLE IF NOT EXISTS selection_reservations (
    reservation_id text PRIMARY KEY CHECK (reservation_id ~ '^sha256:[0-9a-f]{64}$'),
    publisher_id text NOT NULL REFERENCES users(user_id),
    publisher_wallet text NOT NULL CHECK (publisher_wallet ~ '^0x[0-9a-f]{40}$'),
    idempotency_key text NOT NULL CHECK (idempotency_key <> ''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    selection_version text NOT NULL CHECK (selection_version = 'selection-reservation-v1'),
    task_id text NOT NULL REFERENCES tasks(task_id),
    batch_id text NOT NULL REFERENCES overview_batches(batch_id),
    slot_id text NOT NULL REFERENCES overview_slots(slot_id),
    snapshot_id text NOT NULL REFERENCES match_snapshots(snapshot_id),
    agent_id text NOT NULL REFERENCES agents(agent_id),
    provider_id text NOT NULL REFERENCES users(user_id),
    chain_id numeric(78,0) NOT NULL CHECK (chain_id > 0),
    contract_address text NOT NULL CHECK (contract_address ~ '^0x[0-9a-f]{40}$'),
    proof_task_id text NOT NULL CHECK (proof_task_id ~ '^0x[0-9a-f]{64}$'),
    assignment_id text NOT NULL UNIQUE CHECK (assignment_id ~ '^0x[0-9a-f]{64}$'),
    agent_controller text NOT NULL CHECK (agent_controller ~ '^0x[0-9a-f]{40}$'),
    payout_address text NOT NULL CHECK (payout_address ~ '^0x[0-9a-f]{40}$'),
    overview_id text NOT NULL CHECK (overview_id ~ '^0x[0-9a-f]{64}$'),
    allocation_id text NOT NULL UNIQUE REFERENCES fund_allocations(allocation_id),
    proof_allocation_id text NOT NULL UNIQUE CHECK (proof_allocation_id ~ '^0x[0-9a-f]{64}$'),
    quote_hash text NOT NULL CHECK (quote_hash ~ '^0x[0-9a-f]{64}$'),
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^0x[0-9a-f]{64}$'),
    match_revision bigint NOT NULL CHECK (match_revision > 0),
    price_version bigint NOT NULL CHECK (price_version > 0),
    overview_price numeric(78,0) NOT NULL CHECK (overview_price >= 0),
    formal_gross_price numeric(78,0) NOT NULL CHECK (formal_gross_price > 0),
    overview_credit numeric(78,0) NOT NULL CHECK (overview_credit >= 0),
    formal_payable numeric(78,0) NOT NULL CHECK (formal_payable = formal_gross_price - overview_credit),
    policy_hash text NOT NULL CHECK (policy_hash ~ '^0x[0-9a-f]{64}$'),
    selection_nonce text NOT NULL UNIQUE CHECK (selection_nonce ~ '^0x[0-9a-f]{64}$'),
    proof_deadline bigint NOT NULL CHECK (proof_deadline > 0),
    proof_payload_hash text NOT NULL CHECK (proof_payload_hash ~ '^0x[0-9a-f]{64}$'),
    proof_digest text NOT NULL CHECK (proof_digest ~ '^0x[0-9a-f]{64}$'),
    capacity_fencing_token bigint NOT NULL CHECK (capacity_fencing_token > 0),
    capacity_expires_at timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('reserved','submitted','confirmed','failed','expired')),
    transaction_hash text CHECK (transaction_hash IS NULL OR transaction_hash ~ '^0x[0-9a-f]{64}$'),
    failure_reason_code text CHECK (failure_reason_code IS NULL OR failure_reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (publisher_id, idempotency_key),
    CONSTRAINT selection_reservation_batch_identity_fk FOREIGN KEY (batch_id, task_id, match_revision)
        REFERENCES overview_batches(batch_id, task_id, match_revision),
    CONSTRAINT selection_reservation_slot_identity_fk FOREIGN KEY (slot_id, batch_id)
        REFERENCES overview_slots(slot_id, batch_id),
    CHECK (overview_credit <= overview_price AND overview_credit <= formal_gross_price),
    CHECK (extract(epoch FROM capacity_expires_at)::bigint >= proof_deadline),
    CONSTRAINT selection_reservations_terminal_evidence_check CHECK ((status = 'reserved' AND transaction_hash IS NULL AND failure_reason_code IS NULL)
        OR (status = 'submitted' AND transaction_hash IS NOT NULL AND failure_reason_code IS NULL)
        OR (status = 'confirmed' AND transaction_hash IS NOT NULL AND failure_reason_code IS NULL)
        OR (status = 'failed' AND transaction_hash IS NOT NULL AND failure_reason_code IS NOT NULL)
        OR (status = 'expired' AND failure_reason_code = 'reservation_expired'))
);
CREATE UNIQUE INDEX IF NOT EXISTS selection_reservations_active_task_uidx
    ON selection_reservations (task_id) WHERE status IN ('reserved','submitted','confirmed');
CREATE INDEX IF NOT EXISTS selection_reservations_expiry_idx
    ON selection_reservations (proof_deadline) WHERE status IN ('reserved','submitted');

CREATE TABLE IF NOT EXISTS assignments (
    assignment_id text PRIMARY KEY CHECK (assignment_id ~ '^0x[0-9a-f]{64}$'),
    task_id text NOT NULL UNIQUE REFERENCES tasks(task_id),
    reservation_id text NOT NULL UNIQUE REFERENCES selection_reservations(reservation_id),
    agent_id text NOT NULL REFERENCES agents(agent_id),
    provider_id text NOT NULL REFERENCES users(user_id),
    formal_payable numeric(78,0) NOT NULL CHECK (formal_payable >= 0),
    overview_credit numeric(78,0) NOT NULL CHECK (overview_credit >= 0),
    work_nonce bigint NOT NULL CHECK (work_nonce = 1),
    transaction_hash text NOT NULL UNIQUE CHECK (transaction_hash ~ '^0x[0-9a-f]{64}$'),
    confirmed_at timestamptz NOT NULL
);

CREATE TABLE IF NOT EXISTS selection_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    reservation_id text NOT NULL REFERENCES selection_reservations(reservation_id),
    assignment_id text REFERENCES assignments(assignment_id),
    event_type text NOT NULL CHECK (event_type IN ('reserved','submitted','confirmed','failed','expired')),
    transaction_hash text CHECK (transaction_hash IS NULL OR transaction_hash ~ '^0x[0-9a-f]{64}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at timestamptz NOT NULL,
    UNIQUE (reservation_id, event_type)
);

CREATE OR REPLACE FUNCTION enforce_selection_reservation_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'selection reservations are immutable history';
    END IF;
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
       OR (OLD.status = 'submitted' AND NEW.status IN ('confirmed','failed','expired'))) THEN
        RAISE EXCEPTION 'invalid selection reservation transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_selection_history_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'selection history is immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS selection_reservations_transition ON selection_reservations;
CREATE TRIGGER selection_reservations_transition BEFORE UPDATE OR DELETE ON selection_reservations
    FOR EACH ROW EXECUTE FUNCTION enforce_selection_reservation_transition();
DROP TRIGGER IF EXISTS assignments_immutable ON assignments;
CREATE TRIGGER assignments_immutable BEFORE UPDATE OR DELETE ON assignments
    FOR EACH ROW EXECUTE FUNCTION reject_selection_history_mutation();
DROP TRIGGER IF EXISTS selection_events_immutable ON selection_events;
CREATE TRIGGER selection_events_immutable BEFORE UPDATE OR DELETE ON selection_events
    FOR EACH ROW EXECUTE FUNCTION reject_selection_history_mutation();

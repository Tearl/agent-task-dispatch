CREATE TABLE IF NOT EXISTS fund_accounts (
    account_id text PRIMARY KEY CHECK (account_id ~ '^sha256:[0-9a-f]{64}$'),
    account_class text NOT NULL CHECK (account_class IN ('business','system')),
    account_type text NOT NULL CHECK (account_type IN (
        'discovery_pool','formal_escrow','change_order_escrow','dispute_fee_pool',
        'funding_control','agent_receivable','external_cost_clearing'
    )),
    task_id text REFERENCES tasks(task_id),
    reference_id text NOT NULL CHECK (reference_id <> ''),
    asset_key text NOT NULL CHECK (asset_key ~ '^[a-z0-9][a-z0-9:/._-]{0,127}$'),
    principal_owner_id text REFERENCES users(user_id),
    residual_recipient_id text REFERENCES users(user_id),
    refund_policy_version text,
    state text NOT NULL CHECK (state IN ('open','frozen','closed')),
    balance numeric(78,0) NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (account_type, reference_id, asset_key),
    UNIQUE (account_id, asset_key),
    CHECK ((account_class = 'business'
            AND account_type IN ('discovery_pool','formal_escrow','change_order_escrow','dispute_fee_pool')
            AND task_id IS NOT NULL AND principal_owner_id IS NOT NULL
            AND residual_recipient_id IS NOT NULL AND refund_policy_version IS NOT NULL
            AND balance >= 0)
        OR (account_class = 'system'
            AND account_type IN ('funding_control','agent_receivable','external_cost_clearing')
            AND task_id IS NULL AND principal_owner_id IS NULL
            AND residual_recipient_id IS NULL AND refund_policy_version IS NULL)),
    CHECK (account_type <> 'discovery_pool' OR reference_id = task_id)
);
CREATE INDEX IF NOT EXISTS fund_accounts_task_idx ON fund_accounts (task_id, account_type, asset_key);

CREATE TABLE IF NOT EXISTS fund_allocations (
    allocation_id text PRIMARY KEY CHECK (allocation_id ~ '^sha256:[0-9a-f]{64}$'),
    idempotency_key text NOT NULL UNIQUE CHECK (idempotency_key <> ''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    ledger_version text NOT NULL CHECK (ledger_version = 'double-entry-v1'),
    purpose text NOT NULL CHECK (purpose = 'overview'),
    account_id text NOT NULL,
    asset_key text NOT NULL,
    task_id text NOT NULL,
    task_spec_hash text NOT NULL CHECK (task_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    snapshot_id text NOT NULL REFERENCES match_snapshots(snapshot_id),
    match_revision integer NOT NULL CHECK (match_revision > 0),
    agent_id text NOT NULL REFERENCES agents(agent_id),
    price_version integer NOT NULL CHECK (price_version > 0),
    quote_hash text NOT NULL CHECK (quote_hash ~ '^sha256:[0-9a-f]{64}$'),
    overview_price numeric(78,0) NOT NULL CHECK (overview_price >= 0),
    external_cost_cap numeric(78,0) NOT NULL CHECK (external_cost_cap >= 0),
    reserve_amount numeric(78,0) NOT NULL CHECK (reserve_amount = overview_price + external_cost_cap),
    status text NOT NULL CHECK (status IN ('authorized','captured','released')),
    capture_claim_hash text CHECK (capture_claim_hash IS NULL OR capture_claim_hash ~ '^sha256:[0-9a-f]{64}$'),
    captured_overview numeric(78,0) NOT NULL DEFAULT 0 CHECK (captured_overview >= 0),
    captured_cost numeric(78,0) NOT NULL DEFAULT 0 CHECK (captured_cost >= 0),
    capture_journal_id text,
    release_reason_code text,
    deadline timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    FOREIGN KEY (account_id, asset_key) REFERENCES fund_accounts(account_id, asset_key),
    CONSTRAINT fund_allocations_task_spec_fk FOREIGN KEY (task_id, task_spec_hash)
        REFERENCES task_spec_versions(task_id, content_hash),
    CONSTRAINT fund_allocations_snapshot_identity_fk FOREIGN KEY (snapshot_id, task_id, match_revision)
        REFERENCES match_snapshots(snapshot_id, task_id, match_revision),
    UNIQUE (snapshot_id, agent_id),
    CHECK ((status = 'authorized' AND capture_claim_hash IS NULL AND captured_overview = 0
            AND captured_cost = 0 AND capture_journal_id IS NULL AND release_reason_code IS NULL)
        OR (status = 'captured' AND capture_claim_hash IS NOT NULL
            AND captured_overview = overview_price AND captured_cost <= external_cost_cap
            AND release_reason_code IS NULL)
        OR (status = 'released' AND capture_claim_hash IS NULL AND captured_overview = 0
            AND captured_cost = 0 AND capture_journal_id IS NULL AND release_reason_code IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS fund_allocations_reserved_idx
    ON fund_allocations (account_id, status) WHERE status = 'authorized';

CREATE TABLE IF NOT EXISTS fund_allocation_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    allocation_id text NOT NULL REFERENCES fund_allocations(allocation_id),
    event_type text NOT NULL CHECK (event_type IN ('authorized','captured','released')),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at timestamptz NOT NULL,
    UNIQUE (allocation_id, event_type)
);

CREATE TABLE IF NOT EXISTS fund_journals (
    journal_id text PRIMARY KEY CHECK (journal_id ~ '^sha256:[0-9a-f]{64}$'),
    idempotency_key text NOT NULL UNIQUE CHECK (idempotency_key <> ''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    ledger_version text NOT NULL CHECK (ledger_version = 'double-entry-v1'),
    journal_type text NOT NULL CHECK (journal_type IN ('funding','overview_capture','reversal')),
    task_id text REFERENCES tasks(task_id),
    allocation_id text REFERENCES fund_allocations(allocation_id),
    reversal_of text UNIQUE REFERENCES fund_journals(journal_id),
    source_ref text NOT NULL CHECK (source_ref <> ''),
    reason_code text NOT NULL CHECK (reason_code ~ '^[a-z0-9][a-z0-9_,.-]{0,255}$'),
    created_at timestamptz NOT NULL,
    CHECK ((journal_type = 'funding' AND allocation_id IS NULL AND reversal_of IS NULL)
        OR (journal_type = 'overview_capture' AND allocation_id IS NOT NULL AND reversal_of IS NULL)
        OR (journal_type = 'reversal' AND allocation_id IS NULL AND reversal_of IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS fund_entries (
    journal_id text NOT NULL REFERENCES fund_journals(journal_id),
    entry_index integer NOT NULL CHECK (entry_index > 0),
    account_id text NOT NULL,
    account_type text NOT NULL,
    direction text NOT NULL CHECK (direction IN ('debit','credit')),
    amount numeric(78,0) NOT NULL CHECK (amount > 0),
    asset_key text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (journal_id, entry_index),
    FOREIGN KEY (account_id, asset_key) REFERENCES fund_accounts(account_id, asset_key)
);
CREATE INDEX IF NOT EXISTS fund_entries_account_idx ON fund_entries (account_id, created_at, journal_id);

CREATE OR REPLACE FUNCTION enforce_fund_account_mutation() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'fund accounts are immutable history';
    END IF;
    IF NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.account_class IS DISTINCT FROM OLD.account_class
       OR NEW.account_type IS DISTINCT FROM OLD.account_type
       OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.reference_id IS DISTINCT FROM OLD.reference_id
       OR NEW.asset_key IS DISTINCT FROM OLD.asset_key
       OR NEW.principal_owner_id IS DISTINCT FROM OLD.principal_owner_id
       OR NEW.residual_recipient_id IS DISTINCT FROM OLD.residual_recipient_id
       OR NEW.refund_policy_version IS DISTINCT FROM OLD.refund_policy_version
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'fund account identity is immutable';
    END IF;
    IF NEW.balance IS DISTINCT FROM OLD.balance AND pg_trigger_depth() < 2 THEN
        RAISE EXCEPTION 'fund balance can only change through ledger entries';
    END IF;
    IF NOT (NEW.state = OLD.state
        OR (OLD.state = 'open' AND NEW.state IN ('frozen','closed'))
        OR (OLD.state = 'frozen' AND NEW.state IN ('open','closed'))) THEN
        RAISE EXCEPTION 'invalid fund account state transition';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION apply_fund_entry_balance() RETURNS trigger AS $$
DECLARE
    expected_type text;
BEGIN
    SELECT account_type INTO expected_type FROM fund_accounts WHERE account_id = NEW.account_id FOR UPDATE;
    IF expected_type IS NULL OR expected_type <> NEW.account_type THEN
        RAISE EXCEPTION 'fund entry account type mismatch';
    END IF;
    UPDATE fund_accounts
       SET balance = balance + CASE WHEN NEW.direction = 'credit' THEN NEW.amount ELSE -NEW.amount END,
           updated_at = NEW.created_at
     WHERE account_id = NEW.account_id;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION assert_fund_journal_balanced() RETURNS trigger AS $$
DECLARE
    target_id text := COALESCE(NEW.journal_id, OLD.journal_id);
    entry_count integer;
    debit_total numeric(78,0);
    credit_total numeric(78,0);
    asset_count integer;
    target_type text;
    target_allocation text;
    target_reversal text;
    invalid_count integer;
    agent_credit numeric(78,0);
    external_credit numeric(78,0);
BEGIN
    SELECT count(*),
           COALESCE(sum(amount) FILTER (WHERE direction = 'debit'), 0),
           COALESCE(sum(amount) FILTER (WHERE direction = 'credit'), 0),
           count(DISTINCT asset_key)
      INTO entry_count, debit_total, credit_total, asset_count
      FROM fund_entries WHERE journal_id = target_id;
    IF entry_count < 2 OR asset_count <> 1 OR debit_total <> credit_total THEN
        RAISE EXCEPTION 'fund journal must contain one-asset balanced double entries';
    END IF;
    SELECT journal_type, allocation_id, reversal_of
      INTO target_type, target_allocation, target_reversal
      FROM fund_journals WHERE journal_id = target_id;
    IF target_type = 'overview_capture' THEN
        SELECT count(*) INTO invalid_count
          FROM fund_entries entry
          JOIN fund_accounts account ON account.account_id = entry.account_id
          JOIN fund_allocations allocation ON allocation.allocation_id = target_allocation
         WHERE entry.journal_id = target_id
           AND ((entry.direction = 'debit' AND entry.account_id <> allocation.account_id)
             OR (entry.direction = 'credit' AND account.account_type NOT IN ('agent_receivable','external_cost_clearing'))
             OR (entry.direction = 'credit' AND account.account_type = 'agent_receivable'
                 AND account.reference_id <> allocation.agent_id)
             OR (entry.direction = 'credit' AND account.account_type = 'external_cost_clearing'
                 AND account.reference_id <> allocation.allocation_id));
        SELECT COALESCE(sum(entry.amount) FILTER (WHERE account.account_type = 'agent_receivable'),0),
               COALESCE(sum(entry.amount) FILTER (WHERE account.account_type = 'external_cost_clearing'),0)
          INTO agent_credit, external_credit
          FROM fund_entries entry
          JOIN fund_accounts account ON account.account_id = entry.account_id
         WHERE entry.journal_id = target_id AND entry.direction = 'credit';
        IF invalid_count <> 0 OR NOT EXISTS (
            SELECT 1 FROM fund_allocations allocation
             WHERE allocation.allocation_id = target_allocation
               AND allocation.status = 'captured'
               AND debit_total = allocation.captured_overview + allocation.captured_cost
               AND agent_credit = allocation.captured_overview
               AND external_credit = allocation.captured_cost
        ) THEN
            RAISE EXCEPTION 'overview capture journal crossed its allocation boundary';
        END IF;
    END IF;
    IF target_type = 'reversal' AND (EXISTS (
        SELECT account_id, direction, amount, asset_key FROM fund_entries WHERE journal_id = target_id
        EXCEPT ALL
        SELECT account_id, CASE direction WHEN 'debit' THEN 'credit' ELSE 'debit' END, amount, asset_key
          FROM fund_entries WHERE journal_id = target_reversal
    ) OR EXISTS (
        SELECT account_id, CASE direction WHEN 'debit' THEN 'credit' ELSE 'debit' END, amount, asset_key
          FROM fund_entries WHERE journal_id = target_reversal
        EXCEPT ALL
        SELECT account_id, direction, amount, asset_key FROM fund_entries WHERE journal_id = target_id
    )) THEN
        RAISE EXCEPTION 'fund reversal must exactly invert the original journal';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION reject_fund_history_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'fund journals and entries are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION enforce_fund_allocation_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'fund allocations are immutable history';
    END IF;
    IF NEW.allocation_id IS DISTINCT FROM OLD.allocation_id
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
       OR NEW.ledger_version IS DISTINCT FROM OLD.ledger_version
       OR NEW.purpose IS DISTINCT FROM OLD.purpose
       OR NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.asset_key IS DISTINCT FROM OLD.asset_key
       OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.task_spec_hash IS DISTINCT FROM OLD.task_spec_hash
       OR NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id
       OR NEW.match_revision IS DISTINCT FROM OLD.match_revision
       OR NEW.agent_id IS DISTINCT FROM OLD.agent_id
       OR NEW.price_version IS DISTINCT FROM OLD.price_version
       OR NEW.quote_hash IS DISTINCT FROM OLD.quote_hash
       OR NEW.overview_price IS DISTINCT FROM OLD.overview_price
       OR NEW.external_cost_cap IS DISTINCT FROM OLD.external_cost_cap
       OR NEW.reserve_amount IS DISTINCT FROM OLD.reserve_amount
       OR NEW.deadline IS DISTINCT FROM OLD.deadline
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'fund allocation identity is immutable';
    END IF;
    IF NOT (NEW.status = OLD.status OR (OLD.status = 'authorized' AND NEW.status IN ('captured','released'))) THEN
        RAISE EXCEPTION 'invalid fund allocation transition';
    END IF;
    IF OLD.status <> 'authorized' AND ROW(NEW.capture_claim_hash,NEW.captured_overview,NEW.captured_cost,NEW.capture_journal_id,NEW.release_reason_code)
        IS DISTINCT FROM ROW(OLD.capture_claim_hash,OLD.captured_overview,OLD.captured_cost,OLD.capture_journal_id,OLD.release_reason_code) THEN
        RAISE EXCEPTION 'terminal fund allocation evidence is immutable';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS fund_accounts_mutation ON fund_accounts;
CREATE TRIGGER fund_accounts_mutation BEFORE UPDATE OR DELETE ON fund_accounts
    FOR EACH ROW EXECUTE FUNCTION enforce_fund_account_mutation();
DROP TRIGGER IF EXISTS fund_entries_balance ON fund_entries;
CREATE TRIGGER fund_entries_balance AFTER INSERT ON fund_entries
    FOR EACH ROW EXECUTE FUNCTION apply_fund_entry_balance();
DROP TRIGGER IF EXISTS fund_journals_immutable ON fund_journals;
CREATE TRIGGER fund_journals_immutable BEFORE UPDATE OR DELETE ON fund_journals
    FOR EACH ROW EXECUTE FUNCTION reject_fund_history_mutation();
DROP TRIGGER IF EXISTS fund_entries_immutable ON fund_entries;
CREATE TRIGGER fund_entries_immutable BEFORE UPDATE OR DELETE ON fund_entries
    FOR EACH ROW EXECUTE FUNCTION reject_fund_history_mutation();
DROP TRIGGER IF EXISTS fund_allocation_events_immutable ON fund_allocation_events;
CREATE TRIGGER fund_allocation_events_immutable BEFORE UPDATE OR DELETE ON fund_allocation_events
    FOR EACH ROW EXECUTE FUNCTION reject_fund_history_mutation();
DROP TRIGGER IF EXISTS fund_allocations_transition ON fund_allocations;
CREATE TRIGGER fund_allocations_transition BEFORE UPDATE OR DELETE ON fund_allocations
    FOR EACH ROW EXECUTE FUNCTION enforce_fund_allocation_transition();

CREATE CONSTRAINT TRIGGER fund_journal_balance_from_journal
    AFTER INSERT ON fund_journals DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_fund_journal_balanced();
CREATE CONSTRAINT TRIGGER fund_journal_balance_from_entry
    AFTER INSERT ON fund_entries DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION assert_fund_journal_balanced();

ALTER TABLE fund_allocations ADD CONSTRAINT fund_allocations_capture_journal_fk
    FOREIGN KEY (capture_journal_id) REFERENCES fund_journals(journal_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE overview_slots ADD CONSTRAINT overview_slots_fund_allocation_fk
    FOREIGN KEY (allocation_id) REFERENCES fund_allocations(allocation_id);

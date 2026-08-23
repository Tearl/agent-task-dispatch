CREATE TABLE formal_acceptance_intents (
    acceptance_intent_id text PRIMARY KEY CHECK (acceptance_intent_id ~ '^sha256:[0-9a-f]{64}$'),
    acceptance_version text NOT NULL CHECK (acceptance_version='formal-acceptance-v1'),
    package_id text NOT NULL REFERENCES formal_packages(package_id),
    task_id text NOT NULL REFERENCES tasks(task_id),
    formal_version integer NOT NULL CHECK (formal_version BETWEEN 1 AND 5),
    content_hash text NOT NULL CHECK (content_hash ~ '^sha256:[0-9a-f]{64}$'),
    proof_digest text NOT NULL CHECK (proof_digest ~ '^sha256:[0-9a-f]{64}$'),
    work_nonce bigint NOT NULL CHECK (work_nonce > 0),
    package_aggregate_version bigint NOT NULL CHECK (package_aggregate_version > 0),
    publisher_id text NOT NULL REFERENCES users(user_id),
    created_at timestamptz NOT NULL,
    UNIQUE (package_id,formal_version,proof_digest),
    FOREIGN KEY (package_id,formal_version) REFERENCES formal_versions(package_id,version_no)
);

CREATE TABLE formal_acceptance_states (
    acceptance_intent_id text NOT NULL REFERENCES formal_acceptance_intents(acceptance_intent_id),
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    state text NOT NULL CHECK (state IN ('intent_recorded','pending_confirmation','confirmed','orphaned')),
    transaction_hash text CHECK (transaction_hash IS NULL OR transaction_hash ~ '^0x[0-9a-f]{64}$'),
    chain_event_id text REFERENCES chain_events(event_id),
    reason_code text CHECK (reason_code IS NULL OR reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    occurred_at timestamptz NOT NULL,
    PRIMARY KEY (acceptance_intent_id,aggregate_version),
    CHECK ((state='intent_recorded' AND transaction_hash IS NULL AND chain_event_id IS NULL)
      OR (state='pending_confirmation' AND transaction_hash IS NOT NULL AND chain_event_id IS NULL)
      OR (state='confirmed' AND transaction_hash IS NOT NULL AND chain_event_id IS NOT NULL)
      OR (state='orphaned' AND transaction_hash IS NOT NULL AND chain_event_id IS NOT NULL))
);

CREATE UNIQUE INDEX formal_acceptance_one_pending_tx_uidx
    ON formal_acceptance_states(transaction_hash) WHERE state='pending_confirmation';
CREATE UNIQUE INDEX formal_acceptance_one_confirmed_event_uidx
    ON formal_acceptance_states(chain_event_id) WHERE state='confirmed';

CREATE TABLE formal_acceptance_requests (
    publisher_id text NOT NULL REFERENCES users(user_id),
    idempotency_key text NOT NULL CHECK (idempotency_key<>''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    operation text NOT NULL CHECK (operation IN ('create','submit','reconcile')),
    task_id text NOT NULL REFERENCES tasks(task_id),
    acceptance_intent_id text NOT NULL REFERENCES formal_acceptance_intents(acceptance_intent_id),
    response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body)='object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (publisher_id,idempotency_key)
);

ALTER TABLE fund_journals DROP CONSTRAINT fund_journals_journal_type_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_journal_type_check CHECK (journal_type IN (
    'funding','overview_capture','settlement_release','settlement_refund','earnings_withdrawal','change_order_release','change_order_residual','reversal'
));
ALTER TABLE fund_journals DROP CONSTRAINT fund_journals_shape_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_shape_check CHECK (
    (journal_type='funding' AND allocation_id IS NULL AND reversal_of IS NULL)
 OR (journal_type='overview_capture' AND allocation_id IS NOT NULL AND reversal_of IS NULL)
 OR (journal_type IN ('settlement_release','settlement_refund','earnings_withdrawal','change_order_release','change_order_residual') AND allocation_id IS NULL AND reversal_of IS NULL)
 OR (journal_type='reversal' AND allocation_id IS NULL AND reversal_of IS NOT NULL)
);

CREATE TABLE formal_change_order_settlements (
    change_order_id text NOT NULL REFERENCES formal_change_orders(change_order_id),
    acceptance_intent_id text NOT NULL REFERENCES formal_acceptance_intents(acceptance_intent_id),
    chain_event_id text NOT NULL UNIQUE REFERENCES chain_events(event_id),
    journal_id text NOT NULL UNIQUE REFERENCES fund_journals(journal_id),
    residual_journal_id text UNIQUE REFERENCES fund_journals(journal_id),
    amount numeric(78,0) NOT NULL CHECK (amount > 0),
    residual_amount numeric(78,0) NOT NULL CHECK (residual_amount >= 0),
    residual_recipient_id text NOT NULL REFERENCES users(user_id),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (change_order_id,chain_event_id),
    CHECK ((residual_amount=0 AND residual_journal_id IS NULL) OR (residual_amount>0 AND residual_journal_id IS NOT NULL))
);

CREATE OR REPLACE FUNCTION reject_formal_acceptance_mutation() RETURNS trigger AS $$
BEGIN RAISE EXCEPTION 'formal acceptance history is immutable'; END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER formal_acceptance_intents_immutable BEFORE UPDATE OR DELETE ON formal_acceptance_intents
    FOR EACH ROW EXECUTE FUNCTION reject_formal_acceptance_mutation();
CREATE TRIGGER formal_acceptance_states_immutable BEFORE UPDATE OR DELETE ON formal_acceptance_states
    FOR EACH ROW EXECUTE FUNCTION reject_formal_acceptance_mutation();
CREATE TRIGGER formal_acceptance_requests_immutable BEFORE UPDATE OR DELETE ON formal_acceptance_requests
    FOR EACH ROW EXECUTE FUNCTION reject_formal_acceptance_mutation();
CREATE TRIGGER formal_change_order_settlements_immutable BEFORE UPDATE OR DELETE ON formal_change_order_settlements
    FOR EACH ROW EXECUTE FUNCTION reject_formal_acceptance_mutation();

CREATE OR REPLACE FUNCTION validate_change_order_release() RETURNS trigger AS $$
DECLARE target_journal text := COALESCE(NEW.journal_id,OLD.journal_id); invalid_count integer;
BEGIN
  IF EXISTS (SELECT 1 FROM fund_journals WHERE journal_id=target_journal AND journal_type='change_order_release') THEN
    SELECT count(*) INTO invalid_count
      FROM formal_change_order_settlements settlement
      JOIN formal_change_orders change_order ON change_order.change_order_id=settlement.change_order_id
     WHERE settlement.journal_id=target_journal
       AND (settlement.amount<>change_order.authorized_price OR change_order.status<>'consumed'
         OR NOT EXISTS (SELECT 1 FROM fund_entries entry WHERE entry.journal_id=target_journal AND entry.direction='debit' AND entry.account_id=change_order.fund_account_id AND entry.account_type='change_order_escrow' AND entry.amount=settlement.amount)
         OR NOT EXISTS (SELECT 1 FROM fund_entries entry JOIN fund_accounts account ON account.account_id=entry.account_id WHERE entry.journal_id=target_journal AND entry.direction='credit' AND entry.account_type='formal_agent_receivable' AND entry.amount=settlement.amount));
    IF invalid_count<>0 OR NOT EXISTS (SELECT 1 FROM formal_change_order_settlements WHERE journal_id=target_journal) THEN
      RAISE EXCEPTION 'change order release crossed its authorized funding boundary';
    END IF;
  END IF;
  IF EXISTS (SELECT 1 FROM fund_journals WHERE journal_id=target_journal AND journal_type='change_order_residual') THEN
    SELECT count(*) INTO invalid_count
      FROM formal_change_order_settlements settlement
      JOIN formal_change_orders change_order ON change_order.change_order_id=settlement.change_order_id
     WHERE settlement.residual_journal_id=target_journal
       AND (settlement.residual_amount<=0 OR settlement.residual_recipient_id<>change_order.residual_recipient_id
         OR NOT EXISTS (SELECT 1 FROM fund_entries entry WHERE entry.journal_id=target_journal AND entry.direction='debit' AND entry.account_id=change_order.fund_account_id AND entry.account_type='change_order_escrow' AND entry.amount=settlement.residual_amount)
         OR NOT EXISTS (SELECT 1 FROM fund_entries entry WHERE entry.journal_id=target_journal AND entry.direction='credit' AND entry.account_type='funding_control' AND entry.amount=settlement.residual_amount));
    IF invalid_count<>0 OR NOT EXISTS (SELECT 1 FROM formal_change_order_settlements WHERE residual_journal_id=target_journal) THEN
      RAISE EXCEPTION 'change order residual crossed its ownership boundary';
    END IF;
  END IF;
  RETURN NULL;
END;
$$ LANGUAGE plpgsql;
CREATE CONSTRAINT TRIGGER formal_change_order_release_boundary AFTER INSERT ON fund_entries
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION validate_change_order_release();

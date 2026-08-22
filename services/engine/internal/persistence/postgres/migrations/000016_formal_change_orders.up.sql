ALTER TABLE formal_scope_snapshots ADD COLUMN change_order_id text;
ALTER TABLE formal_scope_snapshots ADD COLUMN scope_differences jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(scope_differences) = 'array');

CREATE TABLE formal_change_orders (
    change_order_id text PRIMARY KEY CHECK (change_order_id ~ '^sha256:[0-9a-f]{64}$'),
    change_order_version text NOT NULL CHECK (change_order_version = 'formal-change-order-v1'),
    package_id text NOT NULL REFERENCES formal_packages(package_id),
    task_id text NOT NULL REFERENCES tasks(task_id),
    target_version integer NOT NULL CHECK (target_version IN (4,5)),
    trigger_version integer NOT NULL CHECK (trigger_version = target_version - 1),
    trigger_content_hash text NOT NULL CHECK (trigger_content_hash ~ '^sha256:[0-9a-f]{64}$'),
    feedback_set_id text NOT NULL UNIQUE REFERENCES formal_feedback_sets(feedback_set_id),
    feedback_digest text NOT NULL CHECK (feedback_digest ~ '^sha256:[0-9a-f]{64}$'),
    base_scope_id text NOT NULL REFERENCES formal_scope_snapshots(scope_id),
    base_scope_hash text NOT NULL CHECK (base_scope_hash ~ '^sha256:[0-9a-f]{64}$'),
    new_scope_id text UNIQUE,
    new_scope_hash text,
    new_scope_revision integer,
    new_spec_hash text NOT NULL CHECK (new_spec_hash ~ '^sha256:[0-9a-f]{64}$'),
    difference_digest text NOT NULL CHECK (difference_digest ~ '^sha256:[0-9a-f]{64}$'),
    scope_differences jsonb NOT NULL CHECK (jsonb_typeof(scope_differences) = 'array' AND jsonb_array_length(scope_differences) > 0),
    requested_price numeric(78,0) NOT NULL CHECK (requested_price > 0),
    authorized_price numeric(78,0) NOT NULL DEFAULT 0 CHECK (authorized_price >= 0),
    responsibility text CHECK (responsibility IN ('publisher','agent','platform')),
    responsibility_reason_code text CHECK (responsibility_reason_code IS NULL OR responsibility_reason_code ~ '^[a-z0-9][a-z0-9_-]{0,99}$'),
    funding_source text CHECK (funding_source IN ('publisher','agent_absorbed','platform_incident')),
    fund_account_id text,
    fund_account_type text CHECK (fund_account_type = 'change_order_escrow'),
    principal_owner_id text,
    residual_recipient_id text,
    publisher_compensation_irrevocable boolean NOT NULL DEFAULT false,
    package_aggregate_version bigint NOT NULL CHECK (package_aggregate_version > 0),
    aggregate_version bigint NOT NULL CHECK (aggregate_version > 0),
    status text NOT NULL CHECK (status IN ('responsibility_pending','awaiting_acceptance','awaiting_funding','ready_to_activate','effective','consumed')),
    deadline timestamptz NOT NULL,
    accepted_at timestamptz,
    effective_at timestamptz,
    consumed_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (package_id, target_version),
    UNIQUE (package_id, package_aggregate_version),
    FOREIGN KEY (package_id, trigger_version) REFERENCES formal_versions(package_id, version_no),
    CHECK ((status = 'responsibility_pending' AND responsibility IS NULL AND funding_source IS NULL AND fund_account_id IS NULL AND fund_account_type IS NULL AND accepted_at IS NULL AND effective_at IS NULL AND consumed_at IS NULL)
      OR (status <> 'responsibility_pending' AND responsibility IS NOT NULL AND funding_source IS NOT NULL AND principal_owner_id IS NOT NULL AND residual_recipient_id IS NOT NULL)),
    CHECK ((responsibility = 'agent' AND funding_source = 'agent_absorbed' AND authorized_price = 0 AND fund_account_id IS NULL AND fund_account_type IS NULL)
      OR (responsibility = 'publisher' AND funding_source = 'publisher' AND authorized_price = requested_price AND fund_account_id IS NOT NULL AND fund_account_type = 'change_order_escrow')
      OR (responsibility = 'platform' AND funding_source = 'platform_incident' AND authorized_price = requested_price AND fund_account_id IS NOT NULL AND fund_account_type = 'change_order_escrow')
      OR responsibility IS NULL),
    CHECK ((status IN ('responsibility_pending','awaiting_acceptance') AND accepted_at IS NULL)
      OR (status NOT IN ('responsibility_pending','awaiting_acceptance') AND accepted_at IS NOT NULL)),
    CHECK ((status IN ('effective','consumed') AND new_scope_id IS NOT NULL AND new_scope_hash ~ '^sha256:[0-9a-f]{64}$' AND new_scope_revision > 1 AND effective_at IS NOT NULL)
      OR (status NOT IN ('effective','consumed') AND new_scope_id IS NULL AND new_scope_hash IS NULL AND new_scope_revision IS NULL AND effective_at IS NULL)),
    CHECK ((status = 'consumed' AND consumed_at IS NOT NULL) OR (status <> 'consumed' AND consumed_at IS NULL))
);

ALTER TABLE fund_accounts ADD CONSTRAINT fund_accounts_identity_type_key UNIQUE (account_id,account_type);
ALTER TABLE formal_change_orders ADD CONSTRAINT formal_change_orders_fund_account_fk
    FOREIGN KEY (fund_account_id,fund_account_type) REFERENCES fund_accounts(account_id,account_type);

ALTER TABLE formal_scope_snapshots ADD CONSTRAINT formal_scope_snapshots_change_order_fk
    FOREIGN KEY (change_order_id) REFERENCES formal_change_orders(change_order_id);
ALTER TABLE formal_change_orders ADD CONSTRAINT formal_change_orders_new_scope_fk
    FOREIGN KEY (new_scope_id) REFERENCES formal_scope_snapshots(scope_id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE formal_versions ADD COLUMN change_order_id text REFERENCES formal_change_orders(change_order_id);
ALTER TABLE formal_versions ADD CONSTRAINT formal_versions_change_order_check
    CHECK ((version_no <= 3 AND change_order_id IS NULL) OR (version_no IN (4,5) AND change_order_id IS NOT NULL));
CREATE UNIQUE INDEX formal_versions_change_order_uidx ON formal_versions(change_order_id) WHERE change_order_id IS NOT NULL;

ALTER TABLE formal_billing_results DROP CONSTRAINT formal_billing_results_billing_status_check;
ALTER TABLE formal_billing_results DROP CONSTRAINT formal_billing_results_charge_amount_check;
ALTER TABLE formal_billing_results ADD CONSTRAINT formal_billing_results_billing_status_check
    CHECK (billing_status IN ('included','change_order'));
ALTER TABLE formal_billing_results ADD CONSTRAINT formal_billing_results_charge_amount_check
    CHECK ((billing_status='included' AND charge_amount=0) OR (billing_status='change_order' AND charge_amount>=0));

CREATE TABLE formal_change_order_requests (
    actor_id text NOT NULL REFERENCES users(user_id),
    idempotency_key text NOT NULL CHECK (idempotency_key <> ''),
    request_hash text NOT NULL CHECK (request_hash ~ '^sha256:[0-9a-f]{64}$'),
    operation text NOT NULL CHECK (operation IN ('propose','decide','accept','activate')),
    task_id text NOT NULL REFERENCES tasks(task_id),
    change_order_id text NOT NULL REFERENCES formal_change_orders(change_order_id),
    response_body jsonb NOT NULL CHECK (jsonb_typeof(response_body) = 'object'),
    created_at timestamptz NOT NULL,
    PRIMARY KEY (actor_id, idempotency_key)
);

CREATE TABLE formal_change_order_events (
    event_id text PRIMARY KEY CHECK (event_id ~ '^sha256:[0-9a-f]{64}$'),
    change_order_id text NOT NULL REFERENCES formal_change_orders(change_order_id),
    event_sequence bigint NOT NULL CHECK (event_sequence > 0),
    event_type text NOT NULL CHECK (event_type IN ('proposed','responsibility_decided','accepted','effective','consumed')),
    actor_id text NOT NULL REFERENCES users(user_id),
    payload jsonb NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    occurred_at timestamptz NOT NULL,
    UNIQUE (change_order_id,event_sequence)
);

CREATE OR REPLACE FUNCTION enforce_formal_change_order_transition() RETURNS trigger AS $$
DECLARE
    expected_publisher text;
    expected_provider text;
    account_principal text;
    account_residual text;
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'formal change order history is immutable'; END IF;
    IF NEW.change_order_id IS DISTINCT FROM OLD.change_order_id OR NEW.change_order_version IS DISTINCT FROM OLD.change_order_version
       OR NEW.package_id IS DISTINCT FROM OLD.package_id OR NEW.task_id IS DISTINCT FROM OLD.task_id
       OR NEW.target_version IS DISTINCT FROM OLD.target_version OR NEW.trigger_version IS DISTINCT FROM OLD.trigger_version
       OR NEW.trigger_content_hash IS DISTINCT FROM OLD.trigger_content_hash OR NEW.feedback_set_id IS DISTINCT FROM OLD.feedback_set_id
       OR NEW.feedback_digest IS DISTINCT FROM OLD.feedback_digest OR NEW.base_scope_id IS DISTINCT FROM OLD.base_scope_id
       OR NEW.base_scope_hash IS DISTINCT FROM OLD.base_scope_hash OR NEW.new_spec_hash IS DISTINCT FROM OLD.new_spec_hash
       OR NEW.difference_digest IS DISTINCT FROM OLD.difference_digest OR NEW.scope_differences IS DISTINCT FROM OLD.scope_differences
       OR NEW.requested_price IS DISTINCT FROM OLD.requested_price OR NEW.deadline IS DISTINCT FROM OLD.deadline
       OR NEW.created_at IS DISTINCT FROM OLD.created_at OR NEW.aggregate_version < OLD.aggregate_version
       OR NEW.package_aggregate_version < OLD.package_aggregate_version THEN RAISE EXCEPTION 'formal change order identity is immutable'; END IF;
    IF OLD.responsibility IS NOT NULL AND ROW(NEW.responsibility,NEW.responsibility_reason_code,NEW.funding_source,NEW.fund_account_id,NEW.fund_account_type,NEW.principal_owner_id,NEW.residual_recipient_id,NEW.publisher_compensation_irrevocable,NEW.authorized_price)
       IS DISTINCT FROM ROW(OLD.responsibility,OLD.responsibility_reason_code,OLD.funding_source,OLD.fund_account_id,OLD.fund_account_type,OLD.principal_owner_id,OLD.residual_recipient_id,OLD.publisher_compensation_irrevocable,OLD.authorized_price) THEN RAISE EXCEPTION 'responsibility decision is immutable'; END IF;
    IF OLD.accepted_at IS NOT NULL AND NEW.accepted_at IS DISTINCT FROM OLD.accepted_at THEN RAISE EXCEPTION 'change order acceptance is immutable'; END IF;
    IF OLD.effective_at IS NOT NULL AND ROW(NEW.new_scope_id,NEW.new_scope_hash,NEW.new_scope_revision,NEW.effective_at)
       IS DISTINCT FROM ROW(OLD.new_scope_id,OLD.new_scope_hash,OLD.new_scope_revision,OLD.effective_at) THEN RAISE EXCEPTION 'effective change order scope is immutable'; END IF;
    IF NEW.responsibility IS NOT NULL THEN
        SELECT publisher_id,provider_id INTO expected_publisher,expected_provider FROM formal_packages WHERE package_id=NEW.package_id;
        IF NEW.responsibility='publisher' AND (NEW.publisher_compensation_irrevocable OR NEW.principal_owner_id<>expected_publisher OR NEW.residual_recipient_id<>expected_publisher) THEN
            RAISE EXCEPTION 'publisher responsibility ownership policy mismatch';
        ELSIF NEW.responsibility='agent' AND (NEW.publisher_compensation_irrevocable OR NEW.principal_owner_id<>expected_provider OR NEW.residual_recipient_id<>expected_provider OR NEW.fund_account_id IS NOT NULL) THEN
            RAISE EXCEPTION 'agent responsibility cannot create a debit boundary';
        ELSIF NEW.responsibility IN ('publisher','platform') THEN
            SELECT principal_owner_id,residual_recipient_id INTO account_principal,account_residual FROM fund_accounts
             WHERE account_id=NEW.fund_account_id AND account_type='change_order_escrow' AND task_id=NEW.task_id AND reference_id=NEW.change_order_id;
            IF account_principal IS NULL OR NEW.principal_owner_id<>account_principal OR NEW.residual_recipient_id<>account_residual THEN
                RAISE EXCEPTION 'change order funding account ownership mismatch';
            END IF;
            IF NEW.responsibility='platform' AND ((NOT NEW.publisher_compensation_irrevocable AND NEW.residual_recipient_id<>NEW.principal_owner_id)
              OR (NEW.publisher_compensation_irrevocable AND NEW.residual_recipient_id<>expected_publisher)) THEN
                RAISE EXCEPTION 'platform incident residual policy mismatch';
            END IF;
        END IF;
    END IF;
    IF NOT (NEW.status=OLD.status OR (OLD.status='responsibility_pending' AND NEW.status='awaiting_acceptance')
      OR (OLD.status='awaiting_acceptance' AND NEW.status IN ('awaiting_funding','ready_to_activate'))
      OR (OLD.status IN ('awaiting_funding','ready_to_activate') AND NEW.status='effective')
      OR (OLD.status='effective' AND NEW.status='consumed')) THEN RAISE EXCEPTION 'invalid formal change order transition'; END IF;
    IF OLD.status='consumed' AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'consumed change order is immutable'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER formal_change_orders_transition BEFORE UPDATE OR DELETE ON formal_change_orders
    FOR EACH ROW EXECUTE FUNCTION enforce_formal_change_order_transition();
CREATE TRIGGER formal_change_order_requests_immutable BEFORE UPDATE OR DELETE ON formal_change_order_requests
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();
CREATE TRIGGER formal_change_order_events_immutable BEFORE UPDATE OR DELETE ON formal_change_order_events
    FOR EACH ROW EXECUTE FUNCTION reject_formal_append_only_mutation();

CREATE OR REPLACE FUNCTION enforce_formal_version_transition() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'formal version history is immutable'; END IF;
    IF NEW.package_id IS DISTINCT FROM OLD.package_id OR NEW.version_no IS DISTINCT FROM OLD.version_no
       OR NEW.package_aggregate_version IS DISTINCT FROM OLD.package_aggregate_version OR NEW.scope_id IS DISTINCT FROM OLD.scope_id
       OR NEW.scope_hash IS DISTINCT FROM OLD.scope_hash OR NEW.work_nonce IS DISTINCT FROM OLD.work_nonce
       OR NEW.parent_version IS DISTINCT FROM OLD.parent_version OR NEW.parent_content_hash IS DISTINCT FROM OLD.parent_content_hash
       OR NEW.feedback_set_id IS DISTINCT FROM OLD.feedback_set_id OR NEW.feedback_digest IS DISTINCT FROM OLD.feedback_digest
       OR NEW.feedback_aggregate_version IS DISTINCT FROM OLD.feedback_aggregate_version OR NEW.change_order_id IS DISTINCT FROM OLD.change_order_id
       OR NEW.logical_execution_id IS DISTINCT FROM OLD.logical_execution_id OR NEW.created_at IS DISTINCT FROM OLD.created_at
       OR NEW.used_cost < OLD.used_cost THEN RAISE EXCEPTION 'formal version command is immutable'; END IF;
    IF OLD.result_hash IS NOT NULL AND NEW.result_hash IS DISTINCT FROM OLD.result_hash THEN RAISE EXCEPTION 'formal version result identity is immutable'; END IF;
    IF OLD.status IN ('review','failed') AND NEW IS DISTINCT FROM OLD THEN RAISE EXCEPTION 'terminal formal version is immutable'; END IF;
    IF NOT (NEW.status = OLD.status OR (OLD.status = 'allocated' AND NEW.status IN ('generating','review','failed'))
      OR (OLD.status = 'generating' AND NEW.status IN ('review','failed'))) THEN RAISE EXCEPTION 'invalid formal version state transition'; END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

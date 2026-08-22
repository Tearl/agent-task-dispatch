ALTER TABLE chain_events DROP CONSTRAINT IF EXISTS chain_events_event_type_check;
ALTER TABLE chain_events ADD CONSTRAINT chain_events_event_type_check CHECK (event_type IN (
    'task_created','selection_confirmed','work_nonce_advanced','funds_released',
    'funds_refunded','earnings_accrued','earnings_withdrawn','yield_eligibility_changed',
    'dispute_opened','dispute_resolved'
));

ALTER TABLE chain_events ALTER COLUMN task_chain_id DROP NOT NULL;
ALTER TABLE chain_events DROP CONSTRAINT IF EXISTS chain_events_task_chain_id_check;
ALTER TABLE chain_events ADD CONSTRAINT chain_events_task_chain_id_check CHECK (
    (event_type = 'earnings_withdrawn' AND task_chain_id IS NULL)
 OR (event_type <> 'earnings_withdrawn' AND task_chain_id ~ '^0x[0-9a-f]{64}$')
);

ALTER TABLE fund_journals DROP CONSTRAINT IF EXISTS fund_journals_journal_type_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_journal_type_check CHECK (journal_type IN (
    'funding','overview_capture','settlement_release','settlement_refund','earnings_withdrawal','reversal'
));
ALTER TABLE fund_journals DROP CONSTRAINT IF EXISTS fund_journals_check;
ALTER TABLE fund_journals DROP CONSTRAINT IF EXISTS fund_journals_shape_check;
ALTER TABLE fund_journals ADD CONSTRAINT fund_journals_shape_check CHECK (
    (journal_type = 'funding' AND allocation_id IS NULL AND reversal_of IS NULL)
 OR (journal_type = 'overview_capture' AND allocation_id IS NOT NULL AND reversal_of IS NULL)
 OR (journal_type IN ('settlement_release','settlement_refund','earnings_withdrawal')
     AND allocation_id IS NULL AND reversal_of IS NULL)
 OR (journal_type = 'reversal' AND allocation_id IS NULL AND reversal_of IS NOT NULL)
);

ALTER TABLE fund_accounts DROP CONSTRAINT IF EXISTS fund_accounts_account_type_check;
ALTER TABLE fund_accounts ADD CONSTRAINT fund_accounts_account_type_check CHECK (account_type IN (
    'discovery_pool','formal_escrow','change_order_escrow','dispute_fee_pool',
    'funding_control','agent_receivable','formal_agent_receivable','external_cost_clearing'
));
ALTER TABLE fund_accounts DROP CONSTRAINT IF EXISTS fund_accounts_check;
ALTER TABLE fund_accounts DROP CONSTRAINT IF EXISTS fund_accounts_shape_check;
ALTER TABLE fund_accounts ADD CONSTRAINT fund_accounts_shape_check CHECK (
    (account_class = 'business'
     AND account_type IN ('discovery_pool','formal_escrow','change_order_escrow','dispute_fee_pool')
     AND task_id IS NOT NULL AND principal_owner_id IS NOT NULL
     AND residual_recipient_id IS NOT NULL AND refund_policy_version IS NOT NULL
     AND balance >= 0)
 OR (account_class = 'system'
     AND account_type IN ('funding_control','agent_receivable','formal_agent_receivable','external_cost_clearing')
     AND task_id IS NULL AND principal_owner_id IS NULL
     AND residual_recipient_id IS NULL AND refund_policy_version IS NULL)
);

-- These projections are derived only from the current canonical chain. A reorg
-- changes chain_canonical_blocks, so balances converge without mutating history.
CREATE VIEW chain_agent_earnings_positions AS
SELECT event.chain_id,
       event.contract_address,
       event.payload->>'agentController' AS agent_controller,
       event.payload->>'payout' AS payout_address,
       sum(CASE event.event_type
             WHEN 'earnings_accrued' THEN (event.payload->>'amount')::numeric
             ELSE -(event.payload->>'amount')::numeric
           END) AS claimable_amount
  FROM chain_events event
  JOIN chain_canonical_blocks canonical
    ON canonical.chain_id=event.chain_id
   AND canonical.contract_address=event.contract_address
   AND canonical.block_hash=event.block_hash
 WHERE event.event_type IN ('earnings_accrued','earnings_withdrawn')
 GROUP BY event.chain_id,event.contract_address,
          event.payload->>'agentController',event.payload->>'payout'
HAVING sum(CASE event.event_type
             WHEN 'earnings_accrued' THEN (event.payload->>'amount')::numeric
             ELSE -(event.payload->>'amount')::numeric
           END) >= 0;

CREATE VIEW chain_yield_positions AS
SELECT event.chain_id,event.contract_address,event.task_chain_id,
       sum(CASE WHEN (event.payload->>'eligible')::boolean
                THEN (event.payload->>'amount')::numeric
                ELSE -(event.payload->>'amount')::numeric END) AS eligible_principal
  FROM chain_events event
  JOIN chain_canonical_blocks canonical
    ON canonical.chain_id=event.chain_id
   AND canonical.contract_address=event.contract_address
   AND canonical.block_hash=event.block_hash
 WHERE event.event_type='yield_eligibility_changed'
 GROUP BY event.chain_id,event.contract_address,event.task_chain_id
HAVING sum(CASE WHEN (event.payload->>'eligible')::boolean
                THEN (event.payload->>'amount')::numeric
                ELSE -(event.payload->>'amount')::numeric END) >= 0;

CREATE VIEW chain_task_settlement_positions AS
SELECT DISTINCT ON (event.chain_id,event.contract_address,event.task_chain_id)
       event.chain_id,event.contract_address,event.task_chain_id,
       CASE event.event_type
         WHEN 'funds_released' THEN '3'
         WHEN 'funds_refunded' THEN '4'
         WHEN 'dispute_opened' THEN '5'
         WHEN 'dispute_resolved' THEN
           CASE WHEN event.payload->>'recipient' = created.payload->>'publisher' THEN '4' ELSE '3' END
       END AS contract_status,
       CASE WHEN event.event_type='dispute_opened' THEN NULL ELSE '0' END AS locked_amount,
       event.block_number,event.log_index
  FROM chain_events event
  JOIN chain_canonical_blocks canonical
    ON canonical.chain_id=event.chain_id
   AND canonical.contract_address=event.contract_address
   AND canonical.block_hash=event.block_hash
  LEFT JOIN chain_events created
    ON created.chain_id=event.chain_id
   AND created.contract_address=event.contract_address
   AND created.task_chain_id=event.task_chain_id
   AND created.event_type='task_created'
   AND EXISTS (SELECT 1 FROM chain_canonical_blocks block
                WHERE block.chain_id=created.chain_id
                  AND block.contract_address=created.contract_address
                  AND block.block_hash=created.block_hash)
 WHERE event.event_type IN ('funds_released','funds_refunded','dispute_opened','dispute_resolved')
 ORDER BY event.chain_id,event.contract_address,event.task_chain_id,event.block_number DESC,event.log_index DESC;

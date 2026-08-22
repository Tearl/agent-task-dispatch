DROP VIEW IF EXISTS chain_task_settlement_positions;
DROP VIEW IF EXISTS chain_yield_positions;
DROP VIEW IF EXISTS chain_agent_earnings_positions;

-- Event and journal constraints remain additive so rollback never invalidates or
-- deletes already-confirmed settlement history. A later forward migration may
-- rebuild these reconstructable canonical views.

-- Attempts, leases, stable failure codes and dead-letter timestamps are
-- operational audit evidence. Rollback intentionally preserves all rows and
-- columns; disable the worker and use a forward migration for schema changes.
SELECT 1;

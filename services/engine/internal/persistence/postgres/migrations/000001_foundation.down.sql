-- This foundation contains immutable system-of-record history. It has no
-- automated down migration: rollback disables writers and reverts the binary
-- while retaining the additive schema and every historical record.
SELECT 1;

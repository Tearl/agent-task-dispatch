-- Additive authentication records are retained on rollback. Disable the auth
-- routes and revert the binary; destructive history removal is intentionally omitted.
SELECT 1;

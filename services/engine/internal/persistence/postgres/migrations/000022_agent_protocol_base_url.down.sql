-- Restore the legacy health-endpoint representation for binaries that probe
-- the persisted URL directly. A new health check is required after rollback.
UPDATE agents
SET endpoint_url = rtrim(endpoint_url, '/') || '/health',
    health = 'unknown',
    health_checked_at = NULL,
    health_valid_until = NULL,
    aggregate_version = aggregate_version + 1,
    updated_at = clock_timestamp()
WHERE endpoint_url <> ''
  AND endpoint_url !~ '/health/?$';

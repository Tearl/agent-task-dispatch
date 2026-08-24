-- Agent endpoints are protocol base URLs. Health probing appends /health and
-- execution dispatch appends /v1/executions*, so persisted values must not
-- retain the legacy health-check path.
UPDATE agents
SET endpoint_url = regexp_replace(endpoint_url, '/health/?$', ''),
    health = 'unknown',
    health_checked_at = NULL,
    health_valid_until = NULL,
    aggregate_version = aggregate_version + 1,
    updated_at = clock_timestamp()
WHERE endpoint_url ~ '/health/?$';

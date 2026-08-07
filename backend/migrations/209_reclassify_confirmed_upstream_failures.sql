-- Correct confirmed provider failures that classification v2 attributed to
-- the client. Raw status codes and error messages remain untouched.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'provider_failed',
    responsibility = 'provider',
    error_category = CASE
        WHEN upstream_status_code = 429 THEN 'provider_rate_limit'
        WHEN upstream_status_code = 529 THEN 'provider_overloaded'
        ELSE 'provider_server'
    END,
    counts_toward_sla = TRUE,
    alert_family = 'provider_health',
    classification_reason = CASE
        WHEN upstream_status_code = 429 THEN 'upstream_capacity_or_rate_limit'
        WHEN upstream_status_code = 529 THEN 'upstream_overloaded'
        WHEN upstream_status_code >= 500 THEN 'upstream_server_error'
        ELSE 'upstream_provider_rejected_request'
    END,
    classification_version = 3
WHERE classification_version = 2
  AND error_phase = 'upstream'
  AND responsibility = 'client'
  AND COALESCE(status_code, 0) >= 400
  AND (
      upstream_status_code IN (402, 404, 429)
      OR upstream_status_code >= 500
  );

-- A provider 400/422 rewritten by the gateway to 5xx is a compatibility
-- failure in the platform, not a client rejection.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = 'product_compatibility',
    counts_toward_sla = FALSE,
    alert_family = 'compatibility',
    classification_reason = 'upstream_request_incompatibility_exposed_as_gateway_failure',
    classification_version = 3
WHERE classification_version = 2
  AND error_phase = 'upstream'
  AND responsibility = 'client'
  AND status_code >= 500
  AND upstream_status_code IN (400, 422)
  AND (
      error_type = 'invalid_request_error'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid_request_error%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid request%'
      OR COALESCE(error_message, '') ILIKE '%invalid_request_error%'
      OR COALESCE(error_message, '') ILIKE '%invalid request%'
  );

-- If the final request succeeded, a failed upstream attempt cannot be a
-- client semantic error. Keep it excluded from SLA while preserving provider
-- health or compatibility visibility.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    responsibility = CASE
        WHEN upstream_status_code IN (401, 403) THEN 'platform'
        WHEN upstream_status_code IN (400, 422)
             AND (
                 error_type = 'invalid_request_error'
                 OR COALESCE(upstream_error_message, '') ILIKE '%invalid_request_error%'
                 OR COALESCE(upstream_error_message, '') ILIKE '%invalid request%'
                 OR COALESCE(error_message, '') ILIKE '%invalid_request_error%'
                 OR COALESCE(error_message, '') ILIKE '%invalid request%'
             ) THEN 'platform'
        ELSE 'provider'
    END,
    alert_family = CASE
        WHEN upstream_status_code IN (400, 422)
             AND (
                 error_type = 'invalid_request_error'
                 OR COALESCE(upstream_error_message, '') ILIKE '%invalid_request_error%'
                 OR COALESCE(upstream_error_message, '') ILIKE '%invalid request%'
                 OR COALESCE(error_message, '') ILIKE '%invalid_request_error%'
                 OR COALESCE(error_message, '') ILIKE '%invalid request%'
                 OR COALESCE(error_message, '') ILIKE '%model%not supported%'
                 OR COALESCE(upstream_error_message, '') ILIKE '%model%not supported%'
             ) THEN 'compatibility'
        ELSE 'provider_health'
    END,
    classification_version = 3
WHERE classification_version = 2
  AND error_phase = 'upstream'
  AND responsibility = 'client'
  AND COALESCE(status_code, 0) < 400
  AND upstream_status_code >= 400;

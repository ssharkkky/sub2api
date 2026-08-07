-- Correct the historical false client attribution for confirmed provider
-- workspace failures. The raw status/message columns remain untouched.
-- Only rows written by classification v2 carrying a confirmed provider 402,
-- or a gateway 499 backed by an upstream 429, are updated. Ambiguous rows stay
-- unchanged.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = CASE WHEN COALESCE(status_code, 0) < 400 THEN 'recovered' ELSE 'provider_failed' END,
    responsibility = 'provider',
    error_category = CASE
        WHEN COALESCE(status_code, 0) < 400 THEN 'recovered'
        WHEN upstream_status_code = 429 THEN 'provider_rate_limit'
        ELSE 'provider_server'
    END,
    counts_toward_sla = CASE WHEN COALESCE(status_code, 0) < 400 THEN FALSE ELSE TRUE END,
    alert_family = 'provider_health',
    classification_reason = CASE
        WHEN COALESCE(status_code, 0) < 400 THEN 'final_request_recovered'
        WHEN upstream_status_code = 429 THEN 'upstream_capacity_or_rate_limit'
        ELSE 'upstream_provider_rejected_request'
    END,
    classification_version = 3
WHERE classification_version = 2
  AND error_phase = 'upstream'
  AND responsibility = 'client'
  AND (
      upstream_status_code = 402
      OR (upstream_status_code = 429 AND status_code = 499)
  );

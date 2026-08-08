-- Correct the historical false client attribution for confirmed provider
-- workspace failures. The raw status/message columns remain untouched.
-- Only rows written by classification v2 and carrying the provider-specific
-- 402 status are updated; ambiguous upstream 4xx rows stay unchanged.
UPDATE ops_error_logs
SET
    final_outcome = CASE WHEN COALESCE(status_code, 0) < 400 THEN 'recovered' ELSE 'provider_failed' END,
    responsibility = 'provider',
    error_category = CASE WHEN COALESCE(status_code, 0) < 400 THEN 'recovered' ELSE 'provider_server' END,
    counts_toward_sla = CASE WHEN COALESCE(status_code, 0) < 400 THEN FALSE ELSE TRUE END,
    alert_family = 'provider_health',
    classification_reason = CASE WHEN COALESCE(status_code, 0) < 400 THEN 'final_request_recovered' ELSE 'upstream_provider_rejected_request' END,
    classification_version = 3
WHERE classification_version = 2
  AND error_phase = 'upstream'
  AND upstream_status_code = 402
  AND responsibility = 'client';

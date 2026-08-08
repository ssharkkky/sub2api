-- Correct confirmed provider failures that classification v2 attributed to
-- the client. Raw status codes and error messages remain untouched.
-- sub2api-managed-update: reviewed-compatible
-- A recovered upstream security rejection stays recovered, but must expose the
-- provider/security signal instead of being mistaken for a local block.
UPDATE ops_error_logs
SET
    responsibility = 'provider',
    counts_toward_sla = FALSE,
    alert_family = 'security',
    classification_reason = 'final_request_recovered',
    classification_version = 3
WHERE COALESCE(classification_version, 0) < 3
  AND final_outcome = 'recovered'
  AND (
      COALESCE(upstream_status_code, 0) > 0
      OR error_phase IN ('upstream', 'account_auth')
      OR error_source = 'upstream_http'
      OR error_type = 'upstream_error'
  )
  AND (
      error_type LIKE 'cyber_policy%'
      OR COALESCE(error_message, '') ILIKE '%content policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%content policy%'
      OR COALESCE(error_message, '') ILIKE '%security policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%security policy%'
      OR COALESCE(error_message, '') ILIKE '%cyber policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cyber policy%'
      OR COALESCE(error_message, '') ILIKE '%cybersecurity risk%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cybersecurity risk%'
  );

-- Preserve failed upstream security rejections before generic legacy rules see
-- the reused cyber_policy error type as a local client block.
UPDATE ops_error_logs
SET
    final_outcome = 'security_blocked',
    responsibility = 'provider',
    error_category = 'security_policy',
    counts_toward_sla = FALSE,
    alert_family = 'security',
    classification_reason = 'upstream_security_policy_rejection',
    classification_version = 3
WHERE COALESCE(classification_version, 0) < 3
  AND COALESCE(final_outcome, '') <> 'recovered'
  AND (
      COALESCE(upstream_status_code, 0) > 0
      OR error_phase IN ('upstream', 'account_auth')
      OR error_source = 'upstream_http'
      OR error_type = 'upstream_error'
  )
  AND (
      error_type LIKE 'cyber_policy%'
      OR COALESCE(error_message, '') ILIKE '%content policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%content policy%'
      OR COALESCE(error_message, '') ILIKE '%security policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%security policy%'
      OR COALESCE(error_message, '') ILIKE '%cyber policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cyber policy%'
      OR COALESCE(error_message, '') ILIKE '%cybersecurity risk%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cybersecurity risk%'
  );

-- Final managed upstream 401/403 responses need their own branches. The old
-- v2 rule only covered client-attributed records, leaving already-final logs
-- out of the capacity, credential and capability views.
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = CASE
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
          OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'platform_capacity'
        WHEN COALESCE(error_message, '') ILIKE '%not enabled for this group%'
          OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
            THEN 'product_compatibility'
        ELSE 'platform_credential'
    END,
    counts_toward_sla = CASE
        WHEN COALESCE(error_message, '') ILIKE '%not enabled for this group%'
          OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
            THEN FALSE
        ELSE TRUE
    END,
    alert_family = CASE
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
          OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'capacity'
        WHEN COALESCE(error_message, '') ILIKE '%not enabled for this group%'
          OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
            THEN 'compatibility'
        ELSE 'credential'
    END,
    classification_reason = CASE
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
          OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'managed_upstream_capacity_rejected'
        WHEN COALESCE(error_message, '') ILIKE '%not enabled for this group%'
          OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
            THEN 'managed_upstream_capability_not_enabled'
        ELSE 'managed_upstream_credential_rejected'
    END,
    classification_version = 3
WHERE COALESCE(classification_version, 0) < 3
  AND COALESCE(status_code, 0) >= 400
  AND COALESCE(final_outcome, '') <> 'recovered'
  AND (
      COALESCE(upstream_status_code, 0) > 0
      OR error_phase IN ('upstream', 'account_auth')
      OR error_source = 'upstream_http'
      OR error_type = 'upstream_error'
  )
  AND (
      error_phase = 'account_auth'
      OR upstream_status_code = 401
      OR (
          upstream_status_code = 403
          AND (
              COALESCE(error_message, '') ILIKE '%access token%'
              OR COALESCE(upstream_error_message, '') ILIKE '%access token%'
              OR COALESCE(error_message, '') ILIKE '%credential%'
              OR COALESCE(upstream_error_message, '') ILIKE '%credential%'
              OR COALESCE(error_message, '') ILIKE '%authentication failed%'
              OR COALESCE(upstream_error_message, '') ILIKE '%authentication failed%'
              OR COALESCE(error_message, '') ILIKE '%认证失败%'
              OR COALESCE(upstream_error_message, '') ILIKE '%认证失败%'
              OR COALESCE(error_message, '') ILIKE '%请重新登录%'
              OR COALESCE(upstream_error_message, '') ILIKE '%请重新登录%'
              OR COALESCE(error_message, '') ILIKE '%检查 api key%'
              OR COALESCE(upstream_error_message, '') ILIKE '%检查 api key%'
              OR COALESCE(error_message, '') ILIKE '%insufficient%balance%'
              OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
              OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
              OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
              OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
              OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
              OR COALESCE(error_message, '') ILIKE '%not enabled for this group%'
              OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
          )
      )
  );

-- Semantic request failures must be handled before the broad provider update;
-- otherwise the broad rule would permanently turn context/schema errors into
-- provider SLA failures and the later compatibility correction could not match.
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = 'product_compatibility',
    counts_toward_sla = FALSE,
    alert_family = 'compatibility',
    classification_reason = 'upstream_request_incompatibility_exposed_as_gateway_failure',
    classification_version = 3
WHERE COALESCE(classification_version, 0) < 3
  AND error_phase = 'upstream'
  AND COALESCE(final_outcome, '') <> 'recovered'
  AND COALESCE(responsibility, '') IN ('', 'client', 'provider')
  AND COALESCE(status_code, 0) >= 500
  AND COALESCE(upstream_status_code, 0) >= 500
  AND (
      COALESCE(error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(upstream_error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(error_message, '') ILIKE '%context window%'
      OR COALESCE(upstream_error_message, '') ILIKE '%context window%'
      OR COALESCE(error_message, '') ILIKE '%input too long%'
      OR COALESCE(upstream_error_message, '') ILIKE '%input too long%'
      OR COALESCE(error_message, '') ILIKE '%invalid input%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid input%'
      OR COALESCE(error_message, '') ILIKE '%invalid ''%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid ''%'
  );

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
  )
  AND NOT (
      COALESCE(error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(upstream_error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(error_message, '') ILIKE '%context window%'
      OR COALESCE(upstream_error_message, '') ILIKE '%context window%'
      OR COALESCE(error_message, '') ILIKE '%input too long%'
      OR COALESCE(upstream_error_message, '') ILIKE '%input too long%'
      OR COALESCE(error_message, '') ILIKE '%invalid input%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid input%'
      OR COALESCE(error_message, '') ILIKE '%invalid ''%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid ''%'
	  OR COALESCE(error_message, '') ILIKE '%content policy%'
	  OR COALESCE(upstream_error_message, '') ILIKE '%content policy%'
	  OR COALESCE(error_message, '') ILIKE '%security policy%'
	  OR COALESCE(upstream_error_message, '') ILIKE '%security policy%'
	  OR COALESCE(error_message, '') ILIKE '%cyber policy%'
	  OR COALESCE(upstream_error_message, '') ILIKE '%cyber policy%'
	  OR COALESCE(error_message, '') ILIKE '%cybersecurity risk%'
	  OR COALESCE(upstream_error_message, '') ILIKE '%cybersecurity risk%'
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
		WHEN upstream_status_code = 401
		     OR COALESCE(error_message, '') ILIKE '%access token%'
		     OR COALESCE(upstream_error_message, '') ILIKE '%access token%'
		     OR COALESCE(error_message, '') ILIKE '%credential%'
		     OR COALESCE(upstream_error_message, '') ILIKE '%credential%'
		     OR error_phase = 'account_auth' THEN 'platform'
		WHEN upstream_status_code = 403
		     AND (
		         COALESCE(error_message, '') ILIKE '%insufficient%balance%'
		         OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
		         OR COALESCE(error_message, '') ILIKE '%not enabled for this group%'
		         OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
		     ) THEN 'platform'
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
		WHEN upstream_status_code = 403
		     AND (
		         COALESCE(error_message, '') ILIKE '%insufficient%balance%'
		         OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
		     ) THEN 'capacity'
		WHEN upstream_status_code = 403
		     AND (
		         COALESCE(error_message, '') ILIKE '%not enabled for this group%'
		         OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
		     ) THEN 'compatibility'
		WHEN upstream_status_code = 401
		     OR COALESCE(error_message, '') ILIKE '%access token%'
		     OR COALESCE(upstream_error_message, '') ILIKE '%access token%'
		     OR COALESCE(error_message, '') ILIKE '%credential%'
		     OR COALESCE(upstream_error_message, '') ILIKE '%credential%'
		     OR error_phase = 'account_auth' THEN 'credential'
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

-- Classification v2 treated every HTTP 2xx log as recovered. Stream failures
-- can commit 200 before an error event, so only the dedicated recovery logger's
-- explicit message is trusted as historical recovery evidence.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = 'network_transport',
    counts_toward_sla = TRUE,
    alert_family = 'provider_health',
    classification_reason = 'upstream_transport_or_unclassified_failure',
    classification_version = 3
WHERE classification_version = 2
  AND final_outcome = 'recovered'
  AND COALESCE(status_code, 0) > 0
  AND COALESCE(status_code, 0) < 400
  AND error_type = 'upstream_error'
  AND upstream_status_code IS NULL
  AND COALESCE(error_message, '') NOT ILIKE 'Recovered upstream error%'
  AND COALESCE(error_message, '') NOT ILIKE 'Recovered account authentication failure%';

-- Legacy callers marked some routing failures as business-limited. A gateway
-- 5xx is still a platform availability failure regardless of that broad flag.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = 'platform_internal',
    counts_toward_sla = TRUE,
    alert_family = 'availability',
    classification_reason = 'platform_internal_or_routing_failure',
    classification_version = 3
WHERE classification_version = 2
  AND error_phase IN ('routing', 'internal')
  AND error_owner = 'platform'
  AND COALESCE(status_code, 0) >= 500
  AND final_outcome = 'client_rejected'
  AND responsibility = 'client';

-- Some older logger paths retained phase=request/source=client_request even
-- though error_type explicitly said upstream_error. Preserve that evidence.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = 'network_transport',
    counts_toward_sla = TRUE,
    alert_family = 'provider_health',
    classification_reason = 'upstream_transport_or_unclassified_failure',
    classification_version = 3
WHERE classification_version = 2
  AND error_type = 'upstream_error'
  AND COALESCE(status_code, 0) >= 500
  AND responsibility = 'client'
  AND (
      error_phase = 'request'
      OR error_source = 'client_request'
  )
  AND NOT (
      error_message ILIKE '%upstream%403%insufficient%balance%'
      OR error_message ILIKE '%upstream%401%insufficient%balance%'
  );

-- Some Codex manifest failures did not persist upstream_status_code, but the
-- sanitized message still contains an explicit upstream 401/403 balance error.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'platform_failed',
    responsibility = 'platform',
    error_category = 'platform_capacity',
    counts_toward_sla = TRUE,
    alert_family = 'capacity',
    classification_reason = 'managed_upstream_capacity_rejected',
    classification_version = 3
WHERE classification_version = 2
  AND error_type = 'upstream_error'
  AND COALESCE(status_code, 0) >= 500
  AND responsibility = 'client'
  AND (
      error_message ILIKE '%upstream%403%insufficient%balance%'
      OR error_message ILIKE '%upstream%401%insufficient%balance%'
  );

-- Upstream safety-policy rejections must keep provider ownership. Do not let
-- message text alone turn an upstream rejection into a local client block.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'security_blocked',
    responsibility = 'provider',
    error_category = 'security_policy',
    counts_toward_sla = FALSE,
    alert_family = 'security',
    classification_reason = 'upstream_security_policy_rejection',
    classification_version = 3
WHERE COALESCE(classification_version, 0) < 3
  AND error_phase = 'upstream'
  AND COALESCE(final_outcome, '') <> 'recovered'
  AND (
      COALESCE(error_message, '') ILIKE '%content policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%content policy%'
      OR COALESCE(error_message, '') ILIKE '%security policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%security policy%'
      OR COALESCE(error_message, '') ILIKE '%cyber policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cyber policy%'
  );

-- Session-level cyber policy blocks use a more specific error type than the
-- original classifier recognized, but remain client security rejections.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    final_outcome = 'security_blocked',
    responsibility = 'client',
    error_category = 'security_policy',
    counts_toward_sla = FALSE,
    alert_family = 'security',
    classification_reason = 'security_policy_rejection',
    classification_version = 3
WHERE classification_version = 2
  AND error_type LIKE 'cyber_policy%'
  AND final_outcome IN ('client_rejected', 'recovered')
  AND COALESCE(upstream_status_code, 0) = 0
  AND error_phase NOT IN ('upstream', 'account_auth')
  AND COALESCE(error_source, '') <> 'upstream_http'
  AND COALESCE(error_type, '') <> 'upstream_error';

-- A recovered 401/403 came from a rejected managed upstream credential. The
-- request recovered, but the operational signal belongs to managed credentials.
-- sub2api-managed-update: reviewed-compatible
UPDATE ops_error_logs
SET
    responsibility = CASE
        WHEN error_phase = 'account_auth'
             OR upstream_status_code = 401
             OR COALESCE(error_message, '') ILIKE '%access token%'
             OR COALESCE(upstream_error_message, '') ILIKE '%access token%'
             OR COALESCE(error_message, '') ILIKE '%credential%'
             OR COALESCE(upstream_error_message, '') ILIKE '%credential%'
             OR COALESCE(error_message, '') ILIKE '%认证失败%'
             OR COALESCE(upstream_error_message, '') ILIKE '%认证失败%'
             OR COALESCE(error_message, '') ILIKE '%请重新登录%'
             OR COALESCE(upstream_error_message, '') ILIKE '%请重新登录%'
             OR COALESCE(error_message, '') ILIKE '%检查 api key%'
             OR COALESCE(upstream_error_message, '') ILIKE '%检查 api key%'
            THEN 'platform'
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
             OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
             OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
             OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
             OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
             OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'platform'
        WHEN COALESCE(error_message, '') ILIKE '%not enabled for this group%'
             OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
            THEN 'platform'
        ELSE 'provider'
    END,
    alert_family = CASE
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
             OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
             OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
             OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
             OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
             OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'capacity'
        WHEN COALESCE(error_message, '') ILIKE '%not enabled for this group%'
             OR COALESCE(upstream_error_message, '') ILIKE '%not enabled for this group%'
            THEN 'compatibility'
        WHEN error_phase = 'account_auth'
             OR upstream_status_code = 401
             OR COALESCE(error_message, '') ILIKE '%access token%'
             OR COALESCE(upstream_error_message, '') ILIKE '%access token%'
             OR COALESCE(error_message, '') ILIKE '%credential%'
             OR COALESCE(upstream_error_message, '') ILIKE '%credential%'
             OR COALESCE(error_message, '') ILIKE '%认证失败%'
             OR COALESCE(upstream_error_message, '') ILIKE '%认证失败%'
             OR COALESCE(error_message, '') ILIKE '%请重新登录%'
             OR COALESCE(upstream_error_message, '') ILIKE '%请重新登录%'
             OR COALESCE(error_message, '') ILIKE '%检查 api key%'
             OR COALESCE(upstream_error_message, '') ILIKE '%检查 api key%'
            THEN 'credential'
        ELSE 'provider_health'
    END,
    classification_version = 3
WHERE classification_version = 2
  AND final_outcome = 'recovered'
  AND (
      error_phase = 'account_auth'
      OR upstream_status_code IN (401, 403)
  );

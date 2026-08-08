-- Keep writes from the draining v2 image aligned after the one-time backfill.
-- The trigger only touches rows whose writer explicitly uses an older
-- classification version; v3 writers return immediately.
-- sub2api-managed-update: reviewed-compatible
CREATE OR REPLACE FUNCTION public.normalize_ops_error_log_v3_mixed_writer()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    message_text TEXT := LOWER(
        COALESCE(NEW.error_message, '') || ' ' ||
        COALESCE(NEW.upstream_error_message, '')
    );
    upstream_evidence BOOLEAN :=
        COALESCE(NEW.upstream_status_code, 0) > 0
        OR NEW.error_phase IN ('upstream', 'account_auth')
        OR NEW.error_source = 'upstream_http'
        OR NEW.error_type = 'upstream_error';
    security_message BOOLEAN;
    capacity_message BOOLEAN;
    capability_message BOOLEAN;
    credential_message BOOLEAN;
    request_semantics BOOLEAN;
    recovered_compatibility BOOLEAN;
    effective_upstream_status INTEGER := COALESCE(NEW.upstream_status_code, 0);
BEGIN
    IF COALESCE(NEW.classification_version, 0) >= 3 THEN
        RETURN NEW;
    END IF;

    security_message :=
        message_text LIKE '%content policy%'
        OR message_text LIKE '%security policy%'
        OR message_text LIKE '%cyber policy%'
        OR message_text LIKE '%cyber-security policy%'
        OR message_text LIKE '%cybersecurity risk%';
    capacity_message :=
        message_text LIKE '%insufficient balance%'
        OR message_text LIKE '%insufficient_account_balance%'
        OR message_text LIKE '%insufficient_balance%'
        OR message_text LIKE '%预扣费额度失败%'
        OR message_text LIKE '%用户剩余额度%';
    capability_message :=
        message_text LIKE '%not enabled for this group%'
        OR message_text LIKE '%disabled for this group%'
        OR message_text LIKE '%capability is not enabled%'
        OR message_text LIKE '%permission is not enabled%';
    credential_message :=
        message_text LIKE '%access token%'
        OR message_text LIKE '%oauth token%'
        OR message_text LIKE '%credential%'
        OR message_text LIKE '%token has been revoked%'
        OR message_text LIKE '%token expired%'
        OR message_text LIKE '%invalid token%'
        OR message_text LIKE '%authentication failed%'
        OR message_text LIKE '%认证失败%'
        OR message_text LIKE '%请重新登录%'
        OR message_text LIKE '%检查 api key%';
    request_semantics :=
        (
            NEW.error_type = 'invalid_request_error'
            AND NEW.upstream_status_code IN (400, 422)
        )
        OR message_text LIKE '%context_length_exceeded%'
        OR message_text LIKE '%exceeds the context window%'
        OR message_text LIKE '%maximum context length%'
        OR message_text LIKE '%invalid ''%'
        OR message_text LIKE '%invalid request%'
        OR message_text LIKE '%invalid_request_error%'
        OR message_text LIKE '%invalid parameter%'
        OR message_text LIKE '%unknown parameter%'
        OR message_text LIKE '%unsupported parameter%'
        OR message_text LIKE '%malformed request%';
    recovered_compatibility :=
        request_semantics
        OR message_text LIKE '%invalid%'
        OR (
            message_text LIKE '%model%'
            AND (
                message_text LIKE '%not supported%'
                OR message_text LIKE '%not in whitelist%'
                OR message_text LIKE '%not configured%'
            )
        );

    IF NOT upstream_evidence AND NEW.error_type LIKE 'cyber_policy%' THEN
        NEW.final_outcome := 'security_blocked';
        NEW.responsibility := 'client';
        NEW.error_category := 'security_policy';
        NEW.counts_toward_sla := FALSE;
        NEW.alert_family := 'security';
        NEW.classification_reason := 'security_policy_rejection';
        NEW.classification_version := 3;
        RETURN NEW;
    END IF;

    IF NEW.final_outcome = 'recovered'
       AND NOT (
           COALESCE(NEW.status_code, 0) > 0
           AND COALESCE(NEW.status_code, 0) < 400
           AND NEW.error_type = 'upstream_error'
           AND NEW.upstream_status_code IS NULL
           AND COALESCE(NEW.error_message, '') NOT ILIKE 'Recovered upstream error%'
           AND COALESCE(NEW.error_message, '') NOT ILIKE 'Recovered account authentication failure%'
       ) THEN
        NEW.error_category := 'recovered';
        NEW.counts_toward_sla := FALSE;
        NEW.classification_reason := 'final_request_recovered';
        IF message_text LIKE '%context canceled%'
           OR message_text LIKE '%client disconnected%' THEN
            NEW.responsibility := 'client';
            NEW.alert_family := 'client_quality';
        ELSIF security_message AND upstream_evidence THEN
            NEW.responsibility := 'provider';
            NEW.alert_family := 'security';
        ELSIF capacity_message THEN
            NEW.responsibility := 'platform';
            NEW.alert_family := 'capacity';
        ELSIF capability_message THEN
            NEW.responsibility := 'platform';
            NEW.alert_family := 'compatibility';
        ELSIF NEW.error_phase = 'account_auth'
              OR NEW.upstream_status_code = 401
              OR (NEW.upstream_status_code = 403 AND credential_message) THEN
            NEW.responsibility := 'platform';
            NEW.alert_family := 'credential';
        ELSIF recovered_compatibility THEN
            NEW.responsibility := 'platform';
            NEW.alert_family := 'compatibility';
        ELSIF COALESCE(NEW.upstream_status_code, 0) >= 400
              OR NEW.error_phase IN ('upstream', 'account_auth') THEN
            NEW.responsibility := 'provider';
            NEW.alert_family := 'provider_health';
        ELSE
            NEW.responsibility := COALESCE(NULLIF(NEW.responsibility, ''), 'unknown');
            NEW.alert_family := 'none';
        END IF;
        NEW.classification_version := 3;
        RETURN NEW;
    END IF;

    IF COALESCE(NEW.upstream_status_code, 0) = 0
       AND (
           NEW.status_code IN (408, 499)
           OR message_text LIKE '%context canceled%'
           OR message_text LIKE '%client disconnected%'
           OR message_text LIKE '%client closed%'
           OR message_text LIKE '%request canceled%'
           OR message_text LIKE '%broken pipe%'
       ) THEN
        NEW.final_outcome := 'cancelled';
        NEW.responsibility := 'client';
        NEW.error_category := 'client_cancelled';
        NEW.counts_toward_sla := FALSE;
        NEW.alert_family := 'client_quality';
        NEW.classification_reason := 'client_cancel_or_disconnect';
        NEW.classification_version := 3;
        RETURN NEW;
    END IF;

    IF NOT upstream_evidence AND security_message THEN
        NEW.final_outcome := 'security_blocked';
        NEW.responsibility := 'client';
        NEW.error_category := 'security_policy';
        NEW.counts_toward_sla := FALSE;
        NEW.alert_family := 'security';
        NEW.classification_reason := 'security_policy_rejection';
        NEW.classification_version := 3;
        RETURN NEW;
    END IF;

    IF upstream_evidence THEN
        IF security_message THEN
            NEW.final_outcome := 'security_blocked';
            NEW.responsibility := 'provider';
            NEW.error_category := 'security_policy';
            NEW.counts_toward_sla := FALSE;
            NEW.alert_family := 'security';
            NEW.classification_reason := 'upstream_security_policy_rejection';
        ELSIF capacity_message THEN
            NEW.final_outcome := 'platform_failed';
            NEW.responsibility := 'platform';
            NEW.error_category := 'platform_capacity';
            NEW.counts_toward_sla := TRUE;
            NEW.alert_family := 'capacity';
            NEW.classification_reason := 'managed_upstream_capacity_rejected';
        ELSIF capability_message THEN
            NEW.final_outcome := 'platform_failed';
            NEW.responsibility := 'platform';
            NEW.error_category := 'product_compatibility';
            NEW.counts_toward_sla := FALSE;
            NEW.alert_family := 'compatibility';
            NEW.classification_reason := 'managed_upstream_capability_not_enabled';
        ELSIF NEW.error_phase = 'account_auth'
              OR NEW.upstream_status_code = 401
              OR (NEW.upstream_status_code = 403 AND credential_message) THEN
            NEW.final_outcome := 'platform_failed';
            NEW.responsibility := 'platform';
            NEW.error_category := 'platform_credential';
            NEW.counts_toward_sla := TRUE;
            NEW.alert_family := 'credential';
            NEW.classification_reason := 'managed_upstream_credential_rejected';
        ELSIF request_semantics THEN
            NEW.final_outcome := CASE
                WHEN COALESCE(NEW.status_code, 0) >= 500 OR COALESCE(NEW.status_code, 0) = 0
                    THEN 'platform_failed'
                ELSE 'client_rejected'
            END;
            NEW.responsibility := CASE
                WHEN COALESCE(NEW.status_code, 0) >= 500 OR COALESCE(NEW.status_code, 0) = 0
                    THEN 'platform'
                ELSE 'client'
            END;
            NEW.error_category := CASE
                WHEN COALESCE(NEW.status_code, 0) >= 500 OR COALESCE(NEW.status_code, 0) = 0
                    THEN 'product_compatibility'
                ELSE 'invalid_request'
            END;
            NEW.counts_toward_sla := FALSE;
            NEW.alert_family := CASE
                WHEN COALESCE(NEW.status_code, 0) >= 500 OR COALESCE(NEW.status_code, 0) = 0
                    THEN 'compatibility'
                ELSE 'client_quality'
            END;
            NEW.classification_reason := CASE
                WHEN COALESCE(NEW.status_code, 0) >= 500 OR COALESCE(NEW.status_code, 0) = 0
                    THEN 'upstream_request_incompatibility_exposed_as_gateway_failure'
                ELSE 'upstream_rejected_request_semantics'
            END;
        ELSE
            IF effective_upstream_status = 0
               AND ((NEW.status_code >= 400 AND NEW.status_code < 500) OR NEW.status_code = 529) THEN
                effective_upstream_status := NEW.status_code;
            END IF;
            IF effective_upstream_status >= 400 AND effective_upstream_status < 500
               AND effective_upstream_status <> 429 THEN
                NEW.final_outcome := 'provider_failed';
                NEW.responsibility := 'provider';
                NEW.error_category := 'provider_server';
                NEW.counts_toward_sla := TRUE;
                NEW.alert_family := 'provider_health';
                NEW.classification_reason := 'upstream_provider_rejected_request';
            ELSIF effective_upstream_status = 429 THEN
                NEW.final_outcome := 'provider_failed';
                NEW.responsibility := 'provider';
                NEW.error_category := 'provider_rate_limit';
                NEW.counts_toward_sla := TRUE;
                NEW.alert_family := 'provider_health';
                NEW.classification_reason := 'upstream_capacity_or_rate_limit';
            ELSIF effective_upstream_status = 529
                  OR message_text LIKE '%overloaded%'
                  OR NEW.error_type = 'overloaded_error' THEN
                NEW.final_outcome := 'provider_failed';
                NEW.responsibility := 'provider';
                NEW.error_category := 'provider_overloaded';
                NEW.counts_toward_sla := TRUE;
                NEW.alert_family := 'provider_health';
                NEW.classification_reason := 'upstream_overloaded';
            ELSIF effective_upstream_status >= 500 THEN
                NEW.final_outcome := 'provider_failed';
                NEW.responsibility := 'provider';
                NEW.error_category := 'provider_server';
                NEW.counts_toward_sla := TRUE;
                NEW.alert_family := 'provider_health';
                NEW.classification_reason := 'upstream_server_error';
            ELSE
                NEW.final_outcome := 'platform_failed';
                NEW.responsibility := 'platform';
                NEW.error_category := 'network_transport';
                NEW.counts_toward_sla := TRUE;
                NEW.alert_family := 'provider_health';
                NEW.classification_reason := 'upstream_transport_or_unclassified_failure';
            END IF;
        END IF;
        NEW.classification_version := 3;
        RETURN NEW;
    END IF;

    IF COALESCE(NEW.status_code, 0) >= 500
       AND NEW.error_phase IN ('routing', 'internal') THEN
        NEW.final_outcome := 'platform_failed';
        NEW.responsibility := 'platform';
        NEW.error_category := 'platform_internal';
        NEW.counts_toward_sla := TRUE;
        NEW.alert_family := 'availability';
        NEW.classification_reason := 'platform_internal_or_routing_failure';
        NEW.classification_version := 3;
    END IF;
    RETURN NEW;
END;
$$;

-- sub2api-managed-update: reviewed-compatible
CREATE TRIGGER ops_error_logs_normalize_v3_mixed_writer
BEFORE INSERT ON ops_error_logs
FOR EACH ROW
WHEN (COALESCE(NEW.classification_version, 0) < 3)
EXECUTE FUNCTION public.normalize_ops_error_log_v3_mixed_writer();

-- Correct confirmed provider failures that classification v2 attributed to
-- the client. Raw status codes and error messages remain untouched.
-- sub2api-managed-update: reviewed-compatible
-- A recovered upstream security rejection stays recovered, but must expose the
-- provider/security signal instead of being mistaken for a local block.
UPDATE ops_error_logs
SET
    responsibility = 'provider',
    error_category = 'recovered',
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
      OR COALESCE(error_message, '') ILIKE '%cyber-security policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cyber-security policy%'
      OR COALESCE(error_message, '') ILIKE '%cybersecurity risk%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cybersecurity risk%'
  );

-- Preserve failed upstream security rejections before generic legacy rules see
-- the reused cyber_policy error type as a local client block.
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
      OR COALESCE(error_message, '') ILIKE '%cyber-security policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cyber-security policy%'
      OR COALESCE(error_message, '') ILIKE '%cybersecurity risk%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cybersecurity risk%'
  );

-- Final managed upstream 401/403 responses need their own branches. The old
-- v2 rule only covered client-attributed records, leaving already-final logs
-- out of the capacity, credential and capability views.
-- sub2api-managed-update: reviewed-compatible
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
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
            THEN 'product_compatibility'
        ELSE 'platform_credential'
    END,
    counts_toward_sla = CASE
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
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
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
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
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
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
      OR COALESCE(error_message, '') ILIKE '%insufficient%balance%'
      OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
      OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
      OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
      OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
      OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
      OR COALESCE(error_message, '') ILIKE ANY (ARRAY[
             '%not enabled for this group%',
             '%disabled for this group%',
             '%capability is not enabled%',
             '%permission is not enabled%'
         ])
      OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
             '%not enabled for this group%',
             '%disabled for this group%',
             '%capability is not enabled%',
             '%permission is not enabled%'
         ])
      OR (
          upstream_status_code = 403
          AND (
              COALESCE(error_message, '') ILIKE '%access token%'
              OR COALESCE(upstream_error_message, '') ILIKE '%access token%'
              OR COALESCE(error_message, '') ILIKE '%oauth token%'
              OR COALESCE(upstream_error_message, '') ILIKE '%oauth token%'
              OR COALESCE(error_message, '') ILIKE '%credential%'
              OR COALESCE(upstream_error_message, '') ILIKE '%credential%'
              OR COALESCE(error_message, '') ILIKE '%token has been revoked%'
              OR COALESCE(upstream_error_message, '') ILIKE '%token has been revoked%'
              OR COALESCE(error_message, '') ILIKE '%token expired%'
              OR COALESCE(upstream_error_message, '') ILIKE '%token expired%'
              OR COALESCE(error_message, '') ILIKE '%invalid token%'
              OR COALESCE(upstream_error_message, '') ILIKE '%invalid token%'
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
              OR COALESCE(error_message, '') ILIKE '%disabled for this group%'
              OR COALESCE(upstream_error_message, '') ILIKE '%disabled for this group%'
              OR COALESCE(error_message, '') ILIKE '%capability is not enabled%'
              OR COALESCE(upstream_error_message, '') ILIKE '%capability is not enabled%'
              OR COALESCE(error_message, '') ILIKE '%permission is not enabled%'
              OR COALESCE(upstream_error_message, '') ILIKE '%permission is not enabled%'
          )
      )
  );

-- Semantic request failures must be handled before the broad provider update;
-- otherwise the broad rule would permanently turn context/schema errors into
-- provider SLA failures and the later compatibility correction could not match.
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
WHERE COALESCE(classification_version, 0) < 3
  AND error_phase = 'upstream'
  AND COALESCE(final_outcome, '') <> 'recovered'
  AND COALESCE(responsibility, '') IN ('', 'client', 'provider')
  AND COALESCE(status_code, 0) >= 500
  AND (
      (
          error_type = 'invalid_request_error'
          AND upstream_status_code IN (400, 422)
      )
      OR COALESCE(error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(upstream_error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(error_message, '') ILIKE '%exceeds the context window%'
      OR COALESCE(upstream_error_message, '') ILIKE '%exceeds the context window%'
      OR COALESCE(error_message, '') ILIKE '%maximum context length%'
      OR COALESCE(upstream_error_message, '') ILIKE '%maximum context length%'
      OR COALESCE(error_message, '') ILIKE '%invalid ''%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid ''%'
      OR COALESCE(error_message, '') ILIKE '%invalid request%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid request%'
      OR COALESCE(error_message, '') ILIKE '%invalid_request_error%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid_request_error%'
      OR COALESCE(error_message, '') ILIKE '%invalid parameter%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid parameter%'
      OR COALESCE(error_message, '') ILIKE '%unknown parameter%'
      OR COALESCE(upstream_error_message, '') ILIKE '%unknown parameter%'
      OR COALESCE(error_message, '') ILIKE '%unsupported parameter%'
      OR COALESCE(upstream_error_message, '') ILIKE '%unsupported parameter%'
      OR COALESCE(error_message, '') ILIKE '%malformed request%'
      OR COALESCE(upstream_error_message, '') ILIKE '%malformed request%'
  );

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
      upstream_status_code BETWEEN 400 AND 499
      OR upstream_status_code >= 500
  )
  AND NOT (
      COALESCE(error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(upstream_error_message, '') ILIKE '%context_length_exceeded%'
      OR COALESCE(error_message, '') ILIKE '%exceeds the context window%'
      OR COALESCE(upstream_error_message, '') ILIKE '%exceeds the context window%'
      OR COALESCE(error_message, '') ILIKE '%maximum context length%'
      OR COALESCE(upstream_error_message, '') ILIKE '%maximum context length%'
      OR COALESCE(error_message, '') ILIKE '%invalid ''%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid ''%'
      OR COALESCE(error_message, '') ILIKE '%invalid request%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid request%'
      OR COALESCE(error_message, '') ILIKE '%invalid_request_error%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid_request_error%'
      OR COALESCE(error_message, '') ILIKE '%invalid parameter%'
      OR COALESCE(upstream_error_message, '') ILIKE '%invalid parameter%'
      OR COALESCE(error_message, '') ILIKE '%unknown parameter%'
      OR COALESCE(upstream_error_message, '') ILIKE '%unknown parameter%'
      OR COALESCE(error_message, '') ILIKE '%unsupported parameter%'
      OR COALESCE(upstream_error_message, '') ILIKE '%unsupported parameter%'
      OR COALESCE(error_message, '') ILIKE '%malformed request%'
      OR COALESCE(upstream_error_message, '') ILIKE '%malformed request%'
	  OR COALESCE(error_message, '') ILIKE '%content policy%'
	  OR COALESCE(upstream_error_message, '') ILIKE '%content policy%'
	  OR COALESCE(error_message, '') ILIKE '%security policy%'
	  OR COALESCE(upstream_error_message, '') ILIKE '%security policy%'
		  OR COALESCE(error_message, '') ILIKE '%cyber policy%'
		  OR COALESCE(upstream_error_message, '') ILIKE '%cyber policy%'
		  OR COALESCE(error_message, '') ILIKE '%cyber-security policy%'
		  OR COALESCE(upstream_error_message, '') ILIKE '%cyber-security policy%'
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
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
          OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'platform'
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
            THEN 'platform'
        WHEN upstream_status_code = 401
          OR error_phase = 'account_auth'
          OR (
              upstream_status_code = 403
              AND (
                  COALESCE(error_message, '') ILIKE ANY (ARRAY[
                      '%access token%', '%oauth token%', '%credential%',
                      '%token has been revoked%', '%token expired%', '%invalid token%',
                      '%authentication failed%', '%认证失败%', '%请重新登录%', '%检查 api key%'
                  ])
                  OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                      '%access token%', '%oauth token%', '%credential%',
                      '%token has been revoked%', '%token expired%', '%invalid token%',
                      '%authentication failed%', '%认证失败%', '%请重新登录%', '%检查 api key%'
                  ])
              )
          )
            THEN 'platform'
        WHEN error_type = 'invalid_request_error'
          OR COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%context_length_exceeded%', '%exceeds the context window%',
                 '%maximum context length%', '%invalid ''%', '%invalid request%',
                 '%invalid_request_error%', '%invalid parameter%', '%unknown parameter%',
                 '%unsupported parameter%', '%malformed request%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%context_length_exceeded%', '%exceeds the context window%',
                 '%maximum context length%', '%invalid ''%', '%invalid request%',
                 '%invalid_request_error%', '%invalid parameter%', '%unknown parameter%',
                 '%unsupported parameter%', '%malformed request%'
             ])
            THEN 'platform'
        ELSE 'provider'
    END,
    error_category = 'recovered',
    counts_toward_sla = FALSE,
    alert_family = CASE
        WHEN COALESCE(error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(upstream_error_message, '') ILIKE '%insufficient%balance%'
          OR COALESCE(error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(upstream_error_message, '') ILIKE '%预扣费额度失败%'
          OR COALESCE(error_message, '') ILIKE '%用户剩余额度%'
          OR COALESCE(upstream_error_message, '') ILIKE '%用户剩余额度%'
            THEN 'capacity'
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
            THEN 'compatibility'
        WHEN upstream_status_code = 401
          OR error_phase = 'account_auth'
          OR (
              upstream_status_code = 403
              AND (
                  COALESCE(error_message, '') ILIKE ANY (ARRAY[
                      '%access token%', '%oauth token%', '%credential%',
                      '%token has been revoked%', '%token expired%', '%invalid token%',
                      '%authentication failed%', '%认证失败%', '%请重新登录%', '%检查 api key%'
                  ])
                  OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                      '%access token%', '%oauth token%', '%credential%',
                      '%token has been revoked%', '%token expired%', '%invalid token%',
                      '%authentication failed%', '%认证失败%', '%请重新登录%', '%检查 api key%'
                  ])
              )
          )
            THEN 'credential'
        WHEN error_type = 'invalid_request_error'
          OR COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%context_length_exceeded%', '%exceeds the context window%',
                 '%maximum context length%', '%invalid ''%', '%invalid request%',
                 '%invalid_request_error%', '%invalid parameter%', '%unknown parameter%',
                 '%unsupported parameter%', '%malformed request%', '%model%not supported%'
             ])
          OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%context_length_exceeded%', '%exceeds the context window%',
                 '%maximum context length%', '%invalid ''%', '%invalid request%',
                 '%invalid_request_error%', '%invalid parameter%', '%unknown parameter%',
                 '%unsupported parameter%', '%malformed request%', '%model%not supported%'
             ])
            THEN 'compatibility'
        ELSE 'provider_health'
    END,
    classification_reason = 'final_request_recovered',
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
      OR COALESCE(error_message, '') ILIKE '%cyber-security policy%'
      OR COALESCE(upstream_error_message, '') ILIKE '%cyber-security policy%'
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
             OR COALESCE(error_message, '') ILIKE ANY (ARRAY[
                    '%access token%',
                    '%oauth token%',
                    '%credential%',
                    '%token has been revoked%',
                    '%token expired%',
                    '%invalid token%',
                    '%authentication failed%'
                ])
             OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                    '%access token%',
                    '%oauth token%',
                    '%credential%',
                    '%token has been revoked%',
                    '%token expired%',
                    '%invalid token%',
                    '%authentication failed%'
                ])
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
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
             OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
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
        WHEN COALESCE(error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
             OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                 '%not enabled for this group%',
                 '%disabled for this group%',
                 '%capability is not enabled%',
                 '%permission is not enabled%'
             ])
            THEN 'compatibility'
        WHEN error_phase = 'account_auth'
             OR upstream_status_code = 401
             OR COALESCE(error_message, '') ILIKE ANY (ARRAY[
                    '%access token%',
                    '%oauth token%',
                    '%credential%',
                    '%token has been revoked%',
                    '%token expired%',
                    '%invalid token%',
                    '%authentication failed%'
                ])
             OR COALESCE(upstream_error_message, '') ILIKE ANY (ARRAY[
                    '%access token%',
                    '%oauth token%',
                    '%credential%',
                    '%token has been revoked%',
                    '%token expired%',
                    '%invalid token%',
                    '%authentication failed%'
                ])
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

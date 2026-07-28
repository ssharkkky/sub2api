-- Persist the v2 interpretation of untouched legacy Ops rules. Earlier
-- versions rewrote these rows only while reading them, so the database and UI
-- disagreed about which rules were actually enabled. Every predicate below
-- matches the exact shipped legacy default; operator-modified rules are never
-- changed.

UPDATE ops_alert_rules
SET enabled = FALSE,
    description = description || ' [disabled: replaced by availability failure rules]',
    updated_at = NOW()
WHERE name = '成功率过低'
  AND description = '当成功率低于 95% 且持续 5 分钟时触发告警（服务可用性下降）'
  AND enabled = TRUE
  AND notify_email = TRUE
  AND metric_type = 'success_rate'
  AND operator = '<'
  AND threshold = 95
  AND window_minutes = 5
  AND sustained_minutes = 5
  AND cooldown_minutes = 15
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb);

UPDATE ops_alert_rules
SET enabled = FALSE,
    description = description || ' [disabled: unsupported legacy latency metric]',
    updated_at = NOW()
WHERE name IN ('P95延迟过高', 'P99延迟过高')
  AND enabled = TRUE
  AND notify_email = TRUE
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb)
  AND (
    (
      name = 'P95延迟过高'
      AND description = '当 P95 延迟超过 2000ms 且持续 10 分钟时触发告警'
      AND metric_type = 'p95_latency_ms'
      AND operator = '>'
      AND threshold = 2000
      AND window_minutes = 5
      AND sustained_minutes = 10
      AND cooldown_minutes = 30
      AND severity = 'P2'
    )
    OR
    (
      name = 'P99延迟过高'
      AND description = '当 P99 延迟超过 3000ms 且持续 10 分钟时触发告警'
      AND metric_type = 'p99_latency_ms'
      AND operator = '>'
      AND threshold = 3000
      AND window_minutes = 5
      AND sustained_minutes = 10
      AND cooldown_minutes = 30
      AND severity = 'P2'
    )
  );

UPDATE ops_alert_rules
SET name = '基础设施可用性缓慢下降',
    description = '30 分钟 SLA 合格请求失败率达到 5%，失败至少 10 次且样本至少 100；持续 10 分钟后触发',
    metric_type = 'availability_failure_rate',
    operator = '>=',
    window_minutes = 30,
    sustained_minutes = 10,
    incident_family = 'availability',
    minimum_samples = 100,
    minimum_bad_count = 10,
    recovery_operator = '<',
    recovery_threshold = 2.5,
    recovery_sustained_minutes = 10,
    updated_at = NOW()
WHERE name = '错误率过高'
  AND description = '当错误率超过 5% 且持续 5 分钟时触发告警'
  AND enabled = TRUE
  AND notify_email = TRUE
  AND metric_type = 'error_rate'
  AND operator = '>'
  AND threshold = 5
  AND window_minutes = 5
  AND sustained_minutes = 5
  AND cooldown_minutes = 20
  AND severity = 'P1'
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb)
  AND NOT EXISTS (
    SELECT 1 FROM ops_alert_rules other
    WHERE other.name = '基础设施可用性缓慢下降'
  );

UPDATE ops_alert_rules
SET enabled = FALSE,
    description = description || ' [disabled: duplicate of v2 availability rule]',
    updated_at = NOW()
WHERE name = '错误率过高'
  AND description = '当错误率超过 5% 且持续 5 分钟时触发告警'
  AND enabled = TRUE
  AND notify_email = TRUE
  AND metric_type = 'error_rate'
  AND operator = '>'
  AND threshold = 5
  AND window_minutes = 5
  AND sustained_minutes = 5
  AND cooldown_minutes = 20
  AND severity = 'P1'
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb)
  AND EXISTS (
    SELECT 1 FROM ops_alert_rules other
    WHERE other.name = '基础设施可用性缓慢下降'
  );

UPDATE ops_alert_rules
SET name = '基础设施可用性快速下降',
    description = '5 分钟 SLA 合格请求失败率达到 20%，失败至少 10 次且样本至少 30；持续 3 分钟后触发',
    metric_type = 'availability_failure_rate',
    operator = '>=',
    window_minutes = 5,
    sustained_minutes = 3,
    incident_family = 'availability',
    minimum_samples = 30,
    minimum_bad_count = 10,
    recovery_operator = '<',
    recovery_threshold = 10,
    recovery_sustained_minutes = 5,
    updated_at = NOW()
WHERE name = '错误率极高'
  AND description = '当错误率超过 20% 且持续 1 分钟时触发告警（服务严重异常）'
  AND enabled = TRUE
  AND notify_email = TRUE
  AND metric_type = 'error_rate'
  AND operator = '>'
  AND threshold = 20
  AND window_minutes = 1
  AND sustained_minutes = 1
  AND cooldown_minutes = 15
  AND severity = 'P0'
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb)
  AND NOT EXISTS (
    SELECT 1 FROM ops_alert_rules other
    WHERE other.name = '基础设施可用性快速下降'
  );

UPDATE ops_alert_rules
SET enabled = FALSE,
    description = description || ' [disabled: duplicate of v2 availability rule]',
    updated_at = NOW()
WHERE name = '错误率极高'
  AND description = '当错误率超过 20% 且持续 1 分钟时触发告警（服务严重异常）'
  AND enabled = TRUE
  AND notify_email = TRUE
  AND metric_type = 'error_rate'
  AND operator = '>'
  AND threshold = 20
  AND window_minutes = 1
  AND sustained_minutes = 1
  AND cooldown_minutes = 15
  AND severity = 'P0'
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb)
  AND EXISTS (
    SELECT 1 FROM ops_alert_rules other
    WHERE other.name = '基础设施可用性快速下降'
  );

UPDATE ops_alert_rules
SET incident_family = CASE name
      WHEN '并发队列积压' THEN 'request_queue'
      ELSE 'resource_capacity'
    END,
    recovery_operator = '<',
    recovery_threshold = CASE name
      WHEN 'CPU使用率过高' THEN 75
      WHEN '内存使用率过高' THEN 85
      WHEN '并发队列积压' THEN 50
    END,
    recovery_sustained_minutes = 5,
    updated_at = NOW()
WHERE enabled = TRUE
  AND notify_email = TRUE
  AND COALESCE(incident_family, 'custom') = 'custom'
  AND COALESCE(minimum_samples, 0) = 0
  AND COALESCE(minimum_bad_count, 0) = 0
  AND COALESCE(recovery_operator, '') = ''
  AND recovery_threshold IS NULL
  AND COALESCE(recovery_sustained_minutes, 1) = 1
  AND COALESCE(shadow_mode, FALSE) = FALSE
  AND (filters IS NULL OR filters = '{}'::jsonb)
  AND (
    (
      name = 'CPU使用率过高'
      AND description = '当 CPU 使用率超过 85% 且持续 10 分钟时触发告警'
      AND metric_type = 'cpu_usage_percent'
      AND operator = '>'
      AND threshold = 85
      AND window_minutes = 5
      AND sustained_minutes = 10
      AND cooldown_minutes = 30
      AND severity = 'P2'
    )
    OR
    (
      name = '内存使用率过高'
      AND description = '当内存使用率超过 90% 且持续 10 分钟时触发告警（可能导致 OOM）'
      AND metric_type = 'memory_usage_percent'
      AND operator = '>'
      AND threshold = 90
      AND window_minutes = 5
      AND sustained_minutes = 10
      AND cooldown_minutes = 20
      AND severity = 'P1'
    )
    OR
    (
      name = '并发队列积压'
      AND description = '当并发队列深度超过 100 且持续 5 分钟时触发告警（系统处理能力不足）'
      AND metric_type = 'concurrency_queue_depth'
      AND operator = '>'
      AND threshold = 100
      AND window_minutes = 5
      AND sustained_minutes = 5
      AND cooldown_minutes = 20
      AND severity = 'P1'
    )
  );

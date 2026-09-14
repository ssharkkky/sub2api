-- Step 1 迁移覆盖验证（只读，零风险）
-- 目的：对每个 restrict 渠道的每个定价公开名，检查是否被该渠道绑定账号的
--       显式 model_mapping（存储的 keys，含精确匹配）覆盖。
--       被覆盖 = 删默认注入后仍可由该账号服务（安全）。
--       未覆盖 = 需进一步确认（是否靠原生快照 / 既有数据问题）。
-- 用法：ssh -p 22222 root@198.44.63.169 'docker exec -i -e PGPASSWORD=<pw> sub2api-postgres psql -U sub2api -d sub2api -At' < verify_coverage.sql

\pset footer off

\echo '=== [1] restrict 渠道定价公开名 × 绑定账号显式映射 覆盖表 ==='
WITH pricing AS (
  SELECT c.id AS channel_id, c.name AS channel_name,
         m AS model
  FROM channels c
  JOIN channel_model_pricing p ON p.channel_id = c.id
  CROSS JOIN LATERAL jsonb_array_elements_text(p.models) AS m
  WHERE c.restrict_models AND c.status = 'active'
),
bound AS (
  SELECT DISTINCT c.id AS channel_id, a.id AS account_id,
    CASE WHEN a.credentials->'model_mapping' IS NOT NULL
          AND jsonb_typeof(a.credentials->'model_mapping') = 'object'
         THEN a.credentials->'model_mapping' ELSE '{}'::jsonb END AS mm
  FROM channels c
  JOIN channel_groups cg ON cg.channel_id = c.id
  JOIN groups g ON g.id = cg.group_id AND g.deleted_at IS NULL
  JOIN account_groups ag ON ag.group_id = g.id
  JOIN accounts a ON a.id = ag.account_id
  WHERE c.restrict_models AND c.status = 'active'
),
pairs AS (
  SELECT pr.channel_id, pr.channel_name, pr.model, b.account_id,
         (pr.model IN (SELECT jsonb_object_keys(b.mm))) AS covered
  FROM pricing pr
  JOIN bound b ON b.channel_id = pr.channel_id
)
SELECT channel_id, channel_name, model,
       bool_or(covered)                          AS explicit_covered,
       count(*) FILTER (WHERE covered)           AS n_explicit_accounts
FROM pairs
GROUP BY channel_id, channel_name, model
ORDER BY channel_id, model;

\echo ''
\echo '=== [2] 未覆盖（explicit_covered=f）的定价名 = 迁移关注点 ==='
WITH pricing AS (
  SELECT c.id AS channel_id, c.name AS channel_name, m AS model
  FROM channels c
  JOIN channel_model_pricing p ON p.channel_id = c.id
  CROSS JOIN LATERAL jsonb_array_elements_text(p.models) AS m
  WHERE c.restrict_models AND c.status = 'active'
),
bound AS (
  SELECT DISTINCT c.id AS channel_id, a.id AS account_id,
    CASE WHEN a.credentials->'model_mapping' IS NOT NULL
          AND jsonb_typeof(a.credentials->'model_mapping') = 'object'
         THEN a.credentials->'model_mapping' ELSE '{}'::jsonb END AS mm
  FROM channels c
  JOIN channel_groups cg ON cg.channel_id = c.id
  JOIN groups g ON g.id = cg.group_id AND g.deleted_at IS NULL
  JOIN account_groups ag ON ag.group_id = g.id
  JOIN accounts a ON a.id = ag.account_id
  WHERE c.restrict_models AND c.status = 'active'
),
cov AS (
  SELECT pr.channel_id, pr.channel_name, pr.model,
         bool_or(pr.model IN (SELECT jsonb_object_keys(b.mm))) AS explicit_covered
  FROM pricing pr
  JOIN bound b ON b.channel_id = pr.channel_id
  GROUP BY pr.channel_id, pr.channel_name, pr.model
)
SELECT channel_id, channel_name, model
FROM cov
WHERE NOT explicit_covered
ORDER BY channel_id, model;

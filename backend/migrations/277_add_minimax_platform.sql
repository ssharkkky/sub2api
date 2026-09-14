-- 把 MiniMax 加入国产供应商平台白名单：
--   1. user_platform_quotas.platform CHECK
--   2. composite_model_routes.target_platform CHECK
--   3. channel_monitors / channel_monitor_request_templates.provider CHECK
--
-- （fork 重编号：upstream 237 → 277；重建列表补上 fork 独有的 kiro，
-- 与 256/257/258 的约束终态对齐，避免回退 fork 平台。）
--
-- 形态说明（docs/DOCKER_MANAGED_UPDATES.md 托管蓝绿 + 镜像回滚策略）：
-- 与 224/226/227/240/256-258 同型——DROP IF EXISTS 后重建超集约束，存量行瞬时
-- 校验通过；DROP 语句统一采用 `ALTER TABLE IF EXISTS ONLY <表>` 前缀，与基线内
-- 已发布的 157/240/245/256-258 语句指纹保持区分。upstream 原版的条件重建 DO 块
-- 收敛为无条件超集重建（可达状态下语义等价，且为 checker 允许形态）。

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE IF EXISTS ONLY user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro',
                        'grok', 'kimi', 'zhipu', 'deepseek', 'minimax'));

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE IF EXISTS ONLY composite_model_routes
    DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE composite_model_routes
    ADD CONSTRAINT composite_model_routes_target_platform_check
    CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'kiro',
                               'grok', 'kimi', 'zhipu', 'deepseek', 'minimax'));

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE IF EXISTS ONLY channel_monitors
    DROP CONSTRAINT IF EXISTS channel_monitors_provider_check;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE channel_monitors
    ADD CONSTRAINT channel_monitors_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'kiro', 'kimi', 'zhipu', 'deepseek', 'minimax'));

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE IF EXISTS ONLY channel_monitor_request_templates
    DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE channel_monitor_request_templates
    ADD CONSTRAINT channel_monitor_request_templates_provider_check
    CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok',
                        'antigravity', 'kiro', 'kimi', 'zhipu', 'deepseek', 'minimax'));

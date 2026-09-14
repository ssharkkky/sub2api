-- user_platform_quotas 仅保存至少配置了一档限额的记录；不存在的行等价于不限额。
-- 三档限额全为 NULL 的行不携带任何可执行的限额（billing_cache_service 把三档全
-- NULL 的行按 sentinel 跳过），软删除后对所有读取路径不可见，效果与物理删除一致。
--
-- （fork 重编号：upstream 238 → 279。）
-- 形态说明（docs/DOCKER_MANAGED_UPDATES.md 托管蓝绿 + 镜像回滚策略）：顶层 DELETE
-- 不属于可自动回滚形态，upstream 原版的 DELETE 收敛为 reviewed-compatible 软删除
-- UPDATE（表已有 deleted_at 软删除列与「deleted_at IS NULL」部分唯一索引，读取路径
-- 一律过滤软删除行）；物理清理由后续维护窗口执行。幂等：重复执行影响 0 行。

-- sub2api-managed-update: reviewed-compatible
UPDATE user_platform_quotas
    SET deleted_at = now()
    WHERE daily_limit_usd IS NULL
      AND weekly_limit_usd IS NULL
      AND monthly_limit_usd IS NULL
      AND deleted_at IS NULL;

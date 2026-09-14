-- 276: 收敛 groups 的模型白名单列，修复 275（upstream 235）未落地却已被记账的实例。
-- （fork 重编号：upstream 236 → 276，接在 fork 275 之后）
--
-- 275 采用 expand/contract（新增可空列 + 回填 + 触发器，旧列保留）。但迁移一旦写入
-- schema_migrations 就会按「文件名 + checksum」整份跳过，不再重跑。因此只要数据库在
-- 275 之后回到了旧结构——例如为了回滚到旧版本镜像而手工把新列删掉/改回旧名，或者用
-- 旧结构的备份做了部分恢复——应用依旧能正常启动，但每个关联 groups 的查询都会失败：
--     pq: column groups.model_allowlist does not exist
-- 表现为「API 密钥」「我的订阅」「管理端订阅列表」等页面加载失败（issue #6780）。
--
-- 本迁移可重放，并且与 275 同为 checker 允许形态（docs/DOCKER_MANAGED_UPDATES.md
-- 托管蓝绿 + 镜像回滚策略；列重命名需要维护窗口，此处不做；旧列保留，contract 由后续版本执行）：
--   1) 确保新列存在（275 已落地的实例 no-op；结构回退的实例按可空补建）；
--   2) 一次性回填：新列仍为空时把旧列里的配置原样复制过来（两列 JSON 结构完全
--      一致），条件不满足时整条语句 0 行更新，可安全重放；
--   3) NULL 补默认值后把列收敛为 NOT NULL DEFAULT '{}'，终态与代码预期一致。

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS model_allowlist JSONB NULL;

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET model_allowlist = models_list_config
    WHERE COALESCE(model_allowlist, '{}'::jsonb) = '{}'::jsonb
      AND COALESCE(models_list_config, '{}'::jsonb) <> '{}'::jsonb;

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET model_allowlist = '{}'::jsonb
    WHERE model_allowlist IS NULL;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN model_allowlist SET DEFAULT '{}';

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN model_allowlist SET NOT NULL;

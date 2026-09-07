-- Upstream v0.2.2 migration 235: groups.models_list_config 更名为 model_allowlist，
-- 语义从「仅影响 /v1/models 展示」升级为分组级模型白名单（同时约束模型列表接口与
-- 请求准入）。数据原样保留。
--
-- 兼容性评审（托管蓝绿部署 + 镜像回滚）：不做裸 RENAME COLUMN（旧二进制按旧列名
-- 读写会直接报错），改用 expand/contract：
--   1) 新增可空列 model_allowlist，旧二进制继续按旧列工作；
--   2) 一次性回填：旧列与新列 JSON 结构完全一致（{"enabled": bool, "models": []}），
--      数据原样复制，历史配置不丢失；
--   3) BEFORE INSERT/UPDATE 触发器在数据库边界归一化旧二进制的 NULL 写入
--      （旧二进制不知道新列，INSERT 时产生 NULL，取旧列值兜底）。
-- 旧列 models_list_config 暂保留（contract 步骤由后续版本执行），镜像回滚安全。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS model_allowlist JSONB;

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET model_allowlist = models_list_config
    WHERE model_allowlist IS NULL;

-- sub2api-managed-update: reviewed-compatible
CREATE OR REPLACE FUNCTION sub2api_default_group_model_allowlist()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.model_allowlist IS NULL THEN
        NEW.model_allowlist := NEW.models_list_config;
    END IF;
    RETURN NEW;
END;
$$;

-- sub2api-managed-update: reviewed-compatible
CREATE OR REPLACE TRIGGER groups_default_model_allowlist
BEFORE INSERT OR UPDATE OF model_allowlist ON groups
FOR EACH ROW
EXECUTE FUNCTION sub2api_default_group_model_allowlist();

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN groups.model_allowlist IS
    'Group model allowlist: constrains both model listing responses and request admission';

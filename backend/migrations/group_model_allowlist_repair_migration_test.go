package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGroupModelAllowlistRepairMigration 校验 276（upstream 236）号迁移把三种残留状态
// （275 未落地、旧列回退、两列并存）都收敛到 model_allowlist NOT NULL DEFAULT '{}'，
// 且全部语句在 checker 允许形态内（无 DO 块、无裸 RENAME，旧列保留待 contract）。
func TestGroupModelAllowlistRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("276_group_model_allowlist_repair.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	// 确保新列存在（幂等，可空补建）。
	require.Contains(t, sql, "ALTER TABLE groups ADD COLUMN IF NOT EXISTS model_allowlist JSONB NULL;")
	// 一次性回填：新列为空时复制旧列配置（两列 JSON 结构一致），可重放。
	require.Contains(t, sql, "SET model_allowlist = models_list_config")
	require.Contains(t, sql, "COALESCE(model_allowlist, '{}'::jsonb) = '{}'::jsonb")
	require.Contains(t, sql, "COALESCE(models_list_config, '{}'::jsonb) <> '{}'::jsonb")
	// NULL 补默认值后收敛列终态（SET DEFAULT 单 token 字面量，checker 允许形态）。
	require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN model_allowlist SET DEFAULT '{}';")
	require.Contains(t, sql, "ALTER TABLE groups ALTER COLUMN model_allowlist SET NOT NULL;")

	// checker 约束：无 DO 块、无顶层 RENAME；旧列不删除（contract 由后续版本执行）。
	require.NotContains(t, sql, "DO $$")
	require.NotContains(t, sql, "RENAME COLUMN")
	require.NotContains(t, sql, "DROP COLUMN")
	require.NotContains(t, sql, "DROP TRIGGER")
	// 每条 reviewed 语句必须携带 statement 级注解。
	require.Equal(t, 4, strings.Count(sql, "-- sub2api-managed-update: reviewed-compatible"))
}

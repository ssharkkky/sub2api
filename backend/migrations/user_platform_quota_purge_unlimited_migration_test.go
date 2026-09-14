package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestUserPlatformQuotasPurgeUnlimitedMigration 校验 279（upstream 238）号迁移只处理
// 三档限额全为 NULL 的行：这类行等价于"不存在"，任何一档非 NULL 的记录（含软删历史）
// 都必须保留。顶层 DELETE 不属于可自动回滚形态（docs/DOCKER_MANAGED_UPDATES.md），
// 故采用 reviewed-compatible 软删除 UPDATE（读取路径一律过滤 deleted_at 非空行），
// 物理清理由后续维护窗口执行。
func TestUserPlatformQuotasPurgeUnlimitedMigration(t *testing.T) {
	content, err := FS.ReadFile("279_purge_unlimited_user_platform_quotas.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "-- sub2api-managed-update: reviewed-compatible")
	require.Contains(t, sql, "UPDATE user_platform_quotas SET deleted_at = now()")
	require.Contains(t, sql, "WHERE daily_limit_usd IS NULL AND weekly_limit_usd IS NULL AND monthly_limit_usd IS NULL AND deleted_at IS NULL;")
	require.NotContains(t, sql, "DELETE FROM")
	require.NotContains(t, sql, "TRUNCATE")
	require.NotContains(t, sql, "DROP TABLE")
}

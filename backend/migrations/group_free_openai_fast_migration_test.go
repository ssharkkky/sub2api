package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGroupFreeOpenAIFastMigration pins the rollback-safe form of the
// group-level Free Fast billing migration (upstream v0.2.0, #6444). The
// column is added nullable (plain nullable-column allowlist) so older
// binaries stay compatible during managed blue-green deployment and image
// rollback; a reviewed-compatible default plus one-time backfill keep the
// non-optional application field free of NULL.
func TestGroupFreeOpenAIFastMigration(t *testing.T) {
	content, err := FS.ReadFile("269_group_free_openai_fast.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS free_openai_fast BOOLEAN")
	// Nullable expansion: no inline NOT NULL / DEFAULT on the ADD COLUMN.
	require.NotContains(t, sql, "free_openai_fast BOOLEAN NOT NULL")
	// Reviewed-compatible default + one-time backfill for pre-existing rows.
	require.Contains(t, sql, "ALTER COLUMN free_openai_fast SET DEFAULT false")
	require.Contains(t, sql, "UPDATE groups SET free_openai_fast = false WHERE free_openai_fast IS NULL")
	// The reviewed-compatible annotation must gate the reviewed statements.
	require.Contains(t, sql, "sub2api-managed-update: reviewed-compatible")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.free_openai_fast")
}

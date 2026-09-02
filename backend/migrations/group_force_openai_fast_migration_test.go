package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGroupForceOpenAIFastMigration pins the rollback-safe form of the
// group-level OpenAI Fast switch migration (upstream v0.2.0, #6443). The
// column is added nullable (plain nullable-column allowlist) so older
// binaries stay compatible during managed blue-green deployment and image
// rollback; a reviewed-compatible default plus one-time backfill keep the
// non-optional application field free of NULL.
func TestGroupForceOpenAIFastMigration(t *testing.T) {
	content, err := FS.ReadFile("267_group_force_openai_fast.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS force_openai_fast BOOLEAN")
	// Nullable expansion: no inline NOT NULL / DEFAULT on the ADD COLUMN.
	require.NotContains(t, sql, "force_openai_fast BOOLEAN NOT NULL")
	// Reviewed-compatible default + one-time backfill for pre-existing rows,
	// then SET NOT NULL restores the upstream NOT NULL DEFAULT FALSE invariant.
	require.Contains(t, sql, "ALTER COLUMN force_openai_fast SET DEFAULT false")
	require.Contains(t, sql, "UPDATE groups SET force_openai_fast = false WHERE force_openai_fast IS NULL")
	require.Contains(t, sql, "ALTER COLUMN force_openai_fast SET NOT NULL")
	// The reviewed-compatible annotation must gate the reviewed statements.
	require.Contains(t, sql, "sub2api-managed-update: reviewed-compatible")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.force_openai_fast")
}

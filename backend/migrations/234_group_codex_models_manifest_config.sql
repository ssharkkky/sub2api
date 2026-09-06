-- Pinned-accounts Codex models manifest config (upstream v0.2.1).
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- the expansion starts nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. The reviewed-compatible default covers rows
-- inserted by older binaries that omit the column, the one-time backfill
-- clears NULL on pre-existing rows, and the final SET NOT NULL (safe after
-- the backfill, metadata-only on PostgreSQL 11+) restores the upstream
-- NOT NULL DEFAULT '{}' invariant, so the non-optional application field
-- never observes NULL. Older binaries keep working through the default,
-- so image rollback stays operational.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS codex_models_manifest_config JSONB;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN codex_models_manifest_config SET DEFAULT '{}';

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET codex_models_manifest_config = '{}'
    WHERE codex_models_manifest_config IS NULL;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN codex_models_manifest_config SET NOT NULL;

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN groups.codex_models_manifest_config IS
    'Pinned-accounts Codex models manifest config for OpenAI groups: {"enabled":bool,"account_ids":[int64],"fallback_to_scheduler":bool}; when enabled the Codex /models manifest is fetched only from the pinned accounts';

-- Group-level OpenAI Fast switch (upstream v0.2.0, #6443).
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- the expansion starts nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. The reviewed-compatible default covers rows
-- inserted by older binaries that omit the column, the one-time backfill
-- clears NULL on pre-existing rows, and the final SET NOT NULL (safe after
-- the backfill, metadata-only on PostgreSQL 11+) restores the upstream
-- NOT NULL DEFAULT FALSE invariant, so the non-optional application field
-- never observes NULL. Older binaries keep working through the default,
-- so image rollback stays operational.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS force_openai_fast BOOLEAN;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN force_openai_fast SET DEFAULT false;

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET force_openai_fast = false
    WHERE force_openai_fast IS NULL;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN force_openai_fast SET NOT NULL;

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN groups.force_openai_fast IS
    'Force OpenAI gateway requests in this group to use service_tier=priority before global Fast/Flex policy evaluation';

-- Group-level Free Fast switch (upstream v0.2.0, #6444).
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
    ADD COLUMN IF NOT EXISTS free_openai_fast BOOLEAN;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN free_openai_fast SET DEFAULT false;

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET free_openai_fast = false
    WHERE free_openai_fast IS NULL;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN free_openai_fast SET NOT NULL;

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN groups.free_openai_fast IS
    'Whether Fast/priority requests in this OpenAI/Composite group are billed to users at Standard price';

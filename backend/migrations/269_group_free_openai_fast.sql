-- Group-level Free Fast switch (upstream v0.2.0, #6444).
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- the expansion stays nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. The reviewed-compatible default covers rows
-- inserted by older binaries that omit the column, and the one-time
-- backfill clears NULL on pre-existing rows, so the non-optional
-- application field never observes NULL. Older binaries ignore this
-- column entirely, so image rollback stays operational.
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
COMMENT ON COLUMN groups.free_openai_fast IS
    'Whether Fast/priority requests in this OpenAI/Composite group are billed to users at Standard price';

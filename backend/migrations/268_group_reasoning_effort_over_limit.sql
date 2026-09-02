-- Per-group access control when an explicit OpenAI/Codex reasoning effort
-- exceeds max_reasoning_effort. Existing groups keep the previous behaviour
-- (automatically downgrade to the ceiling).
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- the expansion starts nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. The reviewed-compatible default covers rows
-- inserted by older binaries that omit the column, the one-time backfill
-- clears NULL on pre-existing rows, and the final SET NOT NULL (safe after
-- the backfill, metadata-only on PostgreSQL 11+) restores the upstream
-- NOT NULL DEFAULT 'downgrade' invariant, so the non-optional application
-- field never observes NULL. Older binaries keep working through the
-- default, so image rollback stays operational.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS max_reasoning_effort_over_limit VARCHAR(20);

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN max_reasoning_effort_over_limit SET DEFAULT 'downgrade';

-- sub2api-managed-update: reviewed-compatible
UPDATE groups
    SET max_reasoning_effort_over_limit = 'downgrade'
    WHERE max_reasoning_effort_over_limit IS NULL;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE groups
    ALTER COLUMN max_reasoning_effort_over_limit SET NOT NULL;

-- Compatibility review (managed blue-green deployment + image rollback):
-- the expansion stays nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. New application rows always write an explicit
-- value; historical rows read as NULL, which the application maps to FALSE
-- ("not native OpenAI remote compaction v2"). Older binaries ignore this
-- column entirely, so image rollback stays operational.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS native_compaction_v2 BOOLEAN;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE usage_logs ALTER COLUMN native_compaction_v2 SET DEFAULT FALSE;

-- Descriptive metadata only; ignored by older binaries.
-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN usage_logs.native_compaction_v2 IS
    'True only when the request was identified at runtime as native OpenAI remote compaction v2';

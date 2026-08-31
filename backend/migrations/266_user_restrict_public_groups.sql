-- Per-user access control for public (non-exclusive) groups.
--
-- Public groups have always been bindable by every user. When this flag is
-- enabled for a user, the public groups they may bind are narrowed to the ones
-- listed in user_allowed_groups, which until now only carried exclusive groups.
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- the expansion stays nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. The default covers rows inserted by older
-- binaries that omit the column, and the one-time backfill clears NULL on
-- pre-existing rows, so the non-optional application field never observes
-- NULL. Older binaries ignore this column entirely, so image rollback stays
-- operational.
ALTER TABLE users ADD COLUMN IF NOT EXISTS restrict_public_groups BOOLEAN;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE users ALTER COLUMN restrict_public_groups SET DEFAULT false;

-- sub2api-managed-update: reviewed-compatible
UPDATE users SET restrict_public_groups = false WHERE restrict_public_groups IS NULL;

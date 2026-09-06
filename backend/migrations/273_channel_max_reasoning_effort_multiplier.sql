-- Channel max reasoning-effort pricing multiplier (upstream v0.2.1).
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- the column starts nullable (plain nullable-column allowlist) so older
-- binaries remain compatible. The CHECK constraint only permits NULL or
-- positive values, so old rows and old binaries never observe a violating
-- value; the constraint name stays within PostgreSQL's 63-byte identifier
-- limit. Older binaries keep working, so image rollback stays operational.
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS max_reasoning_effort_multiplier NUMERIC(10,4);

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN channel_model_pricing.max_reasoning_effort_multiplier IS
    'Billing/quota multiplier applied when the forwarded reasoning effort is max';

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE channel_model_pricing
    DROP CONSTRAINT IF EXISTS chk_channel_model_pricing_max_re_effort_mult_pos;

-- sub2api-managed-update: reviewed-compatible
ALTER TABLE channel_model_pricing
    ADD CONSTRAINT chk_channel_model_pricing_max_re_effort_mult_pos
    CHECK (max_reasoning_effort_multiplier IS NULL OR max_reasoning_effort_multiplier > 0);

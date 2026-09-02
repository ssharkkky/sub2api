-- 1h cache-write pricing (upstream v0.2.0, #6474).
--
-- Compatibility review (managed blue-green deployment + image rollback):
-- all four columns are plain nullable NUMERIC expansions (no database
-- default) so older binaries remain compatible; NULL preserves legacy
-- cache_write_price behavior. The reviewed-compatible comments document
-- the columns without changing data. Image rollback stays operational.
ALTER TABLE channel_model_pricing
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

ALTER TABLE channel_account_stats_model_pricing
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

ALTER TABLE channel_account_stats_pricing_intervals
    ADD COLUMN IF NOT EXISTS cache_write_1h_price NUMERIC(20,12);

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN channel_model_pricing.cache_write_1h_price IS
    '1h cache write price per token; NULL preserves legacy cache_write_price behavior';

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN channel_pricing_intervals.cache_write_1h_price IS
    'Interval-specific 1h cache write price per token';

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN channel_account_stats_model_pricing.cache_write_1h_price IS
    '1h cache write price per token for account stats pricing';

-- sub2api-managed-update: reviewed-compatible
COMMENT ON COLUMN channel_account_stats_pricing_intervals.cache_write_1h_price IS
    'Interval-specific 1h cache write price per token for account stats pricing';

-- Fixed-size Channel Monitor V2 rollups for the 24h / 7d / 30d views.
-- Source of truth remains the 1-minute passive rollup; these tables cache the
-- UI bucket sizes so historical buckets do not need to be re-aggregated on each
-- filter change. The background aggregator overwrites affected buckets.

CREATE TABLE IF NOT EXISTS channel_monitor_v2_metrics_rollup (
    bucket_start TIMESTAMPTZ NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    success_requests BIGINT NOT NULL DEFAULT 0,
    error_requests BIGINT NOT NULL DEFAULT 0,
    upstream_affected_requests BIGINT NOT NULL DEFAULT 0,
    upstream_attempt_count BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model)
);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_user_metrics_rollup (
    bucket_start TIMESTAMPTZ NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    success_requests BIGINT NOT NULL DEFAULT 0,
    error_requests BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    cache_creation_tokens BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens BIGINT NOT NULL DEFAULT 0,
    ttft_sum_ms BIGINT NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,
    duration_sum_ms BIGINT NOT NULL DEFAULT 0,
    duration_count BIGINT NOT NULL DEFAULT 0,
    computed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model, user_id)
);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_error_metrics_rollup (
    bucket_start TIMESTAMPTZ NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    error_category TEXT NOT NULL,
    taxonomy_version SMALLINT NOT NULL,
    error_requests BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model, error_category, taxonomy_version)
);

CREATE TABLE IF NOT EXISTS channel_monitor_v2_latency_histograms_rollup (
    bucket_start TIMESTAMPTZ NOT NULL,
    bucket_seconds INTEGER NOT NULL CHECK (bucket_seconds IN (300, 3600, 43200, 86400)),
    platform TEXT NOT NULL,
    group_id BIGINT NOT NULL DEFAULT 0,
    model TEXT NOT NULL,
    user_id BIGINT NOT NULL DEFAULT 0,
    metric TEXT NOT NULL CHECK (metric IN ('ttft', 'duration')),
    upper_bound_ms INTEGER NOT NULL,
    sample_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (bucket_seconds, bucket_start, platform, group_id, model, user_id, metric, upper_bound_ms)
);

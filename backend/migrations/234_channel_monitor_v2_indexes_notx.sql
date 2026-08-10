CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_metrics_platform_time
    ON channel_monitor_v2_metrics_1m (platform, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_metrics_group_time
    ON channel_monitor_v2_metrics_1m (group_id, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_metrics_model_time
    ON channel_monitor_v2_metrics_1m (model, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_user_metrics_user_time
    ON channel_monitor_v2_user_metrics_1m (user_id, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_user_metrics_time
    ON channel_monitor_v2_user_metrics_1m (bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_errors_time
    ON channel_monitor_v2_error_metrics_1m (bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_errors_category_time
    ON channel_monitor_v2_error_metrics_1m (error_category, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_histograms_time
    ON channel_monitor_v2_latency_histograms_1m (bucket_start DESC, metric);

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_metrics_rollup_platform_time
    ON channel_monitor_v2_metrics_rollup (bucket_seconds, platform, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_metrics_rollup_group_time
    ON channel_monitor_v2_metrics_rollup (bucket_seconds, group_id, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_metrics_rollup_model_time
    ON channel_monitor_v2_metrics_rollup (bucket_seconds, model, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_user_rollup_user_time
    ON channel_monitor_v2_user_metrics_rollup (bucket_seconds, user_id, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_user_rollup_time
    ON channel_monitor_v2_user_metrics_rollup (bucket_seconds, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_errors_rollup_time
    ON channel_monitor_v2_error_metrics_rollup (bucket_seconds, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_errors_rollup_category_time
    ON channel_monitor_v2_error_metrics_rollup (bucket_seconds, error_category, bucket_start DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_channel_monitor_v2_histograms_rollup_time
    ON channel_monitor_v2_latency_histograms_rollup (bucket_seconds, bucket_start DESC, metric);

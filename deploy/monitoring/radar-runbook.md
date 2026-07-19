# Model Radar monitoring runbook

The service exports Prometheus data at `GET /metrics`. Mount
`radar-alerts.yml` under the Prometheus rule path and adapt the sample target in
`prometheus-radar.yml` to the deployment's internal service address. The
endpoint contains only fixed, normalized labels and is closed with 404 when
`radar.metrics_bearer_token` is empty. When configured, every peer must present
the matching bearer token; source it from Prometheus `credentials_file`. The
default Caddy configuration additionally returns 404 for `/metrics`, so scrape
the application directly rather than through the public ingress.

Provision `/etc/prometheus/secrets/sub2api-radar-metrics-token` outside Git as
a regular file owned by the account that runs Prometheus. Set mode `0400` when
the runtime owns the file exclusively, or `0600` when it must rewrite/rotate
the file. Group- or world-readable modes are forbidden; validate ownership and
permissions with `stat` before starting or reloading Prometheus. Put only the
token in the file (no `Bearer` prefix or shell assignment), rotate it through
the deployment secret store, and update the application and credentials file
atomically enough to avoid an extended scrape outage.

## Dashboard queries

| Panel | PromQL | Target |
|---|---|---|
| Core module availability | `1 - (sum by (job) (rate(radar_http_requests_total{route=~"/api/v1/public/radar/(service-health|quota-buckets/latest|degradation/latest|lmarena|sources)",status="5xx"}[30m])) or on (job) (0 * sum by (job) (rate(radar_http_requests_total{route=~"/api/v1/public/radar/(service-health|quota-buckets/latest|degradation/latest|lmarena|sources)"}[30m])))) / clamp_min(sum by (job) (rate(radar_http_requests_total{route=~"/api/v1/public/radar/(service-health|quota-buckets/latest|degradation/latest|lmarena|sources)"}[30m])), 0.000001)` | ≥99.5% per job; 2xx-only traffic evaluates to 100% |
| Quota snapshot success | `(sum by (job) (rate(radar_aggregator_runs_total{result="success"}[1h])) or on (job) (0 * sum by (job) (rate(radar_aggregator_runs_total[1h])))) / clamp_min(sum by (job) (rate(radar_aggregator_runs_total[1h])), 0.000001)` | ≥99% per job; failure-only traffic evaluates to 0% |
| Latest success visible | Each thresholded `count by (job)` is completed with `or on (job) (0 * total_sources_by_job)` before the three categories are added and divided by the common source denominator | 100% per job; an all-stale category evaluates to zero rather than disappearing |
| Source age | `min by (job, source) (radar_source_age_seconds >= 0) or max by (job, source) (radar_source_age_seconds) < 0` | status ≤1h; AA models ≤12h; AA performance/LMArena ≤48h |
| Radar API p95 | `histogram_quantile(0.95, sum by (job, le) (rate(radar_http_request_duration_seconds_bucket[10m])))` | <100ms per job |
| Radar API 5xx rate | `sum by (job) (rate(radar_http_requests_total{status="5xx"}[10m])) / clamp_min(sum by (job) (rate(radar_http_requests_total[10m])), 0.000001)` | <0.5% per job |
| Fetch result | `sum by (job, source) (rate(radar_fetch_success_total[15m]))` and `sum by (job, source, reason) (rate(radar_fetch_failure_total[15m]))` | failures explained |
| Fetch p95/status | `histogram_quantile(0.95, sum by (job, source, le) (rate(radar_fetch_duration_seconds_bucket[15m])))` and `sum by (job, source, status) (rate(radar_fetch_http_responses_total[15m]))` | source dependent |
| Aggregation | `histogram_quantile(0.95, sum by (job, le) (rate(radar_aggregator_duration_seconds_bucket[15m])))`, `min by (job) (radar_aggregator_bucket_count)`, `avg_over_time((min by (job) (radar_aggregator_bucket_count))[24h:30s])` | <60s; recent run within 2 configured intervals; no abnormal bucket drop |
| Skips/inference | `sum by (job, reason) (rate(radar_aggregator_skipped_accounts_total[1h]))`, `sum by (job, bucket, result, reason) (rate(radar_inference_total[1h]))` | investigate changes |
| Redis/cache | `sum by (job, access, operation) (rate(radar_redis_operations_total{result="error"}[10m]))`, `max by (job, cache) (radar_cache_memory_bytes)` | zero errors; each job's de-replicated family sum <64MiB |

## RadarSourceSustainedFailure

Check the normalized `source` and `reason`, then inspect
`radar_fetch_http_responses_total`. Confirm upstream reachability and rate-limit
state. Logs contain source/operation/class only; do not add API keys, URLs,
query strings, response bodies, or Redis keys while debugging.

## RadarSourceDataStale

Compare source age with fetch successes and failures. A value of `-1` means the
process has not observed a committed success since startup. Verify Redis
commit operations before forcing a refresh. Existing public data must remain
visible during upstream failure.

## RadarLatestSuccessVisibilityBelowTarget

Find sources with negative age or above their 1h/12h/48h threshold. Confirm the public
`/sources` response still exposes the last successful timestamp and that no
failure path cleared a previously committed payload.

## RadarAggregatorP95Slow

Inspect database batch-query latency, eligible account volume, Redis write
latency, and skipped-account reasons. Do not log account identifiers.

## RadarAggregatorFailure

Group `radar_aggregator_runs_total` by `reason`, correlate with
`radar_redis_operations_total`, and inspect the sanitized
`radar_quota_aggregation_failed` log.

## RadarAggregatorNoRecentRun

For enabled jobs, compare `time() - max by (job) (radar_aggregator_last_run_timestamp_seconds)`
with twice `max by (job) (radar_aggregator_interval_seconds)`. The rule is gated by
`max by (job) (radar_enabled) == 1`. A zero last-run
timestamp means no completed run has been persisted. Check scheduler lifecycle,
distributed-lock acquisition, and Redis state writes; a restarted replica with
zero local state must not mask another replica's shared timestamp.

## RadarBucketCountAnomalousDrop

The current count and every sample in the 24-hour baseline are reduced with `min by (job) (radar_aggregator_bucket_count)` before the subquery range is evaluated. This prevents newly started, duplicated, or lagging replicas from changing the baseline weighting while keeping jobs isolated. The alert fires when the de-replicated current count is zero or falls below 50% of `avg_over_time((min by (job) (radar_aggregator_bucket_count))[24h:30s])` for 15 minutes.

Compare the current count with its 24-hour average. Check privacy thresholds,
usage snapshot availability, and invalid bucket classifications before
changing thresholds.

## RadarQuotaSnapshotSuccessBelowTarget

Use aggregation result/reason, duration, bucket count, and Redis write errors.
The target is at least 99% successful normal-cycle snapshots.

## RadarCoreAvailabilityBelowTarget

Break down `radar_http_requests_total` by route/status and correlate 5xx routes
with Redis read errors. The target is at least 99.5% availability.

## RadarAPI5xxRateHigh

Identify the fixed route with 5xx responses, then correlate it with Redis read
errors and source freshness. Never add request query values to metric labels.

## RadarAPIP95AboveTarget

Break down the duration histogram by fixed route. Validate Redis latency and
payload size; the cached endpoint target is p95 below 100ms.

## RadarRedisErrors

Use `access` and `operation` to separate reads, writes, and locks. Check Redis
availability, timeout, memory pressure, and Lua script compatibility without
copying internal keys into tickets or metrics.

## RadarCacheMemoryHigh

Inspect the fixed cache family breakdown. This gauge sums Redis `MEMORY USAGE`
for active Radar keys by family, including quota history and AA performance
models. Redis uses its bounded default sampling for nested values, so the gauge
tracks allocator pressure and growth without a full history scan; use protected
Redis-side tooling with exhaustive sampling only for one-off exact diagnosis.

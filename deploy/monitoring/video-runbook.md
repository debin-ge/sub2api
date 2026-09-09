# Video Platform Alerts

Thresholds in `video-alerts.yml` are conservative defaults. Recalibrate them after staging load tests and record the approved values in deployment configuration.

The native release exposes only create/list/get/content/delete. Webhooks and callbacks are disabled; do not enable them or configure a signing secret to resolve a video incident. Assign an alert receiver and an incident owner before enabling creation. Alert rules must be installed in the existing Prometheus configuration; this repository does not deploy or configure the production receiver automatically.

## VideoSubmissionUnknownRate

An unconfirmed submission outcome is settled automatically: the task lands on `failed` with `last_error_code = submission_unknown`, the hold is released in full, and `submission_unknown_at` records that the outcome was never confirmed upstream. There is no operator decision to make and no queue to drain — this alert is a rate signal, not a stuck-money signal.

Investigate the cause rather than the individual tasks: check Provider Create latency and error rates, the submit timeout, and network egress. Disable new video creation if the rate keeps climbing, because every unconfirmed submission may have created an upstream job that this platform will never poll or bill. Cross-check with the Provider console before assuming nothing was created; do not retry Create automatically.

## VideoSettlementPendingTooOld

Check the billing outbox, worker logs, database leases, and the task price snapshot. Retry settlement through the safe admin action. Reconcile balance and frozen balance before manual resolution.

## VideoSpoolCleanupFailure

Stop new creation if cleanup failures continue. Check filesystem permissions and capacity without opening media files. Restore cleanup, run an orphan sweep, then confirm bytes and orphan candidates decline.

## VideoSpoolDiskHigh

Disable new creation before the filesystem fills. Confirm active sessions, encrypted bytes, orphan TTL, and cleanup failures. Do not delete active request directories.

## VideoPollPendingTooOld

Check the oldest queued/in-progress task, Provider Get errors, the poll worker, account identity and database leases. Compare elapsed time with the agreed model timeout. Do not repeat Create when the upstream outcome is unknown.

## VideoDeletePendingTooOld

Check `delete_pending_current`, the original delete-request event, the deletion error and account connectivity. Retry only through the existing task delete action after correcting the cause. Do not reset billing or delete database rows. The age metric uses the original request event so retries do not hide a stalled deletion.

## Baseline Capacity Check

Run the release integration suite with its isolated database first. Before production, agree a concurrency ceiling and measure budget-query latency, database pool occupancy, lock waits and content-proxy errors under that load. Inspect `pg_stat_activity`/`pg_locks` with an approved read-only role; do not export SQL text or payloads. An isolated test result is not a production capacity guarantee. Settlement age covers tasks waiting for the capture/release outbox to finish; use `deploy/video-smoke-reconcile.sql` to inspect the specific capture receipt.

## VideoContentProxy5xx

Check Provider status, response-header timeout, content concurrency, and redirect validation. Keep task billing unchanged; download failures do not trigger refunds.

## VideoCapabilityProbeFailures

Check recent account credential or permission changes and OpenAI service status. Re-run the read-only probe for affected accounts. Use manual override only with independently verified endpoint access.

## VideoDedicatedCredentialDisclosure

Confirm the task owner, account owner, account type, and all three disclosure-policy levels. Treat any disclosure from a shared account or any response/log containing the value as a credential incident and rotate it immediately.

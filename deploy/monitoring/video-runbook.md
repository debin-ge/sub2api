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

## FlovaEmptyCompletions

Flova finished three or more runs in 15 minutes without a video artifact. The agent
ended its round without generating: the account is usually out of credits, the
requested model is gated for its plan, or the model refuses text-only requests
(Kling 3.0 Omni needs a reference image). Check the account's `flova_credits`
snapshot and subscription in the admin account list, run "test connection" to
refresh them, and read the task error message (it carries the agent's own
explanation). Three consecutive empty completions pause the account for 15
minutes automatically.

## FlovaPriceDrift

A `high_cost_model_confirm` quote exceeded the configured credits-per-second for
that model and resolution by more than the account's `price_drift_tolerance`.
Flova changed its pricing: compare the quoted `credits_per_second` in the task
metadata with the price table in `flova_video_catalog.go`, update the group
`model_pricing` rules, then raise the tolerance or the table. Runs stay failed
and fully released; nothing is charged upstream until a quote is approved.

## Legacy Grok relay task tracking

Legacy Grok relay tasks use `grok_video_tasks` and `grok_video_task_events`, independently of the
managed hold/refund worker. Admin task queries merge both sources in PostgreSQL.

Rollout: apply migration 285 and deploy every instance before enabling
`grok_video_tracking.enabled`. Then enable `grok_video_tracking.worker_enabled`.
Both default to false. Existing tracked tasks always use their database binding,
including after new-task tracking is disabled. Do not roll back to a binary that
only knows Redis bindings while tracked jobs exist. Stopping the Grok worker does
not stop the durable billing outbox; accepted billing intents still recover.

Use `source=grok_relay` in the admin task list. Refresh/resume performs a leased
observation; settlement retry only replays an already prepared billing snapshot.
A `blocked` task with no snapshot cannot be billed by guessing its resolution or
subscription. Imported legacy tasks with missing attribution must be investigated.

`submission_unknown` means submission may have reached the upstream. Never
resubmit it automatically. A persistence-failure log includes the local task ID
and, when known, provider task ID for reconciliation. There is no general recovery
for a process crash between upstream acceptance and storing its task ID.

Monitor `grok_video_events_total` (persist_failed, submission_unknown, sync_paused,
settlement_failed, settlement_blocked), `grok_video_observation_age_seconds`, and
`grok_video_settlement_delay_seconds`. Admin overview also exposes `grok_sync` and
`grok_settlement` state counts. Task age exceeding 24 hours pauses observation;
it does not establish upstream expiry. Resume grants a new observation window.

Generation completion and customer settlement are separate. Upstream
`cost_in_usd_ticks` is provider cost metadata, not the customer's charge. The
billing identity remains `grok-video:<provider-task-id>` plus API key ID. Durable
usage records/outbox are authoritative for recovery; never clear dedup records to
retry a settlement. Logs and events must not contain bearer tokens, full prompts,
or signed media URLs.


## Grok managed holds and delivery

New Grok tasks can use the same `video_tasks`, wallet hold, budget reservation,
worker and settlement outbox as ByteDance. Keep `gateway.video.enabled` and
`gateway.video.creation_enabled` enabled, and opt into
`gateway.video.grok_managed_enabled`. All three default to false. Existing relay
rows remain in `grok_video_tasks`; never manufacture a hold for a previously
submitted or already billed relay task.

Before cutover, run the read-only admin preview for each affected group/account
and the models, resolutions, operations and user multipliers actually in use:

```bash
curl "$BASE_URL/api/v1/admin/videos/grok-migration/preview" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{"api_key_id":123,"account_id":456,"cases":[
    {"model":"grok-imagine-video-1.5","operation":"generate","resolution":"480p","seconds":1},
    {"model":"grok-imagine-video-1.5","operation":"generate","resolution":"480p","seconds":8},
    {"model":"grok-imagine-video-1.5","operation":"generate","resolution":"480p","seconds":15}
  ]}'
```

`proposed_model_pricing` optionally evaluates an edited group model-price array
without applying it. The response compares live legacy and managed pricing and
also checks `legacy_after_cost` so the proposed configuration cannot silently
reprice still-running legacy jobs. It returns a fingerprint plus scoped
`VideoPricingConfig` rule suggestions; these
are not a whole group configuration and are never written automatically. Merge
and deduplicate compatible suggestions using the existing model-price editor.
Repeat the preview after saving. Do not enable a group with unexplained price
mismatches or missing managed pricing. Subscription groups are unsupported by the
managed video engine. Simple-mode legacy generation was usage-only; managed
video holds still reserve real balance, so a mode change requires deliberate
operator review. No implicit fallback to the legacy price fields is provided.

Editing and extension need separate samples: pass verified `source_seconds`,
output resolution and the requested extension delta. Legacy billing used integer,
clamped output duration and request resolution (default 480p for derived calls);
the report exposes this explicitly. Do not change the retail rate to disguise a
correction from total-output billing to extension-only billing.

Pause new Grok POST requests during configuration cutover, leaving status and
content reads available. Deploy all binaries first, finish price checks, then
switch all instances to the same creation mode before reopening POST traffic.
Do not run mixed creation modes. For an emergency pause after cutover, leave
`grok_managed_enabled` on and turn off `creation_enabled` (this pauses all managed
providers); do not revert to legacy new-task creation or an old binary. Existing
native receipts and status/content reads remain valid. Keep the legacy worker
running until old generation and settlement work has drained; historical blocked
rows remain diagnostic records, not grounds for guessing a charge.

The Grok creation response keeps `request_id` as a UUID, now derived from the
local managed task ID; the upstream ID is internal. `pending` includes settlement
in progress. `done` and signed download URLs are exposed only after captured
funds. The authenticated content endpoint performs the same check, so it cannot
bypass pending settlement. Retries must reuse `Idempotency-Key`; receipts bind to
the original engine before reevaluating composite routes or prices.

Grok edits/extensions freeze and measure the exact source bytes before any hold.
The runtime needs `ffprobe` (provided by ffmpeg in the Docker images). The probe
has a time/output/allocation budget, decodes the video timeline instead of trusting
container duration headers, disables external media references, and uses an
unlinked private file descriptor. Failure to verify the source rejects the request
before payment. Extension billing is the output duration minus verified source
duration; source clips are never silently clamped to ordinary generation limits.

Monitor the existing `video_platform_*` counters with `provider="grok"` alongside
ByteDance: held amount, settlement age, submission reconciliation, releases and
content access. Migration 286 adds a shared creation receipt across both tables. Idempotent
legacy requests retain a minimal durable receipt even when optional full tracking
is off; the prompt is omitted in that mode. A competing engine gets a conflict
before any second submission or hold. Existing cross-engine duplicates cause the
migration to stop rather than guess which charge should survive.

New managed tasks never invoke the old `grok-video:<id>` completion
billing path. An upstream HTTP error during polling is not a terminal generation
result and must not immediately release a known task's hold.

### Docker compilation reports missing Grok fields

Errors such as `GatewayVideoConfig.GrokManagedEnabled undefined` or
`VideoCreateRequest.Grok undefined` indicate that the build source contains the
Grok adapter without its matching shared configuration/types. Include both new
files and modifications to existing config, provider, task, pricing, repository,
handler and Wire files from the same change. Copying only `grok_*.go` is not a
complete upgrade. Uncommitted local modifications are absent from remote Git
build contexts and from another checkout.

Build from the complete repository root (the final `.` is the source context):

```bash
docker build --platform linux/arm64 -t sub2api:local .
# Or use the repository's local-source Compose definition:
docker compose -f deploy/docker-compose.dev.yml build sub2api
```

For a backend-only verification with the same embedded frontend build, add
`--target backend-builder` to the Docker command. Changing build flags or clearing
Docker caches cannot add missing source declarations. Do not deploy a partial
build or turn off tracking as a workaround for a compile error.

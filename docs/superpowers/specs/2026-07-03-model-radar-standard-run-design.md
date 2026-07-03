# Model Radar Standard Run Design

Date: 2026-07-03

## Context

AI Model Radar already has the core benchmark model:

- targets: model plus channel pairs that participate in radar tests
- tasks: prompts plus verifier configuration
- runs: snapshots of selected targets and tasks
- schedules: recurring runs
- public snapshots and trends

The current admin workflow is too open-ended for first use. An admin must understand targets, tasks, verifier types, run creation, manual processing, and schedules before seeing a useful radar result. Some UI copy also still references older concepts such as suite/profile/budget, which does not match the current fixed-task-set design.

## Goal

Make the normal workflow clear and low-effort:

1. Admin adds or keeps enabled benchmark targets.
2. Admin clicks one primary action to run a standard radar test.
3. The system creates any missing standard tasks, runs those standard tasks against all enabled targets by default, and processes the run immediately.
4. Advanced task editing and manual run controls remain available but are not the default path.

## Non-Goals

- Do not add a default recurring schedule in this change.
- Do not introduce LLM-judge scoring or semantic code execution.
- Do not remove the existing target, task, run, schedule, or run detail pages.
- Do not make benchmark targets automatic, because model names and channel IDs depend on local channel setup.

## Standard Task Set

Add a built-in standard task set in backend service code. The task set is intentionally small and compatible with the existing verifier types.

Initial standard tasks:

| Sort | Title | Type | Difficulty | Verifier |
| --- | --- | --- | --- | --- |
| 100 | Standard Reasoning - Arithmetic | reasoning | easy | normalized_match, expected `58` |
| 110 | Standard Reasoning - Multiple Choice | reasoning | easy | normalized_match, expected `B` |
| 120 | Standard Structured Output - JSON Keys | reasoning | medium | json_object, required keys `answer`, `explanation`, `confidence` |
| 130 | Standard Coding - Function Envelope | coding | medium | json_object, required keys `language`, `code`, `notes` |
| 140 | Standard Writing - Concise Summary | writing | easy | json_object, required keys `summary`, `tone`, `constraints_met` |
| 150 | Standard Instruction Following - Label | reasoning | easy | normalized_match, expected `SAFE` |

The exact prompts must instruct the model to return the format required by the verifier. Standard tasks are public-prompt safe and have weight `1`.

Idempotence rule:

- A standard task is identified by its exact title.
- Applying the standard task set creates missing standard tasks.
- Applying the standard task set does not overwrite existing standard tasks, so admin edits are preserved.
- If an admin renames a standard task, applying the set treats the original title as missing and recreates it.

## Backend Design

Add a service-level module, for example `benchmark_standard_tasks.go`, with:

- standard task definitions
- helper to return standard titles
- `EnsureStandardTasks(ctx)` to create missing standard tasks
- `CreateStandardRun(ctx, input)` to create a run from enabled standard tasks

Repository support:

- add `ListTasksByTitles(ctx, titles []string)` so `EnsureStandardTasks` can check existence without scanning every page

Admin API additions:

- `POST /admin/benchmark/tasks/standard/apply`
  - creates missing standard tasks
  - response: `{ created_count, existing_count, enabled_count, tasks }`
- `POST /admin/benchmark/runs/standard`
  - ensures the standard task set first
  - default target selection: all enabled targets
  - optional `target_ids` selects a subset of enabled targets
  - optional `task_count` limits the first N enabled standard tasks by sort order
  - default `process_immediately`: `true`
  - response: the created benchmark run

The existing `POST /admin/benchmark/runs` endpoint remains available for manual and advanced runs.

## Data Flow

Standard run button:

1. Frontend calls `POST /admin/benchmark/runs/standard` with no body for the default path.
2. Backend ensures standard tasks exist.
3. Backend selects all enabled targets.
4. Backend selects enabled standard tasks ordered by sort order, using all standard tasks unless `task_count` is provided.
5. Backend creates the run with snapshots.
6. If `process_immediately` is true, the handler processes the run after creation using the existing processor path.
7. Frontend shows the created run and refreshes the run list.

Task page standard apply button:

1. Frontend calls `POST /admin/benchmark/tasks/standard/apply`.
2. Backend creates missing standard tasks and returns counts.
3. Frontend reloads the task list and shows a success message.

## Frontend Design

Runs page:

- Rename the primary create section to a standard run action.
- Default visible action: `Run Standard Radar Test`.
- Show a short summary of the default behavior: all enabled targets, standard task set, process immediately.
- Move target checkboxes, task count, process-immediately toggle, and trigger type into an advanced options area.
- Default advanced values:
  - no targets selected means all enabled targets
  - task count empty or `0` means all standard tasks
  - process immediately is true
  - trigger type is not shown in the default path and remains `manual`

Tasks page:

- Add `Apply Standard Task Set`.
- Empty state should direct admins to apply the standard task set.
- Keep manual task editing as an advanced management path.

Copy cleanup:

- Replace visible stale terms such as profile, suite, budget, sample, and score snapshot where they refer to removed concepts.
- Use consistent terms: target, standard task set, run, result, public snapshot.

## Error Handling

- If no enabled targets exist, standard run returns the existing no-target error and the frontend explains that at least one target is required.
- If all standard tasks are disabled, standard run returns a clear no-standard-task error.
- If standard task creation partially fails, the request fails and does not create a run.
- Repeated standard-task apply calls are safe and must not duplicate tasks.
- Processing errors after run creation are surfaced as existing process errors; the created run remains visible for inspection.

## Testing

Backend unit tests:

- standard task definitions are non-empty, unique by title, enabled by default, and use supported verifier types
- `EnsureStandardTasks` creates missing tasks and is idempotent
- `CreateStandardRun` ensures tasks and creates a run from enabled standard tasks
- standard run returns a clear error when there are no enabled targets

Backend handler tests:

- `POST /admin/benchmark/tasks/standard/apply` returns created/existing/enabled counts
- `POST /admin/benchmark/runs/standard` creates a run and triggers immediate processing by default

Frontend API tests:

- admin benchmark API exposes `applyStandardTasks`
- admin benchmark API exposes `createStandardRun`

Frontend view tests:

- runs page default action calls `createStandardRun` with default payload
- advanced options pass selected targets, task count, and process-immediately value
- tasks page standard apply button calls the API and reloads the task list

## Acceptance Criteria

- A fresh deployment with benchmark targets but no tasks can create a standard radar run with one click.
- Re-clicking standard task apply does not create duplicate standard tasks.
- Manual task and run workflows still work.
- The default admin path no longer requires understanding verifier JSON before running a radar test.
- User-facing benchmark admin copy no longer relies on stale suite/profile terminology for the current workflow.

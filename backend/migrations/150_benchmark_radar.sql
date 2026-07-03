-- AI Model Radar benchmark persistence schema.
-- Fixed task set + time-trend design: global tasks, schedules carry their own
-- run config, per-run target score doubles as the trend data point.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS benchmark_targets (
    id BIGSERIAL PRIMARY KEY,
    model_name VARCHAR(200) NOT NULL,
    channel_id BIGINT NOT NULL,
    display_name VARCHAR(200),
    channel_name_snapshot VARCHAR(200),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    public_visible BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS benchmark_targets_model_channel_key
    ON benchmark_targets (model_name, channel_id);
CREATE INDEX IF NOT EXISTS benchmark_targets_enabled_public_visible_idx
    ON benchmark_targets (enabled, public_visible);
CREATE INDEX IF NOT EXISTS benchmark_targets_channel_id_idx
    ON benchmark_targets (channel_id);

COMMENT ON COLUMN benchmark_targets.channel_id IS 'Raw-SQL channel id; service/repository validates existence because channels has no Ent schema.';

CREATE TABLE IF NOT EXISTS benchmark_tasks (
    id BIGSERIAL PRIMARY KEY,
    title VARCHAR(200) NOT NULL,
    type VARCHAR(50) NOT NULL,
    difficulty VARCHAR(50),
    prompt TEXT NOT NULL,
    input_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    expected_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    verifier_type VARCHAR(50) NOT NULL,
    verifier_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    weight NUMERIC(10,4) NOT NULL DEFAULT 1,
    public_prompt BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_tasks_type_idx
    ON benchmark_tasks (type);
CREATE INDEX IF NOT EXISTS benchmark_tasks_enabled_idx
    ON benchmark_tasks (enabled);
CREATE INDEX IF NOT EXISTS benchmark_tasks_enabled_sort_idx
    ON benchmark_tasks (enabled, sort_order);

CREATE TABLE IF NOT EXISTS benchmark_schedules (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    cron_expr VARCHAR(100) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    target_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    task_count INT NOT NULL DEFAULT 0,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_schedules_enabled_idx
    ON benchmark_schedules (enabled);
CREATE INDEX IF NOT EXISTS benchmark_schedules_next_run_at_idx
    ON benchmark_schedules (next_run_at);

CREATE TABLE IF NOT EXISTS benchmark_runs (
    id BIGSERIAL PRIMARY KEY,
    status VARCHAR(32) NOT NULL,
    trigger_type VARCHAR(32) NOT NULL,
    schedule_id BIGINT,
    task_count INT NOT NULL DEFAULT 0,
    planned_target_count INT NOT NULL DEFAULT 0,
    planned_task_count INT NOT NULL DEFAULT 0,
    planned_result_count INT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    error_message TEXT,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_runs_status_idx
    ON benchmark_runs (status);
CREATE INDEX IF NOT EXISTS benchmark_runs_schedule_id_idx
    ON benchmark_runs (schedule_id);
CREATE INDEX IF NOT EXISTS benchmark_runs_created_at_idx
    ON benchmark_runs (created_at);

CREATE TABLE IF NOT EXISTS benchmark_run_targets (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    target_id BIGINT NOT NULL REFERENCES benchmark_targets(id) ON DELETE RESTRICT,
    model_name VARCHAR(200) NOT NULL,
    channel_id BIGINT NOT NULL,
    display_name_snapshot VARCHAR(200),
    channel_name_snapshot VARCHAR(200),
    target_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_run_targets_run_id_idx
    ON benchmark_run_targets (run_id);
CREATE INDEX IF NOT EXISTS benchmark_run_targets_target_id_idx
    ON benchmark_run_targets (target_id);
CREATE INDEX IF NOT EXISTS benchmark_run_targets_run_channel_idx
    ON benchmark_run_targets (run_id, channel_id);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_run_targets_run_target_key
    ON benchmark_run_targets (run_id, target_id);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_run_targets_run_id_id_key
    ON benchmark_run_targets (run_id, id);

CREATE TABLE IF NOT EXISTS benchmark_run_tasks (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    task_id BIGINT NOT NULL REFERENCES benchmark_tasks(id) ON DELETE RESTRICT,
    task_order INT NOT NULL DEFAULT 0,
    type VARCHAR(50) NOT NULL,
    difficulty VARCHAR(50),
    weight_snapshot NUMERIC(10,4) NOT NULL DEFAULT 1,
    prompt_snapshot TEXT NOT NULL,
    verifier_type_snapshot VARCHAR(50) NOT NULL,
    verifier_config_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    task_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_run_tasks_run_id_idx
    ON benchmark_run_tasks (run_id);
CREATE INDEX IF NOT EXISTS benchmark_run_tasks_task_id_idx
    ON benchmark_run_tasks (task_id);
CREATE INDEX IF NOT EXISTS benchmark_run_tasks_run_type_idx
    ON benchmark_run_tasks (run_id, type);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_run_tasks_run_task_key
    ON benchmark_run_tasks (run_id, task_id);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_run_tasks_run_id_id_key
    ON benchmark_run_tasks (run_id, id);

CREATE TABLE IF NOT EXISTS benchmark_results (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    run_task_id BIGINT NOT NULL REFERENCES benchmark_run_tasks(id) ON DELETE CASCADE,
    run_target_id BIGINT NOT NULL REFERENCES benchmark_run_targets(id) ON DELETE CASCADE,
    request_id VARCHAR(64),
    status VARCHAR(32) NOT NULL,
    normalized_score NUMERIC(10,4),
    evaluator_type VARCHAR(50),
    evaluator_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    latency_ms INT,
    prompt_tokens INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens INT NOT NULL DEFAULT 0,
    estimated_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    raw_response JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code VARCHAR(100),
    error_message TEXT,
    attempt_count INT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT benchmark_results_run_target_same_run_fkey
        FOREIGN KEY (run_id, run_target_id)
        REFERENCES benchmark_run_targets(run_id, id)
        ON DELETE CASCADE,
    CONSTRAINT benchmark_results_run_task_same_run_fkey
        FOREIGN KEY (run_id, run_task_id)
        REFERENCES benchmark_run_tasks(run_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS benchmark_results_run_id_idx
    ON benchmark_results (run_id);
CREATE INDEX IF NOT EXISTS benchmark_results_run_target_idx
    ON benchmark_results (run_id, run_target_id);
CREATE INDEX IF NOT EXISTS benchmark_results_run_task_idx
    ON benchmark_results (run_id, run_task_id);
CREATE INDEX IF NOT EXISTS benchmark_results_request_id_idx
    ON benchmark_results (request_id);
CREATE INDEX IF NOT EXISTS benchmark_results_status_idx
    ON benchmark_results (status);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_results_run_task_target_key
    ON benchmark_results (run_task_id, run_target_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'benchmark_results_run_target_same_run_fkey'
          AND conrelid = 'benchmark_results'::regclass
    ) THEN
        ALTER TABLE benchmark_results
            ADD CONSTRAINT benchmark_results_run_target_same_run_fkey
            FOREIGN KEY (run_id, run_target_id)
            REFERENCES benchmark_run_targets(run_id, id)
            ON DELETE CASCADE;
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'benchmark_results_run_task_same_run_fkey'
          AND conrelid = 'benchmark_results'::regclass
    ) THEN
        ALTER TABLE benchmark_results
            ADD CONSTRAINT benchmark_results_run_task_same_run_fkey
            FOREIGN KEY (run_id, run_task_id)
            REFERENCES benchmark_run_tasks(run_id, id)
            ON DELETE CASCADE;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS benchmark_target_scores (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    run_target_id BIGINT NOT NULL REFERENCES benchmark_run_targets(id) ON DELETE CASCADE,
    model_name VARCHAR(200) NOT NULL,
    channel_id BIGINT NOT NULL,
    overall_score NUMERIC(10,4) NOT NULL DEFAULT 0,
    passed_count INT NOT NULL DEFAULT 0,
    total_count INT NOT NULL DEFAULT 0,
    dimension_scores JSONB NOT NULL DEFAULT '{}'::jsonb,
    avg_latency_ms NUMERIC(12,4),
    avg_total_tokens NUMERIC(20,4),
    total_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    invalid_reason_breakdown JSONB NOT NULL DEFAULT '{}'::jsonb,
    finished_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT benchmark_target_scores_run_target_same_run_fkey
        FOREIGN KEY (run_id, run_target_id)
        REFERENCES benchmark_run_targets(run_id, id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS benchmark_target_scores_run_id_idx
    ON benchmark_target_scores (run_id);
CREATE INDEX IF NOT EXISTS benchmark_target_scores_run_overall_score_idx
    ON benchmark_target_scores (run_id, overall_score);
CREATE INDEX IF NOT EXISTS benchmark_target_scores_trend_idx
    ON benchmark_target_scores (model_name, channel_id, finished_at);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_target_scores_run_target_key
    ON benchmark_target_scores (run_id, run_target_id);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'benchmark_target_scores_run_target_same_run_fkey'
          AND conrelid = 'benchmark_target_scores'::regclass
    ) THEN
        ALTER TABLE benchmark_target_scores
            ADD CONSTRAINT benchmark_target_scores_run_target_same_run_fkey
            FOREIGN KEY (run_id, run_target_id)
            REFERENCES benchmark_run_targets(run_id, id)
            ON DELETE CASCADE;
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS benchmark_public_snapshots (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_public_snapshots_published_at_idx
    ON benchmark_public_snapshots (published_at);
CREATE INDEX IF NOT EXISTS benchmark_public_snapshots_run_id_idx
    ON benchmark_public_snapshots (run_id);

COMMENT ON TABLE benchmark_public_snapshots IS 'Sanitized AI Model Radar public payloads; raw responses, verifier outputs, and errors stay in private benchmark tables.';

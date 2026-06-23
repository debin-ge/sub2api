-- AI Model Radar benchmark persistence schema.

SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

CREATE TABLE IF NOT EXISTS benchmark_targets (
    id BIGSERIAL PRIMARY KEY,
    model_name VARCHAR(200) NOT NULL,
    channel_id BIGINT NOT NULL,
    display_name VARCHAR(200),
    provider_snapshot VARCHAR(100),
    channel_name_snapshot VARCHAR(200),
    supported_task_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    max_concurrency INT NOT NULL DEFAULT 1,
    per_run_budget NUMERIC(20,10),
    daily_budget NUMERIC(20,10),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    public_visible BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS benchmark_targets_model_channel_key
    ON benchmark_targets (model_name, channel_id);
CREATE INDEX IF NOT EXISTS benchmark_targets_enabled_public_visible_idx
    ON benchmark_targets (enabled, public_visible);
CREATE INDEX IF NOT EXISTS benchmark_targets_channel_id_idx
    ON benchmark_targets (channel_id);

CREATE TABLE IF NOT EXISTS benchmark_suites (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    slug VARCHAR(100) NOT NULL,
    description TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    public_visible BOOLEAN NOT NULL DEFAULT FALSE,
    default_profile_id BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS benchmark_suites_slug_key
    ON benchmark_suites (slug);
CREATE INDEX IF NOT EXISTS benchmark_suites_enabled_public_visible_idx
    ON benchmark_suites (enabled, public_visible);

CREATE TABLE IF NOT EXISTS benchmark_tasks (
    id BIGSERIAL PRIMARY KEY,
    suite_id BIGINT NOT NULL REFERENCES benchmark_suites(id) ON DELETE CASCADE,
    title VARCHAR(200) NOT NULL,
    type VARCHAR(50) NOT NULL,
    category VARCHAR(100),
    difficulty VARCHAR(50),
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    prompt TEXT NOT NULL,
    input_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    expected_output JSONB NOT NULL DEFAULT '{}'::jsonb,
    verifier_type VARCHAR(50) NOT NULL,
    verifier_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    weight NUMERIC(10,4) NOT NULL DEFAULT 1,
    min_scale VARCHAR(20) NOT NULL DEFAULT 'small',
    public_prompt BOOLEAN NOT NULL DEFAULT FALSE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_tasks_suite_id_idx
    ON benchmark_tasks (suite_id);
CREATE INDEX IF NOT EXISTS benchmark_tasks_type_idx
    ON benchmark_tasks (type);
CREATE INDEX IF NOT EXISTS benchmark_tasks_difficulty_idx
    ON benchmark_tasks (difficulty);
CREATE INDEX IF NOT EXISTS benchmark_tasks_enabled_idx
    ON benchmark_tasks (enabled);
CREATE INDEX IF NOT EXISTS benchmark_tasks_suite_type_enabled_idx
    ON benchmark_tasks (suite_id, type, enabled);

CREATE TABLE IF NOT EXISTS benchmark_profiles (
    id BIGSERIAL PRIMARY KEY,
    suite_id BIGINT NOT NULL REFERENCES benchmark_suites(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    target_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    task_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    task_scale VARCHAR(20) NOT NULL DEFAULT 'medium',
    task_count_limit INT,
    per_type_limit JSONB NOT NULL DEFAULT '{}'::jsonb,
    difficulty_filter JSONB NOT NULL DEFAULT '[]'::jsonb,
    tag_filter JSONB NOT NULL DEFAULT '[]'::jsonb,
    sampling_strategy VARCHAR(50) NOT NULL,
    selection_seed BIGINT,
    runtime_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    scoring_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_profiles_suite_id_idx
    ON benchmark_profiles (suite_id);
CREATE INDEX IF NOT EXISTS benchmark_profiles_enabled_idx
    ON benchmark_profiles (enabled);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'benchmark_suites_default_profile_id_fkey'
          AND conrelid = 'benchmark_suites'::regclass
    ) THEN
        ALTER TABLE benchmark_suites
            ADD CONSTRAINT benchmark_suites_default_profile_id_fkey
            FOREIGN KEY (default_profile_id)
            REFERENCES benchmark_profiles(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS benchmark_suites_default_profile_id_idx
    ON benchmark_suites (default_profile_id);

CREATE TABLE IF NOT EXISTS benchmark_schedules (
    id BIGSERIAL PRIMARY KEY,
    profile_id BIGINT NOT NULL REFERENCES benchmark_profiles(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    cron_expr VARCHAR(100) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_schedules_enabled_idx
    ON benchmark_schedules (enabled);
CREATE INDEX IF NOT EXISTS benchmark_schedules_next_run_at_idx
    ON benchmark_schedules (next_run_at);
CREATE INDEX IF NOT EXISTS benchmark_schedules_profile_id_idx
    ON benchmark_schedules (profile_id);

CREATE TABLE IF NOT EXISTS benchmark_runs (
    id BIGSERIAL PRIMARY KEY,
    suite_id BIGINT NOT NULL REFERENCES benchmark_suites(id) ON DELETE RESTRICT,
    profile_id BIGINT NOT NULL REFERENCES benchmark_profiles(id) ON DELETE RESTRICT,
    status VARCHAR(32) NOT NULL,
    trigger_type VARCHAR(32) NOT NULL,
    task_scale VARCHAR(20) NOT NULL DEFAULT 'medium',
    task_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    selection_seed BIGINT,
    planned_target_count INT NOT NULL DEFAULT 0,
    planned_task_count INT NOT NULL DEFAULT 0,
    planned_result_count INT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    config_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_runs_suite_id_idx
    ON benchmark_runs (suite_id);
CREATE INDEX IF NOT EXISTS benchmark_runs_profile_id_idx
    ON benchmark_runs (profile_id);
CREATE INDEX IF NOT EXISTS benchmark_runs_status_idx
    ON benchmark_runs (status);
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
    provider_snapshot VARCHAR(100),
    target_order INT NOT NULL DEFAULT 0,
    config_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
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

CREATE TABLE IF NOT EXISTS benchmark_run_tasks (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    task_id BIGINT NOT NULL REFERENCES benchmark_tasks(id) ON DELETE RESTRICT,
    task_order INT NOT NULL DEFAULT 0,
    type VARCHAR(50) NOT NULL,
    category VARCHAR(100),
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

CREATE TABLE IF NOT EXISTS benchmark_results (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    run_task_id BIGINT NOT NULL REFERENCES benchmark_run_tasks(id) ON DELETE CASCADE,
    run_target_id BIGINT NOT NULL REFERENCES benchmark_run_targets(id) ON DELETE CASCADE,
    request_id VARCHAR(64),
    status VARCHAR(32) NOT NULL,
    score NUMERIC(10,4),
    max_score NUMERIC(10,4),
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
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

CREATE TABLE IF NOT EXISTS benchmark_score_snapshots (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    run_target_id BIGINT NOT NULL REFERENCES benchmark_run_targets(id) ON DELETE CASCADE,
    overall_score NUMERIC(10,4) NOT NULL DEFAULT 0,
    dimension_scores JSONB NOT NULL DEFAULT '{}'::jsonb,
    planned_tasks INT NOT NULL DEFAULT 0,
    scored_tasks INT NOT NULL DEFAULT 0,
    invalid_tasks INT NOT NULL DEFAULT 0,
    coverage_rate NUMERIC(10,4) NOT NULL DEFAULT 0,
    confidence_level VARCHAR(20) NOT NULL DEFAULT 'low',
    insufficient_sample BOOLEAN NOT NULL DEFAULT FALSE,
    success_rate NUMERIC(10,4) NOT NULL DEFAULT 0,
    latency_p50_ms NUMERIC(12,4),
    latency_p95_ms NUMERIC(12,4),
    avg_total_tokens NUMERIC(20,4),
    estimated_cost NUMERIC(20,10) NOT NULL DEFAULT 0,
    invalid_reason_breakdown JSONB NOT NULL DEFAULT '{}'::jsonb,
    ranking_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_score_snapshots_run_id_idx
    ON benchmark_score_snapshots (run_id);
CREATE INDEX IF NOT EXISTS benchmark_score_snapshots_run_target_id_idx
    ON benchmark_score_snapshots (run_target_id);
CREATE INDEX IF NOT EXISTS benchmark_score_snapshots_run_overall_score_idx
    ON benchmark_score_snapshots (run_id, overall_score);
CREATE UNIQUE INDEX IF NOT EXISTS benchmark_score_snapshots_run_target_key
    ON benchmark_score_snapshots (run_id, run_target_id);

CREATE TABLE IF NOT EXISTS benchmark_public_snapshots (
    id BIGSERIAL PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    suite_id BIGINT NOT NULL REFERENCES benchmark_suites(id) ON DELETE RESTRICT,
    profile_id BIGINT NOT NULL REFERENCES benchmark_profiles(id) ON DELETE RESTRICT,
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS benchmark_public_snapshots_published_at_idx
    ON benchmark_public_snapshots (published_at);
CREATE INDEX IF NOT EXISTS benchmark_public_snapshots_run_id_idx
    ON benchmark_public_snapshots (run_id);

COMMENT ON TABLE benchmark_public_snapshots IS 'Sanitized AI Model Radar public payloads; raw responses, verifier outputs, and errors stay in private benchmark tables.';

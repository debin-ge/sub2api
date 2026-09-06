SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';

ALTER TABLE channel_pricing_intervals
    ADD COLUMN IF NOT EXISTS conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS billing_unit VARCHAR(32),
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

ALTER TABLE channel_pricing_intervals
    ALTER COLUMN tier_label TYPE VARCHAR(128);

ALTER TABLE channel_account_stats_pricing_intervals
    ADD COLUMN IF NOT EXISTS conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS billing_unit VARCHAR(32),
    ADD COLUMN IF NOT EXISTS priority INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS valid_until TIMESTAMPTZ;

ALTER TABLE channel_account_stats_pricing_intervals
    ALTER COLUMN tier_label TYPE VARCHAR(128);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'channel_pricing_intervals_conditions_object_check'
          AND conrelid = 'channel_pricing_intervals'::regclass
    ) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_conditions_object_check
            CHECK (jsonb_typeof(conditions) = 'object');
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'channel_pricing_intervals_validity_check'
          AND conrelid = 'channel_pricing_intervals'::regclass
    ) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_validity_check
            CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until > valid_from);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'channel_pricing_intervals_billing_unit_check'
          AND conrelid = 'channel_pricing_intervals'::regclass
    ) THEN
        ALTER TABLE channel_pricing_intervals
            ADD CONSTRAINT channel_pricing_intervals_billing_unit_check
            CHECK (billing_unit IS NULL OR billing_unit IN ('request', 'second', 'token', 'video_token'));
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'account_stats_pricing_intervals_conditions_object_check'
          AND conrelid = 'channel_account_stats_pricing_intervals'::regclass
    ) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_conditions_object_check
            CHECK (jsonb_typeof(conditions) = 'object');
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'account_stats_pricing_intervals_validity_check'
          AND conrelid = 'channel_account_stats_pricing_intervals'::regclass
    ) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_validity_check
            CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until > valid_from);
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'account_stats_pricing_intervals_billing_unit_check'
          AND conrelid = 'channel_account_stats_pricing_intervals'::regclass
    ) THEN
        ALTER TABLE channel_account_stats_pricing_intervals
            ADD CONSTRAINT account_stats_pricing_intervals_billing_unit_check
            CHECK (billing_unit IS NULL OR billing_unit IN ('request', 'second', 'token', 'video_token'));
    END IF;
END $$;

COMMENT ON COLUMN channel_pricing_intervals.conditions IS 'Structured pricing predicates, including video operation, size, input type, audio, quality, and service tier';
COMMENT ON COLUMN channel_pricing_intervals.billing_unit IS 'Explicit unit for conditional pricing: request, second, token, or video_token; NULL preserves legacy behavior';
COMMENT ON COLUMN channel_pricing_intervals.priority IS 'Higher values win after all predicates match; equal-priority ambiguous rules fail closed';
COMMENT ON COLUMN channel_pricing_intervals.valid_from IS 'Inclusive rule activation time';
COMMENT ON COLUMN channel_pricing_intervals.valid_until IS 'Exclusive rule expiration time';

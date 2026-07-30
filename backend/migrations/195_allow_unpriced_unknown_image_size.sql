-- Pricing-unavailable image usage must retain the exact upstream-reported size
-- (for example 8K or 8192x8192) so it can be reviewed and repriced later.
-- Migration 172 correctly constrained settled image rows to known billable
-- tiers, but applying the same constraint to billing_state=1 makes the durable
-- billing transaction retry forever and loses the only settlement evidence.

ALTER TABLE usage_logs
    DROP CONSTRAINT IF EXISTS usage_logs_image_billing_size_check;

ALTER TABLE usage_logs
    ADD CONSTRAINT usage_logs_image_billing_size_check
    CHECK (
        image_count <= 0
        OR billing_mode = 'video'
        OR COALESCE(video_count, 0) > 0
        OR billing_state = 1
        OR (
            image_size IS NOT NULL
            AND image_size IN ('1K', '2K', '4K', 'mixed')
        )
    ) NOT VALID;

COMMENT ON CONSTRAINT usage_logs_image_billing_size_check ON usage_logs IS
    'Settled images require a known billing tier; pricing-unavailable rows retain unknown upstream sizes for recovery';

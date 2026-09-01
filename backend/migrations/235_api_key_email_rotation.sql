-- API key notification email binding and rotate-on-expiry controls.
-- All controls default off so existing keys keep their current behavior.

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS notification_email VARCHAR(320);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS notification_email_verified_at TIMESTAMPTZ;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS change_notify_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotate_on_expiry BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS validity_duration_seconds BIGINT;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS last_rotated_at TIMESTAMPTZ;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_version BIGINT NOT NULL DEFAULT 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'api_keys_validity_duration_positive'
          AND conrelid = 'api_keys'::regclass
    ) THEN
        ALTER TABLE api_keys
            ADD CONSTRAINT api_keys_validity_duration_positive
            CHECK (validity_duration_seconds IS NULL OR validity_duration_seconds > 0);
    END IF;
END;
$$;

CREATE INDEX IF NOT EXISTS idx_api_keys_due_rotation
    ON api_keys (expires_at, id)
    WHERE rotate_on_expiry = TRUE AND deleted_at IS NULL;

COMMENT ON COLUMN api_keys.notification_email IS 'Verified recipient for API key change and rotation notifications';
COMMENT ON COLUMN api_keys.notification_email_verified_at IS 'Verification time for the currently bound notification email';
COMMENT ON COLUMN api_keys.change_notify_enabled IS 'Send email when user-visible API key configuration changes';
COMMENT ON COLUMN api_keys.rotate_on_expiry IS 'Generate a new credential after the API key expires';
COMMENT ON COLUMN api_keys.validity_duration_seconds IS 'Validity duration reused for each automatic rotation';
COMMENT ON COLUMN api_keys.last_rotated_at IS 'Most recent successful automatic credential rotation';
COMMENT ON COLUMN api_keys.rotation_version IS 'Monotonic version used to deduplicate rotation and delivery';

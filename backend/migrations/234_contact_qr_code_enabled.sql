-- Keep the large QR image out of public settings reads and HTML injection.
-- Existing installations that already saved a QR code are backfilled as enabled.
INSERT INTO settings (key, value, updated_at)
SELECT
    'contact_qr_code_enabled',
    CASE
        WHEN EXISTS (
            SELECT 1
            FROM settings
            WHERE key = 'contact_qr_code'
              AND BTRIM(value) <> ''
        ) THEN 'true'
        ELSE 'false'
    END,
    NOW()
ON CONFLICT (key) DO NOTHING;

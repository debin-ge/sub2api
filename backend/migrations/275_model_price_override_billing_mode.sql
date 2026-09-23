-- Explicit billing mode for model price overrides. Selecting a mode makes the
-- override suppress inherited catalog prices that belong to the other modes, so
-- one model is billed by exactly one of token / image / video pricing.
-- Stored as its own column (not inside payload): payload is re-decoded with
-- DisallowUnknownFields, so a new JSON key would break older binaries on rollback.

ALTER TABLE model_price_overrides
    ADD COLUMN IF NOT EXISTS billing_mode VARCHAR(20) NOT NULL DEFAULT 'token';

ALTER TABLE model_price_overrides
    DROP CONSTRAINT IF EXISTS model_price_overrides_billing_mode_check;

ALTER TABLE model_price_overrides
    ADD CONSTRAINT model_price_overrides_billing_mode_check
    CHECK (billing_mode IN ('token', 'image', 'video'));

COMMENT ON COLUMN model_price_overrides.billing_mode IS
    'Billing mode of this override (token, image or video); prices of the other modes inherited from the catalog are suppressed';

-- Model price display currency. Currency is metadata for model-price management
-- and plaza rendering only; billing continues to use the stored numeric values.

ALTER TABLE model_price_overrides
    ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT 'USD';

UPDATE model_price_overrides
SET currency = UPPER(TRIM(currency))
WHERE currency <> UPPER(TRIM(currency));

ALTER TABLE model_price_overrides
    DROP CONSTRAINT IF EXISTS model_price_overrides_currency_check;

ALTER TABLE model_price_overrides
    ADD CONSTRAINT model_price_overrides_currency_check
    CHECK (currency IN ('USD', 'CNY'));

COMMENT ON COLUMN model_price_overrides.currency IS
    'Display currency for this override (USD or CNY); does not change billing arithmetic';

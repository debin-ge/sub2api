SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '10min';
LOCK TABLE video_tasks, grok_video_tasks IN SHARE ROW EXCLUSIVE MODE;

-- This receipt is independent of settlement: legacy jobs keep legacy billing,
-- while managed jobs keep their holds. A key may never launch on both engines.
CREATE TABLE IF NOT EXISTS grok_video_creation_receipts (
    user_id BIGINT NOT NULL,
    operation VARCHAR(32) NOT NULL,
    idempotency_key VARCHAR(255) NOT NULL,
    engine VARCHAR(16) NOT NULL CHECK (engine IN ('legacy', 'managed')),
    task_public_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, operation, idempotency_key)
);

-- Backfill is intentionally strict. Pre-existing cross-engine duplicates must
-- be reconciled, not silently assigned to whichever table happens to win.
INSERT INTO grok_video_creation_receipts(user_id,operation,idempotency_key,engine,task_public_id,created_at)
SELECT user_id,operation,
       'grok:' || encode(sha256(convert_to('grok-video-v1:' || user_id::text || ':' || api_key_id::text || ':' || operation || ':' || idempotency_key,'UTF8')),'hex'),
       'legacy',public_id,created_at
FROM grok_video_tasks WHERE idempotency_key IS NOT NULL AND idempotency_key <> ''
ON CONFLICT DO NOTHING;
INSERT INTO grok_video_creation_receipts(user_id,operation,idempotency_key,engine,task_public_id,created_at)
SELECT user_id,operation,idempotency_key,'managed',public_id,created_at
FROM video_tasks WHERE provider='grok' AND source='managed' AND idempotency_key IS NOT NULL AND idempotency_key <> ''
ON CONFLICT DO NOTHING;

DO $$
BEGIN
 IF EXISTS (
  SELECT 1 FROM grok_video_tasks g LEFT JOIN grok_video_creation_receipts r
  ON r.user_id=g.user_id AND r.operation=g.operation AND r.idempotency_key=
     'grok:' || encode(sha256(convert_to('grok-video-v1:' || g.user_id::text || ':' || g.api_key_id::text || ':' || g.operation || ':' || g.idempotency_key,'UTF8')),'hex')
  WHERE g.idempotency_key IS NOT NULL AND g.idempotency_key<>''
   AND (r.engine IS DISTINCT FROM 'legacy' OR r.task_public_id IS DISTINCT FROM g.public_id)
 ) OR EXISTS (
  SELECT 1 FROM video_tasks v LEFT JOIN grok_video_creation_receipts r
  ON r.user_id=v.user_id AND r.operation=v.operation AND r.idempotency_key=v.idempotency_key
  WHERE v.provider='grok' AND v.source='managed' AND v.idempotency_key IS NOT NULL AND v.idempotency_key<>''
   AND (r.engine IS DISTINCT FROM 'managed' OR r.task_public_id IS DISTINCT FROM v.public_id)
 ) THEN RAISE EXCEPTION 'cross-engine Grok task receipts require reconciliation'; END IF;
END;
$$;

CREATE OR REPLACE FUNCTION bind_grok_video_creation_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    receipt_key TEXT;
    receipt_engine TEXT;
    owner_engine TEXT;
    owner_task TEXT;
BEGIN
    IF NEW.idempotency_key IS NULL OR NEW.idempotency_key = '' THEN RETURN NEW; END IF;
    IF TG_TABLE_NAME = 'grok_video_tasks' THEN
        receipt_key := 'grok:' || encode(sha256(convert_to('grok-video-v1:' || NEW.user_id::text || ':' || NEW.api_key_id::text || ':' || NEW.operation || ':' || NEW.idempotency_key,'UTF8')),'hex');
        receipt_engine := 'legacy';
    ELSE
        IF NEW.provider <> 'grok' OR NEW.source <> 'managed' THEN RETURN NEW; END IF;
        receipt_key := NEW.idempotency_key;
        receipt_engine := 'managed';
    END IF;
    INSERT INTO grok_video_creation_receipts(user_id,operation,idempotency_key,engine,task_public_id)
    VALUES(NEW.user_id,NEW.operation,receipt_key,receipt_engine,NEW.public_id)
    ON CONFLICT DO NOTHING;
    SELECT engine,task_public_id INTO owner_engine,owner_task
    FROM grok_video_creation_receipts
    WHERE user_id=NEW.user_id AND operation=NEW.operation AND idempotency_key=receipt_key;
    IF NOT FOUND OR owner_engine <> receipt_engine OR owner_task <> NEW.public_id THEN
        RAISE EXCEPTION 'grok video creation receipt already belongs to another task'
          USING ERRCODE='23505', CONSTRAINT='grok_video_creation_receipt_conflict';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS bind_legacy_grok_creation_receipt ON grok_video_tasks;
CREATE TRIGGER bind_legacy_grok_creation_receipt BEFORE INSERT ON grok_video_tasks
FOR EACH ROW EXECUTE FUNCTION bind_grok_video_creation_receipt();
DROP TRIGGER IF EXISTS bind_managed_grok_creation_receipt ON video_tasks;
CREATE TRIGGER bind_managed_grok_creation_receipt BEFORE INSERT ON video_tasks
FOR EACH ROW EXECUTE FUNCTION bind_grok_video_creation_receipt();

CREATE INDEX IF NOT EXISTS grok_video_legacy_active_account
ON grok_video_tasks(account_id,generation_state,created_at)
WHERE generation_state IN ('submitting','submission_unknown','queued','in_progress');

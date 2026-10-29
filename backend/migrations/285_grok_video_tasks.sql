SET LOCAL lock_timeout = '5s';
CREATE TABLE grok_video_tasks (
 id BIGSERIAL PRIMARY KEY,
 public_id VARCHAR(64) NOT NULL UNIQUE,
 user_id BIGINT NOT NULL,
 api_key_id BIGINT NOT NULL,
 group_id BIGINT NOT NULL,
 account_id BIGINT NOT NULL,
 operation VARCHAR(16) NOT NULL CHECK (operation IN ('generate','edit','extend')),
 request_hash VARCHAR(64) NOT NULL,
 idempotency_key VARCHAR(255),
 provider_task_id VARCHAR(255),
 generation_state VARCHAR(32) NOT NULL,
 sync_status VARCHAR(16) NOT NULL,
 settlement_status VARCHAR(16) NOT NULL,
 data JSONB NOT NULL DEFAULT '{}'::jsonb,
 version BIGINT NOT NULL DEFAULT 0,
 lease_epoch BIGINT NOT NULL DEFAULT 0,
 lease_owner TEXT,
 lease_expires_at TIMESTAMPTZ,
 next_action_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 CHECK (generation_state IN ('submitting','submission_unknown','queued','in_progress','completed','failed','expired')),
 CHECK (sync_status IN ('scheduled','retrying','paused','finished')),
 CHECK (settlement_status IN ('not_due','pending','settled','retrying','blocked','not_required'))
);
CREATE UNIQUE INDEX grok_video_provider_task ON grok_video_tasks(account_id,provider_task_id) WHERE provider_task_id IS NOT NULL;
CREATE UNIQUE INDEX grok_video_idempotency ON grok_video_tasks(user_id,api_key_id,operation,idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX grok_video_owner ON grok_video_tasks(user_id,api_key_id,group_id,provider_task_id);
CREATE INDEX grok_video_due ON grok_video_tasks(next_action_at,id) WHERE next_action_at IS NOT NULL;
CREATE INDEX grok_video_created ON grok_video_tasks(created_at DESC,id DESC);
CREATE TABLE grok_video_task_events (
 id BIGSERIAL PRIMARY KEY,
 task_id BIGINT NOT NULL REFERENCES grok_video_tasks(id),
 event_type VARCHAR(80) NOT NULL,
 generation_state VARCHAR(32) NOT NULL,
 settlement_status VARCHAR(16) NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX grok_video_events_task ON grok_video_task_events(task_id,id);

-- A read-only projection: Grok rows never enter the managed hold/settlement worker.
CREATE VIEW grok_video_admin_tasks AS
SELECT g.id,g.public_id,'grok_relay'::text AS source,g.user_id,g.api_key_id,g.group_id,g.account_id,
 'grok'::text AS provider,g.operation,g.generation_state,'none'::text AS billing_state,'none'::text AS delete_state,
 g.provider_task_id,COALESCE(g.data->>'requested_model','') AS public_model,g.created_at,
 CASE WHEN g.generation_state='submission_unknown' THEN g.updated_at END AS submission_unknown_at,
 g.settlement_status,
 jsonb_build_object(
 'id',g.id,'public_id',g.public_id,'source','grok_relay','provider','grok',
 'user_id',g.user_id,'api_key_id',g.api_key_id,'group_id',g.group_id,'account_id',g.account_id,
 'operation',g.operation,'endpoint',g.data->>'endpoint','requested_model',g.data->>'requested_model',
 'public_model',g.data->>'requested_model','upstream_model',g.data->>'upstream_model',
 'generation_state',g.generation_state,'billing_state','none','delete_state','none',
 'provider_task_id',g.provider_task_id,'provider_status',g.data->>'provider_status',
 'version',g.version,'lease_epoch',g.lease_epoch,'lease_expires_at',g.lease_expires_at,
 'progress',g.data->'progress','created_at',g.created_at,'updated_at',g.updated_at,
 'submitted_at',g.data->'submitted_at','finished_at',g.data->'finished_at','settled_at',g.data->'settled_at',
 'next_action_at',g.next_action_at,'actual_cost',g.data->'actual_cost','currency','USD',
 'sync_status',g.sync_status,'settlement_status',g.settlement_status,'last_polled_at',g.data->'last_polled_at',
 'imported_from_legacy',g.data->'imported_from_legacy','last_error_code',g.data->>'last_error_code',
 'last_error_message',g.data->>'last_error_code','poll_attempts',g.data->'poll_attempts',
 'request_attributes',jsonb_build_object('prompt',g.data->>'prompt','resolution',g.data->>'resolution','duration',g.data->'duration','aspect_ratio',g.data->>'aspect_ratio','generate_audio',g.data->'generate_audio','input_count',g.data->'input_count'),
 'response_metadata',jsonb_build_object('usage_log_id',g.data->'usage_log_id'),
 'provider_cost_snapshot',jsonb_build_object('cost_in_usd_ticks',g.data->'upstream_cost_ticks'),
 'usage_snapshot',jsonb_build_object('duration',NULLIF((g.data->>'actual_duration')::int,0)),
 'content_variants','[]'::jsonb,'input_manifest',COALESCE(g.data->'input_manifest','[]'::jsonb),'price_snapshot','{}'::jsonb,
 'allowed_actions',
 CASE WHEN g.provider_task_id IS NOT NULL AND g.generation_state IN ('queued','in_progress') THEN CASE WHEN g.sync_status='paused' THEN '["get","resume"]'::jsonb ELSE '["get"]'::jsonb END ELSE '[]'::jsonb END
 || CASE WHEN COALESCE(g.data->>'snapshot_enc','')<>'' AND g.settlement_status IN ('pending','retrying','blocked') THEN '["settlement"]'::jsonb ELSE '[]'::jsonb END
 ) AS task
FROM grok_video_tasks g;
CREATE VIEW video_admin_task_rows AS
SELECT v.id,v.public_id,v.source::text,v.user_id,v.api_key_id,v.group_id,v.account_id,v.provider::text,v.operation,v.generation_state,v.billing_state::text,v.delete_state::text,v.provider_task_id,v.public_model,v.created_at,v.submission_unknown_at,NULL::text AS settlement_status,to_jsonb(v) AS task FROM video_tasks v
UNION ALL SELECT * FROM grok_video_admin_tasks;

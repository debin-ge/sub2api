-- 将本地历史智谱平台 ID glm 归一为上游 v0.1.178 的 zhipu。
-- 运行时仍把 glm 当作 zhipu 别名；本迁移用于存量账号/分组/配额/监控行。

UPDATE accounts
SET platform = 'zhipu'
WHERE platform = 'glm';

UPDATE groups
SET platform = 'zhipu'
WHERE platform = 'glm';

UPDATE user_platform_quotas
SET platform = 'zhipu'
WHERE platform = 'glm';

UPDATE channel_monitors
SET provider = 'zhipu'
WHERE provider = 'glm';

UPDATE channel_monitor_request_templates
SET provider = 'zhipu'
WHERE provider = 'glm';

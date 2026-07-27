-- 邮箱域名策略由白名单改为黑名单。
-- 不复制旧 registration_email_suffix_whitelist 的值：两者语义相反，
-- 原样迁移会把此前允许的域名错误地全部禁止。
INSERT INTO settings (key, value, updated_at)
VALUES ('registration_email_suffix_blacklist', '[]', NOW())
ON CONFLICT (key) DO NOTHING;

-- 邮箱域名策略由白名单改为黑名单。
-- 不复制旧 registration_email_suffix_whitelist 的值：两者语义相反，
-- 原样迁移会把此前允许的域名错误地全部禁止。
INSERT INTO settings (key, value, updated_at)
VALUES ('registration_email_suffix_blacklist', '[]', NOW())
ON CONFLICT (key) DO NOTHING;

-- 破坏性变更告警：若旧库仍配置了非空的注册邮箱后缀白名单，升级后该限制将彻底失效
-- （白名单语义已移除，任何域名都可注册）。此处仅告警、不自动改写数据，避免误伤；
-- 运维需自行改用黑名单或其它注册门槛。告警写入 Postgres 服务端日志。
DO $$
DECLARE
    legacy_whitelist TEXT;
BEGIN
    SELECT value INTO legacy_whitelist
    FROM settings
    WHERE key = 'registration_email_suffix_whitelist';

    IF legacy_whitelist IS NOT NULL
       AND btrim(legacy_whitelist) NOT IN ('', '[]', 'null')
    THEN
        RAISE WARNING '[migration 186] registration_email_suffix_whitelist 已废弃且不再生效：升级后注册邮箱域名限制将失效，请改配置 registration_email_suffix_blacklist。原白名单值仍保留在 settings 表中以便查阅：%', legacy_whitelist;
    END IF;
END $$;

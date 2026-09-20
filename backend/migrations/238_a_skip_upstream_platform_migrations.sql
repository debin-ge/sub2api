-- 把 upstream v0.2.7 带来的两条 238 迁移登记为「已应用」，让 runner 跳过它们。
-- 本文件名按字节序排在两者之前（'a' < 'o' < 'p'），因此先于它们执行。
--
-- 背景：runner 以 filename 为主键、sort.Strings 排序，不做单调版本校验，未应用的
-- 迁移无论编号高低都会在下次启动执行。本 fork 的迁移血脉在 237 之后分叉，已经走到
-- 273，upstream 却在 238 这个位置新插了两条，两条都与 fork 的终态冲突。
--
-- 一、238_opencode_go_platform.sql
--   它 DROP 并重建四个平台 CHECK，平台集含 'opencode_go'，但不含 fork 的
--   'bytedance' / 'windsurf' / 'glm'。两种血脉都会出事：
--     - 已全量迁移的库（至 273）：它是唯一未应用项，最后执行，抹掉 bytedance；
--       若存在 platform='bytedance' 的行，ADD CONSTRAINT 当场报
--       check constraint ... is violated by some row，容器反复重启；
--     - 生产库（止于 241）：它排序靠前先执行，随后 268 / 273 再覆盖，
--       'opencode_go' 被静默删除，OpenCode 配额与复合路由写入失败。
--   这正是 266a_skip_267_user_platform_quotas_bytedance.sql 记录过的同一类事故。
--   跳过是安全的：它的全部效果就是这四个 CHECK，没有任何 DML，而
--   274_platform_superset_opencode_go.sql 用 fork ∪ upstream 的真并集重建同样四个
--   约束，是其平台集的严格超集，终态完全覆盖。
--
-- 二、238_purge_unlimited_user_platform_quotas.sql
--   它无条件 DELETE 三档限额全为 NULL 的配额行，理由是「这些行不携带任何可执行的
--   限额，也不参与任何读取」。该前提对 upstream 成立，对本 fork 不成立：
--   repository/user_platform_quota_repo.go 的 IncrementUsageWithReset 在行不存在时
--   fail-open 建行且刻意保留 limit_* 为 NULL，用来累计「无限额用户」的用量；
--   handler/quotaview/helpers.go 又无条件读取 *_usage_usd 构建窗口并经 DTO 暴露
--   daily_usage_usd / weekly_usage_usd / monthly_usage_usd。
--   照搬会把全部无限额用户 × 平台的累计用量清零（行会在下次计费时重建，因此不报错，
--   但历史用量静默丢失且对用户可见）。故一并跳过。
--
-- 下面的 checksum 必须等于对应文件内容 strings.TrimSpace 之后的 SHA256，否则 runner
-- 会在跳过之后立刻抛 checksum mismatch。由 migrations 包的钉住测试保护。
--
-- 幂等性：已应用过的库由 ON CONFLICT DO NOTHING 退化为空操作。

INSERT INTO schema_migrations (filename, checksum)
VALUES
    ('238_opencode_go_platform.sql',
     '6f987e251519bd3759e60da44620a5d777494cceb333b6ce394aa0ea536ef5a2'),
    ('238_purge_unlimited_user_platform_quotas.sql',
     '052756d1b1ac951f002034bf0434f8a6ea943ec0a2a34541ca4374ba6278cce7')
ON CONFLICT (filename) DO NOTHING;

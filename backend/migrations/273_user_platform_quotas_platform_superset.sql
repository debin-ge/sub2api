-- 修复 main 合并进 test 后 user_platform_quotas_platform_check 丢失 minimax 的问题。
--
-- 背景：迁移按文件名排序执行。
--   237_add_minimax_platform.sql（main）重建该约束，平台集含 minimax、不含 bytedance；
--   267_user_platform_quotas_add_bytedance.sql（test）重建该约束，含 bytedance、不含 minimax。
-- 267 排在 237 之后，因此合并后最终落库的约束是 267 的版本，minimax 被静默删除：
-- 已有 platform='minimax' 的配额行会让 267 的 ADD CONSTRAINT 直接失败，
-- 而在干净库上则表现为此后无法再写入 minimax 配额。
--
-- 两个迁移都已随镜像部署，按已应用对待，不能原地修改（见 migrations/README.md
-- 的不可变性原则与 CI 门禁规则 1），因此这里新开一个迁移把约束重建为二者的并集。
--
-- 该集合必须与以下两处保持同步：
--   1. internal/service/domain_constants.go 的 AllowedQuotaPlatforms
--   2. ent/schema/user_platform_quota.go 的构建期 validator
--
-- 兼容性：这是一次约束放宽（superset），不会与任何既有数据冲突，
-- 旧版本代码只写入更小的平台子集，同样满足新约束，符合 expand-contract。

ALTER TABLE user_platform_quotas
    DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;

ALTER TABLE user_platform_quotas
    ADD CONSTRAINT user_platform_quotas_platform_check
    CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok',
                        'kimi', 'zhipu', 'deepseek', 'minimax', 'bytedance'));

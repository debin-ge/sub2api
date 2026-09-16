-- 把 267 登记为「已应用」，让 runner 跳过它，修复生产启动崩溃循环。
--
-- 故障形态：生产跑的是 release 血脉（2.0.4~2.0.7），迁移止于 241，本次升级首次执行
-- 242~273。237 早在 release 血脉上就把 user_platform_quotas_platform_check 放开到含
-- 'minimax'，库里因此存在 platform='minimax' 的配额行；而 267（test 血脉）把同一个约束
-- 重建为不含 'minimax' 的集合，ADD CONSTRAINT 当场报
--   check constraint "user_platform_quotas_platform_check" of relation
--   "user_platform_quotas" is violated by some row
-- 容器反复重启。273 虽然把约束重建成含 minimax 与 bytedance 的并集，但按文件名排序排在
-- 267 之后，救不了 267 自己。
--
-- 267 已随 test 血脉的镜像部署过，按已应用对待，不能原地修改（见 README 的不可变性原则
-- 与 CI 门禁规则 1），所以这里新开一个排在 267 之前的迁移，直接写 schema_migrations。
--
-- 为什么跳过 267 是安全的：
--   1. 267 的唯一效果就是重建这一个 CHECK，没有任何 DML；273 用二者的并集重建同一个
--      约束，是 267 平台集的严格超集，两条血脉的终态完全一致；
--   2. 267 与 273 之间（268~272）没有任何迁移读写 user_platform_quotas，窗口期内约束
--      停留在 237 的取值（含 minimax、不含 bytedance）不影响它们；
--   3. 蓝绿并存窗口里的旧 slot 是 release 血脉的代码，不认识 bytedance，不会写入
--      bytedance 配额行，因此窗口期内缺少 'bytedance' 也不会有写入失败。
--
-- 下面的 checksum 必须等于 267 文件内容（strings.TrimSpace 之后）的 SHA256，否则 runner
-- 会在跳过之后立刻抛 checksum mismatch。由 migrations 包的
-- TestSkip267MigrationPinsCurrentChecksum 钉住。
--
-- 幂等性：test 血脉的库里 267 已有记录，ON CONFLICT DO NOTHING 让本迁移退化为空操作；
-- 生产若已由人工执行过同一条 INSERT 应急解锁，本迁移同样是空操作。

INSERT INTO schema_migrations (filename, checksum)
VALUES (
    '267_user_platform_quotas_add_bytedance.sql',
    '717307ae0e59c920bbb4e652310dadc989217016ffd2339a4666ba42fad93176'
)
ON CONFLICT (filename) DO NOTHING;

# Database Migrations

## Overview

This directory contains SQL migration files for database schema changes. The migration system uses SHA256 checksums to ensure migration immutability and consistency across environments.

## Migration File Naming

Format: `NNN_description.sql`
- `NNN`: Sequential number (e.g., 001, 002, 003)
- `description`: Brief description in snake_case

Example: `017_add_gemini_tier_id.sql`

### `_notx.sql` 命名与执行语义（并发索引专用）

当迁移包含 `CREATE INDEX CONCURRENTLY` 或 `DROP INDEX CONCURRENTLY` 时，必须使用 `_notx.sql` 后缀，例如：

- `062_add_accounts_priority_indexes_notx.sql`
- `063_drop_legacy_indexes_notx.sql`

运行规则：

1. `*.sql`（不带 `_notx`）按事务执行。
2. `*_notx.sql` 按非事务执行，不会包裹在 `BEGIN/COMMIT` 中。
3. `*_notx.sql` 仅允许并发索引语句，不允许混入事务控制语句或其他 DDL/DML。

幂等要求（必须）：

- 创建索引：`CREATE INDEX CONCURRENTLY IF NOT EXISTS ...`
- 删除索引：`DROP INDEX CONCURRENTLY IF EXISTS ...`

这样可以保证灾备重放、重复执行时不会因对象已存在/不存在而失败。

## Migration File Structure

This project uses a custom migration runner (`internal/repository/migrations_runner.go`) that executes the full SQL file content as-is.

- Regular migrations (`*.sql`): executed in a transaction.
- Non-transactional migrations (`*_notx.sql`): split by statement and executed without transaction (for `CONCURRENTLY`).

```sql
-- Forward-only migration (recommended)
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS example_column VARCHAR(100);
```

> ⚠️ Do **not** place executable "Down" SQL in the same file. The runner does not parse goose Up/Down sections and will execute all SQL statements in the file.

## Expand-Contract（蓝绿发布兼容性，硬约束）

迁移在**每次容器启动时自动执行**（`internal/repository/ent.go` 创建 Ent client 时调用
`applyMigrationsFS`，受 PG Advisory Lock 与 SHA256 校验和保护）。蓝绿发布下，新 slot
一启动就把 schema 升级到版本 N，而旧 slot（版本 N-1 的代码）仍在服务同一个数据库，
并存窗口最短数十秒、最长等于排空时长（默认 960 秒）。

**因此每个版本的迁移必须与上一个应用版本兼容：**

| 操作 | 允许 | 正确做法 |
|---|---|---|
| `ADD COLUMN`（可空或有默认值） | ✅ | 直接添加 |
| `ADD COLUMN NOT NULL` 无默认值 | ❌ | 拆两步：先可空 → 回填 → 下个版本加约束 |
| `DROP COLUMN` / `DROP TABLE` | ❌ | 版本 K 停止读写 → 版本 K+1 才删除 |
| `RENAME COLUMN` | ❌ | 加新列 → 双写 → 回填 → 停用旧列 → 下版本删除 |
| `ALTER COLUMN ... TYPE` | ❌ | 加新列 → 双写 → 切换读 → 删除旧列 |
| `CREATE INDEX` | ⚠️ | 大表必须 `CONCURRENTLY`，且必须放在 `_notx.sql`（见上文） |

**回滚约束**：蓝绿回滚只把流量切回旧 slot，**不撤销已应用的迁移**。旧代码必须能在
新 schema 上正常运行——这正是 expand-contract 保证的。**破坏该规则的版本不能使用
蓝绿发布，必须安排停机窗口。**

**CI 门禁**：`backend/scripts/check-migration-gate.sh`（CI 的 migration-gate job）会拦截：

1. 修改/删除/重命名已存在的迁移文件（不可豁免，与运行时 SHA256 校验一致）；
2. 新增迁移中的破坏性 DDL（`DROP COLUMN` / `DROP TABLE` / `RENAME` /
   `ALTER COLUMN ... TYPE`）——确需如此时给 PR 打 `breaking-migration` 标签并重跑
   该 job，同时该版本必须走停机发布；
3. `CONCURRENTLY` 出现在非 `_notx.sql` 文件中（不可豁免，运行时必然报错）。

本地自查：

```bash
backend/scripts/check-migration-gate.sh origin/main
```

## Important Rules

### ⚠️ Immutability Principle

**Once a migration is applied to ANY environment (dev, staging, production), it MUST NOT be modified.**

Why?
- Each migration has a SHA256 checksum stored in the `schema_migrations` table
- Modifying an applied migration causes checksum mismatch errors
- Different environments would have inconsistent database states
- Breaks audit trail and reproducibility

### ✅ Correct Workflow

1. **Create new migration**
   ```bash
   # Create new file with next sequential number
   touch migrations/018_your_change.sql
   ```

2. **Write forward-only migration SQL**
   - Put only the intended schema change in the file
   - If rollback is needed, create a new migration file to revert

3. **Test locally**
   ```bash
   # Apply migration
   make migrate-up

   # Test rollback
   make migrate-down
   ```

4. **Commit and deploy**
   ```bash
   git add migrations/018_your_change.sql
   git commit -m "feat(db): add your change"
   ```

### ❌ What NOT to Do

- ❌ Modify an already-applied migration file
- ❌ Delete migration files
- ❌ Change migration file names
- ❌ Reorder migration numbers

### 🔧 If You Accidentally Modified an Applied Migration

**Error message:**
```
migration 017_add_gemini_tier_id.sql checksum mismatch (db=abc123... file=def456...)
```

**Solution:**
```bash
# 1. Find the original version
git log --oneline -- migrations/017_add_gemini_tier_id.sql

# 2. Revert to the commit when it was first applied
git checkout <commit-hash> -- migrations/017_add_gemini_tier_id.sql

# 3. Create a NEW migration for your changes
touch migrations/018_your_new_change.sql
```

## Migration System Details

- **Checksum Algorithm**: SHA256 of trimmed file content
- **Tracking Table**: `schema_migrations` (filename, checksum, applied_at)
- **Runner**: `internal/repository/migrations_runner.go`
- **Auto-run**: Migrations run automatically on service startup

## Best Practices

1. **Keep migrations small and focused**
   - One logical change per migration
   - Easier to review and rollback

2. **Write reversible migrations**
   - Always provide a working Down migration
   - Test rollback before committing

3. **Use transactions**
   - Wrap DDL statements in transactions when possible
   - Ensures atomicity

4. **Add comments**
   - Explain WHY the change is needed
   - Document any special considerations

5. **Test in development first**
   - Apply migration locally
   - Verify data integrity
   - Test rollback

## Example Migration

```sql
-- Add tier_id field to Gemini OAuth accounts for quota tracking
UPDATE accounts
SET credentials = jsonb_set(
    credentials,
    '{tier_id}',
    '"LEGACY"',
    true
)
WHERE platform = 'gemini'
  AND type = 'oauth'
  AND credentials->>'tier_id' IS NULL;
```

## Troubleshooting

### Checksum Mismatch
See "If You Accidentally Modified an Applied Migration" above.

### Migration Failed
```bash
# Check migration status
psql -d sub2api -c "SELECT * FROM schema_migrations ORDER BY applied_at DESC;"

# Manually rollback if needed (use with caution)
# Better to fix the migration and create a new one
```

### Need to Skip a Migration (Emergency Only)
```sql
-- DANGEROUS: Only use in development or with extreme caution
INSERT INTO schema_migrations (filename, checksum, applied_at)
VALUES ('NNN_migration.sql', 'calculated_checksum', NOW());
```

## References

- Migration runner: `internal/repository/migrations_runner.go`
- PostgreSQL docs: https://www.postgresql.org/docs/

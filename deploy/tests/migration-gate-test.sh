#!/bin/bash
# backend/scripts/check-migration-gate.sh 的行为测试：
# 在临时 git 仓库中构造迁移变更，验证门禁的放行/拦截分支。

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
GATE="$ROOT_DIR/backend/scripts/check-migration-gate.sh"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

# 隔离全局/系统 git 配置，保证测试可重复
export GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_SYSTEM=/dev/null

git_q() {
    git -c user.name=test -c user.email=test@example.com -c commit.gpgsign=false "$@"
}

# 建一个带基线迁移的仓库，检出 feature 分支等待用例写入变更
make_repo() {
    local repo="$TEMP_DIR/$1"
    rm -rf "$repo"
    mkdir -p "$repo/backend/migrations"
    (
        cd "$repo"
        git_q init -q -b main
        cat > backend/migrations/001_init.sql <<'SQL'
CREATE TABLE users (id BIGINT PRIMARY KEY);
SQL
        cat > backend/migrations/002_notes.sql <<'SQL'
ALTER TABLE users ADD COLUMN IF NOT EXISTS notes TEXT;
SQL
        git_q add -A
        git_q commit -qm base
        git_q checkout -qb feature
    )
    echo "$repo"
}

commit_all() {
    (cd "$1" && git_q add -A && git_q commit -qm change)
}

# expect_pass <描述> <repo> [gate 额外参数...]
expect_pass() {
    local desc="$1" repo="$2"
    shift 2
    if ! (cd "$repo" && "$GATE" main "$@" > "$TEMP_DIR/out" 2>&1); then
        echo "FAIL(expected pass): $desc" >&2
        cat "$TEMP_DIR/out" >&2
        exit 1
    fi
    echo "ok: $desc"
}

# expect_fail <描述> <期望输出片段> <repo> [gate 额外参数...]
expect_fail() {
    local desc="$1" want="$2" repo="$3"
    shift 3
    if (cd "$repo" && "$GATE" main "$@" > "$TEMP_DIR/out" 2>&1); then
        echo "FAIL(expected reject): $desc" >&2
        cat "$TEMP_DIR/out" >&2
        exit 1
    fi
    if ! grep -q "$want" "$TEMP_DIR/out"; then
        echo "FAIL(missing message '$want'): $desc" >&2
        cat "$TEMP_DIR/out" >&2
        exit 1
    fi
    echo "ok: $desc"
}

# 1. 无迁移变更 → 通过
repo=$(make_repo t1)
(cd "$repo" && echo x > other.txt) && commit_all "$repo"
expect_pass "无迁移变更" "$repo"

# 2. 新增兼容迁移(ADD COLUMN) → 通过
repo=$(make_repo t2)
cat > "$repo/backend/migrations/003_add_col.sql" <<'SQL'
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT;
SQL
commit_all "$repo"
expect_pass "新增兼容迁移" "$repo"

# 3. 新增 DROP COLUMN → 无标签拦截,--allow-breaking 放行
repo=$(make_repo t3)
cat > "$repo/backend/migrations/003_drop.sql" <<'SQL'
ALTER TABLE users DROP COLUMN notes;
SQL
commit_all "$repo"
expect_fail "DROP COLUMN 无标签拦截" "expand-contract" "$repo"
expect_pass "DROP COLUMN 带标签放行" "$repo" --allow-breaking

# 4. 修改历史迁移 → 拦截,标签不可豁免
repo=$(make_repo t4)
echo "-- tweak" >> "$repo/backend/migrations/001_init.sql"
commit_all "$repo"
expect_fail "修改历史迁移拦截" "不可修改" "$repo"
expect_fail "修改历史迁移标签不可豁免" "不可修改" "$repo" --allow-breaking

# 5. 删除历史迁移 → 拦截
repo=$(make_repo t5)
rm "$repo/backend/migrations/002_notes.sql"
commit_all "$repo"
expect_fail "删除历史迁移拦截" "不可修改" "$repo"

# 6. CONCURRENTLY 出现在普通 .sql → 拦截,标签不可豁免
repo=$(make_repo t6)
cat > "$repo/backend/migrations/003_idx.sql" <<'SQL'
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_email ON users (email);
SQL
commit_all "$repo"
expect_fail "CONCURRENTLY 非 _notx 拦截" "_notx" "$repo"
expect_fail "CONCURRENTLY 非 _notx 标签不可豁免" "_notx" "$repo" --allow-breaking

# 7. CONCURRENTLY 在 _notx.sql → 通过
repo=$(make_repo t7)
cat > "$repo/backend/migrations/003_idx_notx.sql" <<'SQL'
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_email ON users (email);
SQL
commit_all "$repo"
expect_pass "CONCURRENTLY 在 _notx.sql 放行" "$repo"

# 8. 破坏性关键词仅出现在注释 → 通过(注释剥离)
repo=$(make_repo t8)
cat > "$repo/backend/migrations/003_comment.sql" <<'SQL'
-- 之后的版本会 DROP COLUMN notes,本迁移先停止写入
/* 历史方案曾考虑 RENAME COLUMN,
   已放弃 */
ALTER TABLE users ADD COLUMN IF NOT EXISTS notes_v2 TEXT;
SQL
commit_all "$repo"
expect_pass "注释中的关键词不拦截" "$repo"

# 9. 跨行 ALTER COLUMN ... TYPE → 拦截
repo=$(make_repo t9)
cat > "$repo/backend/migrations/003_type.sql" <<'SQL'
ALTER TABLE users
    ALTER COLUMN id
    TYPE TEXT;
SQL
commit_all "$repo"
expect_fail "跨行 ALTER COLUMN TYPE 拦截" "expand-contract" "$repo"

# 10. 迁移目录内非 SQL 文件(README)修改 → 通过
repo=$(make_repo t10)
echo "# notes" > "$repo/backend/migrations/README.md"
commit_all "$repo"
expect_pass "迁移目录 README 修改不受限" "$repo"

echo "migration-gate-test: all passed"

#!/usr/bin/env bash
# 迁移门禁：保证 backend/migrations/ 的变更满足蓝绿发布的 expand-contract 纪律。
#
# 规则（详见 backend/migrations/README.md 的 Expand-Contract 一节）：
#   1. 已存在的迁移文件不可修改/删除/重命名 —— 与运行时 SHA256 校验一致，提前到 CI 拦截，
#      不可用标签豁免；
#   2. 新增迁移出现破坏性 DDL（DROP COLUMN / DROP TABLE / RENAME / ALTER COLUMN ... TYPE）
#      时，必须由 PR 显式打 breaking-migration 标签（CI 传入 --allow-breaking）方可通过，
#      且该版本不能走蓝绿发布，需安排停机窗口；
#   3. CONCURRENTLY 语句必须放在 *_notx.sql 文件（迁移 runner 的非事务执行路径），
#      普通 *.sql 会被事务包裹、PostgreSQL 直接报错，不可用标签豁免。
#
# 用法：check-migration-gate.sh <base-ref> [--worktree] [--allow-breaking]
# 本地自查：backend/scripts/check-migration-gate.sh origin/main --worktree
#
# 退出码：0 通过；1 存在违规；2 参数错误。

set -euo pipefail

MIGRATIONS_DIR="backend/migrations"
CONTRACT_DOC="backend/migrations/README.md"

usage() {
    echo "usage: $(basename "$0") <base-ref> [--worktree] [--allow-breaking]"
}

BASE=""
ALLOW_BREAKING=false
WORKTREE=false
for arg in "$@"; do
    case "$arg" in
        --allow-breaking) ALLOW_BREAKING=true ;;
        --worktree) WORKTREE=true ;;
        -h|--help) usage; exit 0 ;;
        -*) echo "unknown flag: $arg" >&2; usage >&2; exit 2 ;;
        *)
            if [ -z "$BASE" ]; then
                BASE="$arg"
            else
                usage >&2
                exit 2
            fi
            ;;
    esac
done
if [ -z "$BASE" ]; then
    usage >&2
    exit 2
fi

cd "$(git rev-parse --show-toplevel)"
MERGE_BASE=$(git merge-base "$BASE" HEAD)

# 每个新增 SQL 文件经注释剥离后做全文匹配（多行语句安全），输出命中的 token。
scan_sql() {
    if [ "$WORKTREE" = true ]; then
        sed -n '1,$p' "$1"
    else
        git show "HEAD:$1"
    fi | perl -0777 -ne '
        s{/\*.*?\*/}{ }gs;
        s{--[^\n]*}{}g;
        print "DROP_COLUMN\n"      if /\bDROP\s+COLUMN\b/is;
        print "DROP_TABLE\n"       if /\bDROP\s+TABLE\b/is;
        print "RENAME\n"           if /\bRENAME\b/is;
        print "ALTER_COLUMN_TYPE\n" if /\bALTER\s+COLUMN\b(?:(?!;).)*?\bTYPE\b/is;
        print "CONCURRENTLY\n"     if /\bCONCURRENTLY\b/i;
    '
}

changed_migrations() {
    if [ "$WORKTREE" = true ]; then
        git diff --name-status --no-renames -z "$MERGE_BASE" -- "$MIGRATIONS_DIR"
        while IFS= read -r -d '' path; do
            printf 'A\0%s\0' "$path"
        done < <(git ls-files --others --exclude-standard -z -- "$MIGRATIONS_DIR")
    else
        git diff --name-status --no-renames -z "$MERGE_BASE" HEAD -- "$MIGRATIONS_DIR"
    fi
}

violations=0
breaking_files=""

while IFS= read -r -d '' status && IFS= read -r -d '' path; do
    [ -n "${status:-}" ] || continue
    case "$path" in
        "$MIGRATIONS_DIR"/*.sql) ;;
        *) continue ;; # README 等非 SQL 文件不受迁移不可变性约束
    esac

    if [ "$status" != "A" ]; then
        echo "FAIL: ${path} (status=${status}) —— 已存在的迁移文件不可修改/删除/重命名；" \
             "runner 以 SHA256 校验和保证不可变性，改动会让所有已应用环境启动失败。" \
             "正确做法：保留原文件，另建新编号迁移。（不可用标签豁免）"
        violations=1
        continue
    fi

    hits=$(scan_sql "$path")

    if printf '%s\n' "$hits" | grep -qx "CONCURRENTLY"; then
        case "$path" in
            *_notx.sql)
                echo "INFO: ${path} 含 CONCURRENTLY，已按 _notx.sql 非事务路径命名，放行。"
                ;;
            *)
                echo "FAIL: ${path} 含 CONCURRENTLY 但未使用 _notx.sql 后缀 ——" \
                     "普通迁移会被事务包裹，PostgreSQL 不允许在事务内执行 CONCURRENTLY。" \
                     "见 ${CONTRACT_DOC} 的 _notx.sql 说明。（不可用标签豁免）"
                violations=1
                ;;
        esac
    fi

    breaking=$(printf '%s\n' "$hits" | grep -vx "CONCURRENTLY" | grep -v '^$' || true)
    if [ -n "$breaking" ]; then
        breaking_files="${breaking_files}  ${path}: $(printf '%s' "$breaking" | tr '\n' ' ')
"
    fi
done < <(changed_migrations)

if [ -n "$breaking_files" ]; then
    if [ "$ALLOW_BREAKING" = true ]; then
        echo "WARN: 检测到破坏性迁移，已由 breaking-migration 标签显式放行："
        printf '%s' "$breaking_files"
        echo "WARN: 该版本禁止蓝绿发布（旧代码无法在新 schema 上运行），必须安排停机窗口。"
    else
        echo "FAIL: 检测到破坏性迁移（违反 expand-contract 纪律）："
        printf '%s' "$breaking_files"
        echo ""
        echo "蓝绿发布期间新旧两个版本会同时读写同一数据库，迁移必须与上一个应用版本兼容："
        echo "  - DROP COLUMN / DROP TABLE：拆两个版本 —— 版本 K 停止读写，版本 K+1 才删除；"
        echo "  - RENAME：加新列 → 双写 → 回填 → 停用旧列 → 下版本删除；"
        echo "  - ALTER COLUMN ... TYPE：加新列 → 双写 → 切换读 → 删除旧列。"
        echo "完整规则见 ${CONTRACT_DOC} 的 Expand-Contract 一节。"
        echo "确需破坏性变更：给 PR 打 breaking-migration 标签后重跑本检查，"
        echo "并注意带该标签的版本不能走蓝绿发布，需安排停机窗口。"
        violations=1
    fi
fi

if [ "$violations" -ne 0 ]; then
    exit 1
fi
echo "migration gate: OK"

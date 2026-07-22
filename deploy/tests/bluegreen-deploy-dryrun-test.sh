#!/bin/bash
# s2a-deploy / s2a-rollback / s2a-teardown 主流程分支测试。
# docker / nginx / curl / systemd-run / systemctl 全部用 PATH mock,
# 用标记文件模拟容器运行状态、健康探测结果与排空定时器。

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BG_BIN="$ROOT_DIR/deploy/blue-green/bin"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

export MOCK_LOG="$TEMP_DIR/mock.log"
export MOCK_STATE="$TEMP_DIR/state"
export MOCK_FLAGS="$TEMP_DIR/flags"
mkdir -p "$MOCK_STATE" "$MOCK_FLAGS" "$TEMP_DIR/bin"

# ── mocks ────────────────────────────────────────────────────────────────
cat > "$TEMP_DIR/bin/docker" <<'EOF'
#!/bin/bash
echo "docker $*" >> "$MOCK_LOG"
if [ "$1" = "compose" ]; then
    shift
    proj=""
    while [ $# -gt 0 ]; do
        case "$1" in
            -p) proj="$2"; shift 2 ;;
            --project-directory|-f) shift 2 ;;
            *) break ;;
        esac
    done
    cmd="${1:-}"
    case "$cmd" in
        up)
            if [ -f "$MOCK_FLAGS/up-fail-$proj" ]; then exit 1; fi
            touch "$MOCK_STATE/running-$proj"
            exit 0 ;;
        ps)
            [ -f "$MOCK_STATE/running-$proj" ] && echo "cid-$proj"
            exit 0 ;;
        down)
            rm -f "$MOCK_STATE/running-$proj"
            exit 0 ;;
        logs)
            echo "mock container logs for $proj"
            exit 0 ;;
    esac
    exit 0
fi
case "$1" in
    network)
        name="$3"
        case "$2" in
            inspect) [ -f "$MOCK_STATE/net-$name" ] || exit 1 ;;
            create) touch "$MOCK_STATE/net-$name" ;;
        esac
        exit 0 ;;
    ps) exit 0 ;;
esac
exit 0
EOF

cat > "$TEMP_DIR/bin/curl" <<'EOF'
#!/bin/bash
url=""
for a in "$@"; do case "$a" in http*) url="$a" ;; esac; done
port=$(printf '%s' "$url" | sed -E 's|.*127\.0\.0\.1:([0-9]+)/health.*|\1|')
f="$MOCK_STATE/health-$port"
if [ -f "$f" ]; then cat "$f"; exit 0; fi
exit 22
EOF

cat > "$TEMP_DIR/bin/nginx" <<'EOF'
#!/bin/bash
echo "nginx $*" >> "$MOCK_LOG"
if [ "$1" = "-t" ] && [ -f "$MOCK_FLAGS/nginx-t-fail" ]; then exit 1; fi
exit 0
EOF

cat > "$TEMP_DIR/bin/systemd-run" <<'EOF'
#!/bin/bash
echo "systemd-run $*" >> "$MOCK_LOG"
unit=""
for a in "$@"; do case "$a" in --unit=*) unit="${a#--unit=}" ;; esac; done
touch "$MOCK_STATE/timer-$unit"
exit 0
EOF

cat > "$TEMP_DIR/bin/systemctl" <<'EOF'
#!/bin/bash
echo "systemctl $*" >> "$MOCK_LOG"
case "$1" in
    stop)
        unit="${2%.timer}"; unit="${unit%.service}"
        if [ -f "$MOCK_STATE/timer-$unit" ]; then rm -f "$MOCK_STATE/timer-$unit"; exit 0; fi
        exit 1 ;;
    list-timers)
        for f in "$MOCK_STATE"/timer-*; do
            [ -f "$f" ] && printf '%s.timer left: mock\n' "$(basename "${f#"$MOCK_STATE"/timer-}")"
        done
        exit 0 ;;
esac
exit 0
EOF

chmod +x "$TEMP_DIR/bin/"*
export PATH="$TEMP_DIR/bin:$PATH"

# ── s2a 环境 ─────────────────────────────────────────────────────────────
export S2A_ROOT="$TEMP_DIR/srv"
export S2A_NGINX_DIR="$TEMP_DIR/nginx/sub2api"
export S2A_NGINX_SNIPPET_DIR="$TEMP_DIR/nginx/snippets"
mkdir -p "$S2A_ROOT/registry/envs" "$TEMP_DIR/tls"
touch "$TEMP_DIR/tls/cert.pem" "$TEMP_DIR/tls/key.pem"

SLUG=api-test
cat > "$S2A_ROOT/registry/sites.yaml" <<EOF
defaults:
  image_repo: weishaw/sub2api
  bind_host: 127.0.0.1
  drain_seconds: 60
  health_timeout_seconds: 3
  health_interval_seconds: 1
  tz: UTC
stacks:
  - slug: $SLUG
    domain: t.example.com
    port_base: 18080
    image_tag: v1.0.0
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
EOF
printf 'POSTGRES_PASSWORD=test-secret\n' > "$S2A_ROOT/registry/envs/$SLUG.env"
chmod 600 "$S2A_ROOT/registry/envs/$SLUG.env"

UPSTREAM="$S2A_NGINX_DIR/upstreams/$SLUG.conf"
STATE="$S2A_ROOT/stacks/$SLUG/STATE"

fail() {
    echo "FAIL: $*" >&2
    [ ! -s "$TEMP_DIR/out" ] || sed 's/^/  | /' "$TEMP_DIR/out" >&2
    exit 1
}

run_ok() {
    local desc="$1"; shift
    "$@" > "$TEMP_DIR/out" 2>&1 || fail "$desc (应成功)"
    echo "ok: $desc"
}

run_fail() {
    local desc="$1" want="$2"; shift 2
    if "$@" > "$TEMP_DIR/out" 2>&1; then fail "$desc (应失败)"; fi
    grep -q "$want" "$TEMP_DIR/out" || fail "$desc (缺少提示 '$want')"
    echo "ok: $desc"
}

set_health() { # set_health <port> <version> <slot>
    printf '{"status":"ok","version":"%s","slot":"%s"}\n' "$2" "$3" > "$MOCK_STATE/health-$1"
}

assert_upstream_port() {
    grep -q "server 127.0.0.1:$1" "$UPSTREAM" || fail "upstream 应指向 $1 (实际: $(cat "$UPSTREAM"))"
}

assert_state() { # assert_state <key> <value>
    grep -qx "$1=$2" "$STATE" || fail "STATE 应含 $1=$2 (实际: $(tr '\n' ' ' < "$STATE"))"
}

# ── S0: render + init ────────────────────────────────────────────────────
run_ok "render" "$BG_BIN/s2a-render"
run_ok "init" "$BG_BIN/s2a-init" "$SLUG"
[ -f "$MOCK_STATE/net-$SLUG-net" ] || fail "init 应创建 network"
[ -f "$MOCK_STATE/running-$SLUG-data" ] || fail "init 应拉起数据层"
[ -L "$S2A_ROOT/stacks/$SLUG/.env" ] || fail "init 应创建 .env 软链"
assert_upstream_port 18080

# ── S1: 首次部署(原地 blue,无排空) ───────────────────────────────────────
set_health 18080 1.0.0 blue
run_ok "首次部署" "$BG_BIN/s2a-deploy" "$SLUG" v1.0.0
grep -q "首次部署" "$TEMP_DIR/out" || fail "应识别为首次部署"
assert_upstream_port 18080
assert_state slot blue
assert_state prev_slot ""
[ ! -f "$MOCK_STATE/timer-s2a-drain-$SLUG-blue" ] || fail "首次部署不应安排排空"
[ -f "$MOCK_STATE/running-$SLUG-blue" ] || fail "blue 容器应在运行"

# ── S2: 常规发布 blue→green ──────────────────────────────────────────────
set_health 18081 1.1.0 green
run_ok "常规发布 blue→green" "$BG_BIN/s2a-deploy" "$SLUG" v1.1.0
assert_upstream_port 18081
assert_state slot green
assert_state prev_slot blue
assert_state prev_tag v1.0.0
[ -f "$MOCK_STATE/timer-s2a-drain-$SLUG-blue" ] || fail "应为旧 slot blue 安排排空定时器"
grep -q "s2a-rollback $SLUG" "$TEMP_DIR/out" || fail "成功输出应含回滚提示"
grep -q "nginx -s reload" "$MOCK_LOG" || fail "切流量应 reload nginx"

# ── S3: 快速回滚 green→blue ──────────────────────────────────────────────
run_ok "快速回滚" "$BG_BIN/s2a-rollback" "$SLUG"
grep -q "快速路径" "$TEMP_DIR/out" || fail "应走快速路径"
assert_upstream_port 18080
assert_state slot blue
assert_state prev_slot green
[ ! -f "$MOCK_STATE/timer-s2a-drain-$SLUG-blue" ] || fail "回滚应取消 blue 的排空定时器 (FR-4.3)"
[ -f "$MOCK_STATE/timer-s2a-drain-$SLUG-green" ] || fail "被回滚的 green 应新排排空定时器"
grep -q "不会撤销" "$TEMP_DIR/out" || fail "回滚输出应明示迁移不撤销 (FR-4.5)"

# ── S4: 健康门禁超时 → 自动回收,线上无影响 ──────────────────────────────
rm -f "$MOCK_STATE/health-18081"
run_fail "健康门禁超时自动回收" "健康门禁超时" "$BG_BIN/s2a-deploy" "$SLUG" v1.2.0
assert_upstream_port 18080
[ ! -f "$MOCK_STATE/running-$SLUG-green" ] || fail "门禁失败应回收新 slot 容器 (FR-3.3)"
[ ! -f "$MOCK_STATE/timer-s2a-drain-$SLUG-green" ] || fail "接管目标 slot 前应取消其排空定时器"
grep -q "mock container logs" "$TEMP_DIR/out" || fail "门禁失败应打印容器日志"
grep -q "线上影响: 无" "$TEMP_DIR/out" || fail "失败输出应含线上影响说明"

# ── S5: nginx -t 失败 → 还原备份,不 reload ───────────────────────────────
set_health 18081 1.2.0 green
touch "$MOCK_FLAGS/nginx-t-fail"
: > "$MOCK_LOG"
run_fail "nginx -t 失败还原备份" "nginx -t 校验失败" "$BG_BIN/s2a-deploy" "$SLUG" v1.2.0
rm -f "$MOCK_FLAGS/nginx-t-fail"
assert_upstream_port 18080
[ ! -f "$UPSTREAM.bak" ] || fail "备份文件应已还原(mv 回原名)"
grep -q "nginx -s reload" "$MOCK_LOG" && fail "nginx -t 失败后不得 reload (FR-3.4)"
[ ! -f "$MOCK_STATE/running-$SLUG-green" ] || fail "nginx -t 失败应回收新 slot"

# ── S6: slot 校验失败(端口上是残留旧容器) ────────────────────────────────
set_health 18081 1.2.0 blue
run_fail "slot 校验拦截" "slot 校验失败" "$BG_BIN/s2a-deploy" "$SLUG" v1.2.0
assert_upstream_port 18080
[ ! -f "$MOCK_STATE/running-$SLUG-green" ] || fail "slot 校验失败应回收新 slot"

# ── S7: 版本校验失败 ─────────────────────────────────────────────────────
set_health 18081 9.9.9 green
run_fail "版本校验拦截" "版本校验失败" "$BG_BIN/s2a-deploy" "$SLUG" v1.2.0
assert_upstream_port 18080

# ── S8: 非版本号 tag 跳过强校验 ──────────────────────────────────────────
set_health 18081 1.3.0 green
run_ok "非版本号 tag 发布" "$BG_BIN/s2a-deploy" "$SLUG" main
assert_upstream_port 18081
assert_state slot green
assert_state tag main

# ── S9: teardown 安全闸:拒绝回收当前生效 slot ───────────────────────────
run_fail "teardown 拒绝回收生效 slot" "拒绝回收" "$BG_BIN/s2a-teardown" "$SLUG" green
[ -f "$MOCK_STATE/running-$SLUG-green" ] || fail "生效 slot 不得被回收"

# ── S10: teardown 回收非生效 slot ────────────────────────────────────────
run_ok "teardown 回收旧 slot" "$BG_BIN/s2a-teardown" "$SLUG" blue
[ ! -f "$MOCK_STATE/running-$SLUG-blue" ] || fail "旧 slot 应被回收"

# ── S11: 降级回滚(旧 slot 已回收 → 以 prev_tag 重新发布) ─────────────────
set_health 18080 1.0.0 blue
run_ok "降级回滚" "$BG_BIN/s2a-rollback" "$SLUG"
grep -q "降级" "$TEMP_DIR/out" || fail "应明示降级路径与耗时差异 (FR-4.2)"
assert_upstream_port 18080
assert_state slot blue
[ -f "$MOCK_STATE/running-$SLUG-blue" ] || fail "降级回滚应重新拉起旧版本容器"

# ── S12: s2a-status 输出与不一致检测 ─────────────────────────────────────
run_ok "status 输出" "$BG_BIN/s2a-status" "$SLUG"
grep -q "生效(nginx upstream): blue:18080" "$TEMP_DIR/out" || fail "status 应显示生效 slot"
sed -i.bak 's/^slot=blue/slot=green/' "$STATE" && rm -f "$STATE.bak" # 人为制造不一致
run_ok "status 不一致检测" "$BG_BIN/s2a-status" "$SLUG"
grep -q "不一致" "$TEMP_DIR/out" || fail "STATE 与 nginx 不一致时应告警 (FR-7.3)"
grep -q "以 nginx 为准" "$TEMP_DIR/out" || fail "不一致时应声明以 nginx 为准 (D1)"

echo "bluegreen-deploy-dryrun-test: all passed"

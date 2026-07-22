#!/bin/bash
# s2a-render 行为测试：渲染产物正确性、幂等性、清单校验拦截。
# nginx 用 PATH mock；docker 可用时对渲染产物做 compose config 真实校验。

set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
BG_BIN="$ROOT_DIR/deploy/blue-green/bin"
TEMP_DIR=$(mktemp -d)
trap 'rm -rf "$TEMP_DIR"' EXIT

export MOCK_LOG="$TEMP_DIR/mock.log"
export MOCK_FLAGS="$TEMP_DIR/flags"
mkdir -p "$MOCK_FLAGS" "$TEMP_DIR/bin"

cat > "$TEMP_DIR/bin/nginx" <<'EOF'
#!/bin/bash
echo "nginx $*" >> "$MOCK_LOG"
if [ "$1" = "-t" ] && [ -f "$MOCK_FLAGS/nginx-t-fail" ]; then exit 1; fi
exit 0
EOF
chmod +x "$TEMP_DIR/bin/nginx"
export PATH="$TEMP_DIR/bin:$PATH"

# 每个用例独立的 S2A 环境
reset_env() {
    rm -rf "$TEMP_DIR/srv" "$TEMP_DIR/nginx"
    export S2A_ROOT="$TEMP_DIR/srv"
    export S2A_NGINX_DIR="$TEMP_DIR/nginx/sub2api"
    export S2A_NGINX_SNIPPET_DIR="$TEMP_DIR/nginx/snippets"
    mkdir -p "$S2A_ROOT/registry/envs"
    : > "$MOCK_LOG"
}

# 假 TLS 文件
mkdir -p "$TEMP_DIR/tls"
touch "$TEMP_DIR/tls/cert.pem" "$TEMP_DIR/tls/key.pem"

write_sites() {
    cat > "$S2A_ROOT/registry/sites.yaml"
}

valid_sites() {
    write_sites <<EOF
defaults:
  image_repo: weishaw/sub2api
  bind_host: 127.0.0.1
  drain_seconds: 960
  tz: Asia/Shanghai
stacks:
  - slug: api-prod
    domain: api.example.com
    port_base: 18080
    image_tag: v1.4.2
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
  - slug: api-staging
    domain: staging.example.com
    port_base: 18090
    image_tag: main
    drain_seconds: 60
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
EOF
}

fail() {
    echo "FAIL: $*" >&2
    [ ! -s "$TEMP_DIR/out" ] || cat "$TEMP_DIR/out" >&2
    exit 1
}

expect_render_ok() {
    "$BG_BIN/s2a-render" > "$TEMP_DIR/out" 2>&1 || fail "$1 (render 应成功)"
    echo "ok: $1"
}

expect_render_fail() {
    local desc="$1" want="$2"
    if "$BG_BIN/s2a-render" > "$TEMP_DIR/out" 2>&1; then
        fail "$desc (render 应失败)"
    fi
    grep -q "$want" "$TEMP_DIR/out" || fail "$desc (缺少提示 '$want')"
    echo "ok: $desc"
}

# ── 1. 正常渲染：产物齐全且内容正确 ──────────────────────────────────────
reset_env
valid_sites
expect_render_ok "双站点正常渲染"

for f in "$S2A_ROOT/stacks/api-prod/compose.data.yml" \
    "$S2A_ROOT/stacks/api-prod/compose.app.yml" \
    "$S2A_ROOT/stacks/api-staging/compose.app.yml" \
    "$S2A_NGINX_DIR/sites/api-prod.conf" \
    "$S2A_NGINX_DIR/sites/api-staging.conf" \
    "$S2A_NGINX_DIR/upstreams/api-prod.conf" \
    "$S2A_NGINX_DIR/upstreams/api-staging.conf" \
    "$S2A_NGINX_SNIPPET_DIR/sub2api-proxy.conf"; do
    [ -f "$f" ] || fail "缺少渲染产物: $f"
done
echo "ok: 产物文件齐全"

grep -q "server 127.0.0.1:18080" "$S2A_NGINX_DIR/upstreams/api-prod.conf" \
    || fail "upstream 初始应指向 blue 端口 18080"
grep -q "upstream api_prod" "$S2A_NGINX_DIR/upstreams/api-prod.conf" \
    || fail "upstream 名应做 - → _ 转换"
grep -q "server_name api.example.com" "$S2A_NGINX_DIR/sites/api-prod.conf" \
    || fail "site conf 缺少域名"
echo "ok: upstream/site 内容正确"

app_yml="$S2A_ROOT/stacks/api-prod/compose.app.yml"
grep -q 'container_name' "$app_yml" && fail "compose 产物不得含 container_name (FR-5.7)"
grep -q '"${BIND_HOST}' "$app_yml" && fail "BIND_HOST 应在渲染期解析"
grep -q '"127.0.0.1:${APP_PORT' "$app_yml" || fail "app 端口应绑定 127.0.0.1 且保留运行期 APP_PORT"
grep -q 'stop_grace_period: 960s' "$app_yml" || fail "stop_grace_period 应绑定 drain_seconds"
grep -q 'SERVER_SHUTDOWN_TIMEOUT_SECONDS=960' "$app_yml" || fail "SHUTDOWN 超时应绑定 drain_seconds"
grep -q '${SLOT:?SLOT is required}' "$app_yml" || fail "SLOT 应保留为运行期变量"
grep -q 'stop_grace_period: 60s' "$S2A_ROOT/stacks/api-staging/compose.app.yml" \
    || fail "staging 的 drain_seconds 覆盖未生效"
for var in SLUG IMAGE_REPO DRAIN_SECONDS TZ DOMAIN TLS_CERT PROXY_READ_TIMEOUT; do
    if grep -q "\${${var}}" "$app_yml" "$S2A_NGINX_DIR/sites/api-prod.conf" 2>/dev/null; then
        fail "渲染期变量 \${$var} 未被解析"
    fi
done
echo "ok: 模板渲染期/运行期变量分离正确"

grep -q "nginx -t" "$MOCK_LOG" || fail "render 应执行 nginx -t"
grep -q "nginx -s reload" "$MOCK_LOG" || fail "render 应执行 nginx reload"
echo "ok: nginx 校验与 reload 已执行"

# ── 2. 幂等性：重跑不覆盖 upstream，其余产物无 diff（M7） ────────────────
sed -i.bak 's/18080/18081/' "$S2A_NGINX_DIR/upstreams/api-prod.conf" # 模拟已切至 green
rm -f "$S2A_NGINX_DIR/upstreams/api-prod.conf.bak"
sum_before=$( (cd "$S2A_ROOT/stacks" && find . -type f -exec shasum {} \;) | sort)
expect_render_ok "重复渲染"
grep -q "server 127.0.0.1:18081" "$S2A_NGINX_DIR/upstreams/api-prod.conf" \
    || fail "重复渲染不得改写已存在的 upstream (FR-5.4)"
sum_after=$( (cd "$S2A_ROOT/stacks" && find . -type f -exec shasum {} \;) | sort)
[ "$sum_before" = "$sum_after" ] || fail "重复渲染产物应零 diff (M7)"
echo "ok: 渲染幂等,upstream 不被触碰"

# ── 3. 端口区间重叠拦截 ──────────────────────────────────────────────────
reset_env
write_sites <<EOF
defaults: { tz: UTC }
stacks:
  - slug: a-prod
    domain: a.example.com
    port_base: 18080
    image_tag: v1
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
  - slug: b-prod
    domain: b.example.com
    port_base: 18085
    image_tag: v1
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
EOF
expect_render_fail "端口区间重叠拦截" "端口区间重叠"
[ ! -d "$S2A_ROOT/stacks/a-prod" ] || fail "校验失败时不得产出文件"
echo "ok: 校验失败零产出"

# ── 4. 域名重复拦截 ──────────────────────────────────────────────────────
reset_env
write_sites <<EOF
defaults: { tz: UTC }
stacks:
  - slug: a-prod
    domain: same.example.com
    port_base: 18080
    image_tag: v1
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
  - slug: b-prod
    domain: same.example.com
    port_base: 18090
    image_tag: v1
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
EOF
expect_render_fail "域名重复拦截" "域名重复"

# ── 5. 证书缺失拦截 ──────────────────────────────────────────────────────
reset_env
write_sites <<EOF
defaults: { tz: UTC }
stacks:
  - slug: a-prod
    domain: a.example.com
    port_base: 18080
    image_tag: v1
    tls: { cert: $TEMP_DIR/tls/no-such.pem, key: $TEMP_DIR/tls/key.pem }
EOF
expect_render_fail "证书缺失拦截" "证书不存在"

# ── 6. 非法 slug 拦截 ────────────────────────────────────────────────────
reset_env
write_sites <<EOF
defaults: { tz: UTC }
stacks:
  - slug: Bad_Slug
    domain: a.example.com
    port_base: 18080
    image_tag: v1
    tls: { cert: $TEMP_DIR/tls/cert.pem, key: $TEMP_DIR/tls/key.pem }
EOF
expect_render_fail "非法 slug 拦截" "slug 非法"

# ── 7. nginx -t 失败时不 reload ──────────────────────────────────────────
reset_env
valid_sites
touch "$MOCK_FLAGS/nginx-t-fail"
expect_render_fail "nginx -t 失败中止" "nginx -t 校验失败"
grep -q "nginx -s reload" "$MOCK_LOG" && fail "nginx -t 失败后不得 reload"
rm -f "$MOCK_FLAGS/nginx-t-fail"
echo "ok: nginx -t 失败未触发 reload"

# ── 8. 渲染产物通过真实 docker compose config 校验（docker 可用时） ──────
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    reset_env
    valid_sites
    expect_render_ok "为 compose 校验重新渲染"
    stack="$S2A_ROOT/stacks/api-prod"
    printf 'POSTGRES_PASSWORD=test-secret\n' > "$stack/.env"
    (cd "$stack" \
        && SLOT=blue APP_PORT=18080 IMAGE_TAG=v1.4.2 \
            docker compose -f compose.app.yml config --quiet \
        && docker compose -f compose.data.yml config --quiet) \
        || fail "渲染产物未通过 docker compose config 校验"
    echo "ok: 渲染产物通过 docker compose config 真实校验"
else
    echo "skip: docker 不可用,跳过 compose config 真实校验"
fi

echo "bluegreen-render-test: all passed"

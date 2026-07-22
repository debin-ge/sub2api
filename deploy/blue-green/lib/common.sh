# shellcheck shell=bash
# s2a-* 脚本的公共库。由 bin/ 下各脚本 source，不直接执行。
#
# 可通过环境变量覆盖的路径（测试与非标准布局用）：
#   S2A_ROOT                默认 /srv/sub2api
#   S2A_NGINX_DIR           默认 /etc/nginx/sub2api
#   S2A_NGINX_SNIPPET_DIR   默认 /etc/nginx/snippets

S2A_ROOT="${S2A_ROOT:-/srv/sub2api}"
REGISTRY_FILE="$S2A_ROOT/registry/sites.yaml"
ENVS_DIR="$S2A_ROOT/registry/envs"
STACKS_DIR="$S2A_ROOT/stacks"
NGINX_S2A_DIR="${S2A_NGINX_DIR:-/etc/nginx/sub2api}"
NGINX_UPSTREAMS_DIR="$NGINX_S2A_DIR/upstreams"
NGINX_SITES_DIR="$NGINX_S2A_DIR/sites"
NGINX_SNIPPET_DIR="${S2A_NGINX_SNIPPET_DIR:-/etc/nginx/snippets}"

# 模板/snippet 优先取 $S2A_ROOT 下的部署副本，缺失时回退到脚本所在仓库副本，
# 使脚本既可 rsync 到服务器运行，也可直接从仓库 checkout 运行。
_s2a_lib_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
if [ -d "$S2A_ROOT/templates" ]; then
    TEMPLATES_DIR="$S2A_ROOT/templates"
else
    TEMPLATES_DIR="$_s2a_lib_dir/../templates"
fi
if [ -f "$S2A_ROOT/snippets/sub2api-proxy.conf" ]; then
    SNIPPET_SRC="$S2A_ROOT/snippets/sub2api-proxy.conf"
else
    SNIPPET_SRC="$_s2a_lib_dir/../snippets/sub2api-proxy.conf"
fi

log()  { printf '[%s] %s\n' "$(date '+%H:%M:%S')" "$*"; }
warn() { printf '[%s] WARN: %s\n' "$(date '+%H:%M:%S')" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "$(date '+%H:%M:%S')" "$*" >&2; exit 1; }

require_cmd() {
    local missing=""
    local cmd
    for cmd in "$@"; do
        command -v "$cmd" >/dev/null 2>&1 || missing="$missing $cmd"
    done
    [ -z "$missing" ] || die "缺少依赖:$missing（安装后重试）"
}

# ---------------------------------------------------------------------------
# sites.yaml 解析：转成 JSON 后统一用 jq 查询。
# 解析器优先级：yq(mikefarah v4) > python3+PyYAML。
# ---------------------------------------------------------------------------

SITES_JSON=""

load_sites() {
    [ -f "$REGISTRY_FILE" ] || die "清单不存在: $REGISTRY_FILE"
    require_cmd jq
    if command -v yq >/dev/null 2>&1; then
        SITES_JSON=$(yq -o=json eval '.' "$REGISTRY_FILE")
    else
        # PATH 里的 python3 可能没有 PyYAML（如 Homebrew 版），逐个候选探测
        local py found_py=""
        for py in python3 /usr/bin/python3; do
            if command -v "$py" >/dev/null 2>&1 \
                && "$py" -c 'import yaml' >/dev/null 2>&1; then
                found_py="$py"
                break
            fi
        done
        [ -n "$found_py" ] || die "解析 sites.yaml 需要 yq(v4) 或 python3+PyYAML,均未找到"
        SITES_JSON=$("$found_py" -c '
import json, sys, yaml
with open(sys.argv[1]) as f:
    print(json.dumps(yaml.safe_load(f)))
' "$REGISTRY_FILE")
    fi
    [ -n "$SITES_JSON" ] || die "解析 $REGISTRY_FILE 失败"
}

sites_jq() {
    printf '%s' "$SITES_JSON" | jq "$@"
}

all_slugs() {
    sites_jq -r '.stacks[].slug'
}

slug_exists() {
    [ -n "$(sites_jq -r --arg s "$1" '.stacks[] | select(.slug==$s) | .slug')" ]
}

# site_attr <slug> <key> [default] —— stack 级取值，缺失时回退 defaults，再回退默认参数
site_attr() {
    local slug="$1" key="$2" default="${3:-}"
    local v
    v=$(sites_jq -r --arg s "$slug" --arg k "$key" \
        '((.stacks[] | select(.slug==$s) | .[$k]) // .defaults[$k]) // empty')
    if [ -n "$v" ]; then printf '%s' "$v"; else printf '%s' "$default"; fi
}

# site_path_attr <slug> <jq-path> —— 嵌套字段（如 .tls.cert），无 defaults 回退
site_path_attr() {
    local slug="$1" path="$2"
    sites_jq -r --arg s "$1" ".stacks[] | select(.slug==\$s) | ${path} // empty"
}

slug_us() { printf '%s' "${1//-/_}"; }

blue_port()  { printf '%s' "$1"; }
green_port() { printf '%s' "$(( $1 + 1 ))"; }

port_for_slot() {
    local port_base="$1" slot="$2"
    case "$slot" in
        blue)  blue_port "$port_base" ;;
        green) green_port "$port_base" ;;
        *) die "非法 slot: $slot" ;;
    esac
}

other_slot() {
    case "$1" in
        blue) printf 'green' ;;
        green) printf 'blue' ;;
        *) die "非法 slot: $1" ;;
    esac
}

# ---------------------------------------------------------------------------
# 模板渲染：只替换显式列出的 ${NAME}，其余（含 nginx 的 $host、compose 运行期
# 的 ${SLOT:?} 等）原样保留。参数形如 NAME=value。
# ---------------------------------------------------------------------------
render_template() {
    local file="$1"; shift
    [ -f "$file" ] || die "模板不存在: $file"
    local content pair name value
    content=$(cat "$file")
    for pair in "$@"; do
        name="${pair%%=*}"
        value="${pair#*=}"
        content="${content//\$\{${name}\}/${value}}"
    done
    printf '%s\n' "$content"
}

# ---------------------------------------------------------------------------
# 单一真相源（D1）：当前生效端口只从 nginx upstream 文件读取。
# ---------------------------------------------------------------------------
upstream_conf_path() { printf '%s/%s.conf' "$NGINX_UPSTREAMS_DIR" "$1"; }

current_port_from_upstream() {
    local conf
    conf=$(upstream_conf_path "$1")
    [ -f "$conf" ] || return 1
    grep -oE 'server 127\.0\.0\.1:[0-9]+' "$conf" | head -1 | grep -oE '[0-9]+$'
}

slot_for_port() {
    local port_base="$1" port="$2"
    if [ "$port" = "$(blue_port "$port_base")" ]; then
        printf 'blue'
    elif [ "$port" = "$(green_port "$port_base")" ]; then
        printf 'green'
    else
        return 1
    fi
}

render_upstream_conf() {
    local slug="$1" port="$2" slot="$3" tag="$4"
    render_template "$TEMPLATES_DIR/nginx-upstream.conf.tmpl" \
        "SLUG=$slug" \
        "SLUG_US=$(slug_us "$slug")" \
        "APP_PORT=$port" \
        "SLOT=$slot" \
        "IMAGE_TAG=$tag" \
        "TIMESTAMP=$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
}

nginx_test_and_reload() {
    nginx -t || return 1
    nginx -s reload
}

# ---------------------------------------------------------------------------
# STATE 文件（仅辅助记录，不作为真相源）
# ---------------------------------------------------------------------------
state_file() { printf '%s/%s/STATE' "$STACKS_DIR" "$1"; }

state_get() {
    local file
    file=$(state_file "$1")
    [ -f "$file" ] || return 1
    sed -n "s/^${2}=//p" "$file" | head -1
}

state_write() {
    local slug="$1" slot="$2" tag="$3" prev_slot="$4" prev_tag="$5"
    local file tmp
    file=$(state_file "$slug")
    tmp="${file}.tmp"
    printf 'slot=%s\ntag=%s\nprev_slot=%s\nprev_tag=%s\nat=%s\n' \
        "$slot" "$tag" "$prev_slot" "$prev_tag" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" > "$tmp"
    mv "$tmp" "$file"
}

# ---------------------------------------------------------------------------
# compose 封装：app 层的 compose 文件含运行期变量，所有操作都必须带全变量。
# ---------------------------------------------------------------------------
data_compose() {
    local slug="$1"; shift
    docker compose -p "${slug}-data" \
        --project-directory "$STACKS_DIR/$slug" \
        -f "$STACKS_DIR/$slug/compose.data.yml" "$@"
}

app_compose() {
    local slug="$1" slot="$2" port="$3" tag="$4"; shift 4
    SLOT="$slot" APP_PORT="$port" IMAGE_TAG="$tag" \
        docker compose -p "${slug}-${slot}" \
        --project-directory "$STACKS_DIR/$slug" \
        -f "$STACKS_DIR/$slug/compose.app.yml" "$@"
}

app_running() {
    local slug="$1" slot="$2" port="$3" tag="${4:-unknown}"
    [ -n "$(app_compose "$slug" "$slot" "$port" "$tag" ps --status running -q 2>/dev/null)" ]
}

# ---------------------------------------------------------------------------
# 排空定时器：优先 systemd-run；无 systemd 时降级为后台 sleep 子进程 + pid 文件。
# ---------------------------------------------------------------------------
drain_unit() { printf 's2a-drain-%s-%s' "$1" "$2"; }
drain_pid_file() { printf '%s/%s/drain-%s.pid' "$STACKS_DIR" "$1" "$2"; }

schedule_teardown() {
    local slug="$1" slot="$2" delay="$3"
    local self_dir
    self_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../bin" && pwd)
    cancel_teardown "$slug" "$slot" >/dev/null 2>&1 || true
    if command -v systemd-run >/dev/null 2>&1; then
        systemd-run --unit="$(drain_unit "$slug" "$slot")" --collect \
            --on-active="${delay}s" \
            --setenv=S2A_ROOT="$S2A_ROOT" \
            --setenv=S2A_NGINX_DIR="$NGINX_S2A_DIR" \
            "$self_dir/s2a-teardown" "$slug" "$slot" >/dev/null
    else
        (
            sleep "$delay"
            "$self_dir/s2a-teardown" "$slug" "$slot"
            rm -f "$(drain_pid_file "$slug" "$slot")"
        ) >/dev/null 2>&1 &
        printf '%s\n' "$!" > "$(drain_pid_file "$slug" "$slot")"
    fi
}

cancel_teardown() {
    local slug="$1" slot="$2"
    local cancelled=1
    if command -v systemctl >/dev/null 2>&1; then
        local unit
        unit="$(drain_unit "$slug" "$slot")"
        if systemctl stop "${unit}.timer" >/dev/null 2>&1; then cancelled=0; fi
        if systemctl stop "${unit}.service" >/dev/null 2>&1; then cancelled=0; fi
    fi
    local pid_file
    pid_file=$(drain_pid_file "$slug" "$slot")
    if [ -f "$pid_file" ]; then
        local pid
        pid=$(cat "$pid_file")
        if [ -n "$pid" ] && kill "$pid" >/dev/null 2>&1; then cancelled=0; fi
        rm -f "$pid_file"
    fi
    return "$cancelled"
}

# teardown_pending <slug> <slot> —— 有待执行的排空定时器则输出剩余描述并返回 0
teardown_pending() {
    local slug="$1" slot="$2"
    local unit
    unit="$(drain_unit "$slug" "$slot")"
    if command -v systemctl >/dev/null 2>&1; then
        local left
        left=$(systemctl list-timers --all 2>/dev/null | grep -F "${unit}.timer" || true)
        if [ -n "$left" ]; then
            printf '%s' "$left"
            return 0
        fi
    fi
    local pid_file
    pid_file=$(drain_pid_file "$slug" "$slot")
    if [ -f "$pid_file" ] && kill -0 "$(cat "$pid_file")" >/dev/null 2>&1; then
        printf 'pid=%s (sleep 降级模式,无剩余时间信息)' "$(cat "$pid_file")"
        return 0
    fi
    return 1
}

# ---------------------------------------------------------------------------
# 并发互斥：同一 stack 同时只允许一个 s2a-deploy / s2a-rollback。
# mkdir 原子锁，跨平台（macOS 无 flock）。
# ---------------------------------------------------------------------------
acquire_stack_lock() {
    local slug="$1"
    local lock_dir="$STACKS_DIR/$slug/.op.lock"
    if ! mkdir "$lock_dir" 2>/dev/null; then
        local holder
        holder=$(cat "$lock_dir/pid" 2>/dev/null || true)
        if [ -n "$holder" ] && ! kill -0 "$holder" 2>/dev/null; then
            warn "清理残留锁（持有者 pid=$holder 已退出）"
            rm -rf "$lock_dir"
            mkdir "$lock_dir" 2>/dev/null || die "获取 $slug 操作锁失败"
        else
            die "另一个 s2a 操作正在进行（pid=${holder:-unknown}）,拒绝并发执行"
        fi
    fi
    printf '%s\n' "$$" > "$lock_dir/pid"
    # shellcheck disable=SC2064
    trap "rm -rf '$lock_dir'" EXIT
}

# 健康探测：返回 body（探测失败返回非 0）
health_probe() {
    curl -fsS --max-time 5 "http://127.0.0.1:$1/health" 2>/dev/null
}

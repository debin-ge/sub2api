# Sub2API 蓝绿部署 CLI

`s2a` 是一个原生 Go 单文件部署工具。YAML 解析、模板、Nginx snippet 和 HTTP
健康探测均已编译进二进制；服务器不再需要复制 `bin/`、`lib/`、`templates/`
或 `snippets/`，也不依赖 Bash、curl、jq、yq、Python/PyYAML。

除状态查询外，所有写操作必须使用 root 权限，并直接在部署目录执行：

```bash
cd /srv/sites
sudo ./s2a <command>
```

## 服务器依赖

只需要：

- Docker Engine，且 daemon 可访问；
- Docker Compose v2（`docker compose`）；
- 宿主机 Nginx；
- `systemd-run`（可选）。不存在时，`s2a` 会启动自身的后台子进程执行排空回收。

`sudo ./s2a check` 和 `sudo ./s2a init <slug>` 会在改变 Docker 状态前检查：

- 当前是否为 root；
- `docker compose version`；
- `docker info`；
- `nginx -t`；
- `nginx -T` 是否已加载 `s2a-managed-http-config`；
- main 上下文是否配置 `worker_shutdown_timeout`。

## 构建二进制

需要 Go 1.22 或更高版本。在源码目录运行：

```bash
cd deploy/blue-green

make test
make build                 # 当前操作系统/架构，产物 dist/s2a
make release               # Linux amd64 + arm64
```

Linux 发布产物：

```text
dist/s2a-linux-amd64
dist/s2a-linux-arm64
```

二进制通过 `-trimpath`、`CGO_ENABLED=0` 构建，不依赖服务器上的 Go 或 libc 动态库。

## 一次性安装

以 Linux amd64 为例：

```bash
sudo mkdir -p /srv/sites
sudo cp dist/s2a-linux-amd64 /srv/sites/s2a
sudo chmod 755 /srv/sites/s2a
cd /srv/sites

sudo ./s2a bootstrap
```

`bootstrap` 不覆盖已有配置，会创建：

```text
/srv/sites/
├── s2a
├── env.example
├── registry/
│   ├── sites.yaml
│   └── envs/
└── stacks/
```

日常只需要编辑两类文件：

```text
registry/sites.yaml
registry/envs/<slug>.env
```

`stacks/`、Compose 文件、Nginx 配置、STATE 和排空 PID 均由 `s2a` 管理，不应手工修改。

## Nginx 一次性接入

在 `/etc/nginx/nginx.conf` 的 main 上下文（`events {}` / `http {}` 外）加入：

```nginx
worker_shutdown_timeout 1200s;
```

在 `http {}` 内加入：

```nginx
include /etc/nginx/sub2api/*.conf;
```

不要再同时 include `sub2api/upstreams/*.conf` 或 `sub2api/sites/*.conf`，这两个 include
由 `s2a` 生成的 `http.conf` 统一维护，否则会出现重复配置。

`worker_shutdown_timeout` 应大于最长流式响应时间。默认值 1200 秒覆盖应用默认的
900 秒流上限和额外排空余量，避免多次 reload 后旧 worker 无限堆积。

首次执行 `render` 前，`http.conf` 尚不存在，此时可先检查 Nginx 配置语法；完整接入
检查由后续的 `init` 执行：

```bash
sudo nginx -t
```

## 站点配置

`registry/sites.yaml` 是非密钥配置的唯一真相源：

```yaml
defaults:
  image_repo: weishaw/sub2api
  bind_host: 127.0.0.1
  drain_seconds: 960
  health_timeout_seconds: 300
  health_interval_seconds: 3
  client_max_body_size: 32m
  proxy_connect_timeout: 10s
  proxy_send_timeout: 960s
  proxy_read_timeout: 960s
  tz: Asia/Shanghai

stacks:
  - slug: api-staging
    domain: staging.example.com
    port_base: 18080
    image_tag: v1.4.2
    tls:
      cert: /etc/letsencrypt/live/staging.example.com/fullchain.pem
      key: /etc/letsencrypt/live/staging.example.com/privkey.pem
```

参数说明：

| 参数 | 必填 | 说明 |
|---|---:|---|
| `slug` | 是 | 站点标识，仅允许小写字母、数字和连字符 |
| `domain` | 是 | Nginx `server_name` |
| `port_base` | 是 | blue 使用此端口，green 使用 `port_base+1`；每个站点预留 10 个端口 |
| `image_tag` | 否 | 未在 deploy 命令传 tag 时使用 |
| `image_repo` | 否 | 镜像仓库，默认 `weishaw/sub2api` |
| `bind_host` | 否 | 宿主机监听地址，默认 `127.0.0.1` |
| `drain_seconds` | 否 | 切流后的旧实例排空时间，也用于应用优雅关闭 |
| `health_timeout_seconds` | 否 | 新实例健康门禁总超时，应覆盖数据库迁移时间 |
| `health_interval_seconds` | 否 | 健康探测间隔 |
| `client_max_body_size` | 否 | Nginx 请求体上限 |
| `proxy_*_timeout` | 否 | Nginx 上游连接、发送和读取超时 |
| `tz` | 否 | 容器时区 |
| `tls.cert` / `tls.key` | 是 | 已存在且 Nginx 可读取的证书与私钥绝对路径 |

`s2a render` 会在写文件前检查 slug/域名重复、每站点 10 端口区间重叠、端口范围和
TLS 文件。已存在的 upstream 文件不会被 render 覆盖，当前流量方向始终以该文件为准。

每个站点的密钥：

```bash
sudo cp env.example registry/envs/api-staging.env
sudo chmod 600 registry/envs/api-staging.env
sudo vim registry/envs/api-staging.env
```

`init` 会自动将权限收紧为 `0600`，并在生成的 stack 内创建 `.env` 软链接。

## 首次部署

```bash
cd /srv/sites

sudo ./s2a render
sudo ./s2a init api-staging
sudo ./s2a deploy api-staging v1.4.2
./s2a status api-staging
```

`render` 会生成 Compose/Nginx 配置、安装公共 proxy snippet、执行 `nginx -t` 后
reload。`init` 会执行完整依赖检查、创建共享目录和 external network，然后启动
PostgreSQL/Redis 并等待健康。

首次 deploy 会在 upstream 当前指向的 blue slot 原地启动，不创建无意义的排空任务。

## 日常发布

```bash
sudo ./s2a deploy api-staging v1.4.3
```

流程：

1. 从 Nginx upstream 读取当前 slot；
2. 对同一 stack 加操作锁，并清理已退出进程留下的死锁；
3. 确认数据层健康；
4. 拉起另一 slot，并由应用执行数据库迁移；
5. 使用内置 HTTP 客户端轮询 `/health`；
6. 强制校验响应中的 `slot` 和 `version`；版本号 tag 还会做等值校验；
7. 备份并原子改写 upstream，`nginx -t` 成功后 reload；
8. 写入 STATE，异步排空旧 slot。

健康门禁或身份校验失败会输出新容器日志、回收新 slot，并保持 upstream 不变。
`nginx -t` 或 reload 失败会还原 upstream 备份，不切换线上流量。

## 回滚和回收

```bash
sudo ./s2a rollback api-staging
```

- 排空窗口内，旧 slot 仍运行、健康且身份正确时，仅切回 upstream，走快速路径；
- 旧 slot 已回收或不健康时，用 STATE 的 `prev_tag` 重新执行完整发布；
- 回滚不会撤销已执行的数据库迁移，旧代码必须兼容新 schema。

手工回收：

```bash
sudo ./s2a teardown api-staging green
```

teardown 会再次读取 Nginx upstream，拒绝回收当前生效 slot。即使排空任务未能在
回滚时取消，该安全闸也能避免误杀线上实例。

## 状态与命令

```bash
./s2a status
./s2a status api-staging
sudo ./s2a check
./s2a version
```

完整命令：

```text
bootstrap
check
render
init <slug>
deploy <slug> [image-tag]
rollback <slug>
status [slug]
teardown <slug> <blue|green>
version
```

可选全局参数必须写在命令前：

```bash
sudo ./s2a \
  --root /srv/sites \
  --nginx-dir /etc/nginx/sub2api \
  --nginx-snippet-dir /etc/nginx/snippets \
  render
```

默认根目录是 `s2a` 二进制所在目录。也可通过 `S2A_ROOT`、
`S2A_NGINX_DIR`、`S2A_NGINX_SNIPPET_DIR` 覆盖。

`status` 不要求 root，但执行用户必须有读取 Docker daemon 的权限。输出会同时显示
Nginx 实际方向、STATE、两个 slot 的容器/健康状态和待回收任务；两类状态不一致时
始终以 Nginx upstream 为准。

## 更新部署工具

只需替换单个文件，配置和运行数据不变：

```bash
sudo cp s2a-linux-amd64 /srv/sites/s2a.new
sudo chmod 755 /srv/sites/s2a.new
/srv/sites/s2a.new version
sudo mv /srv/sites/s2a.new /srv/sites/s2a
```

不要在排空后台子进程使用期间删除旧二进制 inode；使用上述同目录原子替换方式时，
已启动进程不受影响，后续任务使用新版本。

## 开发测试

```bash
cd deploy/blue-green
go test -race ./...
make release
```

Go 测试使用假的 Docker/Nginx/systemd 命令和本地 HTTP 服务，覆盖内嵌资源渲染、
依赖预检、初始化、首次发布、blue→green、快速回滚、Nginx 校验失败还原和 teardown
安全闸。数据库迁移规则仍由仓库的 `deploy/tests/migration-gate-test.sh` 单独验证。

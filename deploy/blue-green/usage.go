package main

import (
	"fmt"
	"io"
	"strings"
)

func printUsage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`
bgdeploy —— 单机多站点蓝绿部署工具

说明:
  下文假设可执行文件名为 bgdeploy。如果服务器上命名为 deploy，
  请将示例中的 ./bgdeploy 替换为 ./deploy。
  示例站点名为 api-staging，实际操作时替换为 sites.yaml 中的 slug。

用法:
  bgdeploy [全局参数] <命令> [参数]
  bgdeploy --help
  bgdeploy <命令> --help

全局参数必须写在命令前，例如:
  ./bgdeploy --config /etc/bgdeploy/runtime.yaml status api-staging

命令:
  bootstrap
      创建部署目录骨架和示例配置，但不覆盖已有文件。
      生成 runtime.yaml、registry/sites.yaml、env.example、envs/、stacks/。

  check
      检查 root 权限、Docker daemon、Docker Compose v2、Nginx、
      Nginx 一次性接入配置和部署工具生成的配置是否已加载。

  render
      校验 registry/sites.yaml，生成 Compose、Nginx site/upstream 和公共
      snippet，执行 nginx -t 后 reload。已存在的 upstream 不会被覆盖。
      会根据 Nginx 版本自动选择兼容的 HTTP/2 配置语法。

  init <slug>
      校验 envs/<slug>.env，建立 stack 内 .env 链接和 external network，
      启动并等待 PostgreSQL/Redis healthy。每个站点首次部署前执行一次。

  deploy <slug> [image-tag]
      首次发布或执行蓝绿发布。未传 image-tag 时使用 sites.yaml 的 image_tag。
      建议日常发布显式传入不可变镜像标签。

  rollback <slug>
      回退到上一 slot。排空窗口内直接切回仍运行的旧容器；旧容器已回收时，
      使用 STATE 中的上一镜像标签重新启动。不会回滚数据库迁移。

  status [slug]
      显示全部站点或指定站点的 Nginx 实际方向、STATE、blue/green 容器、
      健康状态和待回收任务。状态冲突时以 Nginx upstream 为准。

  teardown <slug> <blue|green>
      手工回收指定 slot。工具会重新读取 upstream，并拒绝回收当前生效 slot。

  version
      输出部署工具版本。

  help
      显示本帮助。

全局参数:
  --config <path>
      运行配置文件。默认: 当前工作目录/runtime.yaml

  --root <path>
      部署根目录。默认: 执行命令时的当前工作目录（pwd）

  --nginx-dir <path>
      Nginx 配置目录。默认: /etc/nginx/sites

  --nginx-snippet-dir <path>
      Nginx snippet 目录。默认: /etc/nginx/sites/snippets

运行配置 runtime.yaml:
  root: /srv/blue-green
  nginx_dir: /etc/nginx/sites
  nginx_snippet_dir: /etc/nginx/sites/snippets

环境变量:
  BGDEPLOY_CONFIG             指定 runtime.yaml 路径
  BGDEPLOY_ROOT               部署根目录
  BGDEPLOY_NGINX_DIR          Nginx 配置目录
  BGDEPLOY_NGINX_SNIPPET_DIR  Nginx snippet 目录

配置优先级:
  命令行参数 > BGDEPLOY_* 环境变量 > runtime.yaml > 内置默认值

部署目录:
  runtime.yaml                主机级路径配置，通常只需初始化时确认
  registry/sites.yaml         站点、镜像、端口、域名、TLS 和超时配置
  env.example                 站点环境变量模板
  envs/<slug>.env             站点密钥，必须是普通文件且权限为 0600
  stacks/<slug>/              工具生成的 Compose、STATE 和运行数据

通常只需要编辑:
  registry/sites.yaml
  envs/<slug>.env

sites.yaml 最小示例:
  defaults:
    image_repo: ghcr.io/example/application
    bind_host: 127.0.0.1
    drain_seconds: 960
    health_timeout_seconds: 300
    health_interval_seconds: 3
    tz: Asia/Shanghai

  stacks:
    - slug: api-staging
      domain: api.example.com
      port_base: 18080
      image_tag: 1.6.8
      tls:
        cert: /etc/letsencrypt/live/api.example.com/fullchain.pem
        key: /etc/letsencrypt/live/api.example.com/privkey.pem

端口规则:
  blue  使用 port_base
  green 使用 port_base + 1
  例如 port_base=18080: blue=18080，green=18081。

站点环境文件:
  sudo cp env.example envs/api-staging.env
  sudo chmod 600 envs/api-staging.env
  sudo vi envs/api-staging.env

必须填写且不得保留示例值:
  POSTGRES_PASSWORD
  REDIS_PASSWORD
  JWT_SECRET
  TOTP_ENCRYPTION_KEY
  ADMIN_EMAIL
  ADMIN_PASSWORD

Nginx 一次性接入:
  在 nginx.conf 的 main 上下文加入:
    worker_shutdown_timeout 1200s;

  在 nginx.conf 的 http {} 上下文加入:
    include /etc/nginx/sites/*.conf;

  不要再直接 include sites/upstreams/*.conf 或 sites/servers/*.conf，
  它们由生成的 /etc/nginx/sites/http.conf 统一加载。

首次安装:
  cd /srv/blue-green
  sudo ./bgdeploy bootstrap

  # 编辑 runtime.yaml、registry/sites.yaml 和 envs/<slug>.env，
  # 并完成上面的 Nginx 一次性接入后执行:
  sudo ./bgdeploy render
  sudo ./bgdeploy check
  sudo ./bgdeploy init api-staging
  sudo ./bgdeploy deploy api-staging 1.6.8
  ./bgdeploy status api-staging

日常蓝绿发布:
  cd /srv/blue-green
  ./bgdeploy status api-staging
  sudo ./bgdeploy deploy api-staging 1.6.9
  ./bgdeploy status api-staging

  假设当前为 blue:18080，发布会:
    1. 拉起 green:18081；
    2. 等待 /health 返回 status=ok；
    3. 校验 slot/version；旧镜像缺字段时复核 Docker 容器元数据；
    4. 原子改写 upstream，执行 nginx -t 并 reload；
    5. 在 drain_seconds 后自动回收 blue。

  下一次发布自动反向执行 green -> blue。

回滚:
  sudo ./bgdeploy rollback api-staging
  ./bgdeploy status api-staging

手工回收非生效 slot:
  sudo ./bgdeploy teardown api-staging blue

修改配置后的操作:
  修改 sites.yaml、域名、端口、TLS、镜像仓库或 Nginx 参数:
    sudo ./bgdeploy render

  修改 envs/<slug>.env:
    sudo chmod 600 envs/api-staging.env
    sudo ./bgdeploy deploy api-staging 1.6.9

失败与安全行为:
  - 健康或身份校验失败: 回收新 slot，不切换已有线上流量。
  - nginx -t 或 reload 失败: 还原 upstream，不切换流量。
  - 首次部署失败: upstream 没有可用容器，站点仍不可用，应修复后重试。
  - teardown 拒绝回收当前生效 slot。
  - 数据库迁移不会自动回滚，新旧版本必须兼容同一数据库 schema。
  - 环境变量变更应兼容排空窗口内仍运行的旧 slot。

排查命令:
  ./bgdeploy status api-staging
  docker ps --filter name=api-staging-
  nginx -t
  grep -n "server" /etc/nginx/sites/upstreams/api-staging.conf

更多帮助:
  ./bgdeploy --help
  ./bgdeploy help
  ./bgdeploy version
`))
}

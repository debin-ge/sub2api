# Sub2API 蓝绿部署（Nginx + Docker 多站点零停机发布）

本目录是多站点蓝绿部署的全部工具：4 份模板 + 1 份 nginx snippet + 6 个脚本 +
清单示例。设计文档见仓库根 `blue-green-deploy-design.v1.md` 与
`blue-green-deploy-prd.md`。

核心思路：

- 流量切换 = 改写 nginx upstream 单行 + `nginx -s reload`（旧 worker 保留在途
  SSE 连接直至自然结束）；
- 蓝绿只发生在应用容器层（blue/green 两个 compose project 轮换），postgres/redis
  常驻不参与；
- 所有站点的 compose 与 nginx 产物由一份 `sites.yaml` 清单 + 模板渲染，新增站点
  不手写任何配置文件。

## 依赖

服务器需具备（`s2a-*` 启动时自检）：

| 依赖 | 说明 |
|---|---|
| docker + docker compose v2 | 容器运行时 |
| nginx（宿主机） | 反代 + 流量切换，脚本需有 `nginx -t` / `-s reload` 权限（通常 root） |
| curl | 健康门禁探测 |
| jq | 解析 sites.yaml（转 JSON 后查询）与 /health 响应 |
| yq (mikefarah v4) 或 python3+PyYAML | 二选一，YAML → JSON |
| systemd-run（可选） | 排空定时器；缺失时自动降级为后台 sleep 子进程 + pid 文件 |

## 服务器目录布局

```
/srv/sub2api/
├── registry/
│   ├── sites.yaml                  # ★ 唯一真相源（复制 sites.example.yaml 修改）
│   └── envs/<slug>.env             # 每站点密钥，chmod 600，不入 git
├── templates/                      # rsync 自本目录 templates/
├── snippets/                       # rsync 自本目录 snippets/
├── bin/                            # rsync 自本目录 bin/ 与 lib/
├── lib/
└── stacks/<slug>/                  # 渲染产物 + 数据目录（s2a-render/init 生成）
    ├── compose.data.yml  compose.app.yml
    ├── .env -> ../../registry/envs/<slug>.env
    ├── STATE                       # 辅助记录；真相永远是 nginx upstream 文件
    ├── data/                       # /app/data，蓝绿共享（config.yaml + .installed）
    ├── postgres_data/  redis_data/
```

```
/etc/nginx/
├── nginx.conf                      # 见下方「首次引导」需加两行 include
├── snippets/sub2api-proxy.conf     # s2a-render 安装，SSE 透传参数
└── sub2api/
    ├── upstreams/<slug>.conf       # ★ 部署时唯一被改写的文件（单行 server）
    └── sites/<slug>.conf           # 渲染一次，之后不动
```

## 首次引导（每台服务器一次）

```bash
# 1. 目录与文件
mkdir -p /srv/sub2api/{registry/envs,stacks}
rsync -a deploy/blue-green/bin deploy/blue-green/lib \
        deploy/blue-green/templates deploy/blue-green/snippets /srv/sub2api/
cp deploy/blue-green/sites.example.yaml /srv/sub2api/registry/sites.yaml
ln -s /srv/sub2api/bin/s2a-* /usr/local/bin/   # 可选

# 2. nginx.conf 的 http {} 内加：
#      include /etc/nginx/sub2api/upstreams/*.conf;
#      include /etc/nginx/sub2api/sites/*.conf;
#    main 上下文（http {} 外）加：
#      worker_shutdown_timeout 1200s;   # 旧 worker 保留在途 SSE，但不无限期堆积
nginx -t && nginx -s reload
```

`worker_shutdown_timeout` 必须显式设置：nginx 默认不设置意味着旧 worker 永不强制
退出，频繁 reload 会堆积进程；1200s > 900s（流上限）+ 余量。

## 新增一个站点

```bash
# 1. 清单登记（分配一个未使用的 port_base 区间，每站点占 10 个端口）
vim /srv/sub2api/registry/sites.yaml

# 2. 密钥
cp deploy/.env.example /srv/sub2api/registry/envs/site3-prod.env
chmod 600 /srv/sub2api/registry/envs/site3-prod.env
openssl rand -hex 32   # 依次填 POSTGRES_PASSWORD / JWT_SECRET / TOTP_ENCRYPTION_KEY
vim /srv/sub2api/registry/envs/site3-prod.env

# 3. 证书
certbot certonly --webroot -w /var/www/certbot -d api.site3.com
# certbot 续期 deploy-hook 里需有 nginx -s reload

# 4. 渲染 + 初始化 + 首次部署
s2a-render                    # 校验冲突 → 生成产物 → nginx -t && reload
s2a-init   site3-prod         # network / 目录 / env 软链 / 数据层启动
s2a-deploy site3-prod v1.4.2  # 首次部署（无旧 slot，跳过排空）

# 5. 确认
s2a-status site3-prod
```

## 日常发布

```bash
s2a-deploy api-prod v1.4.3
```

流程：读取当前 slot（以 nginx upstream 文件为准）→ 数据层健康确认 → 拉起闲置
slot（此时容器启动会自动执行 DB 迁移，受 PG advisory lock 保护）→ 健康门禁
（默认 3s 间隔 / 300s 超时，需覆盖迁移时间）→ /health 的 version/slot 校验 →
改写 upstream + `nginx -t` + reload → 旧 slot 进入排空期（默认 960s）后自动回收。

- 门禁失败：自动打印新 slot 最近 200 行日志并回收，**nginx 配置未被触碰，线上
  无影响**；
- `nginx -t` 失败：自动还原 upstream 备份，不执行 reload；
- 命令在流量切换后立即返回，排空与回收由 systemd 定时器（或降级的后台进程）
  异步执行。

## 回滚

```bash
s2a-rollback api-prod
```

- **快速路径**（排空窗口内，旧容器还在且健康）：仅改回 upstream + reload，秒级。
  会先取消旧 slot 的排空定时器，防止切回后被定时任务销毁；
- **降级路径**（旧容器已回收）：以 `prev_tag` 重新执行完整发布，分钟级，命令会
  明确提示耗时差异；
- **回滚不撤销已应用的数据库迁移**。旧代码必须能在新 schema 上运行——这由
  expand-contract 迁移纪律保证（见 `backend/migrations/README.md`）。破坏该纪律
  的版本不能用蓝绿发布，必须安排停机窗口。

## 状态查询

```bash
s2a-status            # 全部 stack
s2a-status api-prod   # 单个
```

输出每个 stack 的：nginx upstream 实际指向（真相）、STATE 记录（不一致时高亮
告警）、两个 slot 的容器状态 / `/health` 探测（含 version、slot）、待执行的排空
定时器。

## 排障

| 症状 | 处理 |
|---|---|
| 健康门禁超时 | 脚本已自动回收新 slot，流量未切换。看输出的容器日志定位（多半是迁移失败或配置校验失败） |
| 切完流量后新版本报错 | `s2a-rollback <slug>`——排空窗口内秒级回退 |
| 排空窗口已过才发现问题 | `s2a-rollback` 自动降级为以 prev_tag 重新发布，分钟级 |
| `nginx -t` 失败 | 脚本已还原 `<slug>.conf.bak`，不会 reload 坏配置 |
| 迁移卡住 | 检查僵尸容器持有 advisory lock：`SELECT * FROM pg_locks WHERE locktype='advisory'` |
| 不确定哪个 slot 在服务 | `s2a-status`——以 nginx upstream 文件为准，STATE 仅作记录 |
| 排空期内需要再次发布 | 直接执行 `s2a-deploy`。脚本会取消目标 slot 的回收定时器并接管；若目标 slot 仍有在途流，会明确提示需等待其排空（最长 drain_seconds） |
| 两个 slot 都在跑但都不该跑 | `s2a-teardown <slug> <slot>` 手动回收指定 slot（有安全闸：拒绝回收当前生效 slot） |
| 磁盘被容器日志撑满 | 模板已配 json-file max-size 50m × max-file 5（每容器上限 250MB） |

## 测试

仓库内 `deploy/tests/`：

```bash
deploy/tests/bluegreen-render-test.sh        # 渲染幂等性 / 端口冲突 / 域名重复 / 证书缺失拦截
deploy/tests/bluegreen-deploy-dryrun-test.sh # mock docker/nginx/curl 验证 deploy/rollback 分支
```

## 注意事项

- **迁移纪律（expand-contract）**：蓝绿并存窗口内新旧版本共享同一数据库，迁移
  必须与上一版本兼容，CI 有 migration-gate 门禁拦截破坏性 DDL，详见
  `backend/migrations/README.md`；
- **并存窗口副作用**：进程内并发闸（`GATEWAY_IMAGE_CONCURRENCY_*`）在窗口内实际
  并发为配置值 2 倍；后台任务双实例并发安全性详见
  `docs/blue-green-background-task-audit.md`；
- 应用端口只绑定 127.0.0.1（`bind_host`），不对公网暴露；
- `drain_seconds` 与应用的 `SERVER_SHUTDOWN_TIMEOUT_SECONDS`、compose 的
  `stop_grace_period` 绑定同一值，模板已处理，不要手工拆开配置。

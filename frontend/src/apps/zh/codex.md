# Codex CLI

> OpenAI 官方 CLI。IDE 扩展、桌面版 Codex App 与 CLI 共用 `~/.codex/config.toml`，本页配置完三者都能用。

```client
name: Codex CLI
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.codex/config.toml
homepage: https://developers.openai.com/codex
```

## 1. 安装

```bash
npm install -g @openai/codex
```

**验证**：`codex --version` 输出版本号即成功。

## 2. 配置

打开 `~/.codex/config.toml`（Windows：`%USERPROFILE%\.codex\config.toml`），不存在就手动新建，写入：

```toml download=config.toml
# ================================================================
#  ~/.codex/config.toml · 由 {{SITE_NAME}} 生成
#  ① 本文件不含密钥。使用前在终端执行（务必先做）：
#        export API_KEY="在此粘贴 sk- 开头的密钥"
#     （Windows PowerShell:  $env:API_KEY="..."）
#  ② 把下面 model 换成 {{BASE_URL}}v1/models 里实际开放的模型名。
# ================================================================

model_provider = "{{SITE_NAME}}"
model = "gpt-5.5"                     # ← 改成 /v1/models 里的真实模型名
model_reasoning_effort = "medium"     # minimal | low | medium | high | xhigh

# 智能体行为（可选；新手保持默认即可）
approval_policy = "on-request"        # untrusted | on-request | never
sandbox_mode   = "workspace-write"    # read-only | workspace-write | danger-full-access

[model_providers."{{SITE_NAME}}"]
name     = "{{SITE_NAME}}"
base_url = "{{BASE_URL}}v1"
env_key  = "API_KEY"                  # Codex 从此环境变量读取密钥
wire_api = "responses"                # 固定 responses
# 弱网 / 长响应时可调大以下项
request_max_retries    = 4
stream_max_retries     = 5
stream_idle_timeout_ms = 300000
# 需要生图时取消下面两行的注释（见第 4 节；分组须已开启生图权限）
# requires_openai_auth = false
# http_headers = { "x-openai-actor-authorization" = "{{BASE_URL}}" }

# 生图开关属于 [features] 表，必须放在 provider 表之后（新版默认已开）
# [features]
# image_generation = true
```

- `model` 从 `/v1/models` 返回的 GPT/Codex 类模型名里挑。
- `base_url` **带** `v1`。
- `wire_api` 固定 `responses`。

然后把密钥写入环境变量：

```bash
export API_KEY="在此粘贴 sk- 开头的密钥"
```

> `model_provider` 只能写在**用户级** `~/.codex/config.toml`，写进项目里的 `.codex/config.toml` 不会生效。

## 3. 验证

```bash
codex "用一句话介绍你自己"
```

收到回复即成功。

<details>
<summary>失败时看这里</summary>

- **缺少 API Key** — 环境变量没生效，跑 `echo $API_KEY` 检查；变量名必须与 `env_key` 一致。
- **401** — 密钥无效，去 [快速开始](/docs/quickstart) 用 curl 复验一次。
- **404 / 模型不可用** — `model` 换成 `/v1/models` 里实际存在的名字；确认部署开放了 `/v1/responses`。

</details>

<details>
<summary>永久环境变量 / Nginx 反代注意</summary>

**永久环境变量**（避免每次重开终端都要 `export`）：把 `export API_KEY="..."` 追加到 `~/.zshrc`（macOS）或 `~/.bashrc`（Linux），再 `source` 一次；Windows PowerShell 用 `[Environment]::SetEnvironmentVariable("API_KEY", "值", "User")` 后重开窗口。

**Nginx 反代**：管理员需在 `http` 块加 `underscores_in_headers on;`，否则 Codex 的多账号粘性会话（`session_id` 请求头）会被丢弃。

</details>

## 4. 生图（可选）

Codex 内置的 `image_gen` 工具默认只对 ChatGPT 登录开放。接 {{SITE_NAME}} 这类第三方 API 时，要在 provider 表里额外声明一个 `x-openai-actor-authorization` 请求头，Codex 才会把生图工具挂进会话（需 Codex ≥ 0.143.0）。三个键分属两张表，位置写错不生效：

```toml
[model_providers."{{SITE_NAME}}"]
# ……第 2 节已有的 name / base_url / env_key / wire_api 保持不动，在同一张表内追加：
requires_openai_auth = false          # 必须为 false：仍用上面的 API Key 鉴权
http_headers = { "x-openai-actor-authorization" = "{{BASE_URL}}" }   # 值任意非空，填站点地址便于辨认；{{SITE_NAME}} 不会把它转发给上游

[features]                            # 单独一张表，放在 provider 表之后
image_generation = true               # 新版默认已开，显式写上防止被关
```

- `requires_openai_auth`、`http_headers` 只认 `[model_providers."{{SITE_NAME}}"]` 表；`image_generation` 只认 `[features]` 表，写进 provider 表会被忽略。TOML 里一个表头之后的键都属于该表，所以 `[features]` 要放在 provider 表的所有键之后。
- 生图请求走 `{{BASE_URL}}v1/images/generations`，需要管理员为你所属分组开启「允许生图」。**开启本节配置后，Codex 每次请求都会声明 `image_gen` 工具；分组没开生图权限时所有请求都会 403**，这种情况请先删掉 `http_headers` 这一行。
- `model` 需支持图片输入（如 `gpt-5.5`）；`gpt-5.3-codex-spark` 不支持生图。
- 旧版 Codex（< 0.143.0）没有这条开关，`[features]` 里的旧键名 `imagegenext` 也已被 `image_generation` 取代。先升级：`npm install -g @openai/codex@latest`。

**验证**：`codex "生成一张戴帽子的猫的图片"`，Codex 调用 `image_gen` 并返回图片（含保存路径）即成功。

<details>
<summary>生图失败时看这里</summary>

- **模型说没有 `image_gen` 工具** — 检查三个键是否各在其表；`codex --version` 是否 ≥ 0.143.0；`model` 是否支持图片输入。
- **所有请求 403 `Image generation is not enabled for this group`** — 分组未开生图权限，找管理员开启；或暂时删掉 `http_headers` 行恢复纯文本使用。
- **生图阶段 401 / 404** — `/v1/images/generations` 与对话走同一把密钥、同一个 `base_url`，按第 3 节排查即可。

</details>

# Codex CLI

> OpenAI 官方 CLI。IDE 扩展与 CLI 共用 `~/.codex/config.toml`，本页配置完两边都能用。

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

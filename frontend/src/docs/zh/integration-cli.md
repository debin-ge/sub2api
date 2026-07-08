# CLI 接入

本页覆盖终端命令行 AI 工具如何接入 {{SITE_NAME}}，首版包含 Codex CLI 和 Claude Code。

> Codex 与 Claude Code 也有 IDE / 桌面形态，见 [客户端接入](/docs/integration-clients)。IDE 形态与 CLI 共用同一份配置文件，按本页配好后两边都能用。

## 接入前提

1. 已完成 [接入总览](/docs/integration-overview) 的三步准备（Base URL、API Key、curl 验证）。
2. 会打开终端（macOS：聚焦搜索「终端」；Windows：开始菜单搜「PowerShell」）。

**关于环境变量的两种用法**（后面会用到）：

- **临时**：在终端执行 `export 名字="值"`，只对当前窗口有效，适合先试通。
- **永久**：把同一行 `export` 写进 shell 配置文件（macOS 默认是 `~/.zshrc`，Linux 常见 `~/.bashrc`），保存后执行 `source ~/.zshrc` 或重开终端生效。Windows PowerShell 的等价做法是执行 `[Environment]::SetEnvironmentVariable("名字", "值", "User")` 后重开窗口。

## Codex CLI

```client
name: Codex CLI
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.codex/config.toml
homepage: https://developers.openai.com/codex
```

**配置方式**：配置文件 `config.toml` 定义服务地址 + 环境变量提供密钥。Codex 没有「自定义服务地址」的交互式设置命令，必须改文件。

**第 1 步：安装 Codex CLI**（已安装可跳过）：

```bash
npm install -g @openai/codex
```

验证：执行 `codex --version` 能输出版本号。如果提示找不到 `npm`，先到 nodejs.org 安装 Node.js 20+。

**第 2 步：找到或创建配置文件。** 位置：

- macOS / Linux：`~/.codex/config.toml`
- Windows：`%USERPROFILE%\.codex\config.toml`

`.codex` 目录默认隐藏。最快的打开方式：

```bash
# macOS：如果目录不存在先创建，然后用文本编辑打开
mkdir -p ~/.codex && open -e ~/.codex/config.toml
```

Windows：资源管理器地址栏输入 `%USERPROFILE%\.codex` 回车；没有该文件夹就新建一个，再新建文本文件命名为 `config.toml`（注意不要变成 `config.toml.txt`）。

**第 3 步：写入配置。** 把下面内容整段复制进 `config.toml`（如文件已有内容，追加到末尾即可）：

```toml
# 顶层：选用哪个服务和默认模型
model_provider = "sub2api"
model = "gpt-5.5"

# 服务定义：接入 {{SITE_NAME}}
[model_providers.sub2api]
name = "{{SITE_NAME}}"
base_url = "{{BASE_URL}}v1"
env_key = "API_KEY"
wire_api = "responses"
```

逐项说明：

- `model`：默认使用的模型，请填 `/v1/models` 返回列表里的 GPT/Codex 类模型名。
- `base_url`：注意结尾是 `v1`。
- `env_key`：告诉 Codex 去名为 `API_KEY` 的环境变量里拿密钥（下一步设置）。
- `wire_api`：固定填 `responses`。

> `model_provider` 和 `model_providers` 只能写在用户级 `~/.codex/config.toml`，写进项目里的 `.codex/config.toml` 不会生效。

**第 4 步：设置密钥环境变量。** 先临时设置并测试（引号里换成你的真实密钥；如果你已按 [接入总览](/docs/integration-overview) 设置过 `API_KEY`，可跳过）：

```bash
export API_KEY="在这里粘贴你的API密钥"
```

测试通过后（见下一步），把这一行追加到 `~/.zshrc`（macOS）或 `~/.bashrc`（Linux）做成永久配置；Windows 用上文「接入前提」里的 PowerShell 命令。

**✅ 完成检验**：在设置了环境变量的同一个终端窗口运行：

```bash
codex "用一句话介绍你自己"
```

**预期结果**：Codex 正常启动并输出回复。

**如果失败**：

- 提示缺少 API Key：环境变量没设置成功，执行 `echo $API_KEY` 检查是否有输出；注意变量名必须与 `env_key` 里写的完全一致。
- 401：密钥本身无效，回到 [接入总览](/docs/integration-overview) 第 2 步用 curl 复验。
- 404 / 模型不可用：`model` 换成 `/v1/models` 里实际存在的模型名；确认当前部署开放了 `/v1/responses`。

**⚠️ Nginx 反向代理注意**：如果你的 {{SITE_NAME}} 部署在 Nginx 后面，管理员需要在 Nginx 配置的 `http` 块中添加 `underscores_in_headers on;`。Nginx 默认丢弃带下划线的请求头（如 `session_id`），会导致 Codex 多账号粘性会话失效。这是**服务端**配置，普通用户遇到会话异常时可把这条转给管理员。

## Claude Code

```client
name: Claude Code
logo: anthropic
protocols: [anthropic]
endpoint: {{BASE_URL}}
config: ~/.claude/settings.json
homepage: https://docs.claude.com/claude-code
```

**配置方式**：配置文件（推荐，永久生效）或环境变量（临时试用）。

**第 1 步：安装 Claude Code**（已安装可跳过）：

```bash
npm install -g @anthropic-ai/claude-code
```

验证：执行 `claude --version` 能输出版本号。

**第 2 步（推荐）：写配置文件。** 位置：

- macOS / Linux：`~/.claude/settings.json`
- Windows：`%USERPROFILE%\.claude\settings.json`

打开方式（文件不存在会自动创建）：

```bash
# macOS
mkdir -p ~/.claude && open -e ~/.claude/settings.json
```

写入以下内容（文件已有内容时，把 `env` 块合并进最外层大括号，注意 JSON 逗号规则）：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "${API_KEY}"
  }
}
```

- `ANTHROPIC_BASE_URL`：填根地址，**不带** `v1`。
- `ANTHROPIC_AUTH_TOKEN`：你的 API Key，会以 `Authorization: Bearer` 方式发送。**配置文件不会读取环境变量，请把 `${API_KEY}` 替换为真实密钥。**

**第 2 步（备选）：环境变量。** 不想改文件时，在终端执行：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="${API_KEY}"
```

只对当前终端窗口有效。个别版本或兼容客户端要求的变量名是 `ANTHROPIC_API_KEY`，遇到认证问题可再加一条：

```bash
export ANTHROPIC_API_KEY="${API_KEY}"
```

**第 3 步：项目级覆盖（可选）。** 想让某个项目单独走不同配置，在该项目根目录创建 `.claude/settings.json`，格式与第 2 步相同，优先级高于用户级配置。

**✅ 完成检验**：在任意目录运行：

```bash
claude "用一句话介绍你自己"
```

**预期结果**：Claude Code 启动并输出回复。首次运行如出现登录引导，选择使用 API Key / token 的方式（已在配置中提供），不要走浏览器 OAuth 登录。

**如果失败**：

- 401：确认 `ANTHROPIC_AUTH_TOKEN` 的值完整；先用 curl 复验密钥。
- 仍连到官方地址：配置文件 JSON 格式有误（引号/逗号），或没保存；`claude` 需要完全退出重进。
- 模型报错：让管理员确认当前分组开放了 Claude 兼容模型。

**Antigravity 入口变体**：如果管理员提供的是 Antigravity Claude 渠道，把 Base URL 改为带 `antigravity` 后缀即可，其余不变：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}antigravity",
    "ANTHROPIC_AUTH_TOKEN": "${API_KEY}"
  }
}
```

> 注意：Anthropic Claude 渠道和 Antigravity Claude 渠道**不能在同一会话上下文中混用**，用哪个以管理员分组说明为准。同样，把 `${API_KEY}` 替换为真实密钥。

## 通用注意事项

| 项目 | 说明 |
| --- | --- |
| 环境变量作用域 | `export` 只影响当前终端窗口；IDE 内置终端和图形界面启动的应用不会自动继承，推荐用配置文件方式。 |
| 配置文件 vs 环境变量优先级 | 各工具规则不同（Claude Code 中环境变量通常优先于 settings.json）。两边都配过时，排查问题先检查环境变量里有没有旧值：`echo $ANTHROPIC_BASE_URL`。 |
| 流式与超时 | CLI 工具全程使用 SSE 流式输出，网络代理需放行长连接；公司代理环境下连不通先试关闭代理。 |
| 多账号切换 | 需要在多个站点/密钥间频繁切换时，用 [第三方工具接入](/docs/integration-tools) 里的 CC-Switch 或 Cockpit Tools，避免手工反复改文件。 |

## 计划中

以下 CLI 工具的详细接入步骤将在后续版本补充，思路相同（配置 Base URL + API Key + 模型名）：

- **Gemini CLI** — 走 `/v1beta` Gemini 原生端点。
- **opencode** — 走 OpenAI 兼容端点子集。

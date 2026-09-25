# Claude Code

> Anthropic 官方工具，分 **CLI / 终端**、**VS Code / JetBrains 插件**、**桌面版 App** 三种接入，三者的配置位置与字段各不相同，请对号入座。

> [!IMPORTANT]
> **CLI 与桌面 App 配置互不通用**：CLI 读 `~/.claude/settings.json`；桌面 App 读独立的「第三方推理（3P）」配置，两者不共享。下方按端分别说明——下载的 `settings.json` **只对 CLI 生效**。

```client
name: Claude Code
logo: anthropic
protocols: [anthropic]
endpoint: {{BASE_URL}}
config: ~/.claude/settings.json
homepage: https://docs.claude.com/claude-code
```

## 1. 安装（CLI）

```bash
npm install -g @anthropic-ai/claude-code
```

**验证**：`claude --version` 输出版本号即成功。

## 2. 配置（CLI / 终端）

打开 `~/.claude/settings.json`（Windows：`%USERPROFILE%\.claude\settings.json`），不存在就手动新建：

```json download=settings.json
{
  "_note": "由 {{SITE_NAME}} 生成 · Base URL 已填好，把 ANTHROPIC_AUTH_TOKEN 换成你的 sk- 密钥即可（JSON 不支持注释，本行仅作说明，Claude Code 会忽略）",
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "在此粘贴 sk- 开头的密钥"
  }
}
```

- `ANTHROPIC_BASE_URL` **不带** `v1`。
- `ANTHROPIC_AUTH_TOKEN` 替换为真实密钥（配置文件不读环境变量）。

## 3. 验证（CLI）

新开一个终端窗口：

```bash
claude "用一句话介绍你自己"
```

收到回复即成功。首次运行遇到登录引导，选"使用 API Key"，不要走 OAuth。

## 接入国产模型（模型映射）

如果你的密钥属于 DeepSeek、智谱 GLM、Kimi、MiniMax 等国产模型分组，**必须先做模型映射**，否则 Claude Code 无法正常使用。

**为什么要映射**：Claude Code 会按用途调用不同档位的模型，请求里带的都是 `claude-*` 模型名。主对话默认用 Sonnet / Opus；生成标题、压缩上下文等后台任务用 Haiku；子代理（Task）也可能用单独的模型。国产分组里没有这些模型名，所以会报「模型不存在 / 无可用账号」。常见的情况是只配了主模型：对话能用，但后台任务报错或卡住。把每个档位都指向分组里真实存在的模型，就能解决这个问题。

### ① 查出可用的模型 ID

```bash
curl -s {{BASE_URL}}v1/models -H "Authorization: Bearer 在此粘贴 sk- 开头的密钥"
```

返回结果中的 `id` 就是要填的模型名。大小写必须完全一致，例如 `GLM-5.1` 不能写成 `glm-5.1`。

### ② 在 settings.json 里映射

在上面 `~/.claude/settings.json` 的 `env` 中补充以下变量（以 DeepSeek 为例）：

```json download=settings.json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "在此粘贴 sk- 开头的密钥",
    "ANTHROPIC_MODEL": "deepseek-v4-pro",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "deepseek-v4-pro",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "deepseek-v4-pro",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "deepseek-v4-flash",
    "CLAUDE_CODE_SUBAGENT_MODEL": "deepseek-v4-pro"
  }
}
```

| 变量 | 对应 Claude Code 的用途 | 建议映射到 |
| --- | --- | --- |
| `ANTHROPIC_MODEL` | 启动时的默认主模型 | 分组里最强的编码模型 |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | `/model` 里的 Opus 档、Plan 模式 | 最强模型 |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | `/model` 里的 Sonnet 档（日常编码） | 主力模型 |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | 标题生成、上下文压缩等后台任务 | 便宜、快的模型 |
| `CLAUDE_CODE_SUBAGENT_MODEL` | 子代理（Task） | 主力模型 |

> 较旧版本的 Claude Code 用 `ANTHROPIC_SMALL_FAST_MODEL` 指定后台模型。如果升级前已经配置过，改为 `ANTHROPIC_DEFAULT_HAIKU_MODEL`；不确定版本时两个都填同一个值也可以。

### ③ 各厂商参考映射

| 厂商 | 主模型 / Opus / Sonnet / 子代理 | Haiku（后台任务） |
| --- | --- | --- |
| DeepSeek | `deepseek-v4-pro` | `deepseek-v4-flash` |
| 智谱 GLM | `GLM-5.1` | `GLM-4.5-air` |
| Kimi | `kimi-for-coding` | `kimi-for-coding` |
| MiniMax | `MiniMax-M2.7` | `MiniMax-M2.7-highspeed` |

上表只是示例，请以你的密钥在 `/v1/models` 中实际返回的模型为准。分组只有一个模型时，所有变量都填这一个。

### ④ 验证映射

新开终端，运行 `claude`，输入 `/status` 查看当前模型，再发一个会读写文件的任务。之后到 [使用记录](/usage) 检查：调用记录里应该只出现国产模型名，并且有后台任务的小额调用。

**使用提示**

- **插件同样需要映射**：VS Code / JetBrains 插件把上面这些变量一并写进 `claudeCode.environmentVariables`。
- **不要在同一会话里来回切换**：Claude 与国产模型之间切换时，历史中的思考块可能不被对方接受。切换时用 `/clear` 或新开会话。
- **能力差异**：部分国产模型不支持图片输入，上下文窗口也比 Claude 小。如果报上下文超长，手动执行 `/compact` 或新开会话。
- **临时试用**：可以直接在终端里设置，退出终端后失效：

```bash
export ANTHROPIC_MODEL="deepseek-v4-pro"
export ANTHROPIC_DEFAULT_HAIKU_MODEL="deepseek-v4-flash"
```

## VS Code / JetBrains 插件

插件**不读** `~/.claude/settings.json` 的登录配置，需在 **VS Code 用户设置** 里填 `claudeCode.environmentVariables`（命令面板 → `Preferences: Open User Settings (JSON)`）：

```json
{
  "claudeCode.environmentVariables": [
    { "name": "ANTHROPIC_BASE_URL", "value": "{{BASE_URL}}" },
    { "name": "ANTHROPIC_AUTH_TOKEN", "value": "在此粘贴 sk- 开头的密钥" }
  ]
}
```

改完**完全退出并重开** IDE 才生效。JetBrains 系（IDEA / PyCharm 等）同理，在其 Claude Code 插件设置里填相同的环境变量。

## 桌面版 App（Claude Desktop · 第三方推理）

桌面 App 走 Claude Desktop 内置的「第三方推理（3P）」，**与上面的 settings.json 完全无关**，目前只支持 Anthropic Messages 协议。

**方法 A（推荐）· App 内配置**

打开 Claude Desktop → 菜单 **Developer → Configure third-party inference**（配置第三方推理），填：

| 字段 | 值 |
| --- | --- |
| Provider / 类型 | Gateway（网关 / 自定义） |
| Base URL | `{{BASE_URL}}` |
| API Key | 你的 `sk-` 密钥 |
| Auth scheme | `Bearer` |

保存后 App 自动进入 3P 模式。此法经官方校验、随版本自动适配，最稳妥。

> 嫌手动麻烦？用已收录的 [CC-Switch](/apps/cc-switch) 或 [Cockpit Tools](/apps/cockpit-tools) 可**一键写入**桌面 App 的第三方配置，对新手最省事。

<details>
<summary>方法 B（进阶 / 离线）：手动放置 profile 文件</summary>

Claude Desktop 的 3P 配置目录（不存在就新建）：

- **macOS**：`~/Library/Application Support/Claude-3p/configLibrary/`
- **Windows**：`%LOCALAPPDATA%\Claude-3p\configLibrary\`

**① 放入 profile**，文件名保持为下面这个 UUID（即 `configLibrary/2f9a7b10-…-00000000c001.json`）：

```json download=2f9a7b10-0000-4000-8000-00000000c001.json
{
  "coworkEgressAllowedHosts": ["*"],
  "disableDeploymentModeChooser": true,
  "inferenceProvider": "gateway",
  "inferenceGatewayBaseUrl": "{{BASE_URL}}",
  "inferenceGatewayApiKey": "在此粘贴 sk- 开头的密钥",
  "inferenceGatewayAuthScheme": "bearer",
  "inferenceModels": [
    { "name": "claude-opus-4-8", "supports1m": true },
    "claude-sonnet-4-6",
    "claude-haiku-4-5"
  ]
}
```

**② 同目录放 `_meta.json`**（`appliedId` 必须与上面的文件名一致）：

```json download=_meta.json
{
  "appliedId": "2f9a7b10-0000-4000-8000-00000000c001",
  "entries": [
    { "id": "2f9a7b10-0000-4000-8000-00000000c001", "name": "{{SITE_NAME}}" }
  ]
}
```

**③ 开启 3P 模式**：在 `Claude-3p/claude_desktop_config.json` 里加一行 `"deploymentMode": "3p"`（没有该文件就新建成 `{ "deploymentMode": "3p" }`）。然后**完全退出并重开** App。

> ⚠️ Claude Desktop（约 1.126+）对 3P 配置有严格校验，**任一字段不合法会整组拒收**。`inferenceModels` 里的模型名要与本站 `{{BASE_URL}}v1/models` 实际开放的对得上；不确定就删到只留一个可用的，或直接用上面的方法 A。

</details>

<details>
<summary>失败时看这里</summary>

**CLI**
- **401** — 密钥不完整或字段名写错（必须是 `ANTHROPIC_AUTH_TOKEN`）
- **仍连到官方地址** — JSON 未保存或格式错；`claude` 需完全退出重开
- **模型报错** — 让管理员确认当前分组开放了 Claude 兼容模型
- **国产模型分组报「模型不存在 / 无可用账号」** — 映射不完整，最常见的是漏了 `ANTHROPIC_DEFAULT_HAIKU_MODEL`，见上方「接入国产模型」一节；另外检查模型名大小写是否与 `/v1/models` 一致

**桌面 App**
- **改了 settings.json 却没用** — 桌面 App 不读它，走的是上面的 3P profile
- **保存后仍报错 / 模型为空** — profile 里的模型名不被接受，删减 `inferenceModels` 或改用方法 A（Developer 窗口）重配

更多错误码见 [错误排查](/docs/errors)。

</details>

<details>
<summary>Antigravity / 项目级 / 环境变量 / 反代（CLI）</summary>

**Antigravity 渠道**：Base URL 加 `antigravity` 后缀，其余不变：

```json
{ "env": { "ANTHROPIC_BASE_URL": "{{BASE_URL}}antigravity", "ANTHROPIC_AUTH_TOKEN": "..." } }
```

Anthropic 与 Antigravity 渠道**不能在同一会话上下文中混用**。

**项目级覆盖**：项目根目录建 `.claude/settings.json`（格式同上），优先级高于用户级。

**环境变量备选**（临时试用）：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="在此粘贴密钥"
# 个别兼容客户端需要 ANTHROPIC_API_KEY：
export ANTHROPIC_API_KEY="在此粘贴密钥"
```

**Nginx 反代**：管理员需在 `http` 块加 `underscores_in_headers on;`，否则多账号粘性会话失效。

</details>

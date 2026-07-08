# 客户端接入

本页覆盖带图形界面的 AI 编程客户端（IDE、编辑器和插件）如何接入 {{SITE_NAME}}。每个客户端一张卡片，按步骤照做即可。

> Codex 与 Claude Code 也有命令行版本，如果你在终端里使用它们，请看 [CLI 接入](/docs/integration-cli)。

## 接入前提

开始前请确认已完成 [接入总览](/docs/integration-overview) 的准备：

1. 已拿到 Base URL：`{{BASE_URL}}`
2. 已创建 API Key（`sk-` 开头）。
3. 已用 curl 验证过密钥有效（这一步能帮你把"密钥问题"和"客户端配置问题"分开排查）。

> 各客户端的界面会随版本更新变化，如果找不到文中提到的设置项，请以该客户端官方文档为准。

## Cursor

```client
name: Cursor
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
homepage: https://cursor.com
```

**配置方式**：全部在设置面板完成，无需改配置文件。

**第 1 步：打开模型设置。** 点击右上角齿轮图标（或按 `Cmd/Ctrl + Shift + J`）打开 Cursor Settings，选择左侧的 **Models**。

**第 2 步：填入 API Key 和 Base URL。** 找到 **OpenAI API Key** 区域：

- API Key 输入框：粘贴你的 `${API_KEY}`。
- 展开 **Override OpenAI Base URL**，填入：`{{BASE_URL}}v1`（注意结尾是 `v1`，没有斜杠也可以）。

**第 3 步：启用并验证。** 点击 API Key 输入框旁的 **Verify**（验证）按钮。

- **预期结果**：显示验证成功，开关变为开启状态。
- **如果验证失败**：先检查 Base URL 是否精确为 `{{BASE_URL}}v1`；再确认密钥完整。

**第 4 步：添加模型名。** 在 Models 列表点击 **Add model**，输入 `/v1/models` 返回列表中的模型名（例如 `gpt-4o-mini`），勾选启用。Cursor 对话时在模型下拉框选它。

**✅ 完成检验**：按 `Cmd/Ctrl + L` 打开对话，选择你添加的模型，发送"你好"。收到回复即接入成功。

**常见坑**：

- 开启自定义 Base URL 后，Cursor 的部分内置功能（如 Tab 补全）可能仍走官方通道，属正常现象。
- 报 404 多半是 Base URL 少了 `v1` 或多拼了一层 `/v1/v1`。

## Claude Code（IDE 插件 / 桌面）

```client
name: Claude Code
logo: anthropic
protocols: [anthropic]
endpoint: {{BASE_URL}}
config: ~/.claude/settings.json
homepage: https://docs.claude.com/claude-code
```

**配置方式**：修改配置文件（推荐，一次配置全端生效）。Claude Code 的 VS Code / JetBrains 插件与 CLI 共用同一份配置，**没有单独的图形化 Base URL 设置界面**。

**第 1 步：找到配置文件。** 配置文件位置：

- macOS / Linux：`~/.claude/settings.json`（`~` 代表你的用户主目录）
- Windows：`%USERPROFILE%\.claude\settings.json`

这个目录以 `.` 开头，默认是隐藏的。查看方式：

- macOS：打开访达，按 `Cmd + Shift + .` 显示隐藏文件；或在终端执行 `open ~/.claude`。
- Windows：在资源管理器地址栏直接输入 `%USERPROFILE%\.claude` 回车。

如果目录或文件不存在，先运行一次 Claude Code 让它生成，或手动新建。

**第 2 步：编辑配置文件。** 用任意文本编辑器打开（macOS 可在终端执行 `open -e ~/.claude/settings.json`；Windows 右键 → 打开方式 → 记事本），加入 `env` 配置块：

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "${API_KEY}"
  }
}
```

把 `${API_KEY}` 换成你的真实密钥。如果文件里已有其他内容，只需把 `env` 这一段合并进最外层大括号内（注意各字段之间用逗号分隔，JSON 不允许多余的尾逗号）。

**第 3 步：重启客户端。** 完全退出并重新打开 VS Code / JetBrains（或重启 `claude` 会话），配置才会生效。

**✅ 完成检验**：在 IDE 中打开 Claude Code 面板，发送"你好"。收到回复即接入成功。

**如果失败**：

- 启动报 JSON 解析错误：多半是引号或逗号打错，把文件内容粘贴到 JSON 校验工具检查。
- 报 401：密钥不对，重新复制；确认字段名是 `ANTHROPIC_AUTH_TOKEN`。
- 更多变量说明（如 `ANTHROPIC_API_KEY` 替代写法、Antigravity 入口）见 [CLI 接入](/docs/integration-cli) 的 Claude Code 小节。

## Codex（IDE 扩展 / 桌面）

```client
name: Codex
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.codex/config.toml
homepage: https://developers.openai.com/codex
```

**配置方式**：修改配置文件。Codex 的 IDE 扩展与 CLI 共用 `~/.codex/config.toml`，**自定义服务地址没有图形化设置入口**。

配置步骤与 CLI 完全相同，请按 [CLI 接入](/docs/integration-cli) 的 Codex CLI 小节操作（写 `config.toml`、导出密钥环境变量），完成后重启 IDE 即可在扩展中使用。

**✅ 完成检验**：重启 IDE 后打开 Codex 面板发送一条消息，收到回复即接入成功。

## Cline（VS Code 插件）

```client
name: Cline
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://github.com/cline/cline
```

**配置方式**：全部在插件设置面板完成。Cline 支持多种 Provider 类型，接 {{SITE_NAME}} 推荐用 **Anthropic** 模式（体验最完整），OpenAI Compatible 模式作备选。

**第 1 步：安装插件。** 打开 VS Code，左侧扩展图标（或 `Cmd/Ctrl + Shift + X`），搜索 `Cline`，点击 Install。

**第 2 步：打开设置。** 点击左侧活动栏的 Cline 图标，首次使用会出现 Provider 配置界面（之后可点面板右上角齿轮进入）。

**第 3 步（推荐）：Anthropic 模式。**

- API Provider：选择 **Anthropic**
- Anthropic API Key：填 `${API_KEY}`
- 勾选/展开 **Use custom base URL**，填：`{{BASE_URL}}`（根地址，**不带** `v1`）
- Model：选择或输入 `/v1/models` 里的 Claude 模型名

**第 3 步（备选）：OpenAI Compatible 模式。**

- API Provider：选择 **OpenAI Compatible**
- Base URL：`{{BASE_URL}}v1`（**带** `v1`）
- API Key：`${API_KEY}`
- Model ID：填 `/v1/models` 里的模型名

**✅ 完成检验**：在 Cline 输入框发送"列出当前目录的文件"，它开始响应并规划操作即接入成功。

**常见坑**：两种模式的 Base URL 结尾不同（Anthropic 模式不带 `v1`，OpenAI 模式带 `v1`），填反了会一直 404。

## Continue（VS Code 插件）

```client
name: Continue
logo: openai
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}v1
config: ~/.continue/config.yaml
homepage: https://continue.dev
```

**配置方式**：修改配置文件 `config.yaml`（Continue 的模型配置主要靠文件维护）。

**第 1 步：安装插件。** VS Code 扩展市场搜索 `Continue`，点击 Install。安装后首次打开 Continue 面板，它会自动创建配置目录。

**第 2 步：打开配置文件。** 位置：

- macOS / Linux：`~/.continue/config.yaml`
- Windows：`%USERPROFILE%\.continue\config.yaml`

最快的打开方式：点击 Continue 面板右上角的齿轮图标，再点配置文件名，会直接在 VS Code 中打开。

**第 3 步：添加模型。** 在 `models:` 列表中加入一项（YAML 对缩进敏感，请保持与示例一致的两空格缩进）：

```yaml
models:
  - name: {{SITE_NAME}} GPT
    provider: openai
    model: gpt-4o-mini
    apiBase: {{BASE_URL}}v1
    apiKey: ${API_KEY}
    roles:
      - chat
```

把 `${API_KEY}` 替换为你的真实密钥（配置文件不会自动读取环境变量）。要同时接入 Claude 模型，再加一项，`provider` 改为 `anthropic`、`apiBase` 改为 `{{BASE_URL}}`（不带 `v1`）、`model` 填 Claude 模型名。

> 旧版 Continue 使用 `~/.continue/config.json`（JSON 格式，字段同名）。如果你的目录里只有 `config.json`，在 `"models": []` 数组中加入 `{ "title": "...", "provider": "openai", "model": "...", "apiBase": "...", "apiKey": "..." }` 即可。

**第 4 步：保存并重载。** 保存文件后 Continue 通常会自动重载；没有生效就执行 VS Code 命令面板（`Cmd/Ctrl + Shift + P`）中的 `Developer: Reload Window`。

**✅ 完成检验**：打开 Continue 面板，模型下拉框选择 `{{SITE_NAME}} GPT`，发送"你好"。收到回复即接入成功。

**如果失败**：面板报 YAML 错误就是缩进或冒号问题，对照示例逐行核对；报 401/404 按 [接入总览](/docs/integration-overview) 的速查表排查。

## Trae

```client
name: Trae
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://www.trae.ai
```

**配置方式**：设置面板中添加自定义模型服务。

**第 1 步：打开模型设置。** 点击右上角设置图标，进入 **模型（Models）** 相关设置页，选择 **添加自定义模型 / Custom Provider**。

**第 2 步：填写服务信息。**

- Provider 类型：根据你要用的模型选 **Anthropic**（Claude 模型）或 **OpenAI 兼容**（GPT 等模型）
- Base URL：Anthropic 类型填 `{{BASE_URL}}`；OpenAI 兼容类型填 `{{BASE_URL}}v1`
- API Key：`${API_KEY}`
- 模型名：填 `/v1/models` 返回的名称

**第 3 步：保存并选用。** 保存后在对话的模型选择器中切换到你添加的模型。

**✅ 完成检验**：发送"你好"，收到回复即接入成功。

> Trae 界面迭代较快，不同版本的入口名称可能不同（如「模型服务商」「Model Providers」），找不到时以 Trae 官方文档为准。

## 通用故障排查（GUI 类）

| 现象 | 处理 |
| --- | --- |
| 客户端提示连接失败 | 先回到终端跑一遍 [接入总览](/docs/integration-overview) 第 2 步的 curl。curl 通而客户端不通 → 是客户端配置问题；curl 也不通 → 是密钥或网络问题。 |
| 401 | 密钥粘贴不完整，或填错了字段（有的客户端同时有多个 Key 输入框）。 |
| 404 | Base URL 结尾的 `v1` 与该客户端要求不符，逐字符对照本页示例。 |
| 模型列表为空或模型报错 | 手填 `/v1/models` 返回的模型名，不要依赖客户端内置列表。 |
| 修改配置文件后不生效 | 完全退出并重启客户端（VS Code 可用 `Developer: Reload Window`）。 |

更多错误码见 [错误排查](/docs/errors)。

## 更多客户端

以下客户端同样支持自定义 OpenAI / Anthropic 兼容服务，配置思路与上面相同（Base URL + API Key + 模型名），详细步骤计划后续补充：

- **Zed** — 高性能编辑器，支持 OpenAI 兼容 Provider。
- **Windsurf** — AI IDE。
- **Cherry Studio** — 桌面 AI 聊天客户端，支持多 Provider。
- **ChatBox / NextChat** — 轻量聊天客户端，OpenAI 兼容。

# OpenCode

> 开源 AI 编程智能体，可在终端中运行。既可以通过 OpenAI Responses API 接入 {{SITE_NAME}}，也可以使用 Claude Messages 协议。

```client
name: OpenCode
logo: app
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}
config: ~/.config/opencode/opencode.json
homepage: https://opencode.ai
```

## 1. 安装

使用官方安装脚本：

```bash
curl -fsSL https://opencode.ai/install | bash
```

也可以通过 npm 安装：

```bash
npm install -g opencode-ai
```

**验证**：`opencode --version` 能输出版本号即安装成功。

## 2. 添加密钥并配置 Provider

先运行 `opencode` 进入终端界面，输入 `/connect`：

1. 在 Provider 列表底部选择 **Other**。
2. Provider ID 输入当前站点名称：`{{SITE_NAME}}`。
3. 粘贴你的 `sk-` API 密钥。

> Provider ID 必须与下方配置中的 `{{SITE_NAME}}` 完全一致。密钥会由 OpenCode 单独保存在本机认证文件中，不需要写进 `opencode.json`。

打开全局配置 `~/.config/opencode/opencode.json`（Windows：`%USERPROFILE%\.config\opencode\opencode.json`），不存在就新建。下面的 OpenAI 与 Claude 配置**二选一**，Provider ID 都保持为当前站点名称。

### OpenAI Responses 示例

```json download=opencode.json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "{{SITE_NAME}}": {
      "npm": "@ai-sdk/openai",
      "name": "{{SITE_NAME}}",
      "options": {
        "baseURL": "{{BASE_URL}}"
      },
      "models": {
        "gpt-5.5": {
          "name": "GPT 5.5"
        }
      }
    }
  }
}
```

- `baseURL` 与当前系统站点 URL 完全相同，页面会自动填入系统配置的 API 端点地址；请不要额外追加 `/v1`。
- `@ai-sdk/openai` 会让 OpenCode 基于站点 URL 调用 `/responses`，适合本站的 Responses 兼容模型。
- 把 `gpt-5.5` 替换为 `{{BASE_URL}}v1/models` 返回的实际模型 ID；对象键就是发送给 API 的模型名。
- 如果已有全局配置，请把 `{{SITE_NAME}}` 节点合并进现有的 `provider`，不要覆盖其他设置。

### Claude Messages 示例

如果要使用 Claude 模型，将 `opencode.json` 配置为：

```json download=opencode.json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "{{SITE_NAME}}": {
      "npm": "@ai-sdk/anthropic",
      "name": "{{SITE_NAME}}",
      "options": {
        "baseURL": "{{BASE_URL}}"
      },
      "models": {
        "claude-sonnet-4-6": {
          "name": "Claude Sonnet 4.6"
        }
      }
    }
  }
}
```

- Provider ID 和 `name` 都使用当前站点名称 `{{SITE_NAME}}`。
- `baseURL` 仍与系统站点 URL 完全相同，不追加 `/v1`。
- `@ai-sdk/anthropic` 会基于站点 URL 调用 `/messages`。
- 把 `claude-sonnet-4-6` 替换为 `{{BASE_URL}}v1/models` 中实际可用的 Claude 模型 ID。

## 3. 选择模型并验证

保存配置后重启 OpenCode，输入：

```text
/models
```

选择 **{{SITE_NAME}} → GPT 5.5** 或 **Claude Sonnet 4.6**（以你配置的模型为准），然后发送：

```text
用一句话介绍你自己
```

能正常收到回复并执行工具调用，即表示接入成功。

<details>
<summary>Chat Completions 兼容模式</summary>

如果所选模型未开放 `/responses`，但支持 `/chat/completions`，只需把 Provider 的 npm 包改为：

```json
"npm": "@ai-sdk/openai-compatible"
```

`baseURL` 仍保持 `{{BASE_URL}}`。修改后完全退出并重启 OpenCode，再重新选择模型。

</details>

<details>
<summary>项目级配置</summary>

若只想让某个项目使用 {{SITE_NAME}}，可把同样的配置保存为项目根目录的 `opencode.json`。项目配置会覆盖全局配置中的同名字段，但认证信息仍由 `/connect` 单独管理。

不要把 API 密钥直接写进要提交到 Git 的项目配置。如果必须通过环境变量提供密钥，可在 `options` 中加入：

```json
"apiKey": "{env:API_KEY}"
```

然后在启动 OpenCode 前执行 `export API_KEY="你的 sk- 密钥"`。

</details>

<details>
<summary>失败时看这里</summary>

- **列表里没有 {{SITE_NAME}}** — 检查配置文件路径和 JSON 语法，然后完全重启 OpenCode。
- **提示找不到凭据** — `/connect` 时填写的 Provider ID 必须与当前站点名称 `{{SITE_NAME}}` 完全一致；可运行 `opencode auth list` 检查。
- **401** — 密钥无效或已停用；重新执行 `/connect`，或先到 [快速开始](/docs/quickstart) 用 curl 验证。
- **404** — 检查 `baseURL` 是否与页面显示的系统站点 URL 完全相同、没有额外追加 `/v1`；OpenAI 模式需开放 `/responses`，Claude 模式需开放 `/messages`。
- **模型不可用** — 配置里的模型 ID 必须与 `/v1/models` 返回值完全一致，并且当前 API Key 所属分组有权访问。
- **Responses 请求不兼容** — 按上方说明改用 `@ai-sdk/openai-compatible`，让 OpenCode 调用 `/chat/completions`。
- **Claude 请求格式不正确** — 确认使用的是 `@ai-sdk/anthropic`，并且模型 ID 对应本站可用的 Claude 模型。

</details>

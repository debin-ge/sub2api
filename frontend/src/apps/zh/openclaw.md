# OpenClaw

> 可自托管的个人智能体与自动化网关。{{SITE_NAME}} 同时支持 OpenAI Responses 和 Claude Messages，可按模型类型选择对应配置。

```client
name: OpenClaw
logo: app
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}
config: ~/.openclaw/openclaw.json
homepage: https://openclaw.ai
```

## 1. 安装

通过 npm 安装并运行初始化向导：

```bash
npm install -g openclaw@latest
openclaw onboard --install-daemon
```

**验证**：

```bash
openclaw gateway status
```

能看到 Gateway 状态即安装成功。初始化向导中的消息渠道、技能和远程访问都可以先跳过，模型接入完成后再配置。

## 2. 配置 Provider

先在 `~/.openclaw/.env` 保存 API Key：

```dotenv
API_KEY=在此粘贴你的 sk- 密钥
```

再打开 `~/.openclaw/openclaw.json`。下面的 OpenAI 与 Claude 配置**二选一**，Provider ID 使用当前站点名称。

### OpenAI Responses 示例

```json download=openclaw.json
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "{{SITE_NAME}}/gpt-5.5"
      }
    }
  },
  "models": {
    "mode": "merge",
    "providers": {
      "{{SITE_NAME}}": {
        "baseUrl": "{{BASE_URL}}",
        "apiKey": "${API_KEY}",
        "api": "openai-responses",
        "models": [
          {
            "id": "gpt-5.5",
            "name": "GPT 5.5"
          }
        ]
      }
    }
  }
}
```

- `baseUrl` 与系统站点 URL 完全相同，不追加 `/v1`。
- `openai-responses` 会基于该地址调用 `/responses`。
- 将 `gpt-5.5` 替换为 `{{BASE_URL}}v1/models` 中实际可用的 GPT/Codex 模型 ID。

### Claude Messages 示例

```json download=openclaw.json
{
  "agents": {
    "defaults": {
      "model": {
        "primary": "{{SITE_NAME}}/claude-sonnet-4-6"
      }
    }
  },
  "models": {
    "mode": "merge",
    "providers": {
      "{{SITE_NAME}}": {
        "baseUrl": "{{BASE_URL}}",
        "apiKey": "${API_KEY}",
        "api": "anthropic-messages",
        "models": [
          {
            "id": "claude-sonnet-4-6",
            "name": "Claude Sonnet 4.6"
          }
        ]
      }
    }
  }
}
```

- `anthropic-messages` 会基于站点 URL 调用 `/messages`。
- 将示例模型替换为 `/v1/models` 返回的实际 Claude 模型 ID。
- OpenClaw 对自定义 Anthropic 端点不会自动附加只适用于官方 Claude 的 beta 标头，通常无需额外配置。

## 3. 验证

检查配置和模型列表：

```bash
openclaw doctor
openclaw models list
openclaw gateway restart
openclaw dashboard
```

Dashboard 打开后，在聊天框发送一条消息。能正常回复并执行工具即接入成功。

<details>
<summary>失败时看这里</summary>

- **配置校验失败** — `openclaw.json` 支持 JSON5，但字段名必须准确；用 `openclaw doctor` 查看具体路径。
- **401** — 检查 `~/.openclaw/.env` 中的 `API_KEY`，然后重启 Gateway 使环境变量重新加载。
- **404** — OpenAI 配置检查 `/responses`，Claude 配置检查 `/messages`；`baseUrl` 不要额外追加 `/v1`。
- **模型没有出现在列表** — `models.providers.<站点>.models[]` 必须包含模型的 `id`，并且 `agents.defaults.model.primary` 要写成 `站点名称/模型ID`。
- **模型不可用** — 模型 ID 必须与 `/v1/models` 返回值一致，并确认 API Key 所属分组有访问权限。
- **工具调用失败** — 先确认所选模型支持工具调用；必要时改用 Claude Messages 或 OpenAI Chat Completions 兼容模型。

</details>

> OpenClaw 可以长期运行并访问文件、终端和消息渠道。建议先在隔离环境中测试，只授予完成任务所需的最小权限。

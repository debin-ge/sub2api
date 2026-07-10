# Client Integration

This page explains how GUI AI coding clients, IDEs, editors, and extensions connect to {{SITE_NAME}}. Each client is shown as a card with practical setup steps.

> Codex and Claude Code also have CLI forms. If you use them from a terminal, see [CLI Integration](/docs/integration-cli).

## Prerequisites

Before starting, complete [Integration Overview](/docs/integration-overview):

1. You have the Base URL: `{{BASE_URL}}`
2. You have an API Key beginning with `sk-`.
3. You verified the key with curl so key problems and client configuration problems can be separated.

> Client UI labels change over time. If a setting name differs from this page, use the client's official documentation as the final UI reference.

## Cursor

```client
name: Cursor
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
homepage: https://cursor.com
```

**Configuration method**: use Cursor Settings. No config file editing is required.

**Step 1: Open model settings.** Click the gear icon or press `Cmd/Ctrl + Shift + J`, then choose **Models**.

**Step 2: Enter the API Key and Base URL.**

- OpenAI API Key: paste `${API_KEY}`.
- Override OpenAI Base URL: enter `{{BASE_URL}}v1`.

**Step 3: Verify.** Click **Verify** near the API Key field.

- Expected: verification succeeds and the switch is enabled.
- If it fails: confirm the URL is exactly `{{BASE_URL}}v1` and the key is complete.

**Step 4: Add model names.** Click **Add model**, enter a model returned by `/v1/models`, and enable it. Select that model in Cursor chat.

**Verification**: press `Cmd/Ctrl + L`, choose the model, and send `hello`.

Common pitfalls:

- After overriding the OpenAI Base URL, some built-in Cursor features may still use official channels. That is normal.
- 404 usually means the Base URL is missing `v1` or accidentally contains `/v1/v1`.

## Claude Code (IDE Plugin / Desktop)

```client
name: Claude Code
logo: anthropic
protocols: [anthropic]
endpoint: {{BASE_URL}}
config: ~/.claude/settings.json
homepage: https://docs.claude.com/claude-code
```

**Configuration method**: edit the shared config file. Claude Code's VS Code / JetBrains plugins and CLI share the same configuration.

**Step 1: Find the config file.**

- macOS / Linux: `~/.claude/settings.json`
- Windows: `%USERPROFILE%\.claude\settings.json`

The `.claude` directory is hidden by default. On macOS, press `Cmd + Shift + .` in Finder or run `open ~/.claude`. On Windows, paste `%USERPROFILE%\.claude` into File Explorer.

**Step 2: Edit the config file.** Add this `env` block:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "${API_KEY}"
  }
}
```

Replace `${API_KEY}` with the real key. If the file already has content, merge the `env` block into the outer object and keep valid JSON.

**Step 3: Restart the client.** Fully quit and reopen VS Code, JetBrains, or the `claude` session.

**Verification**: open the Claude Code panel and send `hello`.

If it fails:

- JSON parse error: check quotes and commas.
- 401: re-copy the key and confirm the field is `ANTHROPIC_AUTH_TOKEN`.
- For alternate variables and Antigravity entry points, see [CLI Integration](/docs/integration-cli).

## Codex (IDE Extension / Desktop)

```client
name: Codex
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.codex/config.toml
homepage: https://developers.openai.com/codex
```

**Configuration method**: edit `~/.codex/config.toml`. Codex IDE extensions and Codex CLI share this file.

Follow the Codex CLI section in [CLI Integration](/docs/integration-cli): create `config.toml`, define the provider with `base_url = "{{BASE_URL}}v1"`, set `env_key = "API_KEY"`, export the key, and restart the IDE.

**Verification**: reopen the IDE, open the Codex panel, and send a message.

## Cline (VS Code Extension)

```client
name: Cline
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://github.com/cline/cline
```

**Configuration method**: use the extension settings panel. Anthropic mode is recommended for Claude-style models; OpenAI Compatible mode is a fallback.

**Step 1: Install Cline.** In VS Code Extensions, search `Cline` and click Install.

**Step 2: Open provider settings.** Click the Cline icon in the activity bar or the gear icon in the Cline panel.

**Step 3 (recommended): Anthropic mode.**

- API Provider: **Anthropic**
- Anthropic API Key: `${API_KEY}`
- Use custom base URL: `{{BASE_URL}}` (root address, no `v1`)
- Model: a Claude model returned by `/v1/models`

**Step 3 (fallback): OpenAI Compatible mode.**

- API Provider: **OpenAI Compatible**
- Base URL: `{{BASE_URL}}v1`
- API Key: `${API_KEY}`
- Model ID: a model returned by `/v1/models`

**Verification**: ask Cline to list files in the current directory.

Common pitfall: the Base URL suffix differs by mode. Anthropic mode uses the root address; OpenAI mode uses `v1`.

## Continue (VS Code Extension)

```client
name: Continue
logo: openai
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}v1
config: ~/.continue/config.yaml
homepage: https://continue.dev
```

**Configuration method**: edit `config.yaml`.

**Step 1: Install Continue.** Install it from the VS Code extension marketplace.

**Step 2: Open the config file.**

- macOS / Linux: `~/.continue/config.yaml`
- Windows: `%USERPROFILE%\.continue\config.yaml`

The fastest route is the gear icon in the Continue panel.

**Step 3: Add a model.** Keep YAML indentation exactly as shown:

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

Replace `${API_KEY}` with the real key. To add a Claude model, add another item with `provider: anthropic`, `apiBase: {{BASE_URL}}`, and a Claude model name.

Older Continue versions may use `~/.continue/config.json`; add the same model fields to the `"models": []` array.

**Step 4: Save and reload.** If Continue does not reload automatically, run `Developer: Reload Window` from the VS Code command palette.

**Verification**: select `{{SITE_NAME}} GPT` in Continue and send `hello`.

## Trae

```client
name: Trae
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://www.trae.ai
```

**Configuration method**: add a custom model provider in Trae settings.

1. Open model settings and choose **Add Custom Provider**.
2. Choose **Anthropic** for Claude models or **OpenAI Compatible** for GPT-style models.
3. Enter `{{BASE_URL}}` for Anthropic or `{{BASE_URL}}v1` for OpenAI Compatible.
4. Enter `${API_KEY}` and a model returned by `/v1/models`.
5. Save and select the model in chat.

> Trae changes its UI frequently. Names such as "Model Providers" or "Custom Provider" may vary by version.

## General GUI Troubleshooting

| Symptom | Fix |
| --- | --- |
| Client says connection failed | Run the curl check from [Integration Overview](/docs/integration-overview). If curl works, the client config is wrong; if curl fails, fix the key or network first. |
| 401 | The key is incomplete or placed in the wrong field. |
| 404 | The client requires a different `v1` suffix. Compare against this page. |
| Model list is empty | Manually enter a model returned by `/v1/models`; do not rely only on built-in model lists. |
| Config file changes do not apply | Fully restart the client or reload the VS Code window. |

For more errors, see [Troubleshooting](/docs/errors).

## More Clients

The following clients also support custom OpenAI or Anthropic-compatible services. The configuration idea is the same: Base URL + API Key + model name.

- **Zed** — editor with OpenAI-compatible provider support.
- **Windsurf** — AI IDE.
- **Cherry Studio** — desktop chat client with multiple providers.
- **ChatBox / NextChat** — lightweight OpenAI-compatible chat clients.

# OpenClaw

> A self-hosted personal agent and automation gateway. {{SITE_NAME}} supports both OpenAI Responses and Claude Messages, so choose the configuration that matches your model.

```client
name: OpenClaw
logo: app
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}
config: ~/.openclaw/openclaw.json
homepage: https://openclaw.ai
```

## 1. Install

Install with npm and run the onboarding wizard:

```bash
npm install -g openclaw@latest
openclaw onboard --install-daemon
```

**Verify**:

```bash
openclaw gateway status
```

Seeing the Gateway status confirms the installation. You can skip messaging channels, skills, and remote access during onboarding and configure them after the model connection works.

## 2. Configure the provider

First store the API key in `~/.openclaw/.env`:

```dotenv
API_KEY=paste your sk- key here
```

Then open `~/.openclaw/openclaw.json`. Choose **one** of the OpenAI or Claude configurations below. The Provider ID uses the current site name.

### OpenAI Responses example

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

- `baseUrl` exactly matches the system site URL; do not append `/v1`.
- `openai-responses` calls `/responses` relative to that URL.
- Replace `gpt-5.5` with an available GPT/Codex model ID from `{{BASE_URL}}v1/models`.

### Claude Messages example

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

- `anthropic-messages` calls `/messages` relative to the site URL.
- Replace the example with an actual Claude model ID returned by `/v1/models`.
- OpenClaw suppresses Anthropic beta headers that only apply to official Claude endpoints when using a custom Anthropic-compatible proxy, so extra headers are normally unnecessary.

## 3. Verify

Check the configuration and model catalog:

```bash
openclaw doctor
openclaw models list
openclaw gateway restart
openclaw dashboard
```

Send a message after the dashboard opens. A normal response with working tools means the integration is ready.

<details>
<summary>Troubleshooting</summary>

- **Config validation fails** — `openclaw.json` accepts JSON5, but field names must be exact; run `openclaw doctor` for the failing path.
- **401** — check `API_KEY` in `~/.openclaw/.env`, then restart the Gateway so it reloads the environment.
- **404** — check `/responses` for the OpenAI configuration or `/messages` for Claude; do not append `/v1` to `baseUrl`.
- **Model is missing from the list** — `models.providers.<site>.models[]` must contain its `id`, and `agents.defaults.model.primary` must use `site-name/model-id`.
- **Model unavailable** — the ID must match `/v1/models`, and the API key's group must have access.
- **Tool calling fails** — confirm that the selected model supports tools; if needed, switch between Claude Messages and an OpenAI-compatible model.

</details>

> OpenClaw can run continuously with access to files, terminals, and messaging channels. Test it in an isolated environment first and grant only the minimum permissions needed for each task.

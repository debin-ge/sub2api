# OpenCode

> An open-source AI coding agent that runs in your terminal. Connect {{SITE_NAME}} through either the OpenAI Responses API or the Claude Messages protocol.

```client
name: OpenCode
logo: app
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}
config: ~/.config/opencode/opencode.json
homepage: https://opencode.ai
```

## 1. Install

Use the official install script:

```bash
curl -fsSL https://opencode.ai/install | bash
```

Or install through npm:

```bash
npm install -g opencode-ai
```

**Verify**: `opencode --version` prints a version number.

## 2. Add the key and configure the provider

Run `opencode` to open the terminal UI, then enter `/connect`:

1. Select **Other** at the bottom of the provider list.
2. Enter the current site name, `{{SITE_NAME}}`, as the Provider ID.
3. Paste your `sk-` API key.

> The Provider ID must exactly match `{{SITE_NAME}}` in the config below. OpenCode stores the key separately in its local credential file, so it does not need to appear in `opencode.json`.

Open the global config at `~/.config/opencode/opencode.json` (Windows: `%USERPROFILE%\.config\opencode\opencode.json`); create it if needed. Choose **one** of the OpenAI or Claude configurations below. Both keep the Provider ID equal to the current site name.

### OpenAI Responses example

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

- `baseURL` exactly matches the current system site URL. The page fills in the configured API endpoint automatically; do not append `/v1`.
- `@ai-sdk/openai` makes OpenCode call `/responses` relative to the site URL, which is the recommended path for Responses-compatible models on this deployment.
- Replace `gpt-5.5` with an actual model ID returned by `{{BASE_URL}}v1/models`; the object key is the model name sent to the API.
- If you already have a global config, merge the `{{SITE_NAME}}` entry into the existing `provider` object instead of replacing your other settings.

### Claude Messages example

To use a Claude model, configure `opencode.json` as follows:

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

- The Provider ID and `name` both use the current site name, `{{SITE_NAME}}`.
- `baseURL` still exactly matches the system site URL, without an added `/v1`.
- `@ai-sdk/anthropic` calls `/messages` relative to the site URL.
- Replace `claude-sonnet-4-6` with an available Claude model ID returned by `{{BASE_URL}}v1/models`.

## 3. Select a model and verify

Restart OpenCode after saving the config, then enter:

```text
/models
```

Select **{{SITE_NAME}} → GPT 5.5** or **Claude Sonnet 4.6** (whichever model you configured), then send:

```text
Introduce yourself in one sentence.
```

A normal response with working tool calls means the integration is ready.

<details>
<summary>Chat Completions-compatible mode</summary>

If your selected model does not expose `/responses` but supports `/chat/completions`, change only the provider package to:

```json
"npm": "@ai-sdk/openai-compatible"
```

Keep `baseURL` as `{{BASE_URL}}`. Quit and restart OpenCode, then select the model again.

</details>

<details>
<summary>Project-level configuration</summary>

To use {{SITE_NAME}} only in one project, save the same configuration as `opencode.json` in the project root. Project settings override matching global fields, while credentials remain managed separately by `/connect`.

Do not put an API key directly in a project config committed to Git. If you must provide it through an environment variable, add this under `options`:

```json
"apiKey": "{env:API_KEY}"
```

Then run `export API_KEY="your sk- key"` before starting OpenCode.

</details>

<details>
<summary>Troubleshooting</summary>

- **{{SITE_NAME}} is missing from the model list** — check the config path and JSON syntax, then restart OpenCode completely.
- **Credential not found** — the Provider ID entered through `/connect` must exactly match the current site name, `{{SITE_NAME}}`; run `opencode auth list` to inspect saved credentials.
- **401** — the key is invalid or disabled; reconnect it, or validate it first with curl from [Quick Start](/docs/quickstart).
- **404** — make sure `baseURL` exactly matches the system site URL shown on the page, without an added `/v1`; OpenAI mode requires `/responses`, while Claude mode requires `/messages`.
- **Model unavailable** — the configured model ID must exactly match `/v1/models`, and the API key's group must have access.
- **Responses request is incompatible** — switch to `@ai-sdk/openai-compatible` as described above so OpenCode calls `/chat/completions`.
- **Claude request format is incorrect** — confirm the provider uses `@ai-sdk/anthropic` and that the model ID is an available Claude model on this deployment.

</details>

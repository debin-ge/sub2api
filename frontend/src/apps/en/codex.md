# Codex CLI

> OpenAI's official CLI. The IDE extension and the CLI share `~/.codex/config.toml` — once you configure it, both work.

```client
name: Codex CLI
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.codex/config.toml
homepage: https://developers.openai.com/codex
```

## 1. Install

```bash
npm install -g @openai/codex
```

**Verify**: `codex --version` prints a version.

## 2. Configure

Open `~/.codex/config.toml` (Windows: `%USERPROFILE%\.codex\config.toml`); create it if missing:

```toml
model_provider = "sub2api"
model = "gpt-5.5"

[model_providers.sub2api]
name = "{{SITE_NAME}}"
base_url = "{{BASE_URL}}v1"
env_key = "API_KEY"
wire_api = "responses"
```

- `model` — pick a GPT/Codex model name returned by `/v1/models`.
- `base_url` **with** `v1`.
- `wire_api` is fixed to `responses`.

Then export the key:

```bash
export API_KEY="paste your sk- key here"
```

> `model_provider` must live in the **user-level** `~/.codex/config.toml`. A project-level `.codex/config.toml` will be ignored.

## 3. Verify

```bash
codex "Introduce yourself in one sentence."
```

A reply means success.

<details>
<summary>Troubleshooting</summary>

- **Missing API key** — the env var isn't set; run `echo $API_KEY`. The variable name must match `env_key`.
- **401** — invalid key; retry via curl from [Quick Start](/docs/quickstart).
- **404 / model unavailable** — swap `model` for a name that appears in `/v1/models`; make sure `/v1/responses` is enabled on this deployment.

</details>

<details>
<summary>Persist the env var / Nginx proxy note</summary>

**Persist the env var** so you don't re-export on every terminal launch: append `export API_KEY="..."` to `~/.zshrc` (macOS) or `~/.bashrc` (Linux) and `source` it once. On Windows PowerShell: `[Environment]::SetEnvironmentVariable("API_KEY", "value", "User")` then reopen the window.

**Nginx reverse proxy**: the admin must add `underscores_in_headers on;` to the `http` block, otherwise Codex's `session_id` header is stripped and multi-account sticky sessions break.

</details>

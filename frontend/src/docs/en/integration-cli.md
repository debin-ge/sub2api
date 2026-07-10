# CLI Integration

This page covers terminal-based AI tools. The first version focuses on Codex CLI and Claude Code.

> Codex and Claude Code also have IDE or desktop forms. See [Client Integration](/docs/integration-clients). The IDE and CLI forms share the same config files, so a CLI setup often works for both.

## Prerequisites

1. Complete the three preparation steps in [Integration Overview](/docs/integration-overview): Base URL, API Key, and curl verification.
2. Be able to open a terminal. On macOS, search for Terminal. On Windows, search for PowerShell.

Environment variables are used below:

- **Temporary**: run `export NAME="value"` in a terminal. It only affects that window.
- **Persistent**: add the same `export` line to a shell profile such as `~/.zshrc` on macOS or `~/.bashrc` on Linux, then run `source ~/.zshrc` or reopen the terminal. Windows PowerShell can use `[Environment]::SetEnvironmentVariable("NAME", "value", "User")` and then reopen PowerShell.

## Codex CLI

```client
name: Codex CLI
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.codex/config.toml
homepage: https://developers.openai.com/codex
```

**Configuration method**: `config.toml` defines the service address, and an environment variable provides the key. Codex does not have an interactive command for custom service addresses.

**Step 1: Install Codex CLI** if needed:

```bash
npm install -g @openai/codex
```

Run `codex --version` to verify. If `npm` is missing, install Node.js 20+ first.

**Step 2: Create or open the config file.**

- macOS / Linux: `~/.codex/config.toml`
- Windows: `%USERPROFILE%\.codex\config.toml`

macOS helper:

```bash
mkdir -p ~/.codex && open -e ~/.codex/config.toml
```

On Windows, open `%USERPROFILE%\.codex` in File Explorer and create `config.toml`. Make sure it does not become `config.toml.txt`.

**Step 3: Add provider configuration.**

```toml
model_provider = "sub2api"
model = "gpt-5.5"

[model_providers.sub2api]
name = "{{SITE_NAME}}"
base_url = "{{BASE_URL}}v1"
env_key = "API_KEY"
wire_api = "responses"
```

Notes:

- `model`: use a GPT/Codex-style model returned by `/v1/models`.
- `base_url`: must end with `v1`.
- `env_key`: tells Codex to read the API key from the `API_KEY` environment variable.
- `wire_api`: use `responses`.

> `model_provider` and `model_providers` must be in the user-level `~/.codex/config.toml`. Project-level `.codex/config.toml` does not activate a provider definition.

**Step 4: Set the API key environment variable.**

```bash
export API_KEY="paste-your-api-key-here"
```

After testing, add this line to your shell profile for persistence.

**Verification**:

```bash
codex "Introduce yourself in one sentence"
```

Expected result: Codex starts normally and prints a response.

If it fails:

- Missing API key: run `echo $API_KEY` and confirm it prints a value.
- 401: verify the key with the curl command in [Integration Overview](/docs/integration-overview).
- 404 or model unavailable: change `model` to a real model from `/v1/models` and confirm `/v1/responses` is enabled.

**Nginx reverse proxy note**: if {{SITE_NAME}} is behind Nginx, the server admin should add `underscores_in_headers on;` inside the Nginx `http` block. Nginx otherwise drops headers with underscores such as `session_id`, which can break sticky sessions for Codex.

## Claude Code

```client
name: Claude Code
logo: anthropic
protocols: [anthropic]
endpoint: {{BASE_URL}}
config: ~/.claude/settings.json
homepage: https://docs.claude.com/claude-code
```

**Configuration method**: config file for persistent setup, or environment variables for temporary testing.

**Step 1: Install Claude Code** if needed:

```bash
npm install -g @anthropic-ai/claude-code
```

Run `claude --version` to verify.

**Step 2 (recommended): Write the config file.**

- macOS / Linux: `~/.claude/settings.json`
- Windows: `%USERPROFILE%\.claude\settings.json`

macOS helper:

```bash
mkdir -p ~/.claude && open -e ~/.claude/settings.json
```

Add or merge this JSON:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "${API_KEY}"
  }
}
```

- `ANTHROPIC_BASE_URL`: root address, no `v1`.
- `ANTHROPIC_AUTH_TOKEN`: your API Key. In config files, replace `${API_KEY}` with the real key unless your tool explicitly expands env vars.

**Step 2 (fallback): Use environment variables.**

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="${API_KEY}"
```

Some versions or compatible clients also expect `ANTHROPIC_API_KEY`:

```bash
export ANTHROPIC_API_KEY="${API_KEY}"
```

**Step 3: Optional project override.** Create `.claude/settings.json` in a project root to override the user-level config for that project.

**Verification**:

```bash
claude "Introduce yourself in one sentence"
```

Expected result: Claude Code starts and responds. If the first run asks for login, choose the API key / token path rather than browser OAuth.

If it fails:

- 401: confirm `ANTHROPIC_AUTH_TOKEN` is complete and verify the key with curl.
- Still connects to the official address: the JSON file may be invalid or unsaved; restart `claude` completely.
- Model error: ask the admin whether your group has a Claude-compatible model.

**Antigravity entry variant**: if the admin provides an Antigravity Claude channel, use the `antigravity` suffix:

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}antigravity",
    "ANTHROPIC_AUTH_TOKEN": "${API_KEY}"
  }
}
```

Anthropic Claude channels and Antigravity Claude channels should not be mixed in the same session context. Use the channel specified by the admin.

## General Notes

| Topic | Details |
| --- | --- |
| Environment variable scope | `export` only affects the current terminal. IDE terminals and GUI apps may not inherit it, so config files are often safer. |
| Config files vs env vars | Priority differs by tool. When debugging, first check for stale env vars such as `echo $ANTHROPIC_BASE_URL`. |
| Streaming and timeout | CLI tools usually use SSE streaming. Proxies must allow long-lived connections. |
| Multi-account switching | For frequent switching, use [Third-Party Tools](/docs/integration-tools) such as CC-Switch or Cockpit Tools instead of editing files repeatedly. |

## Planned

Detailed steps for these CLI tools may be added later:

- **Gemini CLI** — through Gemini native `/v1beta` endpoints.
- **opencode** — through an OpenAI-compatible endpoint subset.

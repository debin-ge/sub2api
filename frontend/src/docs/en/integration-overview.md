# Integration Overview

This page helps you prepare an integration from scratch and choose the right path among four integration modes. Even if you have never used an API or opened a terminal before, complete the steps below in order before configuring any client, CLI, or application code.

## Prerequisites

Before using any client, CLI, or code sample, complete these three steps. **All later integration pages assume this preparation is already done.**

### Step 1: Get the Base URL and API Key

**Base URL** is the service address of {{SITE_NAME}}. It is the site address you are currently visiting:

```text
{{BASE_URL}}
```

**API Key** is the credential used to call the service, usually a string beginning with `sk-`.

1. Log in to {{SITE_NAME}}.
2. Open **API Keys** from the left navigation.
3. Click **Create API Key**, enter a recognizable name, and confirm.
4. Copy the key immediately and store it somewhere safe, such as a password manager. For security reasons, the full key is usually shown only once.

> For permissions, groups, and key rotation, see [API Keys and Accounts](/docs/api-keys).

### Step 2: Open a Terminal and Verify the Key

A terminal is a window for running text commands.

- **macOS**: press `Command + Space`, search for `Terminal`, and press Enter.
- **Windows**: open the Start menu, search for `PowerShell`, and press Enter.

Set the key as an environment variable. Replace the value inside the quotes with your real key:

```bash
export API_KEY="paste-your-api-key-here"
```

> Windows PowerShell equivalent: `$env:API_KEY="paste-your-api-key-here"`.

Then copy and run this command as-is:

```bash
curl "{{BASE_URL}}v1/models" \
  -H "Authorization: Bearer ${API_KEY}"
```

**Expected result**: the terminal prints JSON containing `"data": [...]`. Each item's `"id"` is a model name you can use, for example:

```json
{
  "object": "list",
  "data": [
    { "id": "claude-sonnet-4-5", "object": "model" },
    { "id": "gpt-5.5", "object": "model" }
  ]
}
```

If you do not see a model list:

- `401`: the key was not accepted. Check that the key is complete and that there is exactly one space between `Bearer` and the key.
- `404` or connection failure: the Base URL is wrong. Compare it with the address in your browser.
- Other errors: see [Troubleshooting](/docs/errors).

> **Do not continue to configure clients until this step works.** Every later integration path depends on a correct Base URL and API Key.

### Step 3: Confirm the Model Name

The `"id"` values returned by `/v1/models` are the model names available to the current key. When configuring a client or writing code, choose model names from that list instead of guessing.

> Model availability depends on groups and channels. See [Models and Platforms](/docs/models).

## Choose an Integration Path

| Your scenario | Recommended path | Go to |
| --- | --- | --- |
| You are writing your own app or script in Python, TypeScript, Go, or curl | Code integration | [Code Integration](/docs/integration-code) |
| You use a GUI IDE, editor, or extension such as Cursor, VS Code plugins, or Trae | Client integration | [Client Integration](/docs/integration-clients) |
| You use terminal AI tools such as Codex CLI or Claude Code | CLI integration | [CLI Integration](/docs/integration-cli) |
| You manage multiple sites, keys, or AI-tool accounts | Third-party tools | [Third-Party Tools](/docs/integration-tools) |

If you are unsure, ask where you usually use AI coding tools:

- In Cursor, VS Code, or another editor -> Client Integration.
- In a terminal with `claude` or `codex` -> CLI Integration.
- In your own application or automation script -> Code Integration.
- Across multiple relay sites or keys -> Third-Party Tools.

## Shared Configuration Conventions

| Convention | Meaning |
| --- | --- |
| `{{BASE_URL}}` | Your deployment address. Examples on this site are automatically rendered with the current deployment URL. |
| `${API_KEY}` | A placeholder for your key. In terminal commands it reads the `API_KEY` environment variable; in GUI fields and config files, replace it with the real key unless the tool explicitly supports env vars. |
| Base URL suffix | Some clients require the root address `{{BASE_URL}}`; others require `{{BASE_URL}}v1`. Each page states the exact value. Copy it precisely. |
| Environment variables | Commands like `export NAME="value"` only affect the current terminal window unless added to a shell profile such as `~/.zshrc` or `~/.bashrc`. |
| Model names | Always use names returned by `/v1/models` or mapping names provided by an admin. |

## Quick Error Reference

| Symptom | Common cause | Quick fix |
| --- | --- | --- |
| 401 Unauthorized | Missing key, incomplete key, or wrong variable name | Re-copy the key and verify the header is `Authorization: Bearer sk-...` |
| 403 Forbidden | The key's group lacks permission for the model or endpoint | Use a model returned by `/v1/models` or contact an admin |
| 404 Not Found | The Base URL has an extra or missing `/v1`, or the endpoint is disabled | Compare the URL against the integration page character by character |
| Request hangs | Streaming/SSE or network proxy issue | First verify with the `/v1/models` curl command above |

For full diagnostics, see [Troubleshooting](/docs/errors).

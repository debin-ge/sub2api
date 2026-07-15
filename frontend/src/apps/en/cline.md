# Cline

> VS Code AI coding plugin, supports Anthropic and OpenAI protocols. Prefer **Anthropic mode** — most complete experience.

```client
name: Cline
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://github.com/cline/cline
```

## 1. Install

VS Code Extensions (`Cmd/Ctrl + Shift + X`), search `Cline`, click Install.

## 2. Configure

Click the Cline icon in the sidebar; the Provider setup screen appears on first run (later reach it via the gear icon at the top right of the panel).

- **API Provider**: **Anthropic**
- **Anthropic API Key**: paste your key
- Tick **Use custom base URL**, enter `{{BASE_URL}}` (**no** `v1`)
- **Model**: pick a Claude model name from `/v1/models`

## 3. Verify

In the Cline input box, send "list files in the current directory". It should respond and start planning tool calls — that means success.

<details>
<summary>Troubleshooting</summary>

- **404** — in Anthropic mode Base URL does **not** end with `v1`. Reversed = 404.
- **401** — key incomplete or pasted into the wrong row.
- **Model error** — swap in a model name from `/v1/models`.

</details>

<details>
<summary>OpenAI Compatible mode (fallback)</summary>

For GPT-family models:

- **API Provider**: **OpenAI Compatible**
- **Base URL**: `{{BASE_URL}}v1` (**with** `v1`)
- **API Key**: your key
- **Model ID**: from `/v1/models`

The two modes have different Base URL suffixes (Anthropic no `v1`, OpenAI has `v1`); swap them and you'll 404.

</details>

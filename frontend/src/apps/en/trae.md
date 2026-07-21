# Trae

> ByteDance's AI IDE. Add a custom model provider.

```client
name: Trae
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://www.trae.ai
```

## 1. Install

Download from [trae.ai](https://www.trae.ai).

## 2. Configure

Click the gear icon at the top right → **Models** → **Add custom model / Custom Provider**:

- **Provider type**: **Anthropic** for Claude, **OpenAI Compatible** for GPT-family
- **Base URL**: Anthropic → `{{BASE_URL}}`; OpenAI Compatible → `{{BASE_URL}}v1`
- **API Key**: paste your key
- **Model**: from `/v1/models`

Save, then pick the added model in the model selector at the bottom left of the chat panel.

## 3. Verify

Send "hello"; a reply means success.

<details>
<summary>Troubleshooting</summary>

- **404** — Base URL `v1` suffix must match the protocol (Anthropic no `v1`, OpenAI Compatible with `v1`).
- **Can't find the entry** — Trae's UI iterates fast; different versions may call it "Model Providers" or similar. Check Trae's official docs.

</details>

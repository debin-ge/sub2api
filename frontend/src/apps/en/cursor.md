# Cursor

> AI editor. Everything is done in Settings — no config files.

```client
name: Cursor
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
homepage: https://cursor.com
```

## 1. Install

Download from [cursor.com](https://cursor.com).

## 2. Configure

Press `Cmd/Ctrl + Shift + J` to open Cursor Settings → **Models**. In the **OpenAI API Key** area:

- API Key — paste your key
- Expand **Override OpenAI Base URL**, enter `{{BASE_URL}}v1`

Click **Verify**. A success message means the key works.

Then click **Add model** in the Models list and enter a model name from `/v1/models` (e.g. `gpt-4o-mini`); tick it to enable.

## 3. Verify

Press `Cmd/Ctrl + L` to open chat, pick the model you added, send "hello". A reply means success.

<details>
<summary>Troubleshooting</summary>

- **Verify fails** — Base URL must be exactly `{{BASE_URL}}v1` (ends with `v1`); key intact with no spaces.
- **404** — Base URL missing `v1` or duplicated as `/v1/v1`.
- **Chat is silent** — check the chat model picker (bottom left) actually points to your new model.

</details>

<details>
<summary>Notes</summary>

Even with a custom Base URL, some of Cursor's built-in features (like Tab autocomplete) may still hit the official endpoint. This is expected.

</details>

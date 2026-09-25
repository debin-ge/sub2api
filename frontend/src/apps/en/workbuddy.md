# WorkBuddy

> Tencent's AI office agent. Connect it to {{SITE_NAME}} as a custom model. WorkBuddy custom models only speak the OpenAI Chat Completions protocol, so GPT, Claude, Gemini, and other models all share the same setup.

```client
name: WorkBuddy
logo: app
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.workbuddy/models.json
homepage: https://www.workbuddy.ai
```

## 1. Install

Download the desktop app (Windows / macOS) from [workbuddy.ai](https://www.workbuddy.ai), install it, and sign in.

## 2. Add a custom model

### Option A: In Settings (recommended)

Open **Settings → Models → Add Model**, choose **Custom** as the provider, and fill in:

| Field | Value |
| --- | --- |
| URL | `{{BASE_URL}}v1` |
| API Key | Your `sk-` key |
| Model name | An exact model ID from `{{BASE_URL}}v1/models`, e.g. `gpt-5.5` or `claude-sonnet-4-6` |

Under **Advanced settings**, match the toggles to the model:

- **Tool calling**: recommended. WorkBuddy's file handling and task execution rely on tool calls.
- **Image input** and **Reasoning**: enable only when the model supports them. These toggles tell WorkBuddy what it may try; they do not add capabilities to the model.
- **Custom protocol**: leave it off. When off, WorkBuddy appends `/chat/completions` to the URL.

Save, go back to the chat, and pick the new model from the **Custom models** group at the bottom of the model picker. Add each model separately.

> If you prefer the full address, set the URL to `{{BASE_URL}}v1/chat/completions` and turn **on** Custom protocol so WorkBuddy calls it as-is. Use one style or the other, not both.

### Option B: Edit models.json

To add many models at once, edit the config file directly:

| OS | Path |
| --- | --- |
| macOS / Linux | `~/.workbuddy/models.json` |
| Windows | `%USERPROFILE%\.workbuddy\models.json` |

```json download=models.json
{
  "models": [
    {
      "id": "gpt-5.5",
      "name": "GPT-5.5",
      "vendor": "{{SITE_NAME}}",
      "url": "{{BASE_URL}}v1/chat/completions",
      "apiKey": "paste your sk- key here",
      "supportsToolCall": true,
      "supportsImages": true,
      "supportsReasoning": true
    },
    {
      "id": "claude-sonnet-4-6",
      "name": "Claude Sonnet 4.6",
      "vendor": "{{SITE_NAME}}",
      "url": "{{BASE_URL}}v1/chat/completions",
      "apiKey": "paste your sk- key here",
      "supportsToolCall": true,
      "supportsImages": true
    }
  ],
  "availableModels": ["gpt-5.5", "claude-sonnet-4-6"]
}
```

- In the file, `url` must be the full `/v1/chat/completions` endpoint. This differs from the Settings form.
- `id` must match a `/v1/models` entry exactly, and `availableModels` must list the same `id`, or the model will not appear in the picker.
- The example models are placeholders; use models your key's group can actually access.
- Older versions used `~/.codebuddy/models.json`. Existing entries there keep working after upgrading and can be viewed and edited in Settings.
- The file stores your key in plain text. Do not commit it to Git or share it.

WorkBuddy reloads the file automatically after you save it. If nothing changes, quit WorkBuddy completely and reopen it.

## 3. Verify

Select the new model in the chat and send a task that needs tools, for example:

```text
Create hello.txt on my desktop with the text "Hello".
```

A normal reply with the task completed means the setup works. The calls then show up in [Usage](/usage).

<details>
<summary>If something fails</summary>

- **401** — The API key is invalid, disabled, or was pasted with extra spaces.
- **404** — The URL path is wrong. With the Settings form, use `{{BASE_URL}}v1` with Custom protocol off; a doubled `/chat/completions/chat/completions` or a missing `/v1` both return 404.
- **Model not found / no available account** — The model name must come from `/v1/models`, and your key's group must include that model.
- **Model does not appear** — Make sure you saved and entered a model name. For models.json, check that the JSON is valid (missing or trailing commas are the usual culprits) and that `availableModels` includes the `id`. On Windows, make sure Notepad did not save it as `models.json.txt`.
- **models.json is correct but still ignored** — Community reports say some versions disable loading custom models from the file by default. Quit WorkBuddy completely, open `~/.workbuddy/cache/acc-product-config-v3.json` (Windows: `%USERPROFILE%\.workbuddy\cache\acc-product-config-v3.json`), set `"CustomModelsJSON"` to `true` (add it to the top-level object if missing), save, and reopen WorkBuddy. This is not in the official docs, so back the file up first. It is a cache file and may be reset after an update; check it again if the setting stops working. You can also add the model through Settings instead.
- **It chats but never runs tasks** — Turn on Tool calling and use a model that supports function calling. You can check tool calls on `/v1/chat/completions` first with the [code samples](/apps/code).
- **Can't find the menu** — WorkBuddy changes quickly and menu names vary by version; see the [official docs](https://www.workbuddy.ai/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Model).

</details>

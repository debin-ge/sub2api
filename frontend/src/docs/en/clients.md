# Client Integration

This page gives a full integration path for common clients: verify the key and models first, configure an SDK or tool next, then handle streaming, timeouts, and errors. Replace `{{BASE_URL}}` with the address provided by an admin, and pass the API Key through `$YOUR_KEY`.

## Before You Configure a Client

Set common environment variables:

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

List models with the current key:

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

If this command fails, do not configure the SDK yet. Fix Base URL, API Key, group permission, or network connectivity first.

## Minimal curl Requests

Chat Completions:

```bash
curl "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "Hello, {{SITE_NAME}}." }
    ]
  }'
```

Streaming Chat Completions:

```bash
curl -N "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [
      { "role": "user", "content": "Explain {{SITE_NAME}} in three points." }
    ]
  }'
```

If the example model is unavailable, use a model returned by `/v1/models`.

## OpenAI SDK TypeScript

Most OpenAI SDK configurations expect `baseURL` at the `/v1` level:

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

```ts
import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: process.env.OPENAI_BASE_URL,
  apiKey: process.env.OPENAI_API_KEY,
})

const result = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: 'Introduce {{SITE_NAME}} in one sentence.' }],
})

console.log(result.choices[0]?.message?.content)
```

Streaming:

```ts
const stream = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  stream: true,
  messages: [{ role: 'user', content: 'Return a short outline.' }],
})

for await (const chunk of stream) {
  process.stdout.write(chunk.choices[0]?.delta?.content ?? '')
}
```

If the environment variable already includes `/v1`, do not append `/v1` again in code.

## OpenAI SDK Python

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

```python
from openai import OpenAI

client = OpenAI()

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[
        {"role": "user", "content": "Introduce {{SITE_NAME}} in one sentence."},
    ],
)

print(response.choices[0].message.content)
```

If you see `404`, first check whether `OPENAI_BASE_URL` became `{{BASE_URL}}v1/v1` or is missing `/v1`.

## Anthropic Messages Request

```bash
curl "${BASE_URL}v1/messages" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 512,
    "messages": [
      { "role": "user", "content": "Turn these requirements into acceptance criteria." }
    ]
  }'
```

Messages requests usually require `max_tokens`. If you copy an OpenAI `messages` shape to an Anthropic endpoint, also confirm that the fields match the client or upstream requirements.

## Gemini Native Endpoints

Gemini native clients should use `/v1beta` paths and Gemini request body format. Non-streaming example:

```bash
curl "${BASE_URL}v1beta/models/gemini-2.0-flash:generateContent" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "Introduce {{SITE_NAME}} in one sentence." }
        ]
      }
    ]
  }'
```

Streaming:

```bash
curl -N "${BASE_URL}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "Stream a short outline." }
        ]
      }
    ]
  }'
```

Gemini native endpoints do not use the OpenAI `messages` format. Use a Gemini model name or mapping name supported by the current deployment.

## CLI Coding Clients (Coding Agents)

These command-line tools (such as Claude Code CLI, Codex CLI, and OpenCode CLI) run directly in your terminal. You can easily configure and switch their underlying relay services using environment variables.

### Claude Code CLI

The official Claude Code client can be directed to this platform by setting the Anthropic API Base URL and key:

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_API_KEY="$YOUR_KEY"
```

Or for certain versions:

```bash
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

Once configured, run `claude` in your terminal to spin up the agent and interact with compatible models.

If an admin provides a custom `/antigravity` route, you can change the Base URL to:

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
```

### Codex CLI

Codex CLI relies on the OpenAI-formatted Responses API. Set the following environment variables to route requests to this platform:

```bash
export OPENAI_BASE_URL="{{BASE_URL}}v1"
export OPENAI_API_KEY="$YOUR_KEY"
```

**Important Note**: If you use Nginx to reverse-proxy this platform, ensure that Nginx includes the following configuration in its `http` block:
```nginx
underscores_in_headers on;
```
If this setting is missing, Nginx drops headers containing underscores (like `session_id`), causing sticky session functionality to fail in multi-account environments.

### OpenCode CLI

OpenCode CLI connects to the OpenCode2API channel. Configure the endpoints and key as follows:

```bash
export OPENCODE_API_BASE="{{BASE_URL}}v1"
export OPENCODE_API_KEY="$YOUR_KEY"
```

If using a generic compatible client, simply point the OpenAI SDK `OPENAI_BASE_URL` and `OPENAI_API_KEY` to the `/v1` path of this platform.

## Third-Party GUI & Integrated Tools

These graphical tools help save your API keys and let you manage, configure, and switch between different underlying AI clients from a single workspace.

### CC-Switch: Client Switching & One-Click Import

* **Client Management & Switching**: CC-Switch is a local utility that allows you to easily configure, manage, and switch between multiple clients (Claude, Gemini, Codex, etc.).
* **One-Click Configuration**: To avoid modifying environment variables for each client manually, this platform integrates a one-click import button using the `ccswitch://` protocol. In the **"API Keys"** section, click **"Import to CC-Switch"** next to your API Key, select the target client type (Claude, Gemini, or Codex), and CC-Switch will automatically configure the endpoint and key for you.
* **Manual Configuration Parameters**:
  * Claude Client: `app: "claude"`, `endpoint: "{{BASE_URL}}"`
  * Codex Client: `app: "codex"`, `endpoint: "{{BASE_URL}}v1"`, default model `gpt-5.4`
  * Gemini Client: `app: "gemini"`, `endpoint: "{{BASE_URL}}"`

### Cockpit: Centralized Management & Switching

* **Centralized Dashboard**: Cockpit is a local agent workspace GUI that lets you monitor, manage, and switch between parallel CLI agent sessions (like Claude Code CLI, Codex CLI, OpenCode CLI, etc.).
* **Configuration & Switching**: In Cockpit's settings panel, you can independently configure API credentials for different engines, allowing you to switch clients at will. Update the API Base URL (or Endpoint) settings as follows:
  * **Claude Engine**: Set Endpoint to `{{BASE_URL}}` and enter the platform API Key.
  * **Codex Engine**: Set Endpoint to `{{BASE_URL}}v1` and enter the platform API Key.
  * **OpenCode Engine**: Set Endpoint to `{{BASE_URL}}v1` and enter the platform API Key.

### Codex App

For the Codex App desktop client, configure it via its settings panel:
1. Replace the OpenAI API Base URL with: `{{BASE_URL}}v1`
2. Enter the API Key generated by this platform.

## Client Configuration Tips

| Item | Recommendation |
| --- | --- |
| Base URL | Use the root address for curl path joining; OpenAI SDKs usually use `{{BASE_URL}}v1`. |
| API Key | Use `$YOUR_KEY` or the environment variable required by the client, and do not write keys into source code. |
| Model name | Use `/v1/models`, `/v1beta/models`, or admin-provided mapping documentation. |
| Timeout | Long outputs and streaming requests need longer HTTP, proxy, and load balancer timeouts. |
| Retry | Use exponential backoff for 429 and 5xx; do not blindly retry 401, 403, or 404. |
| Logs | Log request path, model, status code, and request ID, but never the full key. |

## Common Integration Issues

| Symptom | Possible cause | Action |
| --- | --- | --- |
| SDK returns 401 | API Key is not passed, variable name is wrong, or Bearer is missing. | Check whether the environment variable is empty and confirm the header format. |
| SDK returns 404 | Base URL duplicates `/v1` or the endpoint is not enabled. | Inspect the final request URL. |
| Model does not exist | The example model is not exposed to the current group. | Use a model returned by `/v1/models`. |
| Streaming has no output | Client or proxy does not support SSE, or timeout is too short. | Verify non-streaming first, then check proxy and timeout settings. |
| Local works but production fails | Production key, group, egress network, or proxy config differs. | Compare environment variables, model lists, and status codes. |

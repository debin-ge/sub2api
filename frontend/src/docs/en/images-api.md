# Image Generation API

This page explains how to generate and edit images through {{SITE_NAME}}. The endpoints are compatible with the OpenAI Images API, so the OpenAI SDK works as soon as its Base URL is set to `{{BASE_URL}}v1`.

> [!NOTE]
> Images and videos for Grok (xAI) groups go through the xAI media path. Follow the instructions provided by your admin; this page does not cover them.

## Prerequisites

| Requirement | Otherwise |
| --- | --- |
| The API Key's group platform is `openai` | 404 `Images API is not supported for this platform` |
| The group has "allow image generation" enabled | 403 `Image generation is not enabled for this group` |
| The group exposes an image model with a configured price | 400 or 403 saying the model or price is unavailable |

Use a model name returned by `/v1/models` or a mapped name provided by your admin. When `model` is omitted, `gpt-image-2` is used.

## Endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/v1/images/generations` | Text to image |
| POST | `/v1/images/edits` | Image to image, inpainting |
| POST | `/v1/images/generations/async`, `/v1/images/edits/async` | Asynchronous submission, see below |
| GET | `/v1/images/tasks/{task_id}` | Poll an asynchronous task |

The same paths without the `/v1` prefix also work.

## Image generation

```bash tab=curl group=images-generate
curl "{{BASE_URL}}v1/images/generations" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "A lighthouse during a winter storm, cinematic",
    "size": "1536x1024",
    "n": 1
  }'
```

```python tab=Python group=images-generate
# pip3 install openai
import base64
import os
from openai import OpenAI

client = OpenAI(base_url="{{BASE_URL}}v1", api_key=os.environ["API_KEY"])

result = client.images.generate(
    model="gpt-image-2",
    prompt="A lighthouse during a winter storm, cinematic",
    size="1536x1024",
)
with open("lighthouse.png", "wb") as f:
    f.write(base64.b64decode(result.data[0].b64_json))
```

Request fields:

| Field | Notes |
| --- | --- |
| `model` | Optional, defaults to `gpt-image-2`. Any non-empty model name is accepted; success depends on the group's model and price configuration. |
| `prompt` | Required. |
| `n` | Optional, defaults to 1. The cap is set by the deployment, 10 by default. With `n` above 1 the balance is pre-checked against unit price times count. |
| `size` | Optional. Accepts `auto`, `1k`, `2k`, `4k` or `WIDTHxHEIGHT`. The longest edge must not exceed 4096 px, otherwise 400. |
| `quality`, `background`, `output_format`, `output_compression`, `moderation`, `input_fidelity`, `style`, `partial_images` | Optional, forwarded upstream with their native OpenAI meaning. |
| `response_format` | `b64_json` (default) or `url`. |
| `stream` | Optional. When `true`, partial image events are returned over SSE. |

> [!TIP]
> Prefer `b64_json` when you need a file. When the image is produced through a ChatGPT account channel, `response_format: "url"` returns an inline `data:` URL rather than a hosted link.

`model`, `size` and `n` must not appear twice in the body, and a wrongly typed field returns 400.

## Image editing

Both JSON and multipart bodies are supported.

```bash tab=curl group=images-edit
curl "{{BASE_URL}}v1/images/edits" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "Replace the sky with a sunset",
    "images": [{ "image_url": "https://example.com/input.png" }],
    "mask": { "image_url": "https://example.com/mask.png" }
  }'
```

```bash tab=curl-multipart group=images-edit
curl "{{BASE_URL}}v1/images/edits" \
  -H "Authorization: Bearer ${API_KEY}" \
  -F model="gpt-image-2" \
  -F prompt="Replace the sky with a sunset" \
  -F image="@input.png" \
  -F mask="@mask.png"
```

```python tab=Python group=images-edit
# pip3 install openai
import base64
import os
from openai import OpenAI

client = OpenAI(base_url="{{BASE_URL}}v1", api_key=os.environ["API_KEY"])

with open("input.png", "rb") as image, open("mask.png", "rb") as mask:
    result = client.images.edit(
        model="gpt-image-2",
        prompt="Replace the sky with a sunset",
        image=image,
        mask=mask,
    )
with open("edited.png", "wb") as f:
    f.write(base64.b64decode(result.data[0].b64_json))
```

- JSON: `images[].image_url` is required and accepts `https://` or `data:` URLs; `mask.image_url` is optional; `file_id` is not supported.
- Multipart: file fields are `image` (repeatable) and an optional `mask`, each at most 20MB; `model`, `size` and `n` are text fields and must not repeat.
- All other fields match image generation.

## Response

```json
{
  "created": 1784092923,
  "data": [
    { "b64_json": "...", "revised_prompt": "..." }
  ],
  "usage": { "input_tokens": 0, "output_tokens": 0, "total_tokens": 0 }
}
```

`usage` is present only when the upstream reports token usage.

> [!IMPORTANT]
> On a long non-streaming request the response may begin with whitespace heartbeats; JSON parsing is unaffected. Once a heartbeat has been sent the status code is fixed at 200, and a later upstream error arrives as a JSON error body. Check the `error` field instead of relying on the status code alone.

## Asynchronous endpoints

> [!NOTE]
> The asynchronous endpoints require object storage enabled by the admin. Without it, submission returns 404 `async image tasks are not enabled`; the synchronous endpoints are unaffected.

- Submit: POST the same body to `/v1/images/generations/async` or `/v1/images/edits/async`. The response is 202 with `Location` and `Retry-After: 3` headers, and a body containing `id`, `status`, `poll_url` and `expires_at`. `stream: true` is rejected.
- Poll: `GET /v1/images/tasks/{task_id}` with the same key that submitted the task. `status` is `processing`, `completed` or `failed`; on completion `image_url` is the stored link and `result` holds the full synchronous response.
- Limits: tasks expire after 24 hours; each user may have at most 20 pending tasks, beyond which 429 is returned with a `Retry-After` header.

## Billing

Token-based pricing is used first; when the upstream reports no image tokens, per-image pricing applies with 1K, 2K and 4K tiers decided by the longest edge. A missing `size` or `auto` is billed as 2K. See [Billing and Usage](/docs/billing-usage).

## Common errors

| Status | Message | Cause |
| --- | --- | --- |
| 400 | `n must be between 1 and 10` | `n` exceeds the deployment cap. |
| 400 | `unsupported image size` | The longest edge exceeds 4096 px, or `size` is not recognized. |
| 400 | `invalid ... field type` | A field has the wrong type, or `model`, `size` or `n` appears twice. |
| 403 | `Image generation is not enabled for this group` | The group lacks image permission. |
| 404 | `Images API is not supported for this platform` | The group platform is not `openai`. |
| 404 | `async image tasks are not enabled` | The deployment has not enabled asynchronous image tasks. |
| 429 | Pending asynchronous task limit reached | Retry after `Retry-After`. |

See [Troubleshooting](/docs/errors) for the general checklist.

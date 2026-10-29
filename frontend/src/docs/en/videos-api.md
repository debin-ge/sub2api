# Video Tasks API

This page explains how to submit video generation tasks through {{SITE_NAME}}. Groups on the ByteDance (Volcengine Ark), fal, WaveSpeed, Runware and Flova platforms all use the Volcengine Ark asynchronous video task protocol: request body, response body and status vocabulary match Ark, so the Ark SDK works once its Base URL is set to `{{BASE_URL}}api/v3`.

> [!NOTE]
> OpenAI (Sora) continues to use `/v1/videos`. Grok keeps its own payload and paths; see “Managed Grok video” below. The Ark parameter tables do not apply to Grok.

## Prerequisites

| Requirement | Otherwise |
| --- | --- |
| The API Key's group platform is ByteDance, fal, WaveSpeed, Runware or Flova, or a composite group routed to one of them | 403 `video_ark_platform_unsupported` |
| You call the paths on this page, not `/v1/videos` | 404 `use_contents_generations_tasks` |
| The group has a video price configured for the model | 400 `video_pricing_missing` |
| The balance can cover the estimated hold | 402 `video_insufficient_balance` |

## Endpoints

`{prefix}` can be `/api/v3`, `/v3`, `/v1` or empty; `/api/v3` is recommended.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `{prefix}/contents/generations/tasks` | Create a task, returns `{"id"}` |
| GET | `{prefix}/contents/generations/tasks/{id}` | Get a task |
| GET | `{prefix}/contents/generations/tasks` | List your own tasks |
| DELETE | `{prefix}/contents/generations/tasks/{id}` | Cancel a running task, or delete a finished one |
| GET | `{prefix}/contents/generations/tasks/{id}/content?variant=video&exp=&sig=` | Signed content link, issued in the task response, no API Key needed |
| GET | `{prefix}/contents/generations/models` | {{SITE_NAME}} extension: durations, resolutions, ratios, flags and reference media limits per model |

`/v1/models` also works for these groups and lists the available video models.

## Flow

Three steps: submit, poll, download.

```bash tab=curl group=video-flow
# 1. Submit
curl "{{BASE_URL}}api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "model": "doubao-seedance-1-0-pro-250528",
    "content": [
      { "type": "text", "text": "Waves gently washing over a beach, slow push-in" },
      { "type": "image_url", "image_url": { "url": "https://example.com/first.png" }, "role": "first_frame" }
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "adaptive"
  }'
# Returns {"id":"video_..."}

# 2. Poll until status is succeeded / failed / cancelled / expired
curl "{{BASE_URL}}api/v3/contents/generations/tasks/${TASK_ID}" \
  -H "Authorization: Bearer ${API_KEY}"

# 3. Download: on succeeded, content.video_url is a signed link that needs no API Key
curl -L -o output.mp4 "${VIDEO_URL}"
```

```python tab=Python group=video-flow
# pip3 install requests
import os
import time
import requests

TASKS = "{{BASE_URL}}api/v3/contents/generations/tasks"
HEADERS = {"Authorization": f"Bearer {os.environ['API_KEY']}"}

task = requests.post(TASKS, headers=HEADERS, json={
    "model": "doubao-seedance-1-0-pro-250528",
    "content": [{"type": "text", "text": "Waves gently washing over a beach, slow push-in"}],
    "duration": 5,
    "resolution": "720p",
}).json()

while True:
    info = requests.get(f"{TASKS}/{task['id']}", headers=HEADERS).json()
    if info["status"] in ("succeeded", "failed", "cancelled", "expired"):
        break
    time.sleep(10)

if info["status"] == "succeeded":
    video = requests.get(info["content"]["video_url"])
    with open("output.mp4", "wb") as f:
        f.write(video.content)
else:
    print(info.get("error"))
```

```python tab=Ark-SDK group=video-flow
# pip3 install 'volcengine-python-sdk[ark]'
import os
from volcenginesdkarkruntime import Ark

client = Ark(base_url="{{BASE_URL}}api/v3", api_key=os.environ["API_KEY"])

task = client.content_generation.tasks.create(
    model="doubao-seedance-1-0-pro-250528",
    content=[{"type": "text", "text": "Waves gently washing over a beach, slow push-in"}],
)
result = client.content_generation.tasks.get(task_id=task.id)
print(result.status)
```

## Request fields

| Field | Required | Notes |
| --- | --- | --- |
| `model` | Yes | The platform's native model ID, e.g. `doubao-seedance-1-0-pro-250528`, `flova/seedance-2.5`, `bytedance/seedance-2.0/text-to-video`. Use `/v1/models` or the models catalog as the source of truth. |
| `content` | Yes | Array of 1 to 32 items with at least one `text` item, see the table below. |
| `duration` | No | Integer seconds, an integer string is also accepted; `-1` means smart duration. Mutually exclusive with `frames`. |
| `frames` | No | Frame count of the form 25+4n within 29 to 289. Mutually exclusive with `duration`. |
| `resolution` | No | `480p`, `720p`, `1080p`, `2k`, `4k`. |
| `ratio` | No | `16:9`, `9:16`, `4:3`, `3:4`, `1:1`, `21:9`, `adaptive`. `adaptive` requires a first frame. |
| `seed` | No | Integer from -1 to 2^32-1. |
| `camera_fixed`, `watermark`, `generate_audio`, `return_last_frame` | No | Boolean flags. Whether they take effect depends on the upstream, see the collapsible section below. |
| `service_tier` | No | `default` or `flex`. |
| `execution_expires_after` | No | 3600 to 259200 seconds. If the task has not started by then it becomes `expired` and the hold is released. |
| `callback_url` | No | Must be a public https URL; called once when the task reaches a final state. |
| `provider_options` | No | {{SITE_NAME}} extension carrying vendor-specific parameters; allowed values are listed under `provider_options` in the models catalog. Not accepted by direct ByteDance accounts. |

Each `content[]` item:

| `type` | `role` | Notes |
| --- | --- | --- |
| `text` | none | The prompt. Inline flags such as `--rs 720p --rt 16:9 --dur 5 --seed 11` override the top-level fields. |
| `image_url` | `first_frame` (default), `last_frame`, `reference_image` | `image_url.url` accepts `https://` or an inline `data:image/...;base64,` URL of at most 8MB per image. |
| `video_url` | `reference_video` | `https://` only. |
| `audio_url` | `reference_audio` | `https://` only. |

- The body is JSON and at most 16MB by default. Unknown fields and duplicate keys are rejected. The Ark fields `priority`, `draft`, `safety_identifier`, `output_format`, `tools` and `omni_reference_task_type` are not supported and return 400 when present.
- The `Idempotency-Key` header is a {{SITE_NAME}} extension: resubmitting the same request with the same key returns the original task, while a different request returns 409.
- Each model's supported durations, resolutions, ratios and reference media counts come from the models catalog. For example, Seedance 1.0 models take 2 to 12 seconds and support neither `-1` nor `frames`.

<details>
<summary>How non-direct upstreams treat the flag fields</summary>

fal, WaveSpeed, Runware, Flova and ByteDance relay accounts cannot honour every Ark field literally. They are handled as follows:

| Handling | Fields |
| --- | --- |
| Accepted without effect and listed under `ignored_parameters` in the task response | `seed`, `camera_fixed`, `watermark`, `generate_audio` and `return_last_frame` (any value) when the upstream cannot apply them, plus `service_tier="default"` |
| Converted to a value the upstream accepts and listed under `adjusted_parameters` | `duration=-1` becomes the model's default duration; `frames` is rounded up to whole seconds at 24 fps, then snapped to the nearest duration the model supports (ties go to the longer one). The resulting duration is reported as `duration` |
| Rejected with 400 `unsupported_parameter` | `service_tier="flex"` |

An ignored `generate_audio=true` is priced as a silent video. Flova writes `camera_fixed=true` into its generation instruction to ask for a locked camera, without guaranteeing the result. Flova requires reference images to be inlined as `data:` URLs. Flags that take effect on each model are listed under `flags` in the models catalog, and reference media roles and limits under `content_roles`.

</details>

## Responses

Create returns 200 `{"id": "video_..."}`. The `id` is the {{SITE_NAME}} task ID; the upstream ID is never exposed.

Get returns the Ark shape:

```json
{
  "id": "video_...",
  "model": "doubao-seedance-1-0-pro-250528",
  "status": "succeeded",
  "content": {
    "video_url": "{{BASE_URL}}api/v3/contents/generations/tasks/video_.../content?variant=video&exp=...&sig=..."
  },
  "usage": { "completion_tokens": 123456, "total_tokens": 123456 },
  "resolution": "720p",
  "ratio": "16:9",
  "duration": 5,
  "created_at": 1760000000,
  "updated_at": 1760000060
}
```

| Field | Notes |
| --- | --- |
| `status` | `queued`, `running`, `succeeded`, `failed`, `cancelled`, `expired`. `running` includes generated-but-unsettled tasks; `succeeded` means billing has settled. |
| `content.video_url` | Present only on `succeeded`. A signed link that needs no API Key, valid for at most 24 hours and never beyond the upstream retention. An expired link returns 410; fetch the task again for a fresh link. |
| `content.last_frame_url` | Present when `return_last_frame=true` and the upstream provides a last frame. |
| `usage` | Present only when the upstream reports tokens, currently direct ByteDance only. |
| `error` | Present on `failed`, `cancelled` and `expired`, with `code` and `message`. |
| `ignored_parameters` | Flags that were accepted without effect. |
| `adjusted_parameters` | Fields whose value was converted, such as `duration` or `frames`. |
| `prompt`, `actual_cost`, `currency`, `provider` | Presence depends on the group's disclosure policy. |

While a task is unfinished the response carries `Retry-After: 10`; poll at that interval.

List: `GET {prefix}/contents/generations/tasks?page_num=1&page_size=20&filter.status=succeeded&filter.model=...` returns `{"items": [...], "page_num": 1, "page_size": 20, "has_more": false}` and includes only your own tasks.

Delete returns 200 `{}`. A running task is cancelled and a finished task is deleted; neither is refunded.

## Callbacks

With `callback_url` set, the task object is POSTed once when the task reaches a final state. The body is the same as the get response without the signed content link. The request carries four headers, `X-SUB2API-Event-ID`, `X-SUB2API-Timestamp`, `X-SUB2API-Content-SHA256` and `X-SUB2API-Signature`; the signature is an HMAC whose secret is provided by your admin. Failed deliveries are retried for 24 hours by default. Treat the callback as a notification and fetch the task afterwards for the content link.

## Billing

Creating a task holds the estimated cost based on model, resolution and duration tier. On success the task settles against the actual duration; on failure, cancellation or expiry the hold is released. Smart duration is held at the model's longest tier. Deleting a task never refunds. Upstream creation failures are not retried automatically, to avoid creating duplicate paid tasks. See [Billing and Usage](/docs/billing-usage).

## Common errors

Errors use `{"error": {"message": "...", "type": "...", "param": null, "code": "..."}}` with a lowercase `code`.

| Status | `code` | Cause |
| --- | --- | --- |
| 400 | `invalid_content` | `content` is empty or has no `text` item. |
| 400 | `invalid_role` | A media item's `role` does not match its type. |
| 400 | `duration_frames_conflict` | Both `duration` and `frames` were given. |
| 400 | `adaptive_ratio_requires_first_frame` | `ratio` is `adaptive` without a first frame. |
| 400 | `unsupported_parameter` | A value the upstream cannot honour; `message` names the field. |
| 400 | `video_capability_unsupported` | Beyond the model's capabilities, e.g. `seconds 13`. |
| 400 | `video_pricing_missing` | No video price configured for the model in this group. |
| 402 | `video_insufficient_balance` | Balance cannot cover the estimated hold. |
| 403 | `video_ark_platform_unsupported` | The group platform is not one of the five video platforms. |
| 403 | `video_creation_disabled` | The admin has disabled task creation. |
| 404 | `video_task_not_found` | The task does not exist or belongs to another key. |
| 404 | `use_contents_generations_tasks` | A video group called `/v1/videos`. |
| 409 | `video_idempotency_conflict` | `Idempotency-Key` reused for a different request. |
| 409 | `video_content_not_ready` | The content link was opened before the task finished. |
| 410 | `video_content_expired` | The content link expired; fetch the task again. |
| 413 | `video_input_too_large` | The body or an inline image exceeds the limit. |
| 429 | `video_account_concurrency_limited` | All upstream accounts are at their concurrency limit; retry later. |
| 502 | `video_no_account_available` | No usable upstream account. |

See [Troubleshooting](/docs/errors) for the general checklist.


## Managed Grok video

When your administrator enables managed Grok, new tasks follow the same funding and delivery flow as ByteDance: reserve estimated funds, track generation in the background, capture payment before delivery, and release the hold on a confirmed failure. Closing the client does not stop processing. Older relay tasks keep their original settlement flow.

| Method | Path | Purpose |
| --- | --- | --- |
| POST | `/v1/videos/generations` | Generate a video |
| POST | `/v1/videos/edits` | Edit a verified source video |
| POST | `/v1/videos/extensions` | Extend a verified source video |
| GET | `/v1/videos/{request_id}` | Read task status |
| GET | `/v1/videos/{request_id}/content` | Download a settled video with an API Key |

```bash
# Generate once per business task; reuse this key and payload for retries.
REQUEST_KEY="$(uuidgen)"
curl "{{BASE_URL}}v1/videos/generations" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: ${REQUEST_KEY}" \
  -d '{
    "model": "grok-imagine-video-1.5",
    "prompt": "An adult gently places a wooden clock on a table in a shaded park.",
    "duration": 8,
    "aspect_ratio": "9:16",
    "resolution": "480p",
    "generate_audio": true
  }'
# Returns {"request_id":"<UUID>"}
```

The UUID identifies the gateway task, not the supplier task. Status and authenticated downloads require the creator API Key and its original group. `pending` includes generation and pending settlement; `done` and `video.url` are exposed only after capture. Follow `Retry-After` when polling. The returned URL is a temporary signed download capability: keep it private and query again when it expires.

Images use `reference_images: [{"url":"https://..."}]`. Do not send Ark's `content` or `ratio` fields. Reference limits and audio/keyframe combinations depend on the model. `usage.cost_in_usd_ticks` reports supplier cost; the platform bill determines the customer charge.

Edits and extensions accept `video: {"url":"https://.../source.mp4"}` or `video: {"id":"<settled Grok request_id>"}` for a managed task owned by the same key. The exact source bytes are frozen and measured. Edits charge the verified source duration; extension `duration` is the added segment and only that segment is charged. Omit `duration` for edits and omit `resolution`, `aspect_ratio`, and `generate_audio` for both derived operations. The current adapter uses `grok-imagine-video` for derived operations, subject to administrator capability settings.

When submission is uncertain, reuse the original idempotency key instead of creating another paid generation. Grok does not expose upstream cancellation or deletion here. Managed video does not support subscription groups, and simple mode still reserves and settles balance; administrators must review prices and funding behavior before switching.

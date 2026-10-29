# 视频任务 API

本页说明通过 {{SITE_NAME}} 提交视频生成任务的方式。ByteDance（火山方舟）、fal、WaveSpeed、Runware、Flova 五类平台的分组统一使用火山方舟 Ark 的异步视频任务协议：请求体、响应体和状态词表都与 Ark 一致，火山 Ark SDK 只需把 Base URL 改成 `{{BASE_URL}}api/v3`。

> [!NOTE]
> OpenAI（Sora）仍使用 `/v1/videos`。Grok 保留自己的请求格式和路径，见文末“Grok 托管视频”；下方 Ark 参数表不适用于 Grok。

## 前置条件

| 条件 | 不满足时 |
| --- | --- |
| API Key 所属分组平台为 ByteDance、fal、WaveSpeed、Runware、Flova 之一，或路由到它们的合成分组 | 403 `video_ark_platform_unsupported` |
| 使用本页路径，而不是 `/v1/videos` | 404 `use_contents_generations_tasks` |
| 分组为该模型配置了视频价格 | 400 `video_pricing_missing` |
| 余额足够冻结预估费用 | 402 `video_insufficient_balance` |

## 端点

`{prefix}` 可取 `/api/v3`、`/v3`、`/v1` 或留空，推荐 `/api/v3`。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `{prefix}/contents/generations/tasks` | 创建任务，返回 `{"id"}` |
| GET | `{prefix}/contents/generations/tasks/{id}` | 查询任务 |
| GET | `{prefix}/contents/generations/tasks` | 列出本人任务 |
| DELETE | `{prefix}/contents/generations/tasks/{id}` | 取消运行中任务，或删除已完成任务 |
| GET | `{prefix}/contents/generations/tasks/{id}/content?variant=video&exp=&sig=` | 签名内容链接，由查询响应下发，不需要 API Key |
| GET | `{prefix}/contents/generations/models` | {{SITE_NAME}} 扩展：各模型的时长、分辨率、比例、开关与参考媒体限制 |

`/v1/models` 对这些分组同样可用，返回当前可用的视频模型。

## 调用流程

三步：提交、轮询、下载。

```bash tab=curl group=video-flow
# 1. 提交
curl "{{BASE_URL}}api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "model": "doubao-seedance-1-0-pro-250528",
    "content": [
      { "type": "text", "text": "海浪轻轻拍打沙滩，镜头缓慢推进" },
      { "type": "image_url", "image_url": { "url": "https://example.com/first.png" }, "role": "first_frame" }
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "adaptive"
  }'
# 返回 {"id":"video_..."}

# 2. 轮询，直到 status 为 succeeded / failed / cancelled / expired
curl "{{BASE_URL}}api/v3/contents/generations/tasks/${TASK_ID}" \
  -H "Authorization: Bearer ${API_KEY}"

# 3. 下载：succeeded 时 content.video_url 是签名直链，不需要再带 API Key
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
    "content": [{"type": "text", "text": "海浪轻轻拍打沙滩，镜头缓慢推进"}],
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
    content=[{"type": "text", "text": "海浪轻轻拍打沙滩，镜头缓慢推进"}],
)
result = client.content_generation.tasks.get(task_id=task.id)
print(result.status)
```

## 请求字段

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `model` | 是 | 各平台的原生模型 ID，例如 `doubao-seedance-1-0-pro-250528`、`flova/seedance-2.5`、`bytedance/seedance-2.0/text-to-video`。以 `/v1/models` 或能力目录为准。 |
| `content` | 是 | 数组，1 到 32 项，至少一个 `text` 项，见下表。 |
| `duration` | 否 | 整数秒，也接受整数字符串；`-1` 为智能时长。与 `frames` 二选一。 |
| `frames` | 否 | 帧数，取值为 25+4n 且在 29 到 289 之间。与 `duration` 二选一。 |
| `resolution` | 否 | `480p`、`720p`、`1080p`、`2k`、`4k`。 |
| `ratio` | 否 | `16:9`、`9:16`、`4:3`、`3:4`、`1:1`、`21:9`、`adaptive`。`adaptive` 需要提供首帧。 |
| `seed` | 否 | -1 到 2^32-1 的整数。 |
| `camera_fixed`、`watermark`、`generate_audio`、`return_last_frame` | 否 | 布尔开关。是否生效取决于上游，见下方折叠块。 |
| `service_tier` | 否 | `default` 或 `flex`。 |
| `execution_expires_after` | 否 | 3600 到 259200 秒。到时仍未开始执行则任务置为 `expired` 并释放预扣。 |
| `callback_url` | 否 | 必须是 https 公网地址，终态时投递一次。 |
| `provider_options` | 否 | {{SITE_NAME}} 扩展，承载厂商特有参数，可选值见能力目录的 `provider_options`。ByteDance 直连账号不接受。 |

`content[]` 的每一项：

| `type` | `role` | 说明 |
| --- | --- | --- |
| `text` | 无 | 提示词。可内嵌 `--rs 720p --rt 16:9 --dur 5 --seed 11` 这类 flag，覆盖顶层同名字段。 |
| `image_url` | `first_frame`（默认）、`last_frame`、`reference_image` | `image_url.url` 接受 `https://` 或 `data:image/...;base64,` 内联，单张不超过 8MB。 |
| `video_url` | `reference_video` | 只接受 `https://`。 |
| `audio_url` | `reference_audio` | 只接受 `https://`。 |

- 请求体为 JSON，默认不超过 16MB。未知字段和重复键会被拒绝。Ark 协议里的 `priority`、`draft`、`safety_identifier`、`output_format`、`tools`、`omni_reference_task_type` 本网关不支持，出现即 400。
- `Idempotency-Key` 请求头是 {{SITE_NAME}} 扩展：同一个 key 重复提交相同请求返回原任务，提交不同请求返回 409。
- 各模型实际支持的时长、分辨率、比例和参考媒体数量以能力目录为准。例如 Seedance 1.0 系列时长为 2 到 12 秒，不支持 `-1` 与 `frames`。

<details>
<summary>非 ByteDance 直连上游对开关字段的处理</summary>

fal、WaveSpeed、Runware、Flova 以及 ByteDance 中继账号不能逐字执行所有 Ark 字段，处理规则如下：

| 处理 | 字段 |
| --- | --- |
| 接受但不生效，在查询响应的 `ignored_parameters` 中列出 | 上游落不了地的 `seed`、`camera_fixed`、`watermark`、`generate_audio`、`return_last_frame`（任意取值），以及 `service_tier="default"` |
| 换算成上游可接受的取值，在查询响应的 `adjusted_parameters` 中列出 | `duration=-1` 换成模型默认时长；`frames` 按 24fps 向上取整成秒数，再取模型支持的最近时长（等距取较长者）。实际时长见响应的 `duration` |
| 返回 400 `unsupported_parameter` | `service_tier="flex"` |

被忽略的 `generate_audio=true` 按无声视频计价。Flova 会把 `camera_fixed=true` 写进生成指令，要求固定机位，但不保证效果。Flova 的参考图必须以 `data:` 内联。各模型上能生效的开关见能力目录的 `flags`，参考媒体的角色与数量限制见 `content_roles`。

</details>

## 响应

创建返回 200 `{"id": "video_..."}`。`id` 是 {{SITE_NAME}} 的任务 ID，不暴露上游 ID。

查询返回与 Ark 一致：

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

| 字段 | 说明 |
| --- | --- |
| `status` | `queued`、`running`、`succeeded`、`failed`、`cancelled`、`expired`。`running` 包含已生成但尚未结算；`succeeded` 表示已结算。 |
| `content.video_url` | 仅 `succeeded` 时出现，签名直链，不需要 API Key。有效期最长 24 小时且不超过上游保留期；过期返回 410，重新查询任务即可拿到新链接。 |
| `content.last_frame_url` | `return_last_frame=true` 且上游提供尾帧时出现。 |
| `usage` | 仅上游回报 token 时出现，目前只有 ByteDance 直连。 |
| `error` | `failed`、`cancelled`、`expired` 时出现，含 `code` 与 `message`。 |
| `ignored_parameters` | 被接受但未生效的开关。 |
| `adjusted_parameters` | 被换算过取值的字段，例如 `duration`、`frames`。 |
| `prompt`、`actual_cost`、`currency`、`provider` | 是否出现取决于分组的披露策略。 |

任务未完成时查询响应带 `Retry-After: 10`，建议按此间隔轮询。

列表：`GET {prefix}/contents/generations/tasks?page_num=1&page_size=20&filter.status=succeeded&filter.model=...`，返回 `{"items": [...], "page_num": 1, "page_size": 20, "has_more": false}`，只包含本人任务。

删除：返回 200 `{}`。运行中的任务被取消，已完成的任务被删除，两种情况都不退款。

## 回调

设置了 `callback_url` 时，任务进入终态后 POST 一次，请求体就是查询响应体，不含签名内容链接。请求头带 `X-SUB2API-Event-ID`、`X-SUB2API-Timestamp`、`X-SUB2API-Content-SHA256`、`X-SUB2API-Signature` 四个头，签名为 HMAC，密钥向管理员索取。投递失败会重试，默认持续 24 小时。回调只是通知，收到后仍应查询任务获取内容链接。

## 计费

创建即按模型、分辨率和时长档位冻结预估费用，成功后按实际时长结算，失败、取消或过期释放冻结。智能时长按模型最长档位预扣。删除任务不退款。上游创建失败不会自动重试，避免重复创建付费任务。详见[计费与用量](/docs/billing-usage)。

## 常见错误

错误响应为 `{"error": {"message": "...", "type": "...", "param": null, "code": "..."}}`，`code` 为小写。

| 状态码 | `code` | 原因 |
| --- | --- | --- |
| 400 | `invalid_content` | `content` 为空或没有 `text` 项。 |
| 400 | `invalid_role` | 媒体项的 `role` 与其类型不匹配。 |
| 400 | `duration_frames_conflict` | 同时给了 `duration` 和 `frames`。 |
| 400 | `adaptive_ratio_requires_first_frame` | `ratio` 为 `adaptive` 但没有首帧。 |
| 400 | `unsupported_parameter` | 上游做不到的参数取值，`message` 里有具体字段。 |
| 400 | `video_capability_unsupported` | 超出模型能力，例如 `seconds 13`。 |
| 400 | `video_pricing_missing` | 分组未配置该模型的视频价格。 |
| 402 | `video_insufficient_balance` | 余额不足以冻结预估费用。 |
| 403 | `video_ark_platform_unsupported` | 分组平台不是五类视频平台。 |
| 403 | `video_creation_disabled` | 管理员关闭了任务创建。 |
| 404 | `video_task_not_found` | 任务不存在，或不属于当前密钥。 |
| 404 | `use_contents_generations_tasks` | 视频分组误用了 `/v1/videos`。 |
| 409 | `video_idempotency_conflict` | `Idempotency-Key` 被用于不同的请求。 |
| 409 | `video_content_not_ready` | 任务未完成就访问了内容链接。 |
| 410 | `video_content_expired` | 内容链接过期，重新查询任务。 |
| 413 | `video_input_too_large` | 请求体或内联图片超限。 |
| 429 | `video_account_concurrency_limited` | 上游账号并发已满，稍后重试。 |
| 502 | `video_no_account_available` | 没有可用的上游账号。 |

其他排查思路见[错误排查](/docs/errors)。


## Grok 托管视频

管理员启用 Grok 托管模式后，新任务与 ByteDance 使用相同的资金和交付流程：创建时冻结预估费用，后台持续同步状态，成功扣款后交付视频，明确失败时释放冻结。关闭客户端不会中止后台处理。升级前的旧任务继续使用原有结算流程。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/v1/videos/generations` | 生成视频 |
| POST | `/v1/videos/edits` | 编辑已验证的源视频 |
| POST | `/v1/videos/extensions` | 延长已验证的源视频 |
| GET | `/v1/videos/{request_id}` | 查询任务 |
| GET | `/v1/videos/{request_id}/content` | 使用 API Key 下载已结算的视频 |

```bash
# 每个业务任务生成一次键；重试必须复用相同键和请求内容。
REQUEST_KEY="$(uuidgen)"
curl "{{BASE_URL}}v1/videos/generations" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: ${REQUEST_KEY}" \
  -d '{
    "model": "grok-imagine-video-1.5",
    "prompt": "公园树荫下，一位成年人把木质挂钟轻轻放在桌上。",
    "duration": 8,
    "aspect_ratio": "9:16",
    "resolution": "480p",
    "generate_audio": true
  }'
# 返回 {"request_id":"<UUID>"}
```

托管模式返回的 UUID 标识本网关任务，不是供应商任务 ID。查询与鉴权下载需要使用创建任务的 API Key，且分组保持一致。`pending` 包括生成中和结算处理中；只有结算完成后才返回 `done` 和 `video.url`。请按返回的 `Retry-After` 轮询。`video.url` 是有期限的签名下载链接，应按敏感链接保管；过期后重新查询获取。

参考图片使用 `reference_images: [{"url":"https://..."}]`；不要将 Ark 的 `content`、`ratio` 等字段直接套用到 Grok。参考素材数量、音频与关键帧组合以当前模型能力为准。`usage.cost_in_usd_ticks` 是供应商成本信息，客户扣费以平台账单为准。

编辑与延长使用 `video: {"url":"https://.../source.mp4"}`，也可用 `video: {"id":"<已结算的 Grok request_id>"}` 引用同一密钥下的托管任务。源视频会被冻结并测量实际时长。编辑按验证后的源时长计费；延长请求的 `duration` 表示新增秒数，按新增部分计费。编辑不传 `duration`；编辑、延长不传 `resolution`、`aspect_ratio` 或 `generate_audio`。当前适配的派生操作使用 `grok-imagine-video`，具体能力受管理员配置限制。

创建结果不确定时，复用原幂等键查询或重试，不要更换键再次付费生成。Grok 暂不提供取消或删除上游任务的操作。托管模式不支持订阅分组；simple mode 下也会冻结和结算余额，切换前由管理员核对价格与余额规则。

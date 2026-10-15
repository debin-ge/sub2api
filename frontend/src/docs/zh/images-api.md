# 图片生成 API

本页说明通过 {{SITE_NAME}} 生成和编辑图片的方式。接口与 OpenAI Images API 兼容，OpenAI SDK 把 Base URL 改成 `{{BASE_URL}}v1` 后可以直接调用。

> [!NOTE]
> Grok（xAI）分组的图片与视频走 xAI 媒体路径，参数以管理员提供的说明为准，本页不展开。

## 前置条件

| 条件 | 不满足时 |
| --- | --- |
| API Key 所属分组平台为 `openai` | 404 `Images API is not supported for this platform` |
| 分组已开启「允许生图」 | 403 `Image generation is not enabled for this group` |
| 分组开放了图片模型并配置了价格 | 400 或 403，提示模型或价格不可用 |

模型名以 `/v1/models` 返回或管理员提供的映射名为准。不传 `model` 时默认 `gpt-image-2`。

## 端点

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| POST | `/v1/images/generations` | 文生图 |
| POST | `/v1/images/edits` | 图生图、局部重绘 |
| POST | `/v1/images/generations/async`、`/v1/images/edits/async` | 异步提交，见下文 |
| GET | `/v1/images/tasks/{task_id}` | 查询异步任务 |

去掉 `/v1` 前缀的同名路径同样可用。

## 图片生成

```bash tab=curl group=images-generate
curl "{{BASE_URL}}v1/images/generations" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "冬夜暴风雪中的灯塔，电影感",
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
    prompt="冬夜暴风雪中的灯塔，电影感",
    size="1536x1024",
)
with open("lighthouse.png", "wb") as f:
    f.write(base64.b64decode(result.data[0].b64_json))
```

请求字段：

| 字段 | 说明 |
| --- | --- |
| `model` | 可选，默认 `gpt-image-2`。任意非空模型名都会被接受，能否成功取决于分组的模型与价格配置。 |
| `prompt` | 必填。 |
| `n` | 可选，默认 1。上限由部署配置，默认 10；`n` 大于 1 时会先按单价乘以张数预检余额。 |
| `size` | 可选。接受 `auto`、`1k`、`2k`、`4k` 或 `宽x高`，长边不超过 4096 像素，超过直接 400。 |
| `quality`、`background`、`output_format`、`output_compression`、`moderation`、`input_fidelity`、`style`、`partial_images` | 可选，按 OpenAI 原生语义透传给上游。 |
| `response_format` | `b64_json`（默认）或 `url`。 |
| `stream` | 可选。为 `true` 时以 SSE 返回部分图片事件。 |

> [!TIP]
> 需要落盘时优先用 `b64_json`。经 ChatGPT 账号通道生成时，`response_format` 为 `url` 返回的是 `data:` 形式的内联 URL，不是托管链接。

请求体里 `model`、`size`、`n` 不能重复出现，字段类型错误会返回 400。

## 图片编辑

支持 JSON 与 multipart 两种写法。

```bash tab=curl group=images-edit
curl "{{BASE_URL}}v1/images/edits" \
  -H "Authorization: Bearer ${API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-image-2",
    "prompt": "把天空换成黄昏",
    "images": [{ "image_url": "https://example.com/input.png" }],
    "mask": { "image_url": "https://example.com/mask.png" }
  }'
```

```bash tab=curl-multipart group=images-edit
curl "{{BASE_URL}}v1/images/edits" \
  -H "Authorization: Bearer ${API_KEY}" \
  -F model="gpt-image-2" \
  -F prompt="把天空换成黄昏" \
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
        prompt="把天空换成黄昏",
        image=image,
        mask=mask,
    )
with open("edited.png", "wb") as f:
    f.write(base64.b64decode(result.data[0].b64_json))
```

- JSON 写法：`images[].image_url` 必填，接受 `https://` 或 `data:` URL；`mask.image_url` 可选；不支持 `file_id`。
- multipart 写法：文件字段为 `image`（可多个）与可选的 `mask`，单个文件不超过 20MB；`model`、`size`、`n` 作为文本字段且不能重复。
- 其余字段与图片生成相同。

## 响应

```json
{
  "created": 1784092923,
  "data": [
    { "b64_json": "...", "revised_prompt": "..." }
  ],
  "usage": { "input_tokens": 0, "output_tokens": 0, "total_tokens": 0 }
}
```

`usage` 是否出现取决于上游是否回报 token 用量。

> [!IMPORTANT]
> 非流式请求等待较久时，响应开头可能先收到若干空白字符作为心跳，JSON 解析不受影响。心跳开始后状态码固定为 200，之后的上游错误以 JSON 错误体返回。客户端要检查响应里的 `error` 字段，不能只看状态码。

## 异步接口

> [!NOTE]
> 异步接口依赖管理员启用的对象存储。未启用时提交返回 404 `async image tasks are not enabled`，同步接口不受影响。

- 提交：把同样的请求体 POST 到 `/v1/images/generations/async` 或 `/v1/images/edits/async`，返回 202。响应头带 `Location` 与 `Retry-After: 3`，响应体含 `id`、`status`、`poll_url`、`expires_at`。不接受 `stream: true`。
- 查询：`GET /v1/images/tasks/{task_id}`，只能用提交时的密钥。`status` 为 `processing`、`completed` 或 `failed`；完成后 `image_url` 是存储链接，`result` 是完整的同步响应。
- 限制：任务 24 小时后过期；每个用户最多 20 个待处理任务，超出返回 429，按 `Retry-After` 重试。

## 计费

图片按 token 计价优先，上游没有回报图片 token 时按张计价，尺寸档位按长边分为 1K、2K、4K。未指定 `size` 或为 `auto` 时按 2K 计价。详见[计费与用量](/docs/billing-usage)。

## 常见错误

| 状态码 | 信息 | 原因 |
| --- | --- | --- |
| 400 | `n must be between 1 and 10` | `n` 超出部署上限。 |
| 400 | `unsupported image size` | 长边超过 4096 像素，或 `size` 格式无法识别。 |
| 400 | `invalid ... field type` | 字段类型错误，或 `model`、`size`、`n` 重复出现。 |
| 403 | `Image generation is not enabled for this group` | 分组未开启生图权限。 |
| 404 | `Images API is not supported for this platform` | 分组平台不是 `openai`。 |
| 404 | `async image tasks are not enabled` | 部署未启用异步图片任务。 |
| 429 | 异步待处理任务达到上限 | 等待 `Retry-After` 后重试。 |

其他排查思路见[错误排查](/docs/errors)。

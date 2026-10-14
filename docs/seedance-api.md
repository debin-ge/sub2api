# 视频任务 API（Ark / Seedance 协议）

ByteDance（火山方舟 Ark）、fal、WaveSpeed、Runware、Flova 五个托管视频平台统一以火山方舟的
异步视频任务协议对外提供服务。请求体、响应体与状态词表与 Ark 一致，Ark 的 SDK 只需把
Base URL 改到本网关即可；OpenAI（Sora）与 Grok 仍走 `/v1/videos`，与本文无关。

## 配置

1. 创建对应平台（ByteDance / fal / WaveSpeed / Runware / Flova）的 **API Key** 账号。ByteDance 账号默认
   直连 Ark（`https://ark.cn-beijing.volces.com/api/v3`），`protocol_mode = openai_compatible` 的中继账号仍受支持。
2. 把账号加入同平台的分组，或加入合成分组并配置模型路由（endpoint 取 `videos`）。
3. 为模型配置 `billing_mode = video` 的价目：条件维度是 `resolutions / seconds / generate_audio` 等；
   `sizes` 条件只对 OpenAI 有意义。智能时长（`duration = -1`）按模型最长档位预扣，按上游回报的实际时长结算。
4. 旧的「OpenAI 账号 + Seedance (Ark) 能力」透传入口已退役；原来这样配置的账号请改建为 ByteDance 平台账号。

## 路径

`{prefix}` 可取 `/api/v3`、`/v3`、`/v1` 或留空。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `{prefix}/contents/generations/tasks` | 创建任务，应答 `{"id"}` |
| GET | `{prefix}/contents/generations/tasks/{id}` | 查询任务 |
| DELETE | `{prefix}/contents/generations/tasks/{id}` | 取消运行中任务 / 删除已完成任务，应答 `{}` |
| GET | `{prefix}/contents/generations/tasks?page_num=&page_size=&filter.status=&filter.model=` | 只列本用户的任务 |
| GET | `{prefix}/contents/generations/tasks/{id}/content?variant=video&exp=&sig=` | 签名内容链接（由查询响应下发，不需要 API Key） |
| GET | `{prefix}/contents/generations/models` | sub2api 扩展：Ark 维度的能力目录 |

## 调用

```bash
curl "$SUB2API_BASE_URL/api/v3/contents/generations/tasks" \
  -H "Authorization: Bearer $SUB2API_KEY" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "model": "doubao-seedance-1-0-pro-250528",
    "content": [
      {"type": "text", "text": "海浪轻轻拍打沙滩"},
      {"type": "image_url", "image_url": {"url": "https://example.com/first.png"}, "role": "first_frame"}
    ],
    "duration": 5,
    "resolution": "720p",
    "ratio": "adaptive",
    "generate_audio": true
  }'

curl "$SUB2API_BASE_URL/api/v3/contents/generations/tasks/$TASK_ID" -H "Authorization: Bearer $SUB2API_KEY"
# 轮询直到 status 为 succeeded / failed / cancelled / expired；succeeded 时 content.video_url 是签名直链。
```

### 请求字段

- `model`（必填）、`content[]`（必填，至少一个 `text` 项）。媒体项：`image_url`（role `first_frame` 默认 / `last_frame` / `reference_image`）、
  `video_url`（`reference_video`）、`audio_url`（`reference_audio`）。图片可用 `https://` 或 `data:image/...;base64,` 内联（单张 ≤ 8MB，请求体默认 ≤ 16MB）；视频 / 音频只接受 `https://`。
- `duration`（整数秒；`-1` 为智能时长）与 `frames`（25+4n，[29, 289]）二选一。内置能力表按 Ark 文档约束 Seedance 1.0 系列：时长 2 到 12 秒，不支持 `-1` 与 `frames`；能力表不认识的模型（新版本、`ep-*` 接入点）由上游自行裁决，可在运行时能力目录里补充声明。
- `resolution`：480p / 720p / 1080p / 2k / 4k；`ratio`：16:9 / 9:16 / 4:3 / 3:4 / 1:1 / 21:9 / adaptive（adaptive 需要 first_frame）。
- `seed`、`camera_fixed`、`watermark`、`generate_audio`、`return_last_frame`、`service_tier`、`execution_expires_after`（[3600, 259200] 秒）、`callback_url`。
- 弱校验写法同样生效：`"text": "海浪 --rs 720p --rt 16:9 --dur 5 --seed 11 --cf false --wm true"`，flag 覆盖顶层字段，预扣按解析后的值。
- `provider_options`：sub2api 扩展，承载厂商特有参数（见能力目录的 `provider_options`）；Ark 直连账号不接受。
- ByteDance 的 `openai_compatible` 中继账号：`generate_audio / seed / camera_fixed / watermark` 按 Ark 字段名原样转发给中继，是否生效取决于中继本身；`return_last_frame` 按空操作接受（中继响应没有尾帧链接），`service_tier=flex` 拒绝。
- 非 Ark 直连的上游（fal / WaveSpeed / Runware / Flova、ByteDance 中继账号）对 Ark 开关的处理分两类：
  - 取值等于该上游固有行为的「中性值」是空操作，会被接受并在查询响应的 `ignored_parameters` 里列出：`watermark=false`、`camera_fixed=false`（模型无此字段时）、`generate_audio=false`（模型无音频字段时）、`seed=-1`（模型无 seed 字段时）、`service_tier="default"`。`return_last_frame` 无论取值都按空操作接受（它只决定响应里是否附带 `last_frame_url`，不影响生成与计费），上游给不出尾帧时响应里没有该字段。Ark 原生客户端带默认值提交时不会再被拒。
  - 真正改变产出或计费而上游做不到的事一律 400（错误码 `unsupported_parameter`），不会静默丢弃：`watermark=true`、`service_tier="flex"`、`duration=-1`、`frames`，以及模型档案没有映射的 `seed>=0`、`camera_fixed=true`、`generate_audio=true`。fal / WaveSpeed / Runware 的具体支持见能力目录的 `flags`；Flova 不支持任何开关，且参考图必须以 `data:` 内联。
- `execution_expires_after` 对所有上游生效：Ark 直连账号原样透传由 Ark 执行；其他上游由本网关执行，任务创建后到时限仍未开始执行即取消并置 `expired`、释放预扣。
- 参考媒体角色：ByteDance 直连接受全部角色；WaveSpeed 的 Seedance 2.5 接受 `reference_image`（≤30，可 `data:` 内联）、`reference_video`（≤10）、`reference_audio`（≤10），均需 https 引用；其他聚合平台模型只接受首尾帧，见能力目录的 `content_roles`。
- `Idempotency-Key` 头是 sub2api 扩展：同 key 同请求重放原任务，同 key 不同请求 409。创建开关关闭时该头必填。
- 校验类错误（`video_capability_unsupported`、`video_input_unsupported`、`video_invalid_request`、`unsupported_parameter` 等）的 `message` 带具体原因，例如 `video capability is not supported: seconds 13`。

### 响应

查询响应与 Ark 一致：`id / model / status / content{video_url,last_frame_url} / usage{completion_tokens,total_tokens} / created_at / updated_at /
seed / resolution / ratio / duration / frames / framespersecond / generate_audio / service_tier / error{code,message}`。
`id` 是本网关的任务 ID（`video_` 前缀），不暴露上游 ID。`usage` 只在上游真实回报 token 时出现（Ark 直连）。
按分组披露策略追加 `prompt / actual_cost / currency / provider / provider_access`；被当作空操作接受的开关列在 `ignored_parameters`。

状态：`queued`（含本地预占与提交中）、`running`（含生成完成但尚未结算）、`succeeded`（已结算）、`failed`、`cancelled`、`expired`。

## 任务与计费

- 创建即预扣（按模型 / 分辨率 / 时长档位报价），成功后按实际时长结算，失败 / 取消 / 过期释放预占；智能时长按模型最长档位预扣。
- 查询、删除、内容下载只能访问本用户的任务；始终使用原提交账号。不开放上游任务列表，列表接口只返回本用户任务。
- `callback_url` 由本网关在终态投递一次，payload 即查询响应体（不含签名内容链接），带 HMAC 签名头；只保留失败重试。
- 内容链接带签名与过期时间，上游原始 URL 不暴露；`return_last_frame=true` 时另有 `variant=last_frame`。
- 异步创建的上游错误不自动重试，以免重复创建付费任务。

协议依据：[火山官方 Go SDK](https://github.com/volcengine/volcengine-go-sdk/blob/master/service/arkruntime/model/content_generation.go)、[创建任务文档](https://docs.volcengine.com/docs/ark/create-video-generation-task-api)、[查询任务文档](https://www.volcengine.com/docs/82379/1521309)。

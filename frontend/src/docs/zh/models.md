# 模型与平台

Sub2API 可以通过不同渠道连接多个模型平台。前端或客户端不应假设某个部署一定支持全部平台、全部模型或全部端点。实际可用模型以当前 API Key 调用 `/v1/models`、`/v1beta/models` 或管理员说明为准。

模型可用性通常由以下因素共同决定：

| 因素 | 影响 |
| --- | --- |
| API Key 分组 | 控制用户能访问哪些渠道、模型和价格策略。 |
| 上游账号 | 上游账号是否有效、是否有额度、是否被限制。 |
| 模型映射 | 管理员可能把请求模型名映射到不同上游模型。 |
| 渠道价格 | 管理员可能按价格、倍率或成本策略开放模型。 |
| 管理员配置 | 路由、优先级、禁用项、并发和速率限制都会影响可用性。 |

## 平台矩阵

| 平台 | 常见接口族 | 说明 |
| --- | --- | --- |
| `openai` | `/v1/chat/completions`、`/v1/responses`、`/v1/embeddings`、图片接口 | 适合 OpenAI SDK 和兼容生态。具体模型取决于渠道与映射。 |
| `anthropic` | `/v1/messages`、`/v1/messages/count_tokens` | 适合 Claude Code、Anthropic SDK 或 Messages 兼容客户端。 |
| `gemini` | `/v1beta/models/*` | 适合 Gemini 原生请求体和 SSE 流式生成。 |
| `antigravity` | `/antigravity/*` | 面向 Antigravity 兼容路径，可覆盖 Claude 与 Gemini 风格接口。 |
| `minimax` | 通常经兼容层或模型映射访问 | 是否开放取决于对应渠道配置。 |
| `glm` | 通常经兼容层或模型映射访问 | 适合接入智谱 GLM 相关模型时统一管理。 |
| `kimi` | 通常经兼容层或模型映射访问 | 具体模型名和上下文能力以部署配置为准。 |
| `deepseek` | 通常经 OpenAI 兼容层或模型映射访问 | 常用于 Chat Completions 兼容客户端。 |
| `windsurf` | 通常面向特定客户端或兼容入口 | 需要管理员配置对应渠道、模型和鉴权。 |
| `opencode` | 通常经 `/v1/responses` 或 OpenAI 兼容入口访问 | 适合支持 Responses API 或 OpenAI 风格配置的编码客户端。 |

## 如何确认模型可用

推荐先运行：

```bash
curl "$BASE_URL/v1/models" \
  -H "Authorization: Bearer $SUB2API_KEY"
```

如果你使用 Gemini 原生客户端，也可以检查：

```bash
curl "$BASE_URL/v1beta/models" \
  -H "Authorization: Bearer $SUB2API_KEY"
```

不要把文档中的示例模型名当成保证可用的模型。示例只用于说明请求格式。生产配置中应使用接口返回的模型名，或管理员明确提供的模型映射名。

## 模型不可用时

| 现象 | 可能原因 |
| --- | --- |
| `/v1/models` 不返回预期模型 | 当前 API Key 分组未开放、模型被禁用、渠道不可用或映射未配置。 |
| 请求返回 404 | 路径未启用、模型名不存在，或兼容端点不匹配。 |
| 请求返回 403 | 密钥有效但当前分组没有访问权限。 |
| 请求返回上游错误 | 上游账号额度、风控、区域限制、模型下线或参数不被支持。 |
| 流式请求中断 | 网络、代理、上游限流、客户端超时或 SSE 解析问题。 |

如需固定某个模型给业务使用，请让管理员确认模型映射、渠道优先级、价格和备用渠道，而不是只依赖客户端侧硬编码。


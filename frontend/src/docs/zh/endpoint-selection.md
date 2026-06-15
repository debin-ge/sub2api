# 端点选择指南

选择端点时，先看你的客户端和请求格式，再看模型。不同接口族的请求体不兼容，模型名相似也不能直接混用。

## 快速选择表

| 你的需求 | 推荐端点 | 请求体格式 |
| --- | --- | --- |
| 查询当前密钥可用模型 | `GET /v1/models` | 无请求体。 |
| OpenAI Chat Completions 客户端 | `POST /v1/chat/completions` | `messages` 数组。 |
| OpenAI Responses 或 Codex 类客户端 | `POST /v1/responses` | `input` 或 Responses 输入结构。 |
| Anthropic Messages 或 Claude Code | `POST /v1/messages` | `messages`、`max_tokens`。 |
| Anthropic token 估算 | `POST /v1/messages/count_tokens` | Messages 请求结构。 |
| Gemini 原生非流式 | `POST /v1beta/models/{model}:generateContent` | `contents`、`parts`。 |
| Gemini 原生流式 | `POST /v1beta/models/{model}:streamGenerateContent?alt=sse` | `contents`、`parts`，SSE 返回。 |
| Embedding | `POST /v1/embeddings` | `input` 和 embedding 模型。 |
| 图片生成 | `POST /v1/images/generations` | 图片生成参数。 |
| Antigravity 兼容客户端 | `/antigravity/...` | 以管理员提供的客户端配置为准。 |

## 选择流程

1. 确认客户端类型：OpenAI SDK、Anthropic/Claude、Gemini 原生、Codex/Responses 或其他兼容客户端。
2. 使用对应接口族的 Base URL 和请求体。
3. 用当前 API Key 调用模型列表接口，确认模型名。
4. 发起一个最小请求验证端点、模型和权限。
5. 再加入业务参数、流式输出、工具调用或多轮上下文。

## Base URL 写法

通用命令行示例使用根地址：

```bash
export BASE_URL="https://tiktoken.net/"
export YOUR_KEY="replace-with-your-api-key"
```

curl 拼接路径时使用：

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

SDK 通常要求更具体的地址：

| 客户端 | 常见 Base URL |
| --- | --- |
| OpenAI SDK | `https://tiktoken.net/v1` |
| Anthropic 兼容客户端 | `https://tiktoken.net/` 或客户端要求的 Anthropic Base URL。 |
| Gemini 原生客户端 | `https://tiktoken.net/`，请求路径包含 `/v1beta`。 |
| Antigravity 客户端 | `https://tiktoken.net/antigravity` |

如果出现 `/v1/v1`、`//v1` 或缺少 `/v1beta` 的路径，通常是 Base URL 和客户端自动拼接规则不匹配。

## OpenAI 兼容还是 Responses

优先按客户端能力选择：

| 选择 | 适合场景 |
| --- | --- |
| `/v1/chat/completions` | 传统聊天、函数调用兼容、生态支持最广。 |
| `/v1/responses` | 支持 Responses API 的编码助手、结构化输入或新客户端。 |
| `/v1/embeddings` | 文本向量、检索、相似度计算。 |
| 图片端点 | 图片生成、编辑或多模态工作流。 |

如果你的客户端只支持 Chat Completions，就不要为了使用某个模型强行改成 Responses；应先确认部署是否为该模型配置了 Chat Completions 兼容路径。

## Anthropic 与 Gemini 的区别

Anthropic Messages 示例：

```json
{
  "model": "claude-3-5-sonnet-latest",
  "max_tokens": 256,
  "messages": [
    { "role": "user", "content": "你好" }
  ]
}
```

Gemini 原生示例：

```json
{
  "contents": [
    {
      "parts": [
        { "text": "你好" }
      ]
    }
  ]
}
```

两者请求体字段不同。即使模型名通过映射可以访问，也需要使用当前端点对应的请求格式。

## 何时选择 Antigravity

只有在管理员提供 Antigravity 兼容入口，或你的客户端明确要求 Antigravity 路径时，才选择 `/antigravity/...`。常见检查项：

1. 当前部署是否启用 Antigravity 路由。
2. API Key 所属分组是否开放相关模型。
3. 客户端 Base URL 是否指向 `https://tiktoken.net/antigravity`。
4. 请求路径是否与客户端期望一致。

## 端点选择自检

| 问题 | 应该确认 |
| --- | --- |
| 返回 401 | 请求头是否包含 `Authorization: Bearer $YOUR_KEY`。 |
| 返回 403 | 当前密钥分组是否有模型和端点权限。 |
| 返回 404 | 路径是否存在，Base URL 是否重复拼接，模型名是否正确。 |
| 返回模型格式错误 | 请求体是否属于对应接口族。 |
| 流式不输出 | 客户端、代理和端点是否都支持 SSE 或流式读取。 |

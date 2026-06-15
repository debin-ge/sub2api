# 快速开始

本页演示如何用 Sub2API 完成第一次请求。示例使用占位地址和密钥，请替换为你的部署信息。

```bash
export BASE_URL="https://tiktoken.net/"
export YOUR_KEY="your-api-key"
```

## 1. 确认 Base URL

Base URL 是你的 Sub2API 部署地址，例如：

```text
https://tiktoken.net/
```

后续示例统一使用 `$BASE_URL`。如果你的部署带有反向代理路径，请以管理员提供的完整地址为准。

## 2. 设置 API Key

Sub2API API Key 通常通过 HTTP Bearer 认证传递：

```http
Authorization: Bearer $YOUR_KEY
```

命令行示例统一使用 `$YOUR_KEY`：

```bash
export YOUR_KEY="your-api-key"
```

请不要在前端公开代码、日志或公共仓库中暴露真实密钥。

## 3. 查询可用模型

先查询 `/v1/models`，确认密钥有效，并查看当前分组实际可用模型。

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

返回结果会随部署配置变化。模型列表取决于 API Key 所属分组、上游账号、模型映射、渠道价格和管理员配置。

## 4. 第一次 Chat Completions 请求

如果你的客户端使用 OpenAI Chat Completions 格式，可以调用 `/v1/chat/completions`：

```bash
curl "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "用一句话介绍 Sub2API。" }
    ]
  }'
```

如果示例模型不可用，请改用 `/v1/models` 返回的模型名。

## 5. 第一次 Messages 请求

如果你的客户端使用 Anthropic Messages 格式，可以调用 `/v1/messages`：

```bash
curl "${BASE_URL}v1/messages" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 256,
    "messages": [
      { "role": "user", "content": "用一句话介绍 Sub2API。" }
    ]
  }'
```

如果部署未为你的分组启用 Anthropic 兼容端点或该模型映射，请联系管理员调整配置，或选择 `/v1/models` 中可用的模型。

## 6. 常见检查项

| 检查项 | 说明 |
| --- | --- |
| Base URL | 确认没有多余路径、空格或结尾拼写错误。 |
| API Key | 确认请求头为 `Authorization: Bearer $YOUR_KEY`。 |
| 模型名 | 优先使用 `/v1/models` 返回的名称。 |
| 端点格式 | OpenAI、Anthropic、Gemini 等接口请求体格式不同，不能直接混用。 |
| 权限配置 | 403、404 或模型不可用通常与分组、渠道或模型映射有关。 |

## 7. 下一步

| 下一步 | 文档 |
| --- | --- |
| 需要了解密钥权限和安全 | API Key 与账户。 |
| 要接入具体 SDK 或命令行工具 | 客户端接入。 |
| 想直接复制完整配置 | 可复制配置模板。 |
| 不确定模型和端点 | 模型与平台、端点选择指南。 |
| 请求失败 | 错误排查。 |

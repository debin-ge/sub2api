# 快速开始

本页演示如何使用 {{SITE_NAME}} 完成第一次请求。示例使用占位地址和密钥，请替换为您的部署信息。

> [!TIP]
> 请确保您已向管理员索取，或在平台控制台生成了您的 **Base URL** 和 **API Key**。

---

## 1. 确认 Base URL

Base URL 是您的 {{SITE_NAME}} 部署节点的网络访问地址，例如：

```text
{{BASE_URL}}
```

后续示例统一使用 `$BASE_URL`。如果您的部署使用了特殊的反向代理或网关子路径，请以管理员提供的完整地址为准。

---

## 2. 设置 API Key

{{SITE_NAME}} API Key 通常通过 HTTP Bearer 认证头传递：

```http
Authorization: Bearer $YOUR_KEY
```

命令行示例统一使用环境变量 `$YOUR_KEY`。在终端中执行以下命令以设置：

```bash
export YOUR_KEY="your-api-key"
```

> [!WARNING]
> **安全警示**：请不要在前端客户端源码、控制台输出日志或公共 Git 仓库中暴露您的真实 API 密钥，否则可能会导致账户额度损失。

---

## 3. 查询可用模型

在配置复杂的 SDK 或开发工具前，建议先查询 `/v1/models` 端点，以验证密钥是否有效，并了解当前 API Key 所属的分组实际被授权访问哪些模型。

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

> [!NOTE]
> 返回的模型列表会因部署配置的不同而变化。它取决于 API Key 的分组配置、上游账号状态、模型映射别名以及管理员所允许的渠道。

---

## 4. 第一次 Chat Completions 请求

如果您的客户端或 SDK 支持标准的 OpenAI Chat Completions 协议，可以直接调用 `/v1/chat/completions`：

```bash
curl "${BASE_URL}v1/chat/completions" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [
      { "role": "user", "content": "用一句话介绍 {{SITE_NAME}}。" }
    ]
  }'
```

> [!TIP]
> 如果您遇到模型不存在的报错，请改用前文第三步 `/v1/models` 返回的实际模型名。

---

## 5. 第一次 Messages 请求

如果您的客户端或 SDK 使用 Anthropic Messages 格式，可以调用 `/v1/messages` 兼容端点：

```bash
curl "${BASE_URL}v1/messages" \
  -H "Authorization: Bearer $YOUR_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-latest",
    "max_tokens": 256,
    "messages": [
      { "role": "user", "content": "用一句话介绍 {{SITE_NAME}}。" }
    ]
  }'
```

> [!IMPORTANT]
> 如果您的分组未被授权使用 Anthropic 兼容端点，或者该模型没有被正确映射，接口可能会返回 403 或 404。请联系管理员进行授权，或更换为 `/v1/models` 中列出的其他兼容模型。

---

## 6. 常见检查项

| 检查项 | 说明及排查建议 |
| :--- | :--- |
| **Base URL** | 确认没有多余的斜杠、空格或多级拼接错误。 |
| **API Key** | 确认传入的 Header 格式为 `Authorization: Bearer $YOUR_KEY`。 |
| **模型名** | 优先并且只使用 `/v1/models` 接口返回的官方名或别名。 |
| **端点格式** | 各家厂商（OpenAI、Anthropic、Gemini 等）的请求体负载格式不同，不可混用。 |
| **权限配置** | 若出现 403 或 404 错误，通常代表当前 API Key 所在的分组未配置该渠道或模型映射。 |

---

## 7. 下一步

| 目标场景 | 推荐阅读文档 |
| :--- | :--- |
| **需要深入了解密钥权限和安全性** | [API Key 与账户](api-keys) |
| **需要接入具体的编辑器/命令行客户端** | [客户端接入](clients) |
| **需要直接复制完整的配置代码片段** | [可复制配置模板](configuration-snippets) |
| **不确定选用哪个接口或模型** | [端点选择指南](endpoint-selection) |
| **遭遇接口请求失败或响应异常** | [错误排查](errors) |

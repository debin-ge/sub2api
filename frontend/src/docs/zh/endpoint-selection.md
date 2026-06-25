# 端点选择指南

在选择 API 端点时，应首先明确您的客户端类型和期望的请求格式，然后再根据可用模型进行匹配。**不同接口族的请求体格式完全不兼容。**

---

## 快速选择指南

| 您的集成需求 | 推荐调用的 API 端点 | 期望的请求负载格式 |
| :--- | :--- | :--- |
| **查询可用模型列表** | `GET /v1/models` | 无请求体 (No Body) |
| **标准 OpenAI 兼容客户端/SDK** | `POST /v1/chat/completions` | `messages` 对象数组 |
| **OpenAI Responses 兼容/Codex CLI** | `POST /v1/responses` | `input` 字符串或 Responses 结构 |
| **Claude Code 或 Anthropic 兼容工具** | `POST /v1/messages` | `messages` 数组与 `max_tokens` |
| **Anthropic 专用 token 估算** | `POST /v1/messages/count_tokens` | Messages 标准请求结构 |
| **Gemini 原生非流式调用** | `POST /v1beta/models/{model}:generateContent` | 原生 `contents` 和 `parts` |
| **Gemini 原生流式调用** | `POST /v1beta/models/{model}:streamGenerateContent?alt=sse` | `contents` 与 `parts`，SSE 返回 |
| **文本向量化存储/检索** | `POST /v1/embeddings` | `input` 文本和 embedding 模型名 |
| **图像生成/绘画** | `POST /v1/images/generations` | 图片尺寸、提示词与数量等参数 |
| **Antigravity 兼容集成** | `/antigravity/...` | 依据管理员提供的独立指南 |

---

## 接入决策流程

1. **确认客户端框架**：判断您当前使用的底层组件（如 OpenAI SDK、Anthropic SDK、Gemini SDK、Codex 或其他开源客户端）。
2. **设定 Base URL**：将对应的中转地址和格式参数填入客户端配置。
3. **确认模型名称**：调用模型列表接口，获取当前可用的官方模型名或别名。
4. **验证最小可行性**：先发起一个不带复杂参数的最小请求，验证端点连通性。
5. **添加高级配置**：连通性正常后，再逐步开启流式输出、函数调用 (Tool Call) 或多轮历史对话。

---

## Base URL 拼装规律

通用的命令行脚本中，我们通常将根地址定义为：

```bash
export BASE_URL="{{BASE_URL}}"
export YOUR_KEY="replace-with-your-api-key"
```

这使得 `curl` 请求可以通过简单追加路径完成拼装：

```bash
curl "${BASE_URL}v1/models" \
  -H "Authorization: Bearer $YOUR_KEY"
```

然而，不同的官方客户端 SDK 通常需要指向更具体的子路径：

| 接入客户端类型 | 推荐填入的 Base URL 格式 |
| :--- | :--- |
| **OpenAI SDK (JS/TS/Python)** | `{{BASE_URL}}v1` |
| **Anthropic SDK / Claude** | `{{BASE_URL}}` (有些客户端在请求时内部会自己追加路径) |
| **Gemini 原生 SDK** | `{{BASE_URL}}` (请求路径会由 SDK 追加 `/v1beta`) |
| **Antigravity 客户端** | `{{BASE_URL}}antigravity` |

> [!WARNING]
> **拼写检查**：如果您在请求中遇到 `/v1/v1`、`//v1` 重复拼接，或是路径丢失等 `404 Not Found` 错误，通常代表您在 SDK 初始化中多写或少写了结尾的斜杠 `/` 或子路径。

---

## OpenAI Chat 还是 Responses

建议根据您所使用的 Coding 辅助客户端特性做出决策：

| 接口端点类型 | 适合场景 | 接入优势 |
| :--- | :--- | :--- |
| `/v1/chat/completions` | 传统问答、通用 Agent、函数调用 (Tool Use) | 行业生态支持最广，调试简单。 |
| `/v1/responses` | 支持 OpenAI Responses 协议的高级编码助手 | 支持系统底层的 Agent 工具交互与结构化指令。 |
| `/v1/embeddings` | 向量数据库构建、语义搜索、智能客服知识库 | 专用向量生成，检索速度极快。 |
| **图像端点** | 多模态内容创作、图像局部编辑 | 提供直接的 DALL-E 兼容输出。 |

> [!IMPORTANT]
> 如果您的客户端目前只支持标准 Chat Completions，**请不要为了使用某个特定的推理模型而强行在代码中组装 Responses 格式的 payload**。您应该先在控制台或联系管理员确认该模型是否已在后台配置了 Chat Completions 的中转路由。

---

## Anthropic 与 Gemini 请求格式差异

#### Anthropic Messages 请求示例
```json
{
  "model": "claude-3-5-sonnet-latest",
  "max_tokens": 256,
  "messages": [
    { "role": "user", "content": "你好" }
  ]
}
```

#### Gemini 原生请求示例
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

> [!NOTE]
> 尽管后台配置了高级的模型映射，将不同平台的模型对齐，但**这两个接口族的请求体是不通用的**。往哪个端点发请求，就必须采用该端点规定的 JSON 格式负载。

---

## 何时选择 Antigravity

只有在管理员向您明确说明支持 `/antigravity` 入口，或者您所使用的 Coding CLI 明确指定了该兼容层时，才需要使用 `/antigravity/...`。
配置时请遵循以下几点自检：
1. 确认该 {{SITE_NAME}} 节点已在配置文件中开启了 Antigravity 路由。
2. 确认当前 API Key 所在的分组已被授权使用相关模型。
3. 将客户端的 API Base URL 直接修改为：`{{BASE_URL}}antigravity`。

---

## 端点调试自检

| 异常代码 | 故障诱因 | 推荐自检与处理 |
| :--- | :--- | :--- |
| **401 Unauthorized** | 凭证校验未通过。 | 确认请求头中是否拼写了 `Bearer`，且环境变量成功读取。 |
| **403 Forbidden** | 分组未开放对应模型。 | 请登录控制台确认当前密钥所在的分组已授权访问该模型。 |
| **404 Not Found** | 路由拼装或模型名不匹配。 | 确认您的 Base URL 没有发生重复拼接，模型名称严格正确。 |
| **模型格式或解析报错** | Payload 结构与端点类型不符。 | 确认传入的 JSON 负载完全契合目标端点的 API 规定格式。 |
| **流式没有打字机输出** | 流式响应被截断。 | 检查沿途的反向代理 (如 Nginx/Cloudflare) 是否关闭了缓存，或开启了 SSE 转发。 |

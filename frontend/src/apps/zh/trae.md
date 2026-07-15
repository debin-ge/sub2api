# Trae

> 字节旗下 AI IDE，通过"添加自定义模型服务"接入。

```client
name: Trae
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://www.trae.ai
```

## 1. 安装

到 [trae.ai](https://www.trae.ai) 下载安装。

## 2. 配置

点击右上角设置图标 → **模型（Models）** → **添加自定义模型 / Custom Provider**：

- **Provider 类型**：Claude 模型选 **Anthropic**；GPT 模型选 **OpenAI 兼容**
- **Base URL**：Anthropic 类型填 `{{BASE_URL}}`；OpenAI 兼容填 `{{BASE_URL}}v1`
- **API Key**：粘贴你的密钥
- **模型名**：从 `/v1/models` 返回列表里挑

保存后在对话左下角的模型选择器切换到你添加的模型。

## 3. 验证

发送"你好"，收到回复即成功。

<details>
<summary>失败时看这里</summary>

- **404** — Base URL 结尾 `v1` 是否与协议匹配（Anthropic 不带，OpenAI 兼容带）。
- **找不到入口** — Trae 界面迭代较快，不同版本入口名可能是「模型服务商」「Model Providers」，以官方文档为准。

</details>

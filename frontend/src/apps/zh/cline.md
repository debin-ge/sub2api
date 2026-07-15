# Cline

> VS Code AI 编程插件，支持 Anthropic 和 OpenAI 协议。推荐 **Anthropic 模式**，体验最完整。

```client
name: Cline
logo: anthropic
protocols: [anthropic, openai]
endpoint: {{BASE_URL}}
homepage: https://github.com/cline/cline
```

## 1. 安装

VS Code 扩展市场（`Cmd/Ctrl + Shift + X`）搜索 `Cline`，点击 Install。

## 2. 配置

点击左侧 Cline 图标打开面板，首次会出现 Provider 配置界面（之后从面板右上角齿轮进入）。

- **API Provider**：选 **Anthropic**
- **Anthropic API Key**：粘贴你的密钥
- 勾选 **Use custom base URL**，填 `{{BASE_URL}}`（**不带** `v1`）
- **Model**：从 `/v1/models` 里选一个 Claude 模型名

## 3. 验证

在 Cline 输入框发送"列出当前目录的文件"，它开始响应并规划操作即成功。

<details>
<summary>失败时看这里</summary>

- **404** — Anthropic 模式的 Base URL **不带** `v1`。填反了就 404。
- **401** — 密钥不完整或字段填错行。
- **模型报错** — 换成 `/v1/models` 返回的模型名。

</details>

<details>
<summary>OpenAI Compatible 模式（备选）</summary>

如果需要 GPT 系列模型：

- **API Provider**：选 **OpenAI Compatible**
- **Base URL**：`{{BASE_URL}}v1`（**带** `v1`）
- **API Key**：你的密钥
- **Model ID**：从 `/v1/models` 里选

两种模式的 Base URL 结尾不同（Anthropic 不带 `v1`，OpenAI 带 `v1`），填反了会一直 404。

</details>

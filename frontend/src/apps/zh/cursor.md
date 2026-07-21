# Cursor

> AI 编辑器。全部在设置面板完成，无需改配置文件。

```client
name: Cursor
logo: openai
protocols: [openai]
endpoint: {{BASE_URL}}v1
homepage: https://cursor.com
```

## 1. 安装

到 [cursor.com](https://cursor.com) 下载安装。

## 2. 配置

按 `Cmd/Ctrl + Shift + J` 打开 Cursor Settings → **Models**，找到 **OpenAI API Key** 区域：

- API Key：粘贴你的密钥
- 展开 **Override OpenAI Base URL**，填 `{{BASE_URL}}v1`

点 **Verify** 按钮，显示成功即密钥生效。

然后在 Models 列表 **Add model**，输入 `/v1/models` 返回的模型名（例如 `gpt-4o-mini`），勾选启用。

## 3. 验证

按 `Cmd/Ctrl + L` 打开对话，选择你添加的模型，发送"你好"。收到回复即成功。

<details>
<summary>失败时看这里</summary>

- **Verify 失败** — Base URL 必须精确到 `{{BASE_URL}}v1`（结尾 `v1`）；密钥完整无空格。
- **404** — Base URL 少了 `v1` 或多拼成了 `/v1/v1`。
- **对话没反应** — 确认对话左下角模型下拉框选的是你新增的模型。

</details>

<details>
<summary>注意事项</summary>

开启自定义 Base URL 后，Cursor 的部分内置功能（如 Tab 补全）可能仍走官方通道，属正常现象。

</details>

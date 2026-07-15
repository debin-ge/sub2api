# Continue

> VS Code / JetBrains AI 编程插件，模型配置主要靠配置文件维护。

```client
name: Continue
logo: openai
protocols: [openai, anthropic]
endpoint: {{BASE_URL}}v1
config: ~/.continue/config.yaml
homepage: https://continue.dev
```

## 1. 安装

VS Code 扩展市场搜 `Continue`，点击 Install。首次打开 Continue 面板会自动创建配置目录。

## 2. 配置

打开 `~/.continue/config.yaml`（Windows：`%USERPROFILE%\.continue\config.yaml`）—— 最快是点 Continue 面板右上角齿轮 → 配置文件名。

在 `models:` 列表加入一项（YAML 对缩进敏感，请保持两空格缩进）：

```yaml
models:
  - name: {{SITE_NAME}} GPT
    provider: openai
    model: gpt-4o-mini
    apiBase: {{BASE_URL}}v1
    apiKey: 在此粘贴 sk- 开头的密钥
    roles:
      - chat
```

- `apiKey` 替换为真实密钥（配置文件不读环境变量）。
- 要接 Claude 模型再加一项，`provider: anthropic`、`apiBase: {{BASE_URL}}`（**不带** `v1`）、`model` 填 Claude 名。

## 3. 验证

保存文件后 Continue 会自动重载。打开 Continue 面板，模型下拉框选 `{{SITE_NAME}} GPT`，发送"你好"。收到回复即成功。

<details>
<summary>失败时看这里</summary>

- **面板报 YAML 错** — 缩进或冒号问题，对照示例逐行核对。
- **401 / 404** — Base URL 结尾与协议是否匹配（openai=带 `v1`，anthropic=不带）。
- **不自动重载** — 命令面板（`Cmd/Ctrl + Shift + P`）→ `Developer: Reload Window`。

</details>

<details>
<summary>旧版 config.json 格式</summary>

旧版 Continue 使用 `~/.continue/config.json`。如果目录里只有 `config.json`，在 `"models": []` 数组中加入：

```json
{ "title": "...", "provider": "openai", "model": "...", "apiBase": "...", "apiKey": "..." }
```

</details>

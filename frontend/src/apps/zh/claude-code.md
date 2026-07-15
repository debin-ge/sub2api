# Claude Code

> Anthropic 官方 CLI。同一份 `~/.claude/settings.json` 也被 VS Code / JetBrains 插件读取，配一次全端可用。

```client
name: Claude Code
logo: anthropic
protocols: [anthropic]
endpoint: {{BASE_URL}}
config: ~/.claude/settings.json
homepage: https://docs.claude.com/claude-code
```

## 1. 安装

```bash
npm install -g @anthropic-ai/claude-code
```

**验证**：`claude --version` 输出版本号即成功。

## 2. 配置

打开 `~/.claude/settings.json`（Windows：`%USERPROFILE%\.claude\settings.json`），不存在就手动新建：

```json download=settings.json
{
  "_note": "由 {{SITE_NAME}} 生成 · Base URL 已填好，把 ANTHROPIC_AUTH_TOKEN 换成你的 sk- 密钥即可（JSON 不支持注释，本行仅作说明，Claude Code 会忽略）",
  "env": {
    "ANTHROPIC_BASE_URL": "{{BASE_URL}}",
    "ANTHROPIC_AUTH_TOKEN": "在此粘贴 sk- 开头的密钥"
  }
}
```

- `ANTHROPIC_BASE_URL` **不带** `v1`。
- `ANTHROPIC_AUTH_TOKEN` 替换为真实密钥（配置文件不读环境变量）。

## 3. 验证

新开一个终端窗口：

```bash
claude "用一句话介绍你自己"
```

收到回复即成功。首次运行遇到登录引导，选"使用 API Key"，不要走 OAuth。

<details>
<summary>失败时看这里</summary>

- **401** — 密钥不完整或字段名写错（必须是 `ANTHROPIC_AUTH_TOKEN`）
- **仍连到官方地址** — JSON 未保存或格式错；`claude` 需完全退出重开
- **模型报错** — 让管理员确认当前分组开放了 Claude 兼容模型
- 更多错误码见 [错误排查](/docs/errors)

</details>

<details>
<summary>Antigravity / 项目级 / 环境变量 / 反代</summary>

**Antigravity 渠道**：Base URL 加 `antigravity` 后缀，其余不变：

```json
{ "env": { "ANTHROPIC_BASE_URL": "{{BASE_URL}}antigravity", "ANTHROPIC_AUTH_TOKEN": "..." } }
```

Anthropic 与 Antigravity 渠道**不能在同一会话上下文中混用**。

**项目级覆盖**：项目根目录建 `.claude/settings.json`（格式同上），优先级高于用户级。

**环境变量备选**（临时试用）：

```bash
export ANTHROPIC_BASE_URL="{{BASE_URL}}"
export ANTHROPIC_AUTH_TOKEN="在此粘贴密钥"
# 个别兼容客户端需要 ANTHROPIC_API_KEY：
export ANTHROPIC_API_KEY="在此粘贴密钥"
```

**Nginx 反代**：管理员需在 `http` 块加 `underscores_in_headers on;`，否则多账号粘性会话失效。

</details>

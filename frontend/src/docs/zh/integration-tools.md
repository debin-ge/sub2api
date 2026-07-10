# 第三方工具接入

当你需要在多个站点、多个密钥或多个 AI 工具之间频繁切换时，手工反复改配置文件既麻烦又容易出错。本页介绍几款能替你管理这些配置的工具。

> 以下工具均为独立的开源社区项目，不是 {{SITE_NAME}} 官方产品。安装使用前请自行审查其源码、权限和数据存储方式。

## 接入前提

已完成 [接入总览](/docs/integration-overview) 的准备：拿到 Base URL（`{{BASE_URL}}`）和 API Key。

## CC-Switch

```client
name: CC-Switch
logo: anthropic
protocols: [anthropic, openai, gemini]
homepage: https://github.com/farion1231/cc-switch
```

**它是什么**：一款跨平台桌面应用（macOS / Windows / Linux），用可视化界面管理 Claude Code、Codex、Gemini CLI、OpenCode 等多个 AI 编程工具的服务商配置，一键在不同站点之间切换，不用再手工编辑 JSON / TOML 文件。

**工作原理**：切换时它会直接改写各工具的实际配置文件（如 `~/.claude/settings.json`、`~/.codex/config.toml`），并把你在外部做的修改同步回来。它自己的数据保存在 `~/.cc-switch/`。

> 正因为它会接管这些配置文件，**请不要一边用 CC-Switch 一边手工改同一份文件**，以免互相覆盖。交给它管理后，切换和修改都在它的界面里完成。

**第 1 步：安装 CC-Switch。** 到其 GitHub 仓库的 Releases 页面下载对应系统的安装包（macOS 选 `.dmg`，Windows 选 `.msi` 或 `.exe`），按常规方式安装并打开。

**第 2 步（推荐）：从 {{SITE_NAME}} 一键导入。** {{SITE_NAME}} 已内置 CC-Switch 快捷导入：

1. 登录 {{SITE_NAME}}，进入左侧菜单 **「API 密钥」**。
2. 找到你要使用的密钥，点击该行的 **「导入到 CC-Switch」** 按钮。
3. 按提示选择客户端类型（Claude Code / Gemini CLI 等）。
4. 浏览器会请求打开 CC-Switch（`ccswitch://` 链接），确认允许。
5. CC-Switch 打开后确认导入，站点配置（名称、地址、密钥、用量脚本）自动填好。

**预期结果**：CC-Switch 的服务商列表中出现 {{SITE_NAME}} 条目。

**如果浏览器没有反应或提示无法打开链接**：说明 CC-Switch 未安装或其链接协议未注册成功——先完成第 1 步安装并至少手动打开过一次，再重试；仍不行就走下面的手动添加。

**第 2 步（备选）：手动添加。** 在 CC-Switch 中点击 **添加服务商（Add Provider）**，选择对应的应用（Claude Code / Codex / Gemini CLI），填写：

- 名称：随意，如 `{{SITE_NAME}}`
- 地址（Endpoint / Base URL）：Claude Code 填 `{{BASE_URL}}`；Codex 填 `{{BASE_URL}}`（CC-Switch 会按应用类型生成正确的配置）；Antigravity 渠道填 `{{BASE_URL}}antigravity`
- API Key：`${API_KEY}`

**第 3 步：切换并生效。** 在列表中点击该条目的 **启用（Enable）**。大多数工具需要**重开一个终端窗口**才会读到新配置（Claude Code 支持热切换）。

**✅ 完成检验**：重开终端，运行 `claude "你好"`（或对应工具），收到回复即切换成功。

## Cockpit Tools

```client
name: Cockpit Tools
logo: antigravity
protocols: [anthropic, openai, gemini]
homepage: https://github.com/jlcodes99/cockpit-tools
```

**它是什么**：一款基于 Tauri 的桌面应用（macOS / Windows / Linux），定位是「通用 AI IDE 账号管理工具」，支持约 15 个 AI IDE / 工具平台（Antigravity IDE、Codex、Cursor、Windsurf、Trae、Zed、Gemini CLI、GitHub Copilot 等）的账号管理，提供一键账号切换、多账号多实例并行、额度监控和定时任务等能力。

**它与 {{SITE_NAME}} 的关系**：Cockpit Tools 本身**不是 API 中转服务**，而是账号/配置管理器。它的典型用法是管理各 AI IDE 的多个账号；对 {{SITE_NAME}} 用户而言，价值在于把「使用 {{SITE_NAME}} 的工具配置」作为其中一个账号条目统一管理，与其他账号快速切换。

**使用建议**：

1. 到其 GitHub 仓库 Releases 下载对应系统安装包（macOS `.dmg` / Windows `.msi`、`.exe`）。
2. 打开后按其界面提示为你使用的工具（如 Codex、Antigravity IDE）添加账号条目。
3. 涉及自定义服务地址的工具，填写本页开头的 Base URL 和你的 API Key，字段含义与 [客户端接入](/docs/integration-clients)、[CLI 接入](/docs/integration-cli) 中对应工具的说明一致。

> 该工具功能较多且更新频繁，具体操作以其仓库 README 为准。

## 密钥安全提示

使用任何第三方工具时，请记住 API Key 等同于账户额度的钥匙：

| 建议 | 说明 |
| --- | --- |
| 最小授权 | 给第三方工具用的密钥单独创建，不要复用你在生产系统里的密钥。 |
| 留意存储方式 | 第三方工具会把密钥保存在本地文件或其自有目录（如 `~/.cc-switch/`），确保设备本身有磁盘加密和锁屏保护。 |
| 定期轮换 | 定期在 {{SITE_NAME}} 的「API 密钥」页删除旧密钥、创建新密钥，工具里同步更新。 |
| 泄露处置 | 一旦怀疑泄露，立即到「API 密钥」页删除或禁用该密钥，再排查用量记录（见 [计费与用量](/docs/billing-usage)）。 |

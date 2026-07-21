# Cockpit Tools

> 通用 AI IDE 账号管理器（macOS / Windows / Linux，基于 Tauri）。定位比 CC-Switch 更宽：不只管 CLI 工具，还覆盖约 15 个 AI IDE / 桌面工具的账号切换与多实例并行，包括 **Antigravity IDE、Codex、Cursor、Windsurf、Trae、Zed、Gemini CLI、GitHub Copilot** 等。

```client
name: Cockpit Tools
logo: antigravity
protocols: [anthropic, openai, gemini]
homepage: https://github.com/jlcodes99/cockpit-tools
```

## 它和 CC-Switch 有什么区别

| | CC-Switch | Cockpit Tools |
| --- | --- | --- |
| 覆盖工具 | Claude Code / Codex / Gemini CLI / OpenCode（4 个 CLI） | 约 15 个（IDE + CLI + Copilot 等） |
| 定位 | 服务商/中转站切换 | AI IDE 账号管理 + 多实例 |
| 数据模型 | 每个工具下多个 Provider | 每个工具下多个 Account |
| 特色能力 | 简单直接，聚焦密钥+URL | 多实例并行、额度监控、定时任务 |

> 对 {{SITE_NAME}} 用户而言：如果你只用 Claude Code / Codex CLI，CC-Switch 就够了；如果你还用 Antigravity IDE / Cursor / Windsurf 等桌面工具，或者需要**同时运行多个实例**（比如两个 Antigravity 窗口跑不同账号），Cockpit Tools 更合适。

## 1. 安装

到 [GitHub Releases](https://github.com/jlcodes99/cockpit-tools/releases) 下载对应系统安装包：

- **macOS**：`.dmg`（Apple Silicon / Intel 分别下载）
- **Windows**：`.msi` 或 `.exe`
- **Linux**：`.AppImage` 或 `.deb`

首次启动会请求「访问隐私文件夹」权限（要读写各 IDE 的配置），允许它。

## 2. 首次配置

打开后主界面左侧列出所有已识别的 AI IDE / 工具。**灰色**表示未检测到安装，**彩色**表示可管理。

**第 1 步：选中你要管理的工具**（比如 Antigravity IDE 或 Codex）。

**第 2 步：点右上角「添加账号」**，进入账号编辑面板。

**第 3 步：填字段**——不同工具字段名略有差异，但共通的三项是：

| 字段 | 填什么 |
| --- | --- |
| **账号名称** | 随意，用来区分。如 `{{SITE_NAME}} 主号`、`直连官方` |
| **Base URL / Endpoint**（自定义服务地址） | 用 {{SITE_NAME}} 填 `{{BASE_URL}}`；Antigravity 渠道填 `{{BASE_URL}}antigravity` |
| **API Key / Token** | 你的 `sk-` 密钥 |

**第 4 步：保存**。账号出现在该工具下的账号列表里，未启用状态。

<details>
<summary>各工具字段命名参考</summary>

| 工具 | 服务地址字段 | 密钥字段 |
| --- | --- | --- |
| Antigravity IDE | Base URL | API Key |
| Codex | model_providers.<name>.base_url | env: API_KEY |
| Cursor | OpenAI Base URL | OpenAI API Key |
| Trae | Custom Provider URL | API Key |
| Zed | assistant.provider.api_url | api_key |
| Gemini CLI | GEMINI_API_BASE | GEMINI_API_KEY |
| GitHub Copilot | 特殊：管理的是 OAuth Token 而非 URL | GitHub OAuth |

对于 {{SITE_NAME}} 用户，Base URL 一律填 `{{BASE_URL}}`；密钥字段填对应工具要求的名称。

</details>

## 3. 切换账号

在账号列表中点某账号的 **「启用」** 按钮：

1. Cockpit Tools 备份该工具的当前配置到自己数据库
2. 把新账号的 URL / Key 写入该工具的真实配置文件
3. 若该工具支持热切换（Claude Code），立即生效；否则需要重开该 IDE 或终端窗口

**验证**：重开目标工具，发送一条对话，收到回复即切换成功。想更严谨可以在工具的「关于」或「设置 → 账号」页看当前登录信息。

## 4. 多实例并行（Cockpit Tools 的招牌功能）

某些 AI IDE（尤其 Antigravity 系）不支持同时用两个账号——因为它们把账号写在全局配置里。Cockpit Tools 通过**沙盒目录**让你并行多开：

**第 1 步**：在账号旁点 **「启动实例」** 按钮（不是普通的「启用」）。

**第 2 步**：Cockpit Tools 会：
- 复制该 IDE 到一个临时沙盒（首次较慢）
- 给沙盒实例注入当前账号的配置
- 启动这个隔离实例

**第 3 步**：想再开一个用不同账号的实例，就在另一个账号上重复。它们的配置互不影响。

<details>
<summary>什么时候用「启动实例」而不是「启用」</summary>

- **启用**：改主配置，之后所有直接启动的窗口都用这个账号。适合日常单账号使用。
- **启动实例**：为这次启动**临时**注入配置，主配置不动。适合：
  - 同时对比两个账号的输出
  - 用备份账号跑一个不打算干扰主账号历史的任务
  - 演示 / 分享屏幕时避免暴露主账号

</details>

## 5. 额度监控

Cockpit Tools 支持给账号绑定「用量脚本」，定时拉取剩余额度并在界面显示：

1. 编辑账号，找 **「用量查询」** 或 **「Usage Script」** 字段
2. 填入 {{SITE_NAME}} 的用量查询 API（在密钥页面可以复制），或让 Cockpit Tools 从预设模板选 `{{SITE_NAME}}` 自动填
3. 保存后可在账号列表看到实时余额

<details>
<summary>用量脚本示例</summary>

如果 Cockpit Tools 没有 {{SITE_NAME}} 的预设模板，可以手工填一段 curl：

```bash
curl -H "Authorization: Bearer $KEY" {{BASE_URL}}api/user/usage
```

具体端点以 {{SITE_NAME}} 「API 密钥」页面提供的复制脚本为准。

</details>

## 6. 数据存储与备份

Cockpit Tools 的数据（账号列表、沙盒实例、用量记录）存放在：

- **macOS**：`~/Library/Application Support/CockpitTools/`
- **Windows**：`%APPDATA%\CockpitTools\`
- **Linux**：`~/.config/CockpitTools/`

备份整个目录即可整机迁移。密钥在数据库里以明文或工具自定义的加密方式存储（版本不同策略不同），迁移设备时**请只在你信任的机器上恢复**。

## 7. 常见问题

<details>
<summary>启动实例失败 / 磁盘占用暴涨</summary>

- 首次启动实例会复制整个目标 IDE 到沙盒，占用可能几百 MB 到 1 GB。空间不足会失败。
- 不再需要的沙盒实例在 Cockpit Tools 里点 **「清理沙盒」** 释放空间。

</details>

<details>
<summary>切换后 IDE 仍连老账号</summary>

- 未完全退出 IDE：IDE 的托盘图标要退出、Dock 也要退出，然后重开。
- 该工具用了 keychain / 系统凭据存储（如 GitHub Copilot 的 OAuth token）：Cockpit Tools 只能改配置文件，改不了系统 keychain 项；需要在 IDE 里手动登出重登。

</details>

<details>
<summary>密钥安全提示</summary>

- 给 Cockpit Tools 用的密钥单独创建，不要复用生产系统的密钥。
- 该工具持久化密钥到本地数据库，确保设备本身有磁盘加密和锁屏保护。
- 泄露怀疑：立即到 {{SITE_NAME}} 「API 密钥」页删除或禁用该密钥。

</details>

> Cockpit Tools 功能较多且更新频繁，具体界面文案以其仓库 README 为准。上述内容涵盖对 {{SITE_NAME}} 用户最常用的核心场景。

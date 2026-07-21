# CC-Switch

> 跨平台桌面账号管理器（macOS / Windows / Linux），用可视化界面同时管理 Claude Code / Codex / Gemini CLI / OpenCode 的**多套**服务商配置，一键切换、免手改 JSON/TOML。

```client
name: CC-Switch
logo: anthropic
protocols: [anthropic, openai, gemini]
homepage: https://github.com/farion1231/cc-switch
```

## 它解决什么问题

如果你手里有多个中转站或多个密钥（{{SITE_NAME}}、直连官方、备用站…），传统办法是每次切换都要打开 `~/.claude/settings.json` 或 `~/.codex/config.toml` 手工改 URL 和 Key，改错还容易破坏 JSON 语法。

CC-Switch 把这些配置文件的读写**托管**起来——你只在它的界面里维护多个「服务商（Provider）」条目，切换时它自动改写对应工具的实际配置文件。

> ⚠️ **它托管的是文件本身**。启用 CC-Switch 后，请不要一边用它、一边手工修改 `~/.claude/settings.json` 等文件，否则会互相覆盖。

## 1. 安装

到 [GitHub Releases](https://github.com/farion1231/cc-switch/releases) 下载对应系统的安装包：

- **macOS**：`.dmg`（Apple Silicon 选 `arm64`，Intel 选 `x64`）
- **Windows**：`.msi` 或 `.exe`
- **Linux**：`.AppImage` 或 `.deb`

装完打开一次，让它注册 `ccswitch://` URL Scheme（后面一键导入会用到）。

## 2. 配置：从 {{SITE_NAME}} 一键导入（推荐）

{{SITE_NAME}} 已经内置了 CC-Switch 快捷导入，避免手动填字段：

1. 登录 {{SITE_NAME}}，进入左侧菜单 **「API 密钥」**。
2. 找到你想用的密钥行，点该行的 **「导入到 CC-Switch」** 按钮。
3. 弹窗里选择目标客户端类型（**Claude Code** / **Codex** / **Gemini CLI** / **OpenCode**）。
4. 浏览器请求打开 `ccswitch://` 链接，点 **允许**。
5. CC-Switch 前台弹出确认，点 **导入**。站点名、Endpoint、密钥、用量脚本全部自动填好。

**预期结果**：CC-Switch 的服务商列表中出现新增的 {{SITE_NAME}} 条目，未启用状态。

<details>
<summary>浏览器提示「无法打开 ccswitch:// 链接」怎么办</summary>

- 确认 **第 1 步** 已经装好 CC-Switch，并**至少手动打开过一次**（让系统注册协议）。
- macOS：若 Safari 阻止外部协议，改用 Chrome 或 Edge 重试。
- 仍不行 → 走下一节「手动添加」。

</details>

## 3. 配置：手动添加

如果不想用一键导入，或要接入其他站点：

1. 打开 CC-Switch，左侧顶部选目标工具（Claude Code / Codex / Gemini CLI / OpenCode）。
2. 点右上角 **「添加」** 或 **「+」**。
3. 填字段：

| 字段 | 值 |
| --- | --- |
| **名称** | 随意，用于区分。如 `{{SITE_NAME}} 主站`、`{{SITE_NAME}} 备用` |
| **Endpoint / Base URL** | Claude Code 与 Codex 填 `{{BASE_URL}}`；Antigravity 渠道填 `{{BASE_URL}}antigravity` |
| **API Key** | 粘贴你的 `sk-` 密钥 |
| **备注（可选）** | 用途说明，如「日常」「大模型专用」 |

4. 点保存。

> Codex 条目的 Endpoint 填法比 Claude Code 稍有不同（不需要手动带 `v1`，CC-Switch 会按工具类型生成正确的 `config.toml`）。按上表照抄即可。

## 4. 切换与验证

在服务商列表点某条的 **「启用」**（星标或对勾按钮），CC-Switch 立刻改写该工具的真实配置文件。

**预期结果**：

| 工具 | 需要的动作 |
| --- | --- |
| **Claude Code** | 支持热切换，正在运行的会话下一次请求会走新配置 |
| **Codex CLI / OpenCode / Gemini CLI** | 需要**重开一个终端窗口**才生效（旧窗口读的是启动时的环境） |
| **IDE 扩展**（VS Code / JetBrains 里的 Claude Code / Codex） | 完全退出 IDE 再打开 |

验证：重开终端后运行对应命令（如 `claude "你好"` 或 `codex "你好"`），能收到回复即切换成功。想更严谨可以先跑 `echo $ANTHROPIC_BASE_URL`（Claude Code）或看 `cat ~/.codex/config.toml`（Codex）确认写入是最新的。

## 5. 管理多个服务商

CC-Switch 的核心价值就是同时维护多个条目：

- **添加多个**：为每个站点/密钥各建一条，取不同名称。
- **一次只能启用一个**：同一工具下只有一个条目处于启用状态。
- **快速切换**：切走时它自动把「旧启用」条目的当前值同步回自己数据库（保留你在外部工具里做过的临时改动），再写入「新启用」的值。
- **导出/备份**：CC-Switch 的自有数据存储在 `~/.cc-switch/`（macOS/Linux）或 `%APPDATA%\cc-switch\`（Windows）。备份这个目录就能整机迁移。

<details>
<summary>各工具真实配置文件位置</summary>

CC-Switch 切换时会读写这些文件；平时不要手工改：

| 工具 | 配置文件路径 |
| --- | --- |
| Claude Code | `~/.claude/settings.json` |
| Codex | `~/.codex/config.toml` + `~/.codex/auth.json`（密钥独立存放） |
| Gemini CLI | `~/.gemini/settings.json` |
| OpenCode | `~/.opencode/config.json` |

Windows 上把 `~/` 替换为 `%USERPROFILE%\`。

</details>

<details>
<summary>Antigravity 渠道配置</summary>

Antigravity 是 Claude 协议的变体，Base URL 后缀多一段 `antigravity`：

- 手动添加时 Endpoint 填 `{{BASE_URL}}antigravity`
- 一键导入时{{SITE_NAME}} 会根据你的分组自动写入正确后缀
- **Anthropic 渠道与 Antigravity 渠道不能在同一会话上下文中混用**——需要切时先把当前对话结束

</details>

<details>
<summary>常见问题</summary>

- **切换后仍连老地址** — 目标工具没重开窗口/进程；Claude Code 需要新会话，Codex/Gemini 需要重开终端。
- **改动被吞掉了** — 你在外部手工改了 `settings.json`，CC-Switch 下次切换又用它数据库里的旧值覆盖；解决办法是所有改动都在 CC-Switch 界面里做，或在切换前先「同步当前配置回 CC-Switch」（若版本支持）。
- **想彻底不用 CC-Switch** — 在它里面把所有条目禁用/删除，然后从 `~/.cc-switch/` 删除数据目录；实际配置文件里最后一次启用的值仍会保留，需要时手动编辑。
- **Nginx 反代**：`{{SITE_NAME}}` 若部署在 Nginx 后面，管理员需在 `http` 块加 `underscores_in_headers on;`，否则粘性会话 `session_id` 头被丢弃。CC-Switch 本身不管这个——是服务端配置。

</details>

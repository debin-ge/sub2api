# 丰富第三方 APP 与客户端接入文档设计方案

本方案旨在丰富和整理文档中关于第三方工具（CC-Switch、Cockpit）的配置、导入、切换，以及各类 AI Coding 客户端（Claude Code CLI、Codex App/CLI、OpenCode CLI 等）的切换与接入使用说明。

## 目标与重构思路

1. **结构化重构 `clients.md`**：
   将 `zh/clients.md` 和 `en/clients.md` 的内容重新分类为三个核心板块：
   - **基础 SDK 与 API 接入**：原有的原生 curl、OpenAI SDK (TS/Python)、Anthropic Messages、Gemini 原生端点。
   - **CLI 编程客户端（Coding Agents）**：集中介绍 **Claude Code CLI**、**Codex CLI**、**OpenCode CLI** 的配置与切换使用说明。
   - **第三方 GUI 与集成工具**：详细介绍 **CC-Switch**、**Cockpit** 和 **Codex App**，说明它们如何作为管理和快捷切换 AI 客户端的中心，并附带底层的 API Endpoint 与 API Key 配置步骤。
2. **丰富 `configuration-snippets.md`**：
   在 `zh/configuration-snippets.md` 和 `en/configuration-snippets.md` 中增加对应的快捷复制环境变量与配置模板。

---

## 详细设计内容

### 1. 修改 `frontend/src/docs/zh/clients.md`
在大纲中插入以下主要内容：

#### CLI 编程客户端（Coding Agents）
- **Claude Code CLI**：
  - 说明如何使用命令行客户端，环境变量配置为 `export ANTHROPIC_BASE_URL="{{BASE_URL}}"` 和 `export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"` 或 `ANTHROPIC_API_KEY`。
  - 使用说明：在终端执行上述环境变量设置后，运行 `claude` 即可拉起客户端。如果管理员提供了特定的 `/antigravity` 路由，则将 Base URL 更改为 `{{BASE_URL}}antigravity`。
- **Codex CLI**：
  - 介绍 Codex CLI 底层依赖 OpenAI 的 Responses API，环境变量配置为 `export OPENAI_BASE_URL="{{BASE_URL}}v1"` 与 `export OPENAI_API_KEY="$YOUR_KEY"`。
  - **重要注意事项**：在使用 Nginx 反向代理时，必须在 Nginx 配置的 `http` 块中加入 `underscores_in_headers on;`，否则由于 `session_id` 等下划线头部被过滤，会导致粘性会话失效，从而影响多账号环境的正常运行。
- **OpenCode CLI**：
  - 介绍 OpenCode 平台（OpenCode2API）的兼容性。配置环境变量 `export OPENCODE_API_BASE="{{BASE_URL}}v1"` 与 `export OPENCODE_API_KEY="$YOUR_KEY"`，或者在使用通用客户端时指向 OpenAI 的 Base URL。

#### 第三方 GUI 与集成工具
- **CC-Switch 一键导入与客户端切换**：
  - **工具定位**：CC-Switch 是一个支持多客户端（Claude、Gemini、Codex 等）统一配置、管理与一键切换的本地快捷工具。
  - **配置与一键导入**：为了免去手动配置多个客户端环境变量的繁琐步骤，本平台在“密钥管理”页面内置了 `ccswitch://` 协议的一键导入支持。用户只需在对应 API Key 旁点击 **“导入到 CC-Switch”** 按钮并选择要导入的目标客户端类型（如 Claude/Gemini/Codex），CC-Switch 将自动配置对应底层的 API Endpoint 以及 API Key，省去频繁手动切换的麻烦。
  - **底层配置参数**：
    - 导入后，CC-Switch 会将 Claude 客户端的 endpoint 指向 `{{BASE_URL}}`，将 Codex 客户端的 endpoint 指向 `{{BASE_URL}}v1`，并将 API Key 写入其本地配置中。
- **Cockpit 集中管理与切换**：
  - **工具定位**：Cockpit 是一款本地 Agent 控制台，旨在以图形化界面统一管理和一键拉起/切换不同的 CLI 客户端（如 Claude Code CLI、Codex CLI、OpenCode CLI 等）。
  - **配置与切换**：用户可以通过 Cockpit 面板为各个底层 Coding App/CLI 快捷地启用或禁用中转服务。在 Cockpit 对应的配置面板中，将对应引擎（如 Claude 引擎或 Codex 引擎）的 API Base URL（或 Endpoint）修改为本平台的中转地址：
    - Claude 引擎：`{{BASE_URL}}`
    - Codex/OpenCode 引擎：`{{BASE_URL}}v1`
    - 并在 Key 字段中填入本平台的 API Key，即可实现不同 AI App 之间的快捷切换与中转分流。
- **Codex App**：
  - 在客户端的配置设置中，将 OpenAI Endpoint 替换为 `{{BASE_URL}}v1`，填入 API Key 即可。

---

### 2. 修改 `frontend/src/docs/en/clients.md`
英文版文档与中文版保持结构及内容上的完全映射，将上述新增章节进行准确的英文翻译，重点词汇对应如下：
- CC-Switch: "CC-Switch: Client Switching & One-Click Import"
- Cockpit: "Cockpit: Centralized Management & Switching"
- Codex CLI, Claude Code CLI, OpenCode CLI 及其环境变量。

---

### 3. 修改 `frontend/src/docs/zh/configuration-snippets.md` 与 `en/configuration-snippets.md`
在文件中补充新增客户端（OpenCode CLI, Codex CLI 等）的一键复制环境变量。

---

## 验证计划

- **静态内容验证**：构建并预览前端，确保 Markdown 的排版与标题层级完全正确，链接可正常点击。
- **配置一致性验证**：仔细检查占位符 `{{BASE_URL}}` 与 `$YOUR_KEY` 拼写，确保其能够被文档解析器正确渲染和替换。

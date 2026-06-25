# Enrich Third-Party Clients and GUI Tools Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize and enrich both Chinese and English documentation to comprehensively explain how to configure and switch between various CLI coding clients (Claude Code CLI, Codex CLI, OpenCode CLI) and third-party GUI management tools (CC-Switch, Cockpit).

**Architecture:** Split the `clients.md` document structure into three distinct parts: (1) Base SDK and API Integration, (2) CLI Coding Clients (Coding Agents), and (3) Third-Party GUI & Integrated Tools. Corresponding configurations will also be added to `configuration-snippets.md`.

**Tech Stack:** Markdown / Vite / vue-i18n / vue-tsc

---

### Task 1: Enrich Chinese Client Integration Documentation (`frontend/src/docs/zh/clients.md`)

**Files:**
- Modify: `frontend/src/docs/zh/clients.md`

- [ ] **Step 1: Replace old sections with structured CLI Coding Clients section**

  Replace lines 121 to 219 (the old Claude Code, Antigravity Claude, Gemini Native, and Codex `/v1/responses` sections) with the new structured **CLI Coding Clients** and **Third-Party GUI** sections.

  Replace:
  ```markdown
  ## Claude Code 环境变量

  Claude Code 或 Anthropic 兼容客户端通常需要 Anthropic Base URL 和 API Key。不同版本的客户端变量名可能不同，请以客户端自身文档为准；常见配置如下：

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}"
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  部分客户端需要 `ANTHROPIC_API_KEY`：

  ```bash
  export ANTHROPIC_API_KEY="$YOUR_KEY"
  ```

  然后选择当前部署支持的 Claude 兼容模型。模型名应来自 `/v1/models` 或管理员提供的映射名。

  ## Anthropic Messages 请求

  ```bash
  curl "${BASE_URL}v1/messages" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "model": "claude-3-5-sonnet-latest",
      "max_tokens": 512,
      "messages": [
        { "role": "user", "content": "把这段需求整理成验收标准。" }
      ]
    }'
  ```

  Messages 请求通常需要 `max_tokens`。如果你把 OpenAI 的 `messages` 格式直接复制到 Anthropic 端点，也要确认字段是否符合该客户端或上游要求。

  ## Antigravity Claude 环境变量

  If the admin provides an Antigravity Claude compatible entry point, point the client to `/antigravity` path:

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  如果出现 404 或模型不可用，请确认当前部署是否启用了 `/antigravity/v1/messages`，以及你的分组是否有对应模型权限。

  ## Gemini 原生端点
  ...
  ## Codex `/v1/responses` 示例
  ...
  ```

  With:
  ```markdown
  ## Anthropic Messages 请求

  ```bash
  curl "${BASE_URL}v1/messages" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "model": "claude-3-5-sonnet-latest",
      "max_tokens": 512,
      "messages": [
        { "role": "user", "content": "把这段需求整理成验收标准。" }
      ]
    }'
  ```

  Messages 请求通常需要 `max_tokens`。如果你把 OpenAI 的 `messages` 格式直接复制到 Anthropic 端点，也要确认字段是否符合该客户端或上游要求。

  ## Gemini 原生端点

  Gemini 原生客户端应使用 `/v1beta` 路径和 Gemini 请求体格式。非流式示例：

  ```bash
  curl "${BASE_URL}v1beta/models/gemini-2.0-flash:generateContent" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "contents": [
        {
          "parts": [
            { "text": "用一句话介绍 {{SITE_NAME}}。" }
          ]
        }
      ]
    }'
  ```

  流式请求使用：

  ```bash
  curl -N "${BASE_URL}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "contents": [
        {
          "parts": [
            { "text": "连续输出一个简短大纲。" }
          ]
        }
      ]
    }'
  ```

  Gemini 原生端点不使用 OpenAI `messages` 格式。请使用当前部署支持的 Gemini 模型名或映射名。

  ## CLI 编程客户端（Coding Agents）

  这些命令行工具（如 Claude Code CLI、Codex CLI、OpenCode CLI 等）直接运行在终端中，通过环境变量可以方便地配置和切换底层的中转服务。

  ### Claude Code CLI

  官方 Claude Code 客户端可以通过指定 Anthropic API Base URL 的环境变量，将流量中转至本平台：

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}"
  export ANTHROPIC_API_KEY="$YOUR_KEY"
  ```

  或者对于某些版本：

  ```bash
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  配置完成后，在终端中直接运行 `claude` 即可拉起助手并调用对应的兼容模型。

  如果管理员提供了特定的 `/antigravity` 路由，可以将 Base URL 更改为：

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  ### Codex CLI

  Codex CLI 依赖 OpenAI 格式的 Responses API，需要配置如下环境变量将其中转至本平台：

  ```bash
  export OPENAI_BASE_URL="{{BASE_URL}}v1"
  export OPENAI_API_KEY="$YOUR_KEY"
  ```

  **注意事项**：若使用 Nginx 反向代理本平台服务，请确保 Nginx 配置的 `http` 块中加入下述配置以保证会话粘性：
  ```nginx
  underscores_in_headers on;
  ```
  如果不配置该项，Nginx 默认会过滤掉请求头中含有下划线的字段（如 `session_id`），导致多账号环境下的粘性会话失效。

  ### OpenCode CLI

  OpenCode CLI 用于接入 OpenCode2API 渠道，需要配置对应的端点与密钥：

  ```bash
  export OPENCODE_API_BASE="{{BASE_URL}}v1"
  export OPENCODE_API_KEY="$YOUR_KEY"
  ```

  若是使用通用兼容客户端，直接将 OpenAI SDK 的 Base URL 和 API Key 指向本平台的 `/v1` 路径即可。

  ## 第三方 GUI 与集成工具

  这类图形界面工具不仅能帮您存储 API Key，还能方便地在一处管理、配置并快速切换底层的不同 AI 客户端。

  ### CC-Switch 客户端切换与导入

  * **客户端管理与切换**：CC-Switch 是一个支持多客户端（Claude、Gemini、Codex 等）统一配置、管理与一键切换的本地快捷工具。
  * **一键配置与导入**：为避免在不同 AI 客户端之间来回修改环境变量，本平台提供了内置的一键导入功能。在平台的**“密钥管理”**页面，点击 API Key 旁的 **“导入到 CC-Switch”**，根据需要选择要导入的客户端类型（如 Claude/Gemini/Codex），CC-Switch 将自动拉起并配置对应底层的 API Endpoint 以及 API Key，省去频繁手动切换的麻烦。
  * **手动配置说明**：
    * 导入/配置 Claude 客户端：`app: "claude"`, `endpoint: "{{BASE_URL}}"`
    * 导入/配置 Codex 客户端：`app: "codex"`, `endpoint: "{{BASE_URL}}v1"`, 默认模型 `gpt-5.4`
    * 导入/配置 Gemini 客户端：`app: "gemini"`, `endpoint: "{{BASE_URL}}"`

  ### Cockpit 集中管理与切换

  * **客户端集中控制台**：Cockpit 是一款本地 Agent 控制台 GUI 软件，能集中管理和一键拉起或切换本地的 **Claude Code CLI**、**Codex CLI**、**OpenCode CLI** 等不同 Agent 的会话。
  * **配置与切换**：在 Cockpit 的配置面板中，可为不同的 AI 引擎独立设置 API 凭证，方便您根据需求一键切换所调用的 Agent 客户端。设置时请在 Cockpit 的设置面板中，将对应引擎的 API Base URL（或 Endpoint）修改为本平台的中转地址：
    * **Claude 引擎**：API Base URL / Endpoint 填入 `{{BASE_URL}}`，并填入本平台 API Key。
    * **Codex 引擎**：API Base URL / Endpoint 填入 `{{BASE_URL}}v1`，并填入本平台 API Key。
    * **OpenCode 引擎**：API Base URL / Endpoint 填入 `{{BASE_URL}}v1`，并填入本平台 API Key。

  ### Codex App

  桌面端客户端 Codex App 可以直接在设置中将 API Base URL 替换为：
  1. 将 OpenAI API Base URL 替换为：`{{BASE_URL}}v1`
  2. 填入本平台生成的 API Key。
  ```

- [ ] **Step 2: Verify changes via git diff**

  Run: `git diff frontend/src/docs/zh/clients.md`
  Expected: Diff shows the reorganized structure and the new sections.

- [ ] **Step 3: Commit**

  Run: `git add frontend/src/docs/zh/clients.md && git commit -m "docs(zh): enrich and reorganize clients integration guide"`

---

### Task 2: Enrich English Client Integration Documentation (`frontend/src/docs/en/clients.md`)

**Files:**
- Modify: `frontend/src/docs/en/clients.md`

- [ ] **Step 1: Replace old sections with structured English CLI Coding Clients section**

  Translate and replace the corresponding sections in English.

  Replace:
  ```markdown
  ## Claude Code Environment Variables

  Claude Code or Anthropic compatible clients usually need an Anthropic Base URL and API Key. Environment variable names vary by client version, so follow the client documentation. A common setup is:

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}"
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  Some clients require `ANTHROPIC_API_KEY`:

  ```bash
  export ANTHROPIC_API_KEY="$YOUR_KEY"
  ```

  Then choose a Claude compatible model supported by the current deployment. Model names should come from `/v1/models` or an admin-provided mapping name.

  ## Anthropic Messages Request
  ...
  ## Antigravity Claude Environment Variables
  ...
  ## Gemini Native Endpoints
  ...
  ## Codex `/v1/responses` Example
  ...
  ```

  With:
  ```markdown
  ## Anthropic Messages Request

  ```bash
  curl "${BASE_URL}v1/messages" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "model": "claude-3-5-sonnet-latest",
      "max_tokens": 512,
      "messages": [
        { "role": "user", "content": "Turn these requirements into acceptance criteria." }
      ]
    }'
  ```

  Messages requests usually require `max_tokens`. If you copy an OpenAI `messages` shape to an Anthropic endpoint, also confirm that the fields match the client or upstream requirements.

  ## Gemini Native Endpoints

  Gemini native clients should use `/v1beta` paths and Gemini request body format. Non-streaming example:

  ```bash
  curl "${BASE_URL}v1beta/models/gemini-2.0-flash:generateContent" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "contents": [
        {
          "parts": [
            { "text": "Introduce {{SITE_NAME}} in one sentence." }
          ]
        }
      ]
    }'
  ```

  Streaming:

  ```bash
  curl -N "${BASE_URL}v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse" \
    -H "Authorization: Bearer $YOUR_KEY" \
    -H "Content-Type: application/json" \
    -d '{
      "contents": [
        {
          "parts": [
            { "text": "Stream a short outline." }
          ]
        }
      ]
    }'
  ```

  Gemini native endpoints do not use the OpenAI `messages` format. Use a Gemini model name or mapping name supported by the current deployment.

  ## CLI Coding Clients (Coding Agents)

  These command-line tools (such as Claude Code CLI, Codex CLI, and OpenCode CLI) run directly in your terminal. You can easily configure and switch their underlying relay services using environment variables.

  ### Claude Code CLI

  The official Claude Code client can be directed to this platform by setting the Anthropic API Base URL and key:

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}"
  export ANTHROPIC_API_KEY="$YOUR_KEY"
  ```

  Or for certain versions:

  ```bash
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  Once configured, run `claude` in your terminal to spin up the agent and interact with compatible models.

  If an admin provides a custom `/antigravity` route, you can change the Base URL to:

  ```bash
  export ANTHROPIC_BASE_URL="{{BASE_URL}}antigravity"
  export ANTHROPIC_AUTH_TOKEN="$YOUR_KEY"
  ```

  ### Codex CLI

  Codex CLI relies on the OpenAI-formatted Responses API. Set the following environment variables to route requests to this platform:

  ```bash
  export OPENAI_BASE_URL="{{BASE_URL}}v1"
  export OPENAI_API_KEY="$YOUR_KEY"
  ```

  **Important Note**: If you use Nginx to reverse-proxy this platform, ensure that Nginx includes the following configuration in its `http` block:
  ```nginx
  underscores_in_headers on;
  ```
  If this setting is missing, Nginx drops headers containing underscores (like `session_id`), causing sticky session functionality to fail in multi-account environments.

  ### OpenCode CLI

  OpenCode CLI connects to the OpenCode2API channel. Configure the endpoints and key as follows:

  ```bash
  export OPENCODE_API_BASE="{{BASE_URL}}v1"
  export OPENCODE_API_KEY="$YOUR_KEY"
  ```

  If using a generic compatible client, simply point the OpenAI SDK `OPENAI_BASE_URL` and `OPENAI_API_KEY` to the `/v1` path of this platform.

  ## Third-Party GUI & Integrated Tools

  These graphical tools help save your API keys and let you manage, configure, and switch between different underlying AI clients from a single workspace.

  ### CC-Switch: Client Switching & One-Click Import

  * **Client Management & Switching**: CC-Switch is a local utility that allows you to easily configure, manage, and switch between multiple clients (Claude, Gemini, Codex, etc.).
  * **One-Click Configuration**: To avoid modifying environment variables for each client manually, this platform integrates a one-click import button using the `ccswitch://` protocol. In the **"API Keys"** section, click **"Import to CC-Switch"** next to your API Key, select the target client type (Claude, Gemini, or Codex), and CC-Switch will automatically configure the endpoint and key for you.
  * **Manual Configuration Parameters**:
    * Claude Client: `app: "claude"`, `endpoint: "{{BASE_URL}}"`
    * Codex Client: `app: "codex"`, `endpoint: "{{BASE_URL}}v1"`, default model `gpt-5.4`
    * Gemini Client: `app: "gemini"`, `endpoint: "{{BASE_URL}}"`

  ### Cockpit: Centralized Management & Switching

  * **Centralized Dashboard**: Cockpit is a local agent workspace GUI that lets you monitor, manage, and switch between parallel CLI agent sessions (like Claude Code CLI, Codex CLI, OpenCode CLI, etc.).
  * **Configuration & Switching**: In Cockpit's settings panel, you can independently configure API credentials for different engines, allowing you to switch clients at will. Update the API Base URL (or Endpoint) settings as follows:
    * **Claude Engine**: Set Endpoint to `{{BASE_URL}}` and enter the platform API Key.
    * **Codex Engine**: Set Endpoint to `{{BASE_URL}}v1` and enter the platform API Key.
    * **OpenCode Engine**: Set Endpoint to `{{BASE_URL}}v1` and enter the platform API Key.

  ### Codex App

  For the Codex App desktop client, configure it via its settings panel:
  1. Replace the OpenAI API Base URL with: `{{BASE_URL}}v1`
  2. Enter the API Key generated by this platform.
  ```

- [ ] **Step 2: Verify changes via git diff**

  Run: `git diff frontend/src/docs/en/clients.md`
  Expected: Diff shows the reorganized structure and the new English sections.

- [ ] **Step 3: Commit**

  Run: `git add frontend/src/docs/en/clients.md && git commit -m "docs(en): enrich and reorganize clients integration guide"`

---

### Task 3: Update Configuration Snippets (`frontend/src/docs/{zh,en}/configuration-snippets.md`)

**Files:**
- Modify: `frontend/src/docs/zh/configuration-snippets.md`
- Modify: `frontend/src/docs/en/configuration-snippets.md`

- [ ] **Step 1: Append OpenCode CLI snippet to Chinese config snippets**

  Add the OpenCode CLI configuration to the end of `frontend/src/docs/zh/configuration-snippets.md`:

  ```markdown
  ## OpenCode CLI 环境配置

  ```bash
  export OPENCODE_API_BASE="{{BASE_URL}}v1"
  export OPENCODE_API_KEY="$YOUR_KEY"
  ```
  ```

- [ ] **Step 2: Append OpenCode CLI snippet to English config snippets**

  Add the OpenCode CLI configuration to the end of `frontend/src/docs/en/configuration-snippets.md`:

  ```markdown
  ## OpenCode CLI Environment Configuration

  ```bash
  export OPENCODE_API_BASE="{{BASE_URL}}v1"
  export OPENCODE_API_KEY="$YOUR_KEY"
  ```
  ```

- [ ] **Step 3: Commit changes**

  Run: `git add frontend/src/docs/zh/configuration-snippets.md frontend/src/docs/en/configuration-snippets.md && git commit -m "docs: append OpenCode CLI snippet to config templates"`

---

### Task 4: Compilation and Verification

**Files:**
- Test: Build frontend compiler and check frontend lint.

- [ ] **Step 1: Check typescript compile and lint in frontend**

  Run: `pnpm --filter sub2api-frontend typecheck` (or `vue-tsc -b` in frontend directory)
  Expected: Compile successfully with no typescript errors.

  Run: `pnpm --filter sub2api-frontend lint:check` (or `eslint` in frontend directory)
  Expected: Lint succeeds or only warnings.

- [ ] **Step 2: Run frontend build to verify complete Vite production build**

  Run: `pnpm --filter sub2api-frontend build`
  Expected: Vite builds production files successfully into `dist/`.

- [ ] **Step 3: Run git status**

  Run: `git status`
  Expected: Working tree is clean.

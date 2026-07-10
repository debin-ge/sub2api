# Third-Party Tools

When you frequently switch between multiple sites, API keys, or AI tools, manually editing configuration files is slow and error-prone. This page introduces tools that can manage those configurations for you.

> The tools below are independent open-source community projects, not official {{SITE_NAME}} products. Review their source code, permissions, and storage behavior before use.

## Prerequisites

Complete [Integration Overview](/docs/integration-overview) first so you have a Base URL (`{{BASE_URL}}`) and API Key.

## CC-Switch

```client
name: CC-Switch
logo: anthropic
protocols: [anthropic, openai, gemini]
homepage: https://github.com/farion1231/cc-switch
```

**What it is**: a cross-platform desktop app for macOS, Windows, and Linux. It manages provider configurations for Claude Code, Codex, Gemini CLI, OpenCode, and similar AI coding tools, then switches between sites without hand-editing JSON or TOML files.

**How it works**: CC-Switch rewrites the actual configuration files used by each tool, such as `~/.claude/settings.json` and `~/.codex/config.toml`, and syncs changes made outside the app back into its own data directory, `~/.cc-switch/`.

> Because it controls those config files, **do not edit the same files manually while CC-Switch manages them**. After import, make switching and changes inside CC-Switch.

**Step 1: Install CC-Switch.** Download the installer for your operating system from its GitHub Releases page. Use `.dmg` on macOS and `.msi` or `.exe` on Windows.

**Step 2 (recommended): Import from {{SITE_NAME}}.** {{SITE_NAME}} has a built-in shortcut import for CC-Switch:

1. Log in to {{SITE_NAME}} and open **API Keys**.
2. Find the key you want to use and click **Import to CC-Switch**.
3. Choose the client type, such as Claude Code or Gemini CLI.
4. Allow the browser to open CC-Switch through the `ccswitch://` link.
5. Confirm the import in CC-Switch. The site name, address, key, and usage script are filled automatically.

**Expected result**: the provider list in CC-Switch contains a {{SITE_NAME}} entry.

If the browser does nothing or says the link cannot be opened, CC-Switch is not installed or the protocol handler is not registered. Install CC-Switch, open it once manually, then try again.

**Step 2 (fallback): Add manually.** In CC-Switch, click **Add Provider**, choose the app type, and enter:

- Name: any recognizable name, for example `{{SITE_NAME}}`
- Endpoint / Base URL: use `{{BASE_URL}}` for Claude Code and Codex; use `{{BASE_URL}}antigravity` for Antigravity channels
- API Key: `${API_KEY}`

**Step 3: Enable the provider.** Click **Enable** for the entry. Most tools need a new terminal window before reading the updated config.

**Verification**: reopen your terminal and run `claude "hello"` or the equivalent tool command. A normal response means the switch worked.

## Cockpit Tools

```client
name: Cockpit Tools
logo: antigravity
protocols: [anthropic, openai, gemini]
homepage: https://github.com/jlcodes99/cockpit-tools
```

**What it is**: a Tauri desktop app for macOS, Windows, and Linux. It focuses on AI IDE account management and supports many platforms such as Antigravity IDE, Codex, Cursor, Windsurf, Trae, Zed, Gemini CLI, and GitHub Copilot. Its features include one-click account switching, parallel multi-account instances, quota monitoring, and scheduled tasks.

**How it relates to {{SITE_NAME}}**: Cockpit Tools is not an API relay service. It is an account and configuration manager. For {{SITE_NAME}} users, the useful pattern is to add the tool configuration that points to {{SITE_NAME}} as one account entry, then switch it alongside other accounts.

**Suggested use**:

1. Download the installer from the project's GitHub Releases page.
2. Add account entries for the tools you use, such as Codex or Antigravity IDE.
3. When a tool requires a custom service address, use the Base URL and API Key from this page. Field meanings match [Client Integration](/docs/integration-clients) and [CLI Integration](/docs/integration-cli).

> Cockpit Tools changes frequently. Treat its repository README as the source of truth for exact UI steps.

## API Key Safety

An API Key is effectively a key to your quota:

| Recommendation | Details |
| --- | --- |
| Use least privilege | Create a separate key for third-party tools instead of reusing a production key. |
| Check storage | Tools may store keys in local files or their own directories such as `~/.cc-switch/`. Use disk encryption and screen lock on the device. |
| Rotate regularly | Delete old keys and create new ones from the **API Keys** page, then update the tool. |
| Respond to leaks | If a key may be exposed, disable or delete it immediately, then review usage records in [Billing and Usage](/docs/billing-usage). |

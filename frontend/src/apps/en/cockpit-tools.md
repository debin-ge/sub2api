# Cockpit Tools

> Universal AI IDE account manager (macOS / Windows / Linux, built on Tauri). Broader scope than CC-Switch: covers ~15 AI IDE / desktop tools including **Antigravity IDE, Codex, Cursor, Windsurf, Trae, Zed, Gemini CLI, GitHub Copilot** — with account switching, parallel multi-instances, quota monitoring, and scheduled tasks.

```client
name: Cockpit Tools
logo: antigravity
protocols: [anthropic, openai, gemini]
homepage: https://github.com/jlcodes99/cockpit-tools
```

## How it compares to CC-Switch

| | CC-Switch | Cockpit Tools |
| --- | --- | --- |
| Tools covered | Claude Code / Codex / Gemini CLI / OpenCode (4 CLIs) | ~15 (IDEs + CLIs + Copilot etc.) |
| Focus | Provider / relay-site switching | AI IDE account management + multi-instance |
| Data model | Multiple Providers per tool | Multiple Accounts per tool |
| Standout features | Simple, focused on key+URL | Parallel instances, quota monitoring, scheduled tasks |

> For {{SITE_NAME}} users: if you only use Claude Code / Codex CLI, CC-Switch is enough. If you also use Antigravity IDE / Cursor / Windsurf-style desktop apps, or need to **run multiple instances at once** (e.g. two Antigravity windows on different accounts), Cockpit Tools fits better.

## 1. Install

Download the installer for your OS from [GitHub Releases](https://github.com/jlcodes99/cockpit-tools/releases):

- **macOS**: `.dmg` (separate builds for Apple Silicon / Intel)
- **Windows**: `.msi` or `.exe`
- **Linux**: `.AppImage` or `.deb`

On first launch it asks for "access to private folders" permission (it needs to read/write each IDE's config files) — allow it.

## 2. First-time setup

The main window's left panel lists every AI IDE / tool it recognizes. **Grey** = not installed on this machine; **coloured** = manageable.

**Step 1**: Pick the tool you want to manage (e.g. Antigravity IDE or Codex).

**Step 2**: Click **Add account** in the top right — the account editor opens.

**Step 3**: Fill fields — names differ slightly per tool, but three are always present:

| Field | Value |
| --- | --- |
| **Account name** | Anything, for identification. e.g. `{{SITE_NAME}} main`, `Official direct` |
| **Base URL / Endpoint** (custom service URL) | For {{SITE_NAME}}: `{{BASE_URL}}`; for the Antigravity channel: `{{BASE_URL}}antigravity` |
| **API Key / Token** | Your `sk-` key |

**Step 4**: Save. The account appears under that tool's list, disabled by default.

<details>
<summary>Field names by tool</summary>

| Tool | Endpoint field | Key field |
| --- | --- | --- |
| Antigravity IDE | Base URL | API Key |
| Codex | model_providers.<name>.base_url | env: API_KEY |
| Cursor | OpenAI Base URL | OpenAI API Key |
| Trae | Custom Provider URL | API Key |
| Zed | assistant.provider.api_url | api_key |
| Gemini CLI | GEMINI_API_BASE | GEMINI_API_KEY |
| GitHub Copilot | Special: manages an OAuth token, not a URL | GitHub OAuth |

For {{SITE_NAME}} users: Base URL is always `{{BASE_URL}}`; the key goes in whatever field the tool names.

</details>

## 3. Switch accounts

Click **Enable** on an account row:

1. Cockpit Tools backs up the tool's current config to its own database
2. Writes the new account's URL / Key into the tool's real config file
3. If the tool supports hot switching (Claude Code), it takes effect immediately; otherwise reopen that IDE or terminal window

**Verify**: reopen the target tool, send a chat message, get a reply → success. For extra rigor, check the "About" or "Settings → Account" page in the tool to confirm the current login info.

## 4. Parallel instances (Cockpit Tools' signature feature)

Some AI IDEs (especially Antigravity-style ones) can't run two accounts at once because they store the account in a global config. Cockpit Tools works around this via **sandbox directories** for parallel multi-open:

**Step 1**: Click **Launch instance** on an account (different from plain **Enable**).

**Step 2**: Cockpit Tools will:
- Copy the IDE to a temporary sandbox (slow on first run)
- Inject the current account's config into the sandbox
- Launch the isolated instance

**Step 3**: To open another instance with a different account, repeat on that other account. The two configs don't interfere.

<details>
<summary>When to use "Launch instance" vs "Enable"</summary>

- **Enable**: mutates the main config; subsequent direct-launched windows use this account. Fits day-to-day single-account use.
- **Launch instance**: **temporarily** injects config for this one launch; main config untouched. Fits:
  - Comparing outputs from two accounts side by side
  - Running a burner task on a backup account without polluting the main account's history
  - Demoing / screen-sharing without exposing the main account

</details>

## 5. Quota monitoring

You can bind a "usage script" to each account so Cockpit Tools polls remaining quota on a schedule and shows it in the UI:

1. Edit the account, find **Usage query** or **Usage Script**
2. Paste the {{SITE_NAME}} usage-query API (there's a copy button on the API Keys page), or pick the `{{SITE_NAME}}` preset template if Cockpit Tools lists one
3. Save — live balance shows in the account list

<details>
<summary>Usage script example</summary>

If Cockpit Tools has no preset for {{SITE_NAME}}, hand-write a curl:

```bash
curl -H "Authorization: Bearer $KEY" {{BASE_URL}}api/user/usage
```

The actual endpoint should come from the copy button on the {{SITE_NAME}} API Keys page.

</details>

## 6. Data storage and backup

Cockpit Tools stores its data (accounts, sandbox instances, usage history) at:

- **macOS**: `~/Library/Application Support/CockpitTools/`
- **Windows**: `%APPDATA%\CockpitTools\`
- **Linux**: `~/.config/CockpitTools/`

Back up that directory to migrate to another machine. Keys live in a local database — either plaintext or with tool-specific encryption depending on the version. **Only restore on a machine you trust.**

## 7. Troubleshooting

<details>
<summary>Launching an instance fails / disk usage spikes</summary>

- The first instance launch copies the entire target IDE into a sandbox — hundreds of MB to over a GB. Low disk space = failure.
- Delete unused sandboxes via **Clean sandbox** in Cockpit Tools to reclaim space.

</details>

<details>
<summary>After switching, the IDE still uses the old account</summary>

- The IDE wasn't fully quit: dismiss the tray icon and the Dock entry, then relaunch.
- The tool uses OS keychain / credential storage (e.g. GitHub Copilot OAuth token): Cockpit Tools can only edit config files, not keychain entries. Sign out and back in from within the IDE.

</details>

<details>
<summary>Key security notes</summary>

- Create a dedicated key for Cockpit Tools — don't reuse production keys.
- The tool persists keys locally; make sure the device itself has disk encryption and screen lock.
- Suspected leak: go to the {{SITE_NAME}} **API Keys** page and delete or disable the key immediately.

</details>

> Cockpit Tools has many features and updates often; the definitive reference is the repo README. The sections above cover the core scenarios most {{SITE_NAME}} users care about.

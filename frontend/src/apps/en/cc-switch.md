# CC-Switch

> Cross-platform desktop account manager (macOS / Windows / Linux). A single GUI to maintain **multiple** provider configs for Claude Code / Codex / Gemini CLI / OpenCode and switch between them in one click — no hand-editing of JSON/TOML.

```client
name: CC-Switch
logo: anthropic
protocols: [anthropic, openai, gemini]
homepage: https://github.com/farion1231/cc-switch
```

## What problem it solves

If you juggle several relay providers or several keys ({{SITE_NAME}}, direct-official, a backup site, etc.), the manual approach is: open `~/.claude/settings.json` or `~/.codex/config.toml` every time and rewrite URL + key by hand. Easy to mistype and easy to break JSON syntax.

CC-Switch **takes over** those config files. You maintain multiple **Provider** entries in its GUI, and switching rewrites the target tool's real config file automatically.

> ⚠️ **It owns the file itself**. Once CC-Switch is managing a tool, don't hand-edit `~/.claude/settings.json` alongside it, or the two will overwrite each other.

## 1. Install

Download the installer for your OS from [GitHub Releases](https://github.com/farion1231/cc-switch/releases):

- **macOS**: `.dmg` (pick `arm64` for Apple Silicon, `x64` for Intel)
- **Windows**: `.msi` or `.exe`
- **Linux**: `.AppImage` or `.deb`

Launch it once so the OS registers the `ccswitch://` URL scheme (one-click import needs it).

## 2. Configure: one-click import from {{SITE_NAME}} (recommended)

{{SITE_NAME}} ships a CC-Switch quick-import so you don't fill fields by hand:

1. Sign into {{SITE_NAME}}, open **API Keys** in the left menu.
2. On the row for the key you want, click **Import to CC-Switch**.
3. Pick the target client (**Claude Code** / **Codex** / **Gemini CLI** / **OpenCode**).
4. The browser asks to open a `ccswitch://` link — click **Allow**.
5. CC-Switch pops up a confirmation — click **Import**. Site name, endpoint, key, and usage script are filled in automatically.

**Expected result**: a new {{SITE_NAME}} entry appears in the CC-Switch provider list, disabled by default.

<details>
<summary>Browser says "cannot open ccswitch:// link"</summary>

- Confirm **Step 1**: CC-Switch is installed and has been **launched at least once** (that's when the protocol registers).
- macOS Safari sometimes blocks external protocols — try Chrome or Edge.
- Still stuck → use manual add below.

</details>

## 3. Configure: manual add

Skip one-click and add any provider by hand:

1. Open CC-Switch, pick the target tool (Claude Code / Codex / Gemini CLI / OpenCode) in the left tabs.
2. Click **Add** or **+** in the top right.
3. Fill:

| Field | Value |
| --- | --- |
| **Name** | Anything, for identification. e.g. `{{SITE_NAME}} main`, `{{SITE_NAME}} backup` |
| **Endpoint / Base URL** | Claude Code and Codex: `{{BASE_URL}}`; Antigravity channel: `{{BASE_URL}}antigravity` |
| **API Key** | Paste your `sk-` key |
| **Notes** (optional) | e.g. "daily", "big-context only" |

4. Save.

> Codex entries have a slightly different Endpoint shape (no manual `v1`) — CC-Switch generates the correct `config.toml` per tool type. Copy the table above verbatim.

## 4. Switch and verify

Click **Enable** (star / check button) on a provider row. CC-Switch immediately rewrites that tool's real config file.

**Expected**:

| Tool | Required action |
| --- | --- |
| **Claude Code** | Hot-switch works; the next request in an already-running session goes to the new config |
| **Codex CLI / OpenCode / Gemini CLI** | **Open a fresh terminal window** (old windows read the env they were spawned with) |
| **IDE extensions** (Claude Code / Codex inside VS Code / JetBrains) | Quit the IDE fully and relaunch |

Verify: open a new terminal, run the tool (`claude "hello"` or `codex "hello"`), you get a reply. For extra rigor: `echo $ANTHROPIC_BASE_URL` (Claude Code) or `cat ~/.codex/config.toml` (Codex) should show the latest values.

## 5. Manage multiple providers

Multiple entries is what CC-Switch is for:

- **Add many**: one entry per site/key, distinct names.
- **Only one enabled per tool at a time**.
- **Fast switch**: on switch, it also syncs the previously-enabled entry's live values back into its own database (preserving any tweaks you made in the external tool), then writes the newly-enabled entry.
- **Export / backup**: CC-Switch stores its own data at `~/.cc-switch/` (macOS/Linux) or `%APPDATA%\cc-switch\` (Windows). Back up that directory to migrate to a new machine.

<details>
<summary>Real config-file locations per tool</summary>

CC-Switch reads and writes these; don't hand-edit while it's managing the tool:

| Tool | Config file |
| --- | --- |
| Claude Code | `~/.claude/settings.json` |
| Codex | `~/.codex/config.toml` + `~/.codex/auth.json` (key stored separately) |
| Gemini CLI | `~/.gemini/settings.json` |
| OpenCode | `~/.opencode/config.json` |

On Windows replace `~/` with `%USERPROFILE%\`.

</details>

<details>
<summary>Antigravity channel configuration</summary>

Antigravity is an Anthropic-protocol variant with an extra `antigravity` suffix in the Base URL:

- Manual add: Endpoint = `{{BASE_URL}}antigravity`
- One-click import: {{SITE_NAME}} writes the correct suffix based on your group
- **Anthropic and Antigravity channels cannot share a session context** — end the current conversation before switching between them.

</details>

<details>
<summary>Troubleshooting</summary>

- **Still hits the old endpoint after switching** — the target tool wasn't restarted properly. Claude Code needs a new session; Codex / Gemini need a fresh terminal.
- **My edits got swallowed** — you hand-edited `settings.json` but CC-Switch overwrote it on the next switch from its stored copy. Fix: make all edits in CC-Switch, or "sync current config back to CC-Switch" first (if your version supports it).
- **Uninstall / stop using CC-Switch** — disable/delete all entries inside it, then remove the `~/.cc-switch/` data directory. The tool's real config file will retain whatever was last enabled; edit manually from there.
- **Nginx reverse proxy**: if {{SITE_NAME}} sits behind Nginx, the admin needs `underscores_in_headers on;` in the `http` block, or the sticky-session `session_id` header is stripped. CC-Switch can't help here — it's a server-side setting.

</details>

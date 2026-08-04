# Hermes Agent

> A general-purpose agent from Nous Research. Connect it to {{SITE_NAME}} through a custom OpenAI-compatible endpoint; GPT, Claude, Gemini, and other models share the same Chat Completions setup.

```client
name: Hermes Agent
logo: app
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.hermes/config.yaml
homepage: https://hermes-agent.nousresearch.com
```

## 1. Install

On Linux, macOS, or WSL2, use the official installer:

```bash
curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash
```

Open a new terminal after installation and run:

```bash
hermes --help
```

Seeing the command help confirms the installation. Native Windows users can also install Hermes Desktop; WSL2 is recommended for CLI use.

## 2. Configure the custom endpoint

The recommended method is the interactive wizard:

```bash
hermes model
```

Enter these values:

| Item | Value |
| --- | --- |
| Provider | **Custom endpoint (self-hosted / VLLM / etc.)** |
| API Base URL | `{{BASE_URL}}v1` |
| API Key | Your `sk-` key |
| Model | An actual model ID from `{{BASE_URL}}v1/models` |

The wizard saves the result to `~/.hermes/config.yaml`. You can also create it manually:

```yaml download=config.yaml
model:
  provider: custom
  default: claude-sonnet-4-6
  base_url: {{BASE_URL}}v1
```

Then put the key in `~/.hermes/.env`:

```dotenv
OPENAI_API_KEY=paste your sk- key here
```

- `base_url` must end in `/v1`; Hermes appends `/chat/completions`.
- The example selects a Claude model over the OpenAI-compatible Chat Completions protocol. GPT, Gemini, DeepSeek, and other compatible models work the same way.
- The model ID must exactly match a value returned by `{{BASE_URL}}v1/models`.
- Never commit the `.env` file containing your key to Git.

## 3. Verify

Start Hermes:

```bash
hermes
```

Send a task that exercises tools, for example:

```text
List the files in the current directory and summarize the project's main languages.
```

A normal response with terminal tool calls means the integration works. Use `/model` inside a session to switch between already-configured models. Exit and run `hermes model` again to add or change an endpoint.

<details>
<summary>Troubleshooting</summary>

- **401** — the API key is invalid or disabled, or `~/.hermes/.env` was not loaded.
- **404** — `base_url` usually lacks `/v1` or contains a duplicated `/v1/v1`.
- **Model unavailable** — `default` must be an exact model ID from `/v1/models`, and the key's group must have access.
- **Context window error at startup** — Hermes agent tools need a large context window; select a model with at least 64K context.
- **Configuration appears ignored** — run `hermes doctor`, then use `hermes model` to save the endpoint again.
- **Tool calling fails** — validate `/v1/chat/completions` with the [Code examples](/apps/code) and confirm that the model supports function/tool calls.

</details>

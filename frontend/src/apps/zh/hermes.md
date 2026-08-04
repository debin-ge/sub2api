# Hermes Agent

> Nous Research 推出的通用智能体，可通过 OpenAI-compatible 自定义端点接入 {{SITE_NAME}}。GPT、Claude、Gemini 等模型都使用同一套 Chat Completions 配置。

```client
name: Hermes Agent
logo: app
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.hermes/config.yaml
homepage: https://hermes-agent.nousresearch.com
```

## 1. 安装

Linux、macOS 或 WSL2 使用官方安装脚本：

```bash
curl -fsSL https://hermes-agent.nousresearch.com/install.sh | bash
```

安装完成后重新打开终端，运行：

```bash
hermes --help
```

能看到命令帮助即安装成功。Windows 原生环境也可以安装 Hermes Desktop；CLI 场景更推荐使用 WSL2。

## 2. 配置自定义端点

推荐运行交互式配置：

```bash
hermes model
```

依次填写：

| 项目 | 值 |
| --- | --- |
| Provider | **Custom endpoint (self-hosted / VLLM / etc.)** |
| API Base URL | `{{BASE_URL}}v1` |
| API Key | 你的 `sk-` 密钥 |
| Model | `{{BASE_URL}}v1/models` 中的实际模型 ID |

配置会保存到 `~/.hermes/config.yaml`。也可以手动创建：

```yaml download=config.yaml
model:
  provider: custom
  default: claude-sonnet-4-6
  base_url: {{BASE_URL}}v1
```

然后将密钥写入 `~/.hermes/.env`：

```dotenv
OPENAI_API_KEY=在此粘贴你的 sk- 密钥
```

- `base_url` 需要以 `/v1` 结束，Hermes 会继续调用 `/chat/completions`。
- 示例使用 Claude 模型，但走的是 OpenAI-compatible Chat Completions 协议；也可以换成 GPT、Gemini、DeepSeek 等模型。
- 模型 ID 必须与 `{{BASE_URL}}v1/models` 返回值完全一致。
- 不要把包含密钥的 `.env` 提交到 Git。

## 3. 验证

启动 Hermes：

```bash
hermes
```

输入一个需要工具调用的任务，例如：

```text
列出当前目录中的文件，并总结这个项目使用的主要语言。
```

能正常回复并调用终端工具即表示接入成功。会话中输入 `/model` 可以切换已经配置过的模型；添加或修改端点需退出会话后重新运行 `hermes model`。

<details>
<summary>失败时看这里</summary>

- **401** — API Key 无效、被禁用，或 `~/.hermes/.env` 没有被加载。
- **404** — `base_url` 通常缺少 `/v1`，或被写成了重复的 `/v1/v1`。
- **模型不可用** — `default` 必须填写 `/v1/models` 返回的真实模型 ID，并确认密钥所属分组有权限。
- **启动时提示上下文不足** — Hermes 的智能体工具需要较大的上下文窗口，请换用至少 64K 上下文的模型。
- **配置看起来没有生效** — 运行 `hermes doctor` 检查，再执行 `hermes model` 重新保存端点。
- **工具调用异常** — 先用 [代码示例](/apps/code) 验证 `/v1/chat/completions`，确认模型支持函数/工具调用。

</details>

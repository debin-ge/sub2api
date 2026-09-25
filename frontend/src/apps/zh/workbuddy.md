# WorkBuddy

> 腾讯推出的 AI 办公智能体，通过"自定义模型"接入 {{SITE_NAME}}。WorkBuddy 的自定义模型只走 OpenAI Chat Completions 协议，GPT、Claude、Gemini 等模型都用同一套配置。

```client
name: WorkBuddy
logo: app
protocols: [openai]
endpoint: {{BASE_URL}}v1
config: ~/.workbuddy/models.json
homepage: https://www.workbuddy.ai
```

## 1. 安装

到 [workbuddy.ai](https://www.workbuddy.ai) 下载桌面客户端（Windows / macOS），安装后登录。

## 2. 配置自定义模型

### 方式一：在设置界面添加（推荐）

打开 **设置 → 模型 → 添加模型**，提供商选择 **自定义 / Custom**，按下表填写：

| 项目 | 值 |
| --- | --- |
| URL | `{{BASE_URL}}v1` |
| API Key | 你的 `sk-` 密钥 |
| 模型名 | `{{BASE_URL}}v1/models` 中的实际模型 ID，例如 `gpt-5.5`、`claude-sonnet-4-6` |

在 **高级设置** 中按模型能力勾选：

- **工具调用**：建议开启。WorkBuddy 的文件处理、执行任务等能力依赖工具调用。
- **图片输入**、**推理模式**：只在模型本身支持时开启。这些开关只是告诉 WorkBuddy 可以尝试使用，不会让模型凭空具备对应能力。
- **自定义协议**：保持关闭。关闭时 WorkBuddy 会在 URL 后自动补全 `/chat/completions`。

保存后回到聊天界面，在模型选择器底部的 **自定义模型** 分组里切换到刚添加的模型。每个模型需要单独添加一次。

> 如果你习惯填写完整地址，也可以把 URL 写成 `{{BASE_URL}}v1/chat/completions`，同时 **开启** 自定义协议，WorkBuddy 会原样请求该地址。两种写法二选一，不要混用。

### 方式二：编辑 models.json

需要批量添加模型时，可以直接编辑配置文件：

| 系统 | 路径 |
| --- | --- |
| macOS / Linux | `~/.workbuddy/models.json` |
| Windows | `%USERPROFILE%\.workbuddy\models.json` |

```json download=models.json
{
  "models": [
    {
      "id": "gpt-5.5",
      "name": "GPT-5.5",
      "vendor": "{{SITE_NAME}}",
      "url": "{{BASE_URL}}v1/chat/completions",
      "apiKey": "在此粘贴你的 sk- 密钥",
      "supportsToolCall": true,
      "supportsImages": true,
      "supportsReasoning": true
    },
    {
      "id": "claude-sonnet-4-6",
      "name": "Claude Sonnet 4.6",
      "vendor": "{{SITE_NAME}}",
      "url": "{{BASE_URL}}v1/chat/completions",
      "apiKey": "在此粘贴你的 sk- 密钥",
      "supportsToolCall": true,
      "supportsImages": true
    }
  ],
  "availableModels": ["gpt-5.5", "claude-sonnet-4-6"]
}
```

- 文件里的 `url` 必须是完整的 `/v1/chat/completions` 地址，这一点与界面填写不同。
- `id` 必须与 `/v1/models` 返回值完全一致；`availableModels` 里要列出同样的 `id`，否则模型不会出现在下拉列表中。
- 示例模型仅作参考，请换成你的密钥所属分组实际可用的模型。
- 早期版本使用 `~/.codebuddy/models.json`，其中已有的配置升级后仍然有效，并可以在设置界面里查看和编辑。
- 该文件以明文保存密钥，不要提交到 Git 或发给他人。

保存文件后 WorkBuddy 会自动重新加载；如果没有生效，完全退出后重新打开。

## 3. 验证

在聊天界面选中刚添加的模型，发送一个需要工具的任务，例如：

```text
帮我在桌面新建一个 hello.txt，内容写"你好"。
```

能正常回复并完成操作即接入成功。之后可以在 [使用记录](/usage) 中看到对应的调用。

<details>
<summary>失败时看这里</summary>

- **401** — API Key 无效、已禁用或复制时带了空格。
- **404** — URL 路径拼接不对：界面方式填 `{{BASE_URL}}v1` 且关闭自定义协议；出现 `/chat/completions/chat/completions` 或缺少 `/v1` 时都会 404。
- **模型不存在 / 无可用账号** — 模型名必须来自 `/v1/models`，并确认密钥所属分组包含该模型。
- **模型不显示** — 确认已点保存且填写了模型名；使用 models.json 时检查 JSON 是否合法（少逗号、多逗号最常见），以及 `availableModels` 是否包含该 `id`。Windows 记事本保存时注意文件名不要变成 `models.json.txt`。
- **models.json 写对了仍不生效** — 社区反馈部分版本默认关闭了从文件加载自定义模型的功能。完全退出 WorkBuddy 后，打开 `~/.workbuddy/cache/acc-product-config-v3.json`（Windows：`%USERPROFILE%\.workbuddy\cache\acc-product-config-v3.json`），把 `"CustomModelsJSON"` 改为 `true`（没有该字段就在顶层对象里加上），保存后重新打开。这不是官方文档记载的做法，修改前请先备份；该文件属于缓存，客户端更新后可能被重置，失效时再检查一遍。也可以直接改用界面方式添加。
- **只会聊天、不执行任务** — 检查是否开启了工具调用，并换用支持函数调用的模型。可先用 [代码示例](/apps/code) 验证 `/v1/chat/completions` 的工具调用是否正常。
- **找不到入口** — WorkBuddy 迭代较快，不同版本的菜单名称可能不同，以 [官方文档](https://www.workbuddy.ai/docs/zh/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Model) 为准。

</details>

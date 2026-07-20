# 需求：合并 v0.1.161 后 i18n 本地改写文案的审查与恢复

- **需求发起背景**：2026-07-20 完成上游 v0.1.161 合并（merge commit `ff63738c7`）
- **优先级**：中（不影响功能运行，影响界面措辞与合规文案）
- **预计工作量**：审查约 0.5~1 天；实施视勾选数量而定

## 1. 背景

本仓库基于上游 [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) 二次开发。本次合并上游 v0.1.161 时，上游将 i18n 从单文件（`frontend/src/i18n/locales/zh.ts` / `en.ts`）重构为目录结构（`zh/`、`en/` 下按域拆分多文件）。合并采用了上游新结构，并把二开**新增**的文案全部迁入 `zh/local.ts`、`en/local.ts`——**新增 key 已逐一验证零丢失，不在本需求范围内**。

但二开对上游**既有** key 的**文案改写**（改的是值，不是新增 key）绝大部分未能保留：

| 语言 | 本地改写过的 key | 合并后保留本地文案 | 回退为上游文案 | 需审查合计 |
|---|---|---|---|---|
| zh | 245 | 7 | 238 | **238** |
| en | 239 | 7 | 232 | **232** |

回退原因是合并机制而非疏漏：`local.ts` 文件头声明的装配策略为"**如键重叠，以上游模块文案为准**"（见 `frontend/src/i18n/locales/merge.ts` 的 `mergeLocaleFallbacks`），`local.ts` 只补缺口、不覆盖上游同名 key。

涉及的历史版本（用于取证比对，均可 `git show <sha>:<path>` 查看）：

| 代号 | SHA | 含义 |
|---|---|---|
| BASE | `d7a6a4513` | 合并基点（分叉点） |
| LOCAL | `ae1a2b681` | 合并前的本地二开分支（单文件 zh.ts/en.ts，含全部本地文案） |
| UP | `19149ca19` | 上游 v0.1.161 |
| MERGE | `ff63738c7` | 合并结果（当前 main） |

## 2. 目标

1. 对附录对照表中的每一条（zh 238 + en 232）做出**保留上游 / 恢复本地**的决策并记录；
2. 将决策为"恢复本地"的条目落实到代码，恢复后的文案与 LOCAL 版本逐字一致（占位符、标点、空格含在内）；
3. 全部 i18n 测试通过，不引入 key 冲突或悬空引用。

## 3. 范围

**范围内**：附录表中列出的 key 的**文案值**修改；zh 与 en 的同一逻辑 key 应作为一组统一决策。

**范围外**（勿动）：
- 新增/删除 i18n key（新增 key 已确认零丢失）；
- `local.ts` 及 `merge.ts` 的装配逻辑；
- 任何组件代码、路由、业务逻辑；
- 表中未列出的 key。

## 4. 审查决策标准（建议）

逐条填写附录表最后一列"决策"（`保留上游` / `恢复本地`）：

- **优先恢复本地**的典型：品牌化/去上游痕迹措辞、法务合规文案（`legal.*`、`adminCompliance.*` 系列为二开定制的协议提示语）、二开功能特有的语境措辞（如 `usage.cyber`）;
- **优先保留上游**的典型：上游修正了错别字/更准确的表达、上游为新功能扩充的描述（本地旧文案未覆盖新功能语义）、含新增占位符参数的条目（见 5.3）；
- 拿不准的条目：默认保留上游，并在表中备注原因。

注意：表中"类型"分两种——绝大多数是**回退为旧上游文案**（当前文案 = 分叉点时的上游文案）；zh 与 en 各有 2 条是**被新上游文案覆盖**（上游 v0.1.161 自己也改了该条，当前是上游新文案）。后者恢复本地时更需谨慎，可能丢失上游的语义更新。

### 4.1 本轮审查结论

| 语言 | 恢复本地 | 保留上游 | 合计 |
|---|---:|---:|---:|
| zh | 32 | 206 | 238 |
| en | 30 | 202 | 232 |
| 合计 | **62** | **408** | **470** |

决策口径如下：

- `legal.*`、`adminCompliance.*` 恢复二开定制的法务与合规措辞；
- `home.footer.allRightsReserved`、`admin.settings.payment.easypayCustomMethodsHint` 恢复本地品牌化/去上游品牌措辞；
- zh 的 `usage.cyber` 与 `admin.accounts.oauth.grok.refreshTokenPlaceholder` 恢复本地术语；对应英文当前值已经与 LOCAL 一致，不在英文待审表中；
- 20 条占位符集合发生变化的记录全部保留上游；调用处已核对，继续保留 `{name}`、`{time}`、`{status}`、`{user}`、`{count}`、`{feeRate}`、`{total}` 等当前传参语义；
- `admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocksHint` 保留上游扩充后的 block JSON、动态 billing header 与预设语义；页脚虽然也是新上游文案，但属于品牌化例外，恢复本地；
- 其余措辞没有明确的二开品牌、法务或业务语境要求，按保守原则保留上游。

## 5. 实施规范

### 5.1 修改位置

**必须直接修改附录表"定义文件"列指向的上游模块文件**（`frontend/src/i18n/locales/zh/**.ts`、`en/**.ts`）。
**严禁写入 `local.ts`**——它在装配中优先级低于上游模块，同名 key 写入后不生效。

### 5.2 取本地原文案

表中"本地原文案"列即 LOCAL 版本原文（`⏎` 表示换行符，恢复时还原为真实换行）。如需核对上下文：

```bash
git show ae1a2b681:frontend/src/i18n/locales/zh.ts | less   # 搜索 key 的末段
```

### 5.3 占位符与插值

vue-i18n 的 `{param}` 占位符必须与组件传参一致。若"当前文案（上游）"含有而"本地原文案"没有的占位符（如 `admin.accounts.dataImportIgnoredFiles` 上游为 `已忽略 {count} 个非 JSON 文件`、本地为 `已忽略 {count} 个文件`；又如 `dataImportInvalidFile` 上游含 `{name}` 而本地无），说明上游改动伴随了组件传参变化或语义细化。此类条目**恢复本地前必须检查组件调用处**，确保不产生占位符缺失/多余；必要时以本地措辞风格改写但保留上游占位符集合。

### 5.4 zh / en 同步

同一逻辑 key 在 zh 表与 en 表通常各有一条，决策与实施须同组处理，避免中英文语义漂移。仅单语言存在的条目按该语言单独处理。

### 5.5 提交要求

- 独立分支 + 单独 PR，不夹带其他改动；
- commit message 建议：`i18n: restore forked copy overridden by v0.1.161 merge`；
- PR 描述中附勾选汇总（恢复 N 条 / 保留 M 条）。

## 6. 验收标准

1. 附录表每条均有决策记录（表格回填后随 PR 提交，或存档到本文档）；
2. 决策"恢复本地"的条目，运行时文案与 LOCAL 逐字一致（抽查 ≥10 条，含至少 1 条多行文案）；
3. 测试全绿：

```bash
cd frontend
npx vitest run src/i18n/__tests__   # 12 个测试文件须全部通过（含 key 冲突与全量 t() 引用校验）
npx vue-tsc --noEmit                # 类型检查零错误
```

4. `local.ts` 与 `merge.ts` 无改动（`git diff --stat` 中不得出现）；
5. 界面抽查：登录协议弹窗（`legal.loginAgreementPrompt.*`）、管理端合规确认（`adminCompliance.*`）、用量页（`usage.*`）三处渲染正常、占位符无裸露。

### 6.1 验收记录（2026-07-20）

- 470 条决策已全部回填：恢复本地 62 条，保留上游 408 条；
- 使用 TypeScript AST 对全部 470 条逐项核对定义文件、MERGE 当前值和 LOCAL 原值，缺失与偏差均为 0；实施后再次全量反向校验，62 条恢复项逐字等于 LOCAL、408 条保留项仍等于 MERGE；
- 逐字抽查覆盖以下 11 条：
  - `admin.accounts.oauth.grok.refreshTokenPlaceholder`（zh，多行文案）；
  - `admin.settings.payment.easypayCustomMethodsHint`（zh/en）；
  - `adminCompliance.legalNote`（zh/en）；
  - `legal.loginAgreementPrompt.dialogDescription`（zh/en）；
  - `home.footer.allRightsReserved`（zh/en）；
  - `usage.cyber`（zh）；
  - `legal.updatedAt`（zh）；
- `pnpm exec vitest run src/i18n/__tests__`：12 个测试文件、38 个测试全部通过；
- `pnpm exec vue-tsc --noEmit`：通过，零错误；
- 本地 Vite + mock API 界面抽查通过：登录协议弹窗、管理端合规确认、中文管理端用量类型下拉均显示恢复文案，未发现裸露占位符；
- `frontend/src/i18n/locales/{zh,en}/local.ts` 与 `frontend/src/i18n/locales/merge.ts` 无改动。

## 7. 附录：审查对照表与决策记录

说明：按定义文件分组排序；"当前文案（上游）"= MERGE/当前 main 上的实际生效文案；"本地原文案"= 合并前二开文案；`⏎` 为换行符；`\|` 为转义的竖线。决策列填 `保留上游` 或 `恢复本地`（可加备注）。

### zh 共 238 条

| # | key | 定义文件 | 类型 | 当前文案（上游） | 本地原文案 | 决策 |
|---|---|---|---|---|---|---|
| 1 | `admin.accounts.dataImportIgnoredFiles` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 已忽略 {count} 个非 JSON 文件 | 已忽略 {count} 个文件 | 保留上游 |
| 2 | `admin.accounts.dataImportInvalidFile` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 文件 {name} 不是受支持的导出数据文件 | 文件格式不正确 | 保留上游（保留当前占位符语义） |
| 3 | `admin.accounts.dataImportParseFailedFile` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 文件 {name} 解析失败 | 文件解析失败 | 保留上游（保留当前占位符语义） |
| 4 | `admin.accounts.gemini.oauthType.googleOneDesc` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 个人账号，享受 Google One 订阅配额 | Google One / AI Pro 账号授权 | 保留上游 |
| 5 | `admin.accounts.grok.apiKeyHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Grok 订阅支持使用 OAuth refresh token；API Key 账号不在本次范围内。 | Grok 订阅账号使用 OAuth Refresh Token；该账号类型不支持 API Key 创建。 | 保留上游 |
| 6 | `admin.accounts.grok.baseUrlHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Grok OAuth 账号会转发到官方 xAI API Base URL。 | Grok OAuth 账号将转发到官方 xAI API Base URL。 | 保留上游 |
| 7 | `admin.accounts.headerOverride.bulkDisableHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 保存后将关闭所选账号的请求头覆写并清空已有配置。 | 留空并提交可批量禁用自定义请求头。 | 保留上游 |
| 8 | `admin.accounts.headerOverride.bulkEmptyRows` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 请至少添加一行请求头再保存；如需清空已有配置，请关闭上方开关。 | 请至少添加一个请求头覆盖项。 | 保留上游 |
| 9 | `admin.accounts.headerOverride.bulkReplaceHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 保存后将用下方配置整体替换所选账号已有的请求头覆写配置。 | 批量编辑会替换所选账号当前的请求头覆盖配置。 | 保留上游 |
| 10 | `admin.accounts.headerOverride.emptyValueHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 值留空的行不会参与覆盖，仅作为待填写的占位。 | 值留空表示发送空字符串；删除行表示不覆盖该请求头。 | 保留上游 |
| 11 | `admin.accounts.headerOverride.hint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 转发时用配置值覆盖同名请求头（不区分大小写） | 为该账号转发请求时附加或覆盖指定 HTTP Header。 | 保留上游 |
| 12 | `admin.accounts.headerOverride.info` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 仅对本账号的出站请求生效：配置的请求头会在转发前覆盖客户端/网关生成的同名头。认证头（authorization、x-api-key）与连接控制头不允许覆写。 | 请求头名称不区分大小写，重复名称会以后面的配置为准。 | 保留上游 |
| 13 | `admin.accounts.headerOverride.namePlaceholder` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 请求头名称（如 user-agent） | 例如 Authorization | 保留上游 |
| 14 | `admin.accounts.headerOverride.title` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 请求头覆写 | 请求头覆盖 | 保留上游 |
| 15 | `admin.accounts.headerOverride.valuePlaceholder` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 覆写值（留空表示不覆写） | 请求头值 | 保留上游 |
| 16 | `admin.accounts.oauth.grok.authCodeDesc` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 授权完成后，粘贴 callback URL、查询字符串或授权码： | 授权完成后，当页面地址变为 http://localhost:xxx/auth/callback?code=... 时： | 保留上游 |
| 17 | `admin.accounts.oauth.grok.authCodeHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 支持完整 callback URL、查询字符串或裸 code。 | 您可以直接复制整个链接或仅复制 code 参数值，系统会自动识别 | 保留上游 |
| 18 | `admin.accounts.oauth.grok.authCodePlaceholder` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 粘贴完整 callback URL、?code=... 查询字符串或 code 值 | 方式1：复制完整的链接⏎(http://localhost:xxx/auth/callback?code=...)⏎方式2：仅复制 code 参数的值 | 保留上游 |
| 19 | `admin.accounts.oauth.grok.failedToValidateRT` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 验证 Grok refresh token 失败 | 验证 Grok Refresh Token 失败 | 保留上游 |
| 20 | `admin.accounts.oauth.grok.followSteps` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 请按照以下步骤授权您的 xAI/Grok 账号： | 请按照以下步骤完成 xAI/Grok 账户授权： | 保留上游 |
| 21 | `admin.accounts.oauth.grok.importantNotice` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 当浏览器跳转到本地 callback URL 后，请复制完整 URL 或 code 参数回填到这里。 | 重要提示：授权后页面可能会加载较长时间，请耐心等待。当浏览器地址栏变为 http://localhost... 开头时，表示授权已完成。 | 保留上游 |
| 22 | `admin.accounts.oauth.grok.openUrlDesc` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 在新标签页中打开授权链接，登录 xAI 并授权 API 访问。 | 请在新标签页中打开授权链接，登录您的 xAI 账户并授权。 | 保留上游 |
| 23 | `admin.accounts.oauth.grok.refreshTokenDesc` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 输入已有的 xAI refresh token，支持批量输入（每行一个）。 | 输入您已有的 xAI Refresh Token，支持批量输入（每行一个），系统将自动验证并创建账号。 | 保留上游 |
| 24 | `admin.accounts.oauth.grok.refreshTokenPlaceholder` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 粘贴您的 xAI refresh token...⏎支持多个，每行一个 | 粘贴您的 xAI Refresh Token...⏎支持多个，每行一个 | 恢复本地（术语一致，多行文案） |
| 25 | `admin.accounts.oauth.grok.title` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Grok 账号授权 | Grok 账户授权 | 保留上游 |
| 26 | `admin.accounts.oauth.openai.codexPatAuth` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Codex Personal Access Token | Codex PAT 授权 | 保留上游 |
| 27 | `admin.accounts.oauth.openai.codexPatDesc` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 输入 Codex at- Personal Access Token，系统会先调用 OpenAI whoami 校验后再创建账号。 | 导入 Codex Personal Access Token，用于 Codex 相关请求。 | 保留上游 |
| 28 | `admin.accounts.oauth.openai.codexPatHint` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 这是独立认证模式，不保存 refresh_token，也不会写入 OAuth access_token 过期时间。 | 每行一个 PAT，批量导入时会逐个创建账号。 | 保留上游 |
| 29 | `admin.accounts.oauth.openai.codexPatImportAndCreate` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 校验并创建 Codex PAT 账号 | 导入 PAT 并创建账号 | 保留上游 |
| 30 | `admin.accounts.oauth.openai.codexPatPlaceholder` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | at-... | 粘贴 Codex PAT，每行一个 | 保留上游 |
| 31 | `admin.accounts.openai.codexCLIOnlyAppServer` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 允许 Codex app-server 客户端 | 仅允许 Codex CLI / App Server 客户端 | 保留上游 |
| 32 | `admin.accounts.openai.codexCLIOnlyAppServerDesc` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 仅在上方开关开启时生效。开启后本账号额外放行内嵌 Codex 引擎、经 app-server 协议接入的第三方客户端（如 Claude Code 的 codex 插件），仍需通过全局引擎指纹门；与全局 app-server 开关取 OR（任一开即放行）。 | 启用后仅接受符合 Codex CLI 或 App Server 指纹的请求。 | 保留上游 |
| 33 | `admin.accounts.openai.wsModeHttpBridge` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | HTTP 桥接（http_bridge） | HTTP 桥接模式 | 保留上游 |
| 34 | `admin.accounts.openaiQuotaReset.collapseExpirations` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 收起重置次数到期时间 | 收起重置额度到期时间 | 保留上游 |
| 35 | `admin.accounts.openaiQuotaReset.confirmMessage` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 将消耗 1 次重置次数立即恢复当前窗口，剩余 {count} 次。此操作不可撤销，确定继续吗？ | 确认消耗 1 次重置额度？当前可用 {count} 次。 | 保留上游 |
| 36 | `admin.accounts.openaiQuotaReset.confirmTitle` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 确认重置周限 | 确认重置 OpenAI 额度 | 保留上游 |
| 37 | `admin.accounts.openaiQuotaReset.countTooltipLoad` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 点击查询剩余重置次数 | 查询可用重置次数 | 保留上游 |
| 38 | `admin.accounts.openaiQuotaReset.countTooltipRefresh` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 点击刷新剩余重置次数 | 刷新可用重置次数 | 保留上游 |
| 39 | `admin.accounts.openaiQuotaReset.expandExpirations` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 展开其余 {count} 张重置次数到期时间 | 展开 {count} 个到期时间 | 保留上游 |
| 40 | `admin.accounts.openaiQuotaReset.expirationDetails` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 重置次数到期明细 | 重置额度到期明细 | 保留上游 |
| 41 | `admin.accounts.openaiQuotaReset.expiresAt` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 到期 {time} | {time} 到期 | 保留上游 |
| 42 | `admin.accounts.openaiQuotaReset.expiresAtFull` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 重置次数到期时间: {time} | 到期时间：{time} | 保留上游 |
| 43 | `admin.accounts.openaiQuotaReset.noCreditsAvailable` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 没有可用的重置次数 | 没有可用的重置额度 | 保留上游 |
| 44 | `admin.accounts.openaiQuotaReset.resetSuccess` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 已重置 {windows} 个窗口 | 已重置额度窗口：{windows} | 保留上游 |
| 45 | `admin.accounts.openaiQuotaReset.resetTooltipNeedQuery` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 先点击「次数」加载剩余重置次数 | 请先查询可用重置次数 | 保留上游 |
| 46 | `admin.accounts.openaiQuotaReset.resetTooltipNoCredits` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 没有可用的重置次数 | 没有可用重置次数 | 保留上游 |
| 47 | `admin.accounts.openaiQuotaReset.resetTooltipReady` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 消耗 1 次重置次数以立即恢复当前窗口 | 消耗 1 次重置额度 | 保留上游 |
| 48 | `admin.accounts.openaiQuotaReset.resetTooltipShadow` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Spark 影子账号不能重置次数;请在母账号上重置 | 影子账号不支持直接重置，请在母账号操作 | 保留上游 |
| 49 | `admin.accounts.usageWindow.grokLastHeadersSeen` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 响应头 {time} | 最近响应头 | 保留上游（保留当前占位符语义） |
| 50 | `admin.accounts.usageWindow.grokLastProbe` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 探测 {time} | 最近探测 | 保留上游（保留当前占位符语义） |
| 51 | `admin.accounts.usageWindow.grokLastStatus` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 状态 {status} | 最近状态 | 保留上游（保留当前占位符语义） |
| 52 | `admin.accounts.usageWindow.grokNoHeaders` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 未观察到配额响应头 | 未检测到 Grok 配额响应头 | 保留上游 |
| 53 | `admin.accounts.usageWindow.grokProbe` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 探测 | 探测 Grok 配额 | 保留上游 |
| 54 | `admin.accounts.usageWindow.grokProbeTooltip` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 发送最小 xAI Responses 探测请求并读取配额响应头 | 发送一次轻量探测请求以刷新 Grok 配额响应头 | 保留上游 |
| 55 | `admin.accounts.usageWindow.grokRequests` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | 请求 | Grok 请求 | 保留上游 |
| 56 | `admin.accounts.usageWindow.grokResetUnsupportedTooltip` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | xAI 未向 Grok OAuth 账号开放重置额度接口 | Grok 当前不支持通过控制台重置额度窗口 | 保留上游 |
| 57 | `admin.accounts.usageWindow.grokRetryAfter` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | {time} 后重试 | Retry-After | 保留上游（保留当前占位符语义） |
| 58 | `admin.accounts.usageWindow.grokTokens` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Token | Grok Tokens | 保留上游 |
| 59 | `admin.accounts.usageWindow.grokUnknown` | frontend/src/i18n/locales/zh/admin/accounts.ts | 回退为旧上游文案 | Grok 配额需等待首次上游响应返回 xAI rate-limit 头后显示。 | Grok 配额未知 | 保留上游 |
| 60 | `admin.channelMonitor.form.jitterSeconds` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 随机抖动 (± 秒) | 抖动秒数 | 保留上游 |
| 61 | `admin.channelMonitor.form.jitterSecondsHint` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 每次检测在间隔基础上正负随机偏移该秒数，0 表示固定间隔；需满足 间隔 - 抖动 ≥ 15 秒 | 为监控调度增加随机抖动，避免同一时间集中请求。 | 保留上游 |
| 62 | `admin.channels.form.inclusive` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | （含） | 包含 | 保留上游 |
| 63 | `admin.channels.form.maxTokens` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 最大 | 最大 Tokens | 保留上游 |
| 64 | `admin.channels.form.minTokens` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 最小 | 最小 Tokens | 保留上游 |
| 65 | `admin.channels.mappingConflict` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 模型映射源 '{model1}' 和 '{model2}' 冲突：匹配范围重叠。源模式按大小写不敏感匹配，已有条目已覆盖其所有大小写变体。 | 模型映射源 '{model1}' 和 '{model2}' 冲突：匹配范围重叠 | 保留上游 |
| 66 | `admin.channels.modelConflict` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 模型模式 '{model1}' 和 '{model2}' 冲突：匹配范围重叠。模型名称按大小写不敏感匹配，已有条目已覆盖其所有大小写变体，无需重复添加。 | 模型模式 '{model1}' 和 '{model2}' 冲突：匹配范围重叠 | 保留上游 |
| 67 | `admin.riskControl.action.cyberPolicy` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 网络安全策略 | Cyber 策略 | 保留上游 |
| 68 | `admin.riskControl.cyberPolicyExcludeBan` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | cyber_policy 不计入封号次数 | 排除封禁 | 保留上游 |
| 69 | `admin.riskControl.cyberPolicyExcludeBanHint` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 开启后，cyber_policy 拦截不再计入自动封号的违规次数：当次不判定封号，历史累计亦排除。风控日志与通知邮件照常。 | 启用后，Cyber 策略命中不会触发封禁动作。 | 保留上游 |
| 70 | `admin.riskControl.defaultBlockMessage` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 内容审计命中风险规则，请调整输入后重试 | 请求因风险控制策略被拦截。 | 保留上游 |
| 71 | `admin.riskControl.violationNotCounted` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 未计入封号 | 未计入违规次数 | 保留上游 |
| 72 | `admin.subscriptions.guide.actions.revokeDesc` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 立即终止该用户的订阅，可在已撤销列表中恢复 | 立即终止该用户的订阅，不可恢复 | 保留上游 |
| 73 | `admin.subscriptions.restoreConfirm` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 确定要恢复 '{user}' 的订阅吗？如果原订阅已过期，恢复后将显示为已过期。 | 确认恢复该订阅？ | 保留上游（保留当前占位符语义） |
| 74 | `admin.subscriptions.revokeConfirm` | frontend/src/i18n/locales/zh/admin/channels.ts | 回退为旧上游文案 | 确定要撤销 '{user}' 的订阅吗？可稍后在已撤销列表中恢复。 | 确定要撤销 '{user}' 的订阅吗？此操作无法撤销。 | 保留上游 |
| 75 | `admin.ops.systemLogs.caller` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 调用方 | 记录调用方 | 保留上游 |
| 76 | `admin.ops.systemLogs.cleanCurrentFilters` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 清理当前筛选结果 | 清理当前筛选 | 保留上游 |
| 77 | `admin.ops.systemLogs.cleanupConfirm` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 确定要清理匹配当前筛选条件的系统日志吗？此操作不可撤销。 | 确认清理当前筛选条件匹配的系统日志？ | 保留上游 |
| 78 | `admin.ops.systemLogs.cleanupSuccess` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 清理完成，已删除 {count} 条日志。 | 系统日志已清理 | 保留上游（保留当前占位符语义） |
| 79 | `admin.ops.systemLogs.componentPlaceholder` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 例如 http.access | 例如 gateway | 保留上游 |
| 80 | `admin.ops.systemLogs.description` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 优先显示最新日志，可按条件筛选、搜索和清理。 | 查看系统运行日志、写入健康状态和运行时日志配置。 | 保留上游 |
| 81 | `admin.ops.systemLogs.dropped` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 已丢弃 | 丢弃 | 保留上游 |
| 82 | `admin.ops.systemLogs.endTime` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 结束时间（可选） | 结束时间 | 保留上游 |
| 83 | `admin.ops.systemLogs.failed` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 写入失败 | 失败 | 保留上游 |
| 84 | `admin.ops.systemLogs.keyId` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | KEY ID | Key ID | 保留上游 |
| 85 | `admin.ops.systemLogs.keywordPlaceholder` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | message/request_id | 搜索消息、错误或请求 ID | 保留上游 |
| 86 | `admin.ops.systemLogs.resetDefaults` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 重置默认值 | 恢复默认 | 保留上游 |
| 87 | `admin.ops.systemLogs.resetRuntimeConfigConfirm` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 确定要重置为启动配置（env/yaml）并立即应用吗？ | 确认恢复系统日志运行配置为默认值？ | 保留上游 |
| 88 | `admin.ops.systemLogs.runtimeConfig` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 运行时日志配置（立即生效） | 运行时配置 | 保留上游 |
| 89 | `admin.ops.systemLogs.runtimeConfigActive` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 运行时日志配置已生效 | 系统日志运行配置已生效 | 保留上游 |
| 90 | `admin.ops.systemLogs.runtimeConfigReset` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 已重置为启动日志配置 | 系统日志运行配置已恢复默认 | 保留上游 |
| 91 | `admin.ops.systemLogs.runtimeConfigResetFailed` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 重置日志配置失败 | 恢复系统日志运行配置失败 | 保留上游 |
| 92 | `admin.ops.systemLogs.runtimeConfigSaveFailed` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 保存日志配置失败 | 保存系统日志运行配置失败 | 保留上游 |
| 93 | `admin.ops.systemLogs.sampling` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 采样 | 启用采样 | 保留上游 |
| 94 | `admin.ops.systemLogs.samplingInitial` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 采样初始条数 | 初始保留条数 | 保留上游 |
| 95 | `admin.ops.systemLogs.search` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 搜索 | 搜索日志 | 保留上游 |
| 96 | `admin.ops.systemLogs.startTime` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 开始时间（可选） | 开始时间 | 保留上游 |
| 97 | `admin.ops.systemLogs.written` | frontend/src/i18n/locales/zh/admin/ops.ts | 回退为旧上游文案 | 已写入 | 写入 | 保留上游 |
| 98 | `admin.users.apiKeyGroupDisabled` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 已禁用分组 | 已禁用 | 保留上游 |
| 99 | `admin.users.apiKeyGroupExclusive` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 专用分组 | 独享 | 保留上游 |
| 100 | `admin.users.apiKeyGroupFilter` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | API Key 分组 | API Key 分组筛选 | 保留上游 |
| 101 | `admin.users.apiKeyGroupPublic` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 公开分组 | 公开 | 保留上游 |
| 102 | `admin.users.apiKeyGroupSubscription` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 订阅分组 | 订阅 | 保留上游 |
| 103 | `admin.users.authorizedGroupFilter` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 授权分组 | 授权分组筛选 | 保留上游 |
| 104 | `admin.users.searchApiKeyGroups` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 搜索 API Key 分组... | 搜索 API Key 分组 | 保留上游 |
| 105 | `admin.users.searchAuthorizedGroups` | frontend/src/i18n/locales/zh/admin/overview.ts | 回退为旧上游文案 | 搜索授权分组... | 搜索授权分组 | 保留上游 |
| 106 | `admin.usage.cacheCreationCost` | frontend/src/i18n/locales/zh/admin/resources.ts | 回退为旧上游文案 | 缓存创建费用 | 缓存创建成本 | 保留上游 |
| 107 | `admin.usage.cacheReadCost` | frontend/src/i18n/locales/zh/admin/resources.ts | 回退为旧上游文案 | 缓存读取费用 | 缓存读取成本 | 保留上游 |
| 108 | `admin.usage.inputCost` | frontend/src/i18n/locales/zh/admin/resources.ts | 回退为旧上游文案 | 输入费用 | 输入成本 | 保留上游 |
| 109 | `admin.usage.outputCost` | frontend/src/i18n/locales/zh/admin/resources.ts | 回退为旧上游文案 | 输出费用 | 输出成本 | 保留上游 |
| 110 | `admin.settings.features.riskControl.cyberSessionBlock` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | cyber 会话自动屏蔽 | Cyber 会话封禁 | 保留上游 |
| 111 | `admin.settings.features.riskControl.cyberSessionBlockHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 开启后,被上游网络安全策略(cyber_policy)拦截的会话将在 TTL 内被本地屏蔽,不再发往上游。仅屏蔽该会话,不影响同 Key 其他会话。 | 按会话维度阻断连续命中的 Cyber 风险请求。 | 保留上游 |
| 112 | `admin.settings.features.riskControl.cyberSessionBlockTTL` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 屏蔽时长(秒) | Cyber 会话封禁时长 | 保留上游 |
| 113 | `admin.settings.gatewayForwarding.addSystemBlock` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 添加 block | 添加系统提示块 | 保留上游 |
| 114 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocks` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Claude OAuth System Blocks | Claude OAuth 系统提示块 | 保留上游 |
| 115 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocksHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 被新上游文案覆盖 | 每个 block 会保存为带 enabled、type、text、可选 cache_control 的 JSON。{'{'}billing_header{'}'} 会按请求动态生成；Claude Code 身份提示词和扩展提示词可直接编辑，也可用预设恢复默认值。 | 按顺序向 Claude OAuth 请求注入系统提示，可控制每段是否缓存。 | 保留上游（上游扩充语义） |
| 116 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjection` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Claude OAuth System 注入 | Claude OAuth 系统提示注入 | 保留上游 |
| 117 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjectionHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 为非 Claude Code 客户端的 Claude OAuth 请求注入 Claude Code 形态的 system blocks。默认开启。 | 启用后为 Claude OAuth 请求自动注入额外系统提示。 | 保留上游 |
| 118 | `admin.settings.gatewayForwarding.clientDatelineNormalization` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 客户端 dateline 归一化 | 客户端日期线标准化 | 保留上游 |
| 119 | `admin.settings.gatewayForwarding.clientDatelineNormalizationHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 默认开启。将 Anthropic OAuth/Setup Token 请求体中 "Today's date is …" 语句里的撇号与日期分隔符还原为 ASCII 撇号 + 短横线 (2026-07-01) 的规范形态，抹除某些客户端在检测到非官方 base URL 时注入的隐写指纹位。仅作用于 system prompt 与 <system-reminder> 块内，API Key 账号不受影响。 | 规范化客户端日期线信息，降低上游因时区/日期差异导致的异常。 | 保留上游 |
| 120 | `admin.settings.gatewayForwarding.codexAddRow` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 添加一条 | 添加规则 | 保留上游 |
| 121 | `admin.settings.gatewayForwarding.codexAllowAppServer` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Codex app-server | 允许 App Server 客户端 | 保留上游 |
| 122 | `admin.settings.gatewayForwarding.codexAllowAppServerDesc` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 放行内嵌 Codex 引擎、经 app-server 协议接入的第三方客户端（如 Claude Code 的 codex 插件）。默认关闭；开启后此类客户端通过引擎指纹门（下方信号列表）即放行，关闭则仅放行官方客户端与白名单。 | 在 Codex CLI 限制模式下，额外允许 App Server 指纹的客户端。 | 保留上游 |
| 123 | `admin.settings.gatewayForwarding.codexBlacklist` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | User-Agent/Originator 黑名单 | Codex 黑名单 | 保留上游 |
| 124 | `admin.settings.gatewayForwarding.codexBlacklistDesc` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 命中任一字段即拒，优先于一切放行。originator 精确匹配，User-Agent 为包含匹配（多个用逗号分隔）。 | 匹配 Originator 或 User-Agent 的请求将被拒绝。 | 保留上游 |
| 125 | `admin.settings.gatewayForwarding.codexClientRestrictionTitle` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Codex 客户端限制 | 客户端限制 | 保留上游 |
| 126 | `admin.settings.gatewayForwarding.codexFingerprintNoRequiredWarn` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 未勾选任何「必须」信号——引擎指纹门当前不生效，等于放行所有通过身份/版本的候选。如需启用校验，请至少勾选一条信号。 | 至少建议保留一个必需指纹信号。 | 保留上游 |
| 127 | `admin.settings.gatewayForwarding.codexFingerprintSignals` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Codex 引擎指纹信号 | Codex 指纹信号 | 保留上游 |
| 128 | `admin.settings.gatewayForwarding.codexFingerprintSignalsDesc` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 定义引擎指纹信号：勾「必须」的信号需全部命中（AND），每条 / 分隔的变体取或（OR）；一条都不勾即不校验。默认只勾 x-codex- 前缀。类型：头精确 / 头前缀 / body 路径。 | 配置用于识别 Codex 客户端的请求头或响应体路径。 | 保留上游 |
| 129 | `admin.settings.gatewayForwarding.codexFpMatchPlaceholder` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 匹配，变体用 / 分隔（如 session-id / session_id 或 x-codex-） | 匹配值或路径 | 保留上游 |
| 130 | `admin.settings.gatewayForwarding.codexFpRequired` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 必须 | 必需 | 保留上游 |
| 131 | `admin.settings.gatewayForwarding.codexFpTypeBodyPath` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | body 路径 | Body 路径 | 保留上游 |
| 132 | `admin.settings.gatewayForwarding.codexFpTypeHeaderExact` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 头精确 | Header 精确 | 保留上游 |
| 133 | `admin.settings.gatewayForwarding.codexFpTypeHeaderPrefix` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 头前缀 | Header 前缀 | 保留上游 |
| 134 | `admin.settings.gatewayForwarding.codexHardeningDesc` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 仅对已开启「仅允许 Codex 官方客户端」的 OpenAI OAuth 账号生效（全局）。在 User-Agent/Originator 之外，用版本区间、引擎指纹门与黑/白名单巩固判定。 | 限制 Codex 客户端版本、指纹和来源，降低异常客户端风险。 | 保留上游 |
| 135 | `admin.settings.gatewayForwarding.codexHardeningTitle` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Codex 设置 | Codex 安全加固 | 保留上游 |
| 136 | `admin.settings.gatewayForwarding.codexOriginatorPlaceholder` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | originator（精确，如 opencode） | Originator | 保留上游 |
| 137 | `admin.settings.gatewayForwarding.codexUaContainsPlaceholder` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | User-Agent 包含标记，逗号分隔（如 opencode/） | User-Agent 包含 | 保留上游 |
| 138 | `admin.settings.gatewayForwarding.codexVersionHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 仅对官方客户端生效，校验其版本是否落在 [最低, 最高] 区间。留空表示该侧不限制。 | 留空表示不限制对应版本边界。 | 保留上游 |
| 139 | `admin.settings.gatewayForwarding.codexWhitelist` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | User-Agent/Originator 白名单 | Codex 白名单 | 保留上游 |
| 140 | `admin.settings.gatewayForwarding.codexWhitelistDesc` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 放行官方集之外的客户端：需 originator 精确，且每个 User-Agent 标记都命中。默认仍需过引擎指纹门，勾「跳过引擎指纹」可免。 | 匹配白名单的请求可被显式允许。 | 保留上游 |
| 141 | `admin.settings.gatewayForwarding.codexWhitelistSkipFingerprint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 跳过引擎指纹 | 跳过指纹 | 保留上游 |
| 142 | `admin.settings.gatewayForwarding.codexWhitelistSkipFingerprintTooltip` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 风险：勾选后该条仅凭 originator + User-Agent（均可伪造）放行，不再要求引擎指纹兜底。仅用于确属可信、但本身不发 codex 引擎指纹的第三方客户端。 | 命中该白名单规则时不再校验 Codex 指纹。 | 保留上游 |
| 143 | `admin.settings.gatewayForwarding.maxCodexVersionPlaceholder` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 例如 0.200.0 | 例如 0.35.0 | 保留上游 |
| 144 | `admin.settings.gatewayForwarding.minCodexVersionPlaceholder` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 例如 0.142.0 | 例如 0.20.0 | 保留上游 |
| 145 | `admin.settings.gatewayForwarding.resetSystemBlocks` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 恢复默认 | 重置系统提示块 | 保留上游 |
| 146 | `admin.settings.gatewayForwarding.systemBlockCacheControl` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Cache Control | 启用缓存控制 | 保留上游 |
| 147 | `admin.settings.gatewayForwarding.systemBlockHide` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 隐藏 block 详情 | 收起 | 保留上游 |
| 148 | `admin.settings.gatewayForwarding.systemBlockPresetBilling` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Billing Header | 计费说明 | 保留上游 |
| 149 | `admin.settings.gatewayForwarding.systemBlockPresetExpansion` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Claude Code 扩展提示词 | 能力扩展 | 保留上游 |
| 150 | `admin.settings.gatewayForwarding.systemBlockPresetIdentity` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | Claude Code 身份提示词 | 身份说明 | 保留上游 |
| 151 | `admin.settings.gatewayForwarding.systemBlockShow` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 展示 block 详情 | 展开 | 保留上游 |
| 152 | `admin.settings.gatewayForwarding.systemBlockText` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 内容 | 提示内容 | 保留上游 |
| 153 | `admin.settings.gatewayForwarding.systemBlockTitle` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | System Block {index} | 系统提示块 #{index} | 保留上游 |
| 154 | `admin.settings.openaiExperimentalScheduler.defaultPlaceholder` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 配置/默认：{value} | 默认：{value} | 保留上游 |
| 155 | `admin.settings.openaiExperimentalScheduler.errorRateWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 错误率 | 错误率权重 | 保留上游 |
| 156 | `admin.settings.openaiExperimentalScheduler.loadWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 负载 | 负载权重 | 保留上游 |
| 157 | `admin.settings.openaiExperimentalScheduler.previousResponseWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | previous_response 粘性 | 上一响应权重 | 保留上游 |
| 158 | `admin.settings.openaiExperimentalScheduler.priorityWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 优先级 | 优先级权重 | 保留上游 |
| 159 | `admin.settings.openaiExperimentalScheduler.queueWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 排队 | 队列权重 | 保留上游 |
| 160 | `admin.settings.openaiExperimentalScheduler.quotaHeadroomWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 额度余量 | 额度余量权重 | 保留上游 |
| 161 | `admin.settings.openaiExperimentalScheduler.resetWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 重置窗口 | 重置权重 | 保留上游 |
| 162 | `admin.settings.openaiExperimentalScheduler.sessionStickyWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | session_hash 粘性 | 会话粘性权重 | 保留上游 |
| 163 | `admin.settings.openaiExperimentalScheduler.stickyWeightedDescription` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 开启后 previous_response_id 和 session_hash 粘性进入高级调度打分；关闭时仍按旧逻辑硬命中粘性账号。 | 综合账号负载、错误率、延迟和会话粘性选择上游账号。 | 保留上游 |
| 164 | `admin.settings.openaiExperimentalScheduler.stickyWeightedTitle` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 粘性加权 | 启用粘性加权调度 | 保留上游 |
| 165 | `admin.settings.openaiExperimentalScheduler.subscriptionPriorityDescription` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 开启后先在 ChatGPT 订阅账号池中按权值选取；订阅池拿不到席位时再回退到非订阅账号池。 | 订阅优先级会参与账号调度评分。 | 保留上游 |
| 166 | `admin.settings.openaiExperimentalScheduler.subscriptionPriorityTitle` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 订阅优先 | 订阅优先级 | 保留上游 |
| 167 | `admin.settings.openaiExperimentalScheduler.topKLabel` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | TopK | 候选 Top K | 保留上游 |
| 168 | `admin.settings.openaiExperimentalScheduler.ttftWeight` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 首包延迟 | TTFT 权重 | 保留上游 |
| 169 | `admin.settings.openaiExperimentalScheduler.weightsDescription` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 留空时使用配置/环境变量值；配置未设置时使用内置默认值。页面非空设置优先。 | 调整各因子在调度评分中的权重。 | 保留上游 |
| 170 | `admin.settings.openaiExperimentalScheduler.weightsTitle` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 调度权值覆盖 | 调度权重 | 保留上游 |
| 171 | `admin.settings.payment.addCustomMethod` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 添加方式 | 添加自定义方式 | 保留上游 |
| 172 | `admin.settings.payment.customMethodType` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 支付方式 | 方式标识 | 保留上游 |
| 173 | `admin.settings.payment.customMethodUpstreamType` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 上游 type | 上游方式 | 保留上游 |
| 174 | `admin.settings.payment.easypayCustomMethods` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 易支付自定义支付方式 | EasyPay 自定义支付方式 | 保留上游 |
| 175 | `admin.settings.payment.easypayCustomMethodsHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 添加当前易支付服务商额外支持的支付方式。支付方式会记录到 Sub2API 订单中，上游 type 会作为易支付 type 参数提交。 | 用于映射 EasyPay 返回的自定义支付类型。 | 恢复本地（去上游品牌） |
| 176 | `admin.settings.payment.enabledPaymentTypesHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 禁用服务商将同时禁用对应的实例。 | 禁用支付方式将同时禁用对应的服务商实例。 | 保留上游 |
| 177 | `admin.settings.payment.findProvider` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 正在寻找合适的易支付服务商？ | 查看支持的支付方式 | 保留上游 |
| 178 | `admin.settings.payment.subscriptionUsdToCnyRate` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 订阅 CNY 换算汇率 | 订阅美元兑人民币汇率 | 保留上游 |
| 179 | `admin.settings.payment.subscriptionUsdToCnyRateDisabled` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 未启用（按 price 直付） | 未启用人民币订阅支付时不使用该汇率。 | 保留上游 |
| 180 | `admin.settings.payment.subscriptionUsdToCnyRateHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | CNY 支付通道下，套餐每 1 USD 价格收取多少 CNY（如 7.15）。0 或留空 = 不换算，订阅按 price 数值直接收款。启用后所有套餐 price 必须按 USD 定价 | 用于订阅套餐的美元价格换算为人民币支付金额。 | 保留上游 |
| 181 | `admin.settings.payment.validationEasyPayCustomMethodDuplicate` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 易支付自定义支付方式不能重复 | 自定义支付方式不能重复。 | 保留上游 |
| 182 | `admin.settings.payment.validationEasyPayCustomMethodPrefixReserved` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 易支付自定义支付方式不能以 alipay 或 wxpay 开头 | 自定义支付方式不能使用保留前缀。 | 保留上游 |
| 183 | `admin.settings.payment.validationEasyPayCustomMethodRequired` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 每个易支付自定义方式都必须填写支付方式和上游 type | 请填写自定义支付方式。 | 保留上游 |
| 184 | `admin.settings.payment.validationEasyPayCustomMethodReserved` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 易支付自定义支付方式不能使用内置的 alipay 或 wxpay | 该支付方式为系统保留值。 | 保留上游 |
| 185 | `admin.settings.payment.validationEasyPayCustomMethodTypeInvalid` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 易支付自定义支付方式只能包含小写字母、数字、下划线和短横线 | 支付方式标识格式不正确。 | 保留上游 |
| 186 | `admin.settings.payment.validationEasyPayCustomMethodUpstreamTypeInvalid` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 易支付上游 type 只能包含小写字母、数字、下划线和短横线 | 上游支付方式格式不正确。 | 保留上游 |
| 187 | `admin.settings.site.apiBaseUrlHint` | frontend/src/i18n/locales/zh/admin/settings.ts | 回退为旧上游文案 | 用于"使用密钥"、"导入到 CC Switch"和回调地址建议，留空则使用当前站点地址 | 用于"使用密钥"和"导入到 CC Switch"功能，留空则使用当前站点地址 | 保留上游 |
| 188 | `adminCompliance.accepted` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 合规确认已记录 | 合规承诺已确认 | 恢复本地（法务/合规定制） |
| 189 | `adminCompliance.acceptFailed` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 提交确认失败 | 确认失败，请稍后重试 | 恢复本地（法务/合规定制） |
| 190 | `adminCompliance.blockingNotice` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 继续使用控制台前，须完成部署与运营合规确认。 | 请先完成合规承诺确认 | 恢复本地（法务/合规定制） |
| 191 | `adminCompliance.documentSource` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 协议正文来自本项目仓库中的 Markdown 文件。修改协议内容时必须同步递增协议版本；已确认的旧版本将失效，控制台使用者须重新确认。 | 文档来源于仓库中的合规承诺文件，确认记录按管理员账号单独保存。 | 恢复本地（法务/合规定制） |
| 192 | `adminCompliance.inputLabel` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 请逐字输入以下确认短语 | 请逐字输入以下确认语 | 恢复本地（法务/合规定制） |
| 193 | `adminCompliance.inputMismatch` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 确认短语不匹配，请逐字输入提示内容。 | 请输入与上方完全一致的确认语 | 恢复本地（法务/合规定制） |
| 194 | `adminCompliance.inputPlaceholder` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 输入确认短语以继续 | 输入确认语以启用确认按钮 | 恢复本地（法务/合规定制） |
| 195 | `adminCompliance.legalNote` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 本确认用于明确自部署实例与开源项目、著作权人、贡献者及维护者之间的非关联关系和责任边界；部署、运营或控制相关实例的主体应独立承担其适用义务。 | 系统会记录确认版本、时间、账号标识、IP 地址和 User-Agent，用于合规留痕。 | 恢复本地（法务/合规定制） |
| 196 | `adminCompliance.openDocument` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 在 GitHub 查看协议文件 | 查看原始文档 | 恢复本地（法务/合规定制） |
| 197 | `adminCompliance.riskNotice` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 本确认用于以清晰、显著、可留痕的方式提示自部署实例的合规义务与运营风险。 | 继续使用管理控制台前，请阅读并确认当前版本的部署与运营合规承诺。 | 恢复本地（法务/合规定制） |
| 198 | `adminCompliance.version` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 协议版本 | 版本 | 恢复本地（法务/合规定制） |
| 199 | `common.peakRateImageNote` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | ；token 计费的图片 token 同样适用，图片按次计费不受高峰影响 | ；图像价格按图像计费规则另行计算 | 保留上游 |
| 200 | `common.peakRateTooltip` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 高峰倍率：{window} | 峰值倍率窗口：{window} | 保留上游 |
| 201 | `legal.empty` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 暂无正文内容 | 暂无文档内容 | 恢复本地（法务/合规定制） |
| 202 | `legal.loginAgreement` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 登录条款 | 登录协议 | 恢复本地（法务/合规定制） |
| 203 | `legal.loginAgreementPrompt.checkboxPrefix` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 我已阅读并同意 | 我已阅读并同意  | 恢复本地（法务/合规定制） |
| 204 | `legal.loginAgreementPrompt.dialogDescription` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 我们的服务条款已于 {date} 更新。在继续使用服务之前，请仔细阅读并同意以下条款。 | 协议版本更新于 {date}。请阅读相关文档后继续。 | 恢复本地（法务/合规定制） |
| 205 | `legal.loginAgreementPrompt.dialogTitle` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 条款更新通知 | 登录协议确认 | 恢复本地（法务/合规定制） |
| 206 | `legal.loginAgreementPrompt.loginRejectedWarning` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 未同意最新条款前，无法输入账号密码或使用快捷登录。 | 需要同意登录协议后才能继续登录。 | 恢复本地（法务/合规定制） |
| 207 | `legal.loginAgreementPrompt.loginRequiredWarning` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 请先阅读并同意最新条款后再登录。 | 请先确认登录协议。 | 恢复本地（法务/合规定制） |
| 208 | `legal.loginAgreementPrompt.noticeDescription` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 未同意前，账号密码输入和快捷登录会保持禁用。 | 继续登录或注册前，请阅读并确认相关协议。 | 恢复本地（法务/合规定制） |
| 209 | `legal.loginAgreementPrompt.noticeTitle` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 继续登录前需要先同意最新条款。 | 需要确认登录协议 | 恢复本地（法务/合规定制） |
| 210 | `legal.loginAgreementPrompt.recently` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 近期 | 最近 | 恢复本地（法务/合规定制） |
| 211 | `legal.loginAgreementPrompt.registerRejectedWarning` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 未同意最新条款前，无法注册或使用快捷登录。 | 需要同意登录协议后才能继续注册。 | 恢复本地（法务/合规定制） |
| 212 | `legal.loginAgreementPrompt.registerRequiredWarning` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 请先阅读并同意最新条款后再注册。 | 请先确认登录协议。 | 恢复本地（法务/合规定制） |
| 213 | `legal.loginAgreementPrompt.reject` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 拒绝 | 暂不同意 | 恢复本地（法务/合规定制） |
| 214 | `legal.loginAgreementPrompt.viewTerms` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 查看条款 | 查看协议 | 恢复本地（法务/合规定制） |
| 215 | `legal.notFoundDescription` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 当前条款文档不存在或已被管理员移除。 | 该法律文档不存在或暂未发布。 | 恢复本地（法务/合规定制） |
| 216 | `legal.retryLater` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 请稍后刷新页面重试。 | 请稍后重试或联系管理员。 | 恢复本地（法务/合规定制） |
| 217 | `legal.updatedAt` | frontend/src/i18n/locales/zh/common.ts | 回退为旧上游文案 | 更新日期：{date} | 更新于 {date} | 恢复本地（法务/合规定制） |
| 218 | `usage.cacheBreakdown` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 缓存 Token 明细 | 缓存明细 | 保留上游 |
| 219 | `usage.cacheCreationTokensLabel` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 缓存创建 | 缓存创建 Tokens | 保留上游 |
| 220 | `usage.cacheReadTokensLabel` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 缓存读取 | 缓存读取 Tokens | 保留上游 |
| 221 | `usage.cacheTotal` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 缓存 | 缓存合计 | 保留上游 |
| 222 | `usage.costDetails` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 费用明细 | 成本明细 | 保留上游 |
| 223 | `usage.cyber` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 安全策略 | Cyber | 恢复本地（二开术语） |
| 224 | `usage.errors.allStatuses` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 全部状态码 | 全部状态 | 保留上游 |
| 225 | `usage.imageOutputCost` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 图片输出费用 | 图片输出成本 | 保留上游 |
| 226 | `usage.ipGeo.batchFailed` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 批量获取地区信息失败 | 批量获取地区失败 | 保留上游 |
| 227 | `usage.ipGeo.batchFetching` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 获取中... | 批量获取中... | 保留上游 |
| 228 | `usage.ipGeo.detailAccuracy` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 定位精度 | 精度 | 保留上游 |
| 229 | `usage.ipGeo.detailOrg` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 运营商 | 组织 | 保留上游 |
| 230 | `usage.ipGeo.refreshTitle` | frontend/src/i18n/locales/zh/dashboard.ts | 回退为旧上游文案 | 刷新地区信息 | 刷新 IP 地区 | 保留上游 |
| 231 | `home.footer.allRightsReserved` | frontend/src/i18n/locales/zh/landing.ts | 被新上游文案覆盖 | TikToken is owned by Jerrywell Pte. Ltd. | net is owned by Jerrywell Pte. Ltd. | 恢复本地（品牌化） |
| 232 | `customPage.copiedCode` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | 已复制 | 代码已复制 | 保留上游 |
| 233 | `customPage.copyCode` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | 复制 | 复制代码 | 保留上游 |
| 234 | `customPage.copyCodeFailed` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | 失败 | 复制代码失败 | 保留上游 |
| 235 | `payment.admin.refundPending` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | 退款处理中，待网关确认 | 退款处理中 | 保留上游 |
| 236 | `payment.admin.subscriptionCnyPayPreview` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | CNY 通道实扣预览：{amount} | 人民币支付预览：¥{amount} | 保留上游 |
| 237 | `payment.admin.subscriptionCnyPayPreviewWithFee` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | （含 {feeRate}% 手续费：{total}） | 含手续费预览：¥{amount} | 保留上游（保留当前占位符语义） |
| 238 | `payment.planCard.peakRate` | frontend/src/i18n/locales/zh/misc.ts | 回退为旧上游文案 | 高峰倍率 | 峰值倍率 {rate}x | 保留上游（保留当前占位符语义） |

### en 共 232 条

| # | key | 定义文件 | 类型 | 当前文案（上游） | 本地原文案 | 决策 |
|---|---|---|---|---|---|---|
| 1 | `admin.accounts.createSparkShadow` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Create Spark Shadow | Create Spark shadow account | 保留上游 |
| 2 | `admin.accounts.dataImportIgnoredFiles` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Ignored {count} non-JSON file(s) | Ignored {count} files | 保留上游 |
| 3 | `admin.accounts.dataImportInvalidFile` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | {name} is not a supported data export file | Invalid file format | 保留上游（保留当前占位符语义） |
| 4 | `admin.accounts.dataImportParseFailedFile` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Failed to parse {name} | Failed to parse file | 保留上游（保留当前占位符语义） |
| 5 | `admin.accounts.gemini.oauthType.googleOneDesc` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Personal account with Google One subscription quota | Google One / AI Pro account authorization | 保留上游 |
| 6 | `admin.accounts.grokAccount` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Grok Account | Grok account | 保留上游 |
| 7 | `admin.accounts.headerOverride.addRow` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Add Header | Add header | 保留上游 |
| 8 | `admin.accounts.headerOverride.bulkDisableHint` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Saving will disable header override and clear existing configuration on the selected accounts. | Submit with no rows to disable custom headers in bulk. | 保留上游 |
| 9 | `admin.accounts.headerOverride.bulkEmptyRows` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Add at least one header row before saving, or turn the toggle off to clear existing configuration. | Add at least one header override. | 保留上游 |
| 10 | `admin.accounts.headerOverride.bulkReplaceHint` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Saving will replace the existing header override configuration on all selected accounts with the rows below. | Bulk edit replaces the current header override configuration on selected accounts. | 保留上游 |
| 11 | `admin.accounts.headerOverride.emptyValueHint` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Rows with an empty value are placeholders and do not override anything. | An empty value sends an empty string; deleting the row stops overriding that header. | 保留上游 |
| 12 | `admin.accounts.headerOverride.hint` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Override same-named request headers on forwarding (case-insensitive) | Append or override HTTP headers when forwarding requests through this account. | 保留上游 |
| 13 | `admin.accounts.headerOverride.info` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Applies to outbound requests of this account only: configured headers override client/gateway-generated headers of the same name before forwarding. Auth headers (authorization, x-api-key) and connection-control headers cannot be overridden. | Header names are case-insensitive. Later duplicate rows take precedence. | 保留上游 |
| 14 | `admin.accounts.headerOverride.namePlaceholder` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Header name (e.g. user-agent) | e.g. Authorization | 保留上游 |
| 15 | `admin.accounts.headerOverride.title` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Header Override | Header overrides | 保留上游 |
| 16 | `admin.accounts.headerOverride.valuePlaceholder` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Override value (leave empty to skip) | Header value | 保留上游 |
| 17 | `admin.accounts.oauth.grok.authCodeDesc` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | After authorization, paste the callback URL, query string, or authorization code: | After authorization, when the page URL becomes http://localhost:xxx/auth/callback?code=...: | 保留上游 |
| 18 | `admin.accounts.oauth.grok.authCodeHint` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Full callback URLs, query strings, and bare codes are accepted. | You can copy the entire URL or just the code parameter value, the system will auto-detect | 保留上游 |
| 19 | `admin.accounts.oauth.grok.authCodePlaceholder` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Paste the full callback URL, ?code=... query string, or code value | Option 1: Copy the complete URL⏎(http://localhost:xxx/auth/callback?code=...)⏎Option 2: Copy only the code parameter value | 保留上游 |
| 20 | `admin.accounts.oauth.grok.importantNotice` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | When the browser reaches the local callback URL, copy the full URL or the code query parameter back here. | Important: The page may take a while to load after authorization. Please wait patiently. When the browser address bar shows http://localhost..., authorization is complete. | 保留上游 |
| 21 | `admin.accounts.oauth.grok.openUrlDesc` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Open the authorization URL in a new tab, sign in to xAI, and authorize API access. | Open the authorization URL in a new tab, log in to your xAI account and authorize. | 保留上游 |
| 22 | `admin.accounts.oauth.grok.refreshTokenDesc` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Enter existing xAI refresh token(s). Supports batch input, one per line. | Enter your existing xAI Refresh Token. Supports batch input (one per line). The system will automatically validate and create accounts. | 保留上游 |
| 23 | `admin.accounts.oauth.openai.codexPatAuth` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Codex Personal Access Token | Codex PAT auth | 保留上游 |
| 24 | `admin.accounts.oauth.openai.codexPatDesc` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Enter a Codex at- personal access token. The system validates it with OpenAI whoami before creating the account. | Import Codex Personal Access Tokens for Codex-related requests. | 保留上游 |
| 25 | `admin.accounts.oauth.openai.codexPatHint` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | This is a separate auth mode. It does not save refresh_token or write an OAuth access_token expiration. | One PAT per line. Batch import creates one account per token. | 保留上游 |
| 26 | `admin.accounts.oauth.openai.codexPatImportAndCreate` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Validate & Create Codex PAT Account | Import PAT and create accounts | 保留上游 |
| 27 | `admin.accounts.oauth.openai.codexPatPlaceholder` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | at-... | Paste Codex PATs, one per line | 保留上游 |
| 28 | `admin.accounts.openai.codexCLIOnlyAppServer` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Allow Codex app-server clients | Only allow Codex CLI / App Server clients | 保留上游 |
| 29 | `admin.accounts.openai.codexCLIOnlyAppServerDesc` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Effective only when the switch above is on. When enabled, this account also allows third-party clients that embed the Codex engine over the app-server protocol (e.g. Claude Code's codex plugin); they still pass the global engine-fingerprint gate. OR-combined with the global app-server toggle. | When enabled, only requests matching Codex CLI or App Server fingerprints are accepted. | 保留上游 |
| 30 | `admin.accounts.openai.wsModeHttpBridge` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | HTTP Bridge (http_bridge) | HTTP bridge mode | 保留上游 |
| 31 | `admin.accounts.openaiQuotaReset.collapseExpirations` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Collapse reset credit expirations | Collapse reset-credit expirations | 保留上游 |
| 32 | `admin.accounts.openaiQuotaReset.confirmMessage` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | This will consume 1 reset credit to immediately restore the current window ({count} remaining). This action cannot be undone. Continue? | Consume 1 reset credit? {count} credits are currently available. | 保留上游 |
| 33 | `admin.accounts.openaiQuotaReset.confirmTitle` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Confirm Weekly Limit Reset | Confirm OpenAI quota reset | 保留上游 |
| 34 | `admin.accounts.openaiQuotaReset.countTooltipLoad` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Click to load the available reset-credit count | Query available reset credits | 保留上游 |
| 35 | `admin.accounts.openaiQuotaReset.countTooltipRefresh` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Click to refresh the available reset-credit count | Refresh available reset credits | 保留上游 |
| 36 | `admin.accounts.openaiQuotaReset.expandExpirations` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Expand the other {count} reset credit expiration(s) | Show {count} expiration times | 保留上游 |
| 37 | `admin.accounts.openaiQuotaReset.expirationDetails` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Reset credit expiration details | Reset-credit expiration details | 保留上游 |
| 38 | `admin.accounts.openaiQuotaReset.expiresAtFull` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Reset credit expires at {time} | Expires at: {time} | 保留上游 |
| 39 | `admin.accounts.openaiQuotaReset.resetSuccess` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Reset {windows} window(s) | Reset quota windows: {windows} | 保留上游 |
| 40 | `admin.accounts.openaiQuotaReset.resetTooltipNeedQuery` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Click Credits first to load the available count | Query available reset credits first | 保留上游 |
| 41 | `admin.accounts.openaiQuotaReset.resetTooltipReady` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Consume 1 reset credit to immediately restore the window | Consume 1 reset credit | 保留上游 |
| 42 | `admin.accounts.openaiQuotaReset.resetTooltipShadow` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Spark shadow accounts cannot reset credits; reset on the parent account | Shadow accounts cannot be reset directly. Reset the parent account instead. | 保留上游 |
| 43 | `admin.accounts.usageWindow.grokLastHeadersSeen` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Headers {time} | Last headers seen | 保留上游（保留当前占位符语义） |
| 44 | `admin.accounts.usageWindow.grokLastProbe` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Probe {time} | Last probe | 保留上游（保留当前占位符语义） |
| 45 | `admin.accounts.usageWindow.grokLastStatus` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Status {status} | Last status | 保留上游（保留当前占位符语义） |
| 46 | `admin.accounts.usageWindow.grokNoHeaders` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | No quota headers observed | No Grok quota headers detected | 保留上游 |
| 47 | `admin.accounts.usageWindow.grokProbe` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Probe | Probe Grok quota | 保留上游 |
| 48 | `admin.accounts.usageWindow.grokProbeTooltip` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Send a minimal xAI Responses probe and read quota headers | Send a lightweight probe request to refresh Grok quota headers. | 保留上游 |
| 49 | `admin.accounts.usageWindow.grokRequests` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Req | Grok requests | 保留上游 |
| 50 | `admin.accounts.usageWindow.grokResetUnsupportedTooltip` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | xAI does not expose reset credits for Grok OAuth accounts | Grok quota windows cannot currently be reset from the console. | 保留上游 |
| 51 | `admin.accounts.usageWindow.grokRetryAfter` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Retry after {time} | Retry-After | 保留上游（保留当前占位符语义） |
| 52 | `admin.accounts.usageWindow.grokTokens` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Tok | Grok tokens | 保留上游 |
| 53 | `admin.accounts.usageWindow.grokUnknown` | frontend/src/i18n/locales/en/admin/accounts.ts | 回退为旧上游文案 | Grok quota is unknown until the first upstream response includes xAI rate-limit headers. | Grok quota unknown | 保留上游 |
| 54 | `admin.channelMonitor.form.jitterSeconds` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Random Jitter (± seconds) | Jitter seconds | 保留上游 |
| 55 | `admin.channelMonitor.form.jitterSecondsHint` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Each check fires at interval ± a random offset within this value; 0 means fixed interval. Interval minus jitter must be ≥ 15s | Add random jitter to monitor scheduling to avoid clustered requests. | 保留上游 |
| 56 | `admin.channels.form.cacheReadPriceShort` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Cache R | Cache read | 保留上游 |
| 57 | `admin.channels.form.cacheWritePriceShort` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Cache W | Cache write | 保留上游 |
| 58 | `admin.channels.form.inclusive` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | (inclusive) | Inclusive | 保留上游 |
| 59 | `admin.channels.form.maxTokens` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Max | Max tokens | 保留上游 |
| 60 | `admin.channels.form.minTokens` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Min | Min tokens | 保留上游 |
| 61 | `admin.channels.mappingConflict` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Mapping source patterns '{model1}' and '{model2}' conflict: overlapping match range. Source patterns are matched case-insensitively, so an existing entry already covers all case variants. | Mapping source patterns '{model1}' and '{model2}' conflict: overlapping match range | 保留上游 |
| 62 | `admin.channels.modelConflict` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Model patterns '{model1}' and '{model2}' conflict: overlapping match range. Model names are matched case-insensitively, so an existing entry already covers all case variants — no need to add the variant separately. | Model patterns '{model1}' and '{model2}' conflict: overlapping match range | 保留上游 |
| 63 | `admin.riskControl.cyberPolicyExcludeBan` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Exclude Cyber Policy Hits from Ban Count | Exclude ban | 保留上游 |
| 64 | `admin.riskControl.cyberPolicyExcludeBanHint` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | When enabled, cyber_policy hits no longer count toward auto-ban violations: no ban judgment on the hit itself, and history rows are excluded from the rolling count. Logs and notice emails are unaffected. | When enabled, Cyber policy matches will not trigger ban actions. | 保留上游 |
| 65 | `admin.riskControl.defaultBlockMessage` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Content audit matched a risk rule. Please adjust your input and try again. | The request was blocked by risk control policy. | 保留上游 |
| 66 | `admin.riskControl.matchedKeyword` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Matched Keyword | Matched keyword | 保留上游 |
| 67 | `admin.riskControl.violationNotCounted` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Not counted | Not counted as violation | 保留上游 |
| 68 | `admin.subscriptions.guide.actions.revokeDesc` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Immediately terminate the subscription (restorable from the revoked list) | Immediately terminate the subscription (irreversible) | 保留上游 |
| 69 | `admin.subscriptions.restoreConfirm` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Restore the subscription for '{user}'? If the original subscription has expired, it will be restored as expired. | Restore this subscription? | 保留上游（保留当前占位符语义） |
| 70 | `admin.subscriptions.restoreSubscription` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Restore Subscription | Restore subscription | 保留上游 |
| 71 | `admin.subscriptions.revokeConfirm` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Are you sure you want to revoke the subscription for '{user}'? You can restore it later from the revoked list. | Are you sure you want to revoke the subscription for '{user}'? This action cannot be undone. | 保留上游 |
| 72 | `admin.subscriptions.subscriptionRestored` | frontend/src/i18n/locales/en/admin/channels.ts | 回退为旧上游文案 | Subscription restored successfully | Subscription restored | 保留上游 |
| 73 | `admin.ops.systemLogs.caller` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | caller | Record caller | 保留上游 |
| 74 | `admin.ops.systemLogs.cleanupConfirm` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Clean up system logs matching the current filters? This cannot be undone. | Clean system logs matching the current filters? | 保留上游 |
| 75 | `admin.ops.systemLogs.cleanupFailed` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Failed to clean up system logs | Failed to clean system logs | 保留上游 |
| 76 | `admin.ops.systemLogs.cleanupSuccess` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Cleanup complete. Deleted {count} log entries. | System logs cleaned | 保留上游（保留当前占位符语义） |
| 77 | `admin.ops.systemLogs.componentPlaceholder` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | e.g. http.access | e.g. gateway | 保留上游 |
| 78 | `admin.ops.systemLogs.description` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Newest logs are shown first. Filter, search, and clean up by condition. | View system runtime logs, write health, and runtime logging configuration. | 保留上游 |
| 79 | `admin.ops.systemLogs.endTime` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | End time (optional) | End time | 保留上游 |
| 80 | `admin.ops.systemLogs.keyId` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | KEY ID | Key ID | 保留上游 |
| 81 | `admin.ops.systemLogs.keywordPlaceholder` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | message/request_id | Search message, error, or request ID | 保留上游 |
| 82 | `admin.ops.systemLogs.latestWriteError` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Latest write error: | Latest write error:  | 保留上游 |
| 83 | `admin.ops.systemLogs.logDetails` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Log Details | Log details | 保留上游 |
| 84 | `admin.ops.systemLogs.resetRuntimeConfigConfirm` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Reset to startup configuration (env/yaml) and apply immediately? | Reset system log runtime configuration to defaults? | 保留上游 |
| 85 | `admin.ops.systemLogs.runtimeConfig` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Runtime Log Configuration (applies immediately) | Runtime config | 保留上游 |
| 86 | `admin.ops.systemLogs.runtimeConfigActive` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Runtime log configuration is active | System log runtime config is active | 保留上游 |
| 87 | `admin.ops.systemLogs.runtimeConfigReset` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Reset to startup log configuration | System log runtime config reset to defaults | 保留上游 |
| 88 | `admin.ops.systemLogs.runtimeConfigResetFailed` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Failed to reset log configuration | Failed to reset system log runtime config | 保留上游 |
| 89 | `admin.ops.systemLogs.runtimeConfigSaveFailed` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Failed to save log configuration | Failed to save system log runtime config | 保留上游 |
| 90 | `admin.ops.systemLogs.sampling` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | sampling | Enable sampling | 保留上游 |
| 91 | `admin.ops.systemLogs.samplingInitial` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Sampling initial | Initial entries | 保留上游 |
| 92 | `admin.ops.systemLogs.samplingThereafter` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Sampling thereafter | Sample every N after | 保留上游 |
| 93 | `admin.ops.systemLogs.search` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Search | Search logs | 保留上游 |
| 94 | `admin.ops.systemLogs.startTime` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | Start time (optional) | Start time | 保留上游 |
| 95 | `admin.ops.systemLogs.title` | frontend/src/i18n/locales/en/admin/ops.ts | 回退为旧上游文案 | System Logs | System logs | 保留上游 |
| 96 | `admin.groups.rateLabel` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | rate | Rate | 保留上游 |
| 97 | `admin.users.allApiKeyGroups` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | All API Key Groups | All API key groups | 保留上游 |
| 98 | `admin.users.allAuthorizedGroups` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | All Authorized Groups | All authorized groups | 保留上游 |
| 99 | `admin.users.apiKeyGroupDisabled` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Disabled Groups | Disabled | 保留上游 |
| 100 | `admin.users.apiKeyGroupExclusive` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Exclusive Groups | Exclusive | 保留上游 |
| 101 | `admin.users.apiKeyGroupFilter` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | API Key Group | API key group filter | 保留上游 |
| 102 | `admin.users.apiKeyGroupPublic` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Public Groups | Public | 保留上游 |
| 103 | `admin.users.apiKeyGroupSubscription` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Subscription Groups | Subscription | 保留上游 |
| 104 | `admin.users.authorizedGroupFilter` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Authorized Group | Authorized group filter | 保留上游 |
| 105 | `admin.users.searchApiKeyGroups` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Search API Key groups... | Search API key groups | 保留上游 |
| 106 | `admin.users.searchAuthorizedGroups` | frontend/src/i18n/locales/en/admin/overview.ts | 回退为旧上游文案 | Search authorized groups... | Search authorized groups | 保留上游 |
| 107 | `admin.settings.features.riskControl.cyberSessionBlock` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Cyber session auto-block | Cyber session block | 保留上游 |
| 108 | `admin.settings.features.riskControl.cyberSessionBlockHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | When enabled, sessions hit by upstream cyber_policy are blocked locally for the TTL and no longer forwarded. Only the offending session is blocked; other sessions on the same key are unaffected. | Block repeated Cyber risk hits by session. | 保留上游 |
| 109 | `admin.settings.features.riskControl.cyberSessionBlockTTL` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Block TTL (seconds) | Cyber session block TTL | 保留上游 |
| 110 | `admin.settings.gatewayForwarding.addSystemBlock` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Add block | Add system block | 保留上游 |
| 111 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocks` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Claude OAuth System Blocks | Claude OAuth system prompt blocks | 保留上游 |
| 112 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptBlocksHint` | frontend/src/i18n/locales/en/admin/settings.ts | 被新上游文案覆盖 | Each block is saved as JSON with enabled, type, text, and optional cache_control. {'{'}billing_header{'}'} stays dynamic per request; the Claude Code identity and expansion prompts can be edited directly or restored from presets. | Inject ordered system prompt blocks into Claude OAuth requests and control cache behavior per block. | 保留上游（上游扩充语义） |
| 113 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjection` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Claude OAuth System Blocks | Claude OAuth system prompt injection | 保留上游 |
| 114 | `admin.settings.gatewayForwarding.claudeOAuthSystemPromptInjectionHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Inject Claude Code-like system blocks for Claude OAuth requests from non-Claude-Code clients. Enabled by default. | Automatically inject extra system prompts into Claude OAuth requests. | 保留上游 |
| 115 | `admin.settings.gatewayForwarding.clientDatelineNormalization` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Client Dateline Normalization | Client dateline normalization | 保留上游 |
| 116 | `admin.settings.gatewayForwarding.clientDatelineNormalizationHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Default on. Rewrites the "Today's date is …" sentence in Anthropic OAuth/Setup Token requests back to a canonical ASCII apostrophe and hyphen date format, erasing steganographic fingerprint bits some clients inject when they detect a non-official base URL. Applies to system prompts and <system-reminder> blocks only; API-Key accounts are unaffected. | Normalize client dateline information to reduce upstream issues caused by timezone/date differences. | 保留上游 |
| 117 | `admin.settings.gatewayForwarding.codexAddRow` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Add entry | Add rule | 保留上游 |
| 118 | `admin.settings.gatewayForwarding.codexAllowAppServer` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Codex app-server | Allow App Server clients | 保留上游 |
| 119 | `admin.settings.gatewayForwarding.codexAllowAppServerDesc` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Allow third-party clients that embed the Codex engine and connect over the app-server protocol (e.g. Claude Code's codex plugin). Off by default; when on, such clients are allowed once they pass the engine-fingerprint gate (the signal list below); off = only official clients and the whitelist are allowed. | In Codex CLI restriction mode, also allow clients matching App Server fingerprints. | 保留上游 |
| 120 | `admin.settings.gatewayForwarding.codexBlacklist` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | User-Agent/Originator Blacklist | Codex blacklist | 保留上游 |
| 121 | `admin.settings.gatewayForwarding.codexBlacklistDesc` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Deny if any field matches; takes precedence over any allow. originator is exact; User-Agent is a 'contains' match (comma-separated). | Requests matching Originator or User-Agent rules are rejected. | 保留上游 |
| 122 | `admin.settings.gatewayForwarding.codexClientRestrictionTitle` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Codex client restriction | Client restrictions | 保留上游 |
| 123 | `admin.settings.gatewayForwarding.codexFingerprintNoRequiredWarn` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | No signal is marked Required — the engine-fingerprint gate is inactive, allowing every candidate that passes identity/version. Check at least one signal to enable it. | At least one required fingerprint signal is recommended. | 保留上游 |
| 124 | `admin.settings.gatewayForwarding.codexFingerprintSignals` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Codex engine fingerprint signals | Codex fingerprint signals | 保留上游 |
| 125 | `admin.settings.gatewayForwarding.codexFingerprintSignalsDesc` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Define engine-fingerprint signals: every Required signal must match (AND); within a row, '/'-separated variants are OR'd. None checked = not enforced. Default checks only the x-codex- prefix. Types: header exact / header prefix / body path. | Configure headers or body paths used to identify Codex clients. | 保留上游 |
| 126 | `admin.settings.gatewayForwarding.codexFpMatchPlaceholder` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | match; '/'-separate variants (e.g. session-id / session_id or x-codex-) | Match value or path | 保留上游 |
| 127 | `admin.settings.gatewayForwarding.codexHardeningDesc` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Only affects OpenAI OAuth accounts with 'Codex official clients only' enabled (global). Beyond User-Agent/Originator, harden the decision with a version range, an engine-fingerprint gate, and black/whitelists. | Restrict Codex client versions, fingerprints, and origins to reduce abnormal client risk. | 保留上游 |
| 128 | `admin.settings.gatewayForwarding.codexHardeningTitle` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Codex Settings | Codex hardening | 保留上游 |
| 129 | `admin.settings.gatewayForwarding.codexOriginatorPlaceholder` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | originator (exact, e.g. opencode) | Originator | 保留上游 |
| 130 | `admin.settings.gatewayForwarding.codexUaContainsPlaceholder` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | User-Agent contains markers, comma-separated (e.g. opencode/) | User-Agent contains | 保留上游 |
| 131 | `admin.settings.gatewayForwarding.codexVersionHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Official clients only: checks their version against the [min, max] range. Leave a side empty to not limit it. | Leave blank to disable that version boundary. | 保留上游 |
| 132 | `admin.settings.gatewayForwarding.codexWhitelist` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | User-Agent/Originator Whitelist | Codex whitelist | 保留上游 |
| 133 | `admin.settings.gatewayForwarding.codexWhitelistDesc` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Allow clients outside the official set: requires exact originator and every User-Agent marker present. Still subject to the fingerprint gate unless 'Skip engine fingerprint' is checked. | Requests matching whitelist rules can be explicitly allowed. | 保留上游 |
| 134 | `admin.settings.gatewayForwarding.codexWhitelistSkipFingerprint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Skip engine fingerprint | Skip fingerprint | 保留上游 |
| 135 | `admin.settings.gatewayForwarding.codexWhitelistSkipFingerprintTooltip` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Risk: when checked this entry is allowed on originator + User-Agent alone (both forgeable), with no engine-fingerprint backstop. Use only for trusted third-party clients that genuinely do not send a codex engine fingerprint. | Skip Codex fingerprint checks when this whitelist rule matches. | 保留上游 |
| 136 | `admin.settings.gatewayForwarding.maxCodexVersion` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Max Codex Version | Maximum Codex version | 保留上游 |
| 137 | `admin.settings.gatewayForwarding.maxCodexVersionPlaceholder` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | e.g. 0.200.0 | e.g. 0.35.0 | 保留上游 |
| 138 | `admin.settings.gatewayForwarding.minCodexVersion` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Min Codex Version | Minimum Codex version | 保留上游 |
| 139 | `admin.settings.gatewayForwarding.minCodexVersionPlaceholder` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | e.g. 0.142.0 | e.g. 0.20.0 | 保留上游 |
| 140 | `admin.settings.gatewayForwarding.resetSystemBlocks` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Reset defaults | Reset system blocks | 保留上游 |
| 141 | `admin.settings.gatewayForwarding.systemBlockCacheControl` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Cache control | Enable cache control | 保留上游 |
| 142 | `admin.settings.gatewayForwarding.systemBlockHide` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Hide block details | Collapse | 保留上游 |
| 143 | `admin.settings.gatewayForwarding.systemBlockPresetBilling` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Billing header | Billing note | 保留上游 |
| 144 | `admin.settings.gatewayForwarding.systemBlockPresetExpansion` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Claude Code expansion | Capability expansion | 保留上游 |
| 145 | `admin.settings.gatewayForwarding.systemBlockPresetIdentity` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Claude Code identity | Identity note | 保留上游 |
| 146 | `admin.settings.gatewayForwarding.systemBlockShow` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Show block details | Expand | 保留上游 |
| 147 | `admin.settings.gatewayForwarding.systemBlockText` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Content | Prompt text | 保留上游 |
| 148 | `admin.settings.gatewayForwarding.systemBlockTitle` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | System Block {index} | System block #{index} | 保留上游 |
| 149 | `admin.settings.openaiExperimentalScheduler.defaultPlaceholder` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | config/default: {value} | Default: {value} | 保留上游 |
| 150 | `admin.settings.openaiExperimentalScheduler.errorRateWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Error rate | Error rate weight | 保留上游 |
| 151 | `admin.settings.openaiExperimentalScheduler.loadWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Load | Load weight | 保留上游 |
| 152 | `admin.settings.openaiExperimentalScheduler.previousResponseWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | previous_response sticky | Previous response weight | 保留上游 |
| 153 | `admin.settings.openaiExperimentalScheduler.priorityWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Priority | Priority weight | 保留上游 |
| 154 | `admin.settings.openaiExperimentalScheduler.queueWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Queue | Queue weight | 保留上游 |
| 155 | `admin.settings.openaiExperimentalScheduler.quotaHeadroomWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Quota headroom | Quota headroom weight | 保留上游 |
| 156 | `admin.settings.openaiExperimentalScheduler.resetWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Reset window | Reset weight | 保留上游 |
| 157 | `admin.settings.openaiExperimentalScheduler.sessionStickyWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | session_hash sticky | Session sticky weight | 保留上游 |
| 158 | `admin.settings.openaiExperimentalScheduler.stickyWeightedDescription` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | When enabled, previous_response_id and session_hash affinity are scored by the advanced scheduler. When disabled, sticky accounts keep the legacy hard-hit behavior. | Select upstream accounts using account load, error rate, latency, and session stickiness. | 保留上游 |
| 159 | `admin.settings.openaiExperimentalScheduler.stickyWeightedTitle` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Sticky weighting | Enable sticky weighted scheduling | 保留上游 |
| 160 | `admin.settings.openaiExperimentalScheduler.subscriptionPriorityDescription` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | When enabled, the scheduler scores ChatGPT subscription accounts first and falls back to non-subscription accounts only if no subscription slot can be acquired. | Subscription priority participates in account scheduling score. | 保留上游 |
| 161 | `admin.settings.openaiExperimentalScheduler.topKLabel` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | TopK | Candidate Top K | 保留上游 |
| 162 | `admin.settings.openaiExperimentalScheduler.ttftWeight` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | TTFT | TTFT weight | 保留上游 |
| 163 | `admin.settings.openaiExperimentalScheduler.weightsDescription` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Blank values use config/environment values; when config is not set, built-in defaults apply. Non-blank page settings take priority. | Tune each factor used in the scheduling score. | 保留上游 |
| 164 | `admin.settings.openaiExperimentalScheduler.weightsTitle` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Scheduler weight overrides | Scheduling weights | 保留上游 |
| 165 | `admin.settings.payment.addCustomMethod` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Add method | Add custom method | 保留上游 |
| 166 | `admin.settings.payment.customMethodType` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Payment type | Method key | 保留上游 |
| 167 | `admin.settings.payment.customMethodUpstreamType` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Upstream type | Upstream method | 保留上游 |
| 168 | `admin.settings.payment.easypayCustomMethods` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Custom EasyPay methods | EasyPay custom methods | 保留上游 |
| 169 | `admin.settings.payment.easypayCustomMethodsHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Add provider-specific methods supported by this EasyPay endpoint. The payment type is stored on Sub2API orders; the upstream type is sent as EasyPay type. | Map custom payment types returned by EasyPay. | 恢复本地（去上游品牌） |
| 170 | `admin.settings.payment.enabledPaymentTypesHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Disabling a provider will also disable its instances. | Disabling a payment method will also disable matching provider instances. | 保留上游 |
| 171 | `admin.settings.payment.findProvider` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Looking for a suitable EasyPay provider? | View supported payment methods | 保留上游 |
| 172 | `admin.settings.payment.subscriptionUsdToCnyRate` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Subscription USD to CNY Rate | Subscription USD to CNY rate | 保留上游 |
| 173 | `admin.settings.payment.subscriptionUsdToCnyRateDisabled` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Disabled (price charged as-is) | This rate is not used when CNY subscription payments are disabled. | 保留上游 |
| 174 | `admin.settings.payment.subscriptionUsdToCnyRateHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | CNY charged per 1 USD of plan price on CNY channels (e.g. 7.15). 0 or empty = disabled, plan price is charged as-is. When enabled, all plan prices must be set in USD | Used to convert subscription plan USD prices to CNY payment amounts. | 保留上游 |
| 175 | `admin.settings.payment.validationEasyPayCustomMethodDuplicate` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Custom EasyPay payment types must be unique | Custom payment methods cannot be duplicated. | 保留上游 |
| 176 | `admin.settings.payment.validationEasyPayCustomMethodPrefixReserved` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Custom EasyPay payment types cannot start with alipay or wxpay | Custom payment method cannot use a reserved prefix. | 保留上游 |
| 177 | `admin.settings.payment.validationEasyPayCustomMethodRequired` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Each custom EasyPay method requires both a payment type and an upstream type | Enter a custom payment method. | 保留上游 |
| 178 | `admin.settings.payment.validationEasyPayCustomMethodReserved` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Custom EasyPay payment types cannot use built-in alipay or wxpay | This payment method is reserved by the system. | 保留上游 |
| 179 | `admin.settings.payment.validationEasyPayCustomMethodTypeInvalid` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Custom EasyPay payment types may only contain lowercase letters, digits, underscores, and hyphens | Payment method key format is invalid. | 保留上游 |
| 180 | `admin.settings.payment.validationEasyPayCustomMethodUpstreamTypeInvalid` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | EasyPay upstream types may only contain lowercase letters, digits, underscores, and hyphens | Upstream payment method format is invalid. | 保留上游 |
| 181 | `admin.settings.site.apiBaseUrlHint` | frontend/src/i18n/locales/en/admin/settings.ts | 回退为旧上游文案 | Used for "Use Key", "Import to CC Switch", and callback URL suggestions. Leave empty to use current site URL. | Used for "Use Key" and "Import to CC Switch" features. Leave empty to use current site URL. | 保留上游 |
| 182 | `adminCompliance.accept` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Acknowledge and Continue | Confirm and continue | 恢复本地（法务/合规定制） |
| 183 | `adminCompliance.accepted` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Compliance acknowledgment recorded | Compliance commitment confirmed | 恢复本地（法务/合规定制） |
| 184 | `adminCompliance.acceptFailed` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Failed to submit acknowledgment | Confirmation failed. Please try again later. | 恢复本地（法务/合规定制） |
| 185 | `adminCompliance.blockingNotice` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Deployment and operation compliance acknowledgment is required before continuing to use the console. | Compliance commitment confirmation is required | 恢复本地（法务/合规定制） |
| 186 | `adminCompliance.documentSource` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | The agreement text comes from Markdown files in this project repository. When the agreement content changes, the document version must be incremented; acknowledgments of older versions become invalid and console users must acknowledge again. | This document comes from the compliance commitment file in the repository. Acknowledgements are stored per admin account. | 恢复本地（法务/合规定制） |
| 187 | `adminCompliance.inputLabel` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Type the following confirmation phrase exactly | Type the confirmation phrase exactly | 恢复本地（法务/合规定制） |
| 188 | `adminCompliance.inputMismatch` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | The confirmation phrase does not match. Type the displayed text exactly. | Enter the exact confirmation phrase shown above | 恢复本地（法务/合规定制） |
| 189 | `adminCompliance.inputPlaceholder` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Type the confirmation phrase to continue | Enter the confirmation phrase to enable confirmation | 恢复本地（法务/合规定制） |
| 190 | `adminCompliance.legalNote` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | This acknowledgment defines the no-affiliation relationship and responsibility boundary between self-hosted instances and the open-source project, copyright holders, contributors, and maintainers. The party that deploys, operates, or controls the relevant instance remains independently responsible for its applicable obligations. | The system records the confirmed version, time, account identifier, IP address, and User-Agent for compliance audit trail. | 恢复本地（法务/合规定制） |
| 191 | `adminCompliance.openDocument` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Open the GitHub document | Open source document | 恢复本地（法务/合规定制） |
| 192 | `adminCompliance.riskNotice` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | This acknowledgment provides clear, conspicuous, and reproducible notice of compliance obligations and operation risks for self-hosted instances. | Before continuing to the admin console, please review and confirm the current Deployment and Operation Compliance Commitment. | 恢复本地（法务/合规定制） |
| 193 | `adminCompliance.title` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Deployment and Operation Compliance Acknowledgment | Deployment and Operation Compliance Confirmation | 恢复本地（法务/合规定制） |
| 194 | `adminCompliance.version` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Document Version | Version | 恢复本地（法务/合规定制） |
| 195 | `common.peakRateImageNote` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | ; image tokens billed as tokens are also affected, per-image billing is unaffected | ; image pricing follows image billing rules | 保留上游 |
| 196 | `common.peakRateTooltip` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Peak rate: {window} | Peak rate window: {window} | 保留上游 |
| 197 | `legal.empty` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | No content | No document content available | 恢复本地（法务/合规定制） |
| 198 | `legal.loginAgreement` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Login Agreement | Login agreement | 恢复本地（法务/合规定制） |
| 199 | `legal.loginAgreementPrompt.accept` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Accept and continue | Agree and continue | 恢复本地（法务/合规定制） |
| 200 | `legal.loginAgreementPrompt.dialogDescription` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Our service terms were updated on {date}. Please read and accept the following terms before continuing. | The agreement version was updated on {date}. Please review the related documents before continuing. | 恢复本地（法务/合规定制） |
| 201 | `legal.loginAgreementPrompt.dialogTitle` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Terms Update Notice | Confirm login agreement | 恢复本地（法务/合规定制） |
| 202 | `legal.loginAgreementPrompt.loginRejectedWarning` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Account/password login and quick sign-in are disabled until you accept the latest terms. | You must accept the login agreement before continuing to sign in. | 恢复本地（法务/合规定制） |
| 203 | `legal.loginAgreementPrompt.loginRequiredWarning` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Please read and accept the latest terms before logging in. | Please confirm the login agreement first. | 恢复本地（法务/合规定制） |
| 204 | `legal.loginAgreementPrompt.noticeDescription` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Account/password login and quick sign-in stay disabled until you accept. | Please read and accept the related agreements before continuing. | 恢复本地（法务/合规定制） |
| 205 | `legal.loginAgreementPrompt.noticeTitle` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Accept the latest terms before continuing. | Login agreement confirmation required | 恢复本地（法务/合规定制） |
| 206 | `legal.loginAgreementPrompt.registerRejectedWarning` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Registration and quick sign-in are disabled until you accept the latest terms. | You must accept the login agreement before continuing to register. | 恢复本地（法务/合规定制） |
| 207 | `legal.loginAgreementPrompt.registerRequiredWarning` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Please read and accept the latest terms before registering. | Please confirm the login agreement first. | 恢复本地（法务/合规定制） |
| 208 | `legal.loginAgreementPrompt.reject` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Reject | Decline | 恢复本地（法务/合规定制） |
| 209 | `legal.notFoundDescription` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | This legal document does not exist or has been removed by an administrator. | This legal document does not exist or has not been published. | 恢复本地（法务/合规定制） |
| 210 | `legal.retryLater` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Refresh the page and try again later. | Please try again later or contact the administrator. | 恢复本地（法务/合规定制） |
| 211 | `legal.updatedAt` | frontend/src/i18n/locales/en/common.ts | 回退为旧上游文案 | Updated: {date} | Updated on {date} | 恢复本地（法务/合规定制） |
| 212 | `keys.columnSettings` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Column Settings | Column settings | 保留上游 |
| 213 | `keys.currentConcurrency` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Current Concurrency | Current concurrency | 保留上游 |
| 214 | `usage.cacheBreakdown` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Cache Token Breakdown | Cache breakdown | 保留上游 |
| 215 | `usage.cacheCreationTokensLabel` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Cache Creation | Cache creation tokens | 保留上游 |
| 216 | `usage.cacheReadTokensLabel` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Cache Read | Cache read tokens | 保留上游 |
| 217 | `usage.cacheTotal` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Cache | Cache total | 保留上游 |
| 218 | `usage.errors.allStatuses` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | All status codes | All statuses | 保留上游 |
| 219 | `usage.ipGeo.batchFailed` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Failed to batch fetch IP regions | Failed to batch fetch regions | 保留上游 |
| 220 | `usage.ipGeo.batchFetching` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Fetching... | Batch fetching... | 保留上游 |
| 221 | `usage.ipGeo.detailOrg` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | ISP | Organization | 保留上游 |
| 222 | `usage.ipGeo.refreshTitle` | frontend/src/i18n/locales/en/dashboard.ts | 回退为旧上游文案 | Refresh region info | Refresh IP region | 保留上游 |
| 223 | `home.footer.allRightsReserved` | frontend/src/i18n/locales/en/landing.ts | 被新上游文案覆盖 | TikToken is owned by Jerrywell Pte. Ltd. | net is owned by Jerrywell Pte. Ltd. | 恢复本地（品牌化） |
| 224 | `customPage.copiedCode` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Copied | Code copied | 保留上游 |
| 225 | `customPage.copyCode` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Copy | Copy code | 保留上游 |
| 226 | `customPage.copyCodeFailed` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Failed | Failed to copy code | 保留上游 |
| 227 | `customPage.tableOfContents` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Contents | Table of contents | 保留上游 |
| 228 | `payment.admin.refundPending` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Refund pending gateway confirmation | Refund pending | 保留上游 |
| 229 | `payment.admin.subscriptionCnyPayPreview` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | CNY channel charge preview: {amount} | CNY payment preview: ¥{amount} | 保留上游 |
| 230 | `payment.admin.subscriptionCnyPayPreviewWithFee` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | ({feeRate}% fee included: {total}) | Preview with fee: ¥{amount} | 保留上游（保留当前占位符语义） |
| 231 | `payment.planCard.peakRate` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Peak Rate | Peak rate {rate}x | 保留上游（保留当前占位符语义） |
| 232 | `payment.status.refund_pending` | frontend/src/i18n/locales/en/misc.ts | 回退为旧上游文案 | Refund Pending | Refund pending | 保留上游 |

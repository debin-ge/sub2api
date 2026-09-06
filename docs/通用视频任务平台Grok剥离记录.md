# 通用视频任务平台 Grok 剥离记录

> 用户授权：移除前一会话新增的 Grok 视频实现，同时保留项目原有 Grok 功能和 OpenAI 视频必需的共享逻辑。
> 当前状态：新增 Grok 运行时、未发布 SQL 和残留代码清理完成；后续完整审查发现的四项问题已修复。处理前提为用户确认的“生产未执行本次迁移、本地可直接处理”；最新修复与验证见第 8 节，不以第 5、7 节旧结果代替。
> 范围：本记录替代《通用视频任务平台收口清单》中“尚未删除代码”的阶段说明。
> 后续范围再次变更（2026-09-06）：用户已重新开放编辑、延长、角色和可选回调。当前合同与最新测试见《原生视频首发实施与验收方案》；本记录仍只描述 Grok 剥离，不作为原生视频能力开关依据。

## 1. 已删除的范围

- 32 个新增 `grok_video_*.go` 服务、仓储及专属测试文件：耐久作业、后台完成观察、legacy 导入/提升/复核、历史账务预览/审批/apply、专属缓存投影 outbox。
- 4 个 Grok 派发意图复核服务/仓储及测试文件；原生 `video_tasks` 的 unknown 和计费复核仍保留。
- `/admin/videos/grok/*` 和 Grok 派发复核使用的 `/admin/videos/intents*` 路由、DTO、控制器方法、前端 API 与类型。
- 管理页面的 Grok、legacy 和派发意图复核标签、列表、弹窗、命令及专属翻译。
- `GrokVideoRecoveryWorker`、`GrokVideoBillingCorrectionOutboxWorker` 的启动、注入、关闭流程；Wire 已重新生成。
- 原生创建会话中的 Grok 派发、作业绑定、加密回执、回执清理、冻结 Grok 计价与历史 API Key 恢复读取；相关专属测试同步移除。
- 未发布的 255-263、265-269 共 14 个迁移：Grok 作业、派发快照/复核、回执清理、legacy 导入、更正账本及全局 effect archive；不增加退役迁移或只读历史表。
- 254 中的 Grok 状态、派发和回执字段；原生创建意图仅保留 `prepared/native_bound/untracked`，原生绑定不可变与 untracked 用户保留保护仍在。
- 未消费的 `VideoCreateReceipt`、旧状态分支、过时测试桩及全量 multipart 二次解析；保留响应完成检查前的缓冲、流式 parts hash 和上传准入。

删除文件均是本工作树中新增、未跟踪的专属实现；没有按 `grok` 字符串全仓库删除，也没有回退无关改动。

## 2. 明确保留的范围

| 保留项 | 原因 |
|---|---|
| 原有 Grok 网关、OAuth、普通 pending/claim/usage 计费 | 属于项目原有功能；请求链路按 HEAD 原逻辑还原，不迁移成新任务平台 |
| 已有入口分派所需的最小路由桥接 | 原有 Grok 请求不得误入原生 task/hold 或派发意图；不启动耐久作业或历史更正 |
| OpenAI 原生创建幂等与原子 task/hold 绑定 | 同 key 只能绑定一个原生任务，重放不能重复收费或提交 |
| 原生 JSON/multipart 规范 hash 与加密临时输入 | 仍是原生视频的正确性和输入安全要求 |
| owner/分组授权、执行/计价快照、预算、资金 outbox、回调和删除恢复 | 这些保护对 OpenAI 视频及已有共享链路仍然必要 |
| 原有 usage billing outbox、dedup 及 dedup archive | 仍承担原生与原有业务的幂等计费；只为 Grok 更正引入的 effect archive 已删除，不能混为一谈 |

`Grok` 关键词仍会出现在项目原有功能、最小路由分派及历史文档中，不能用“全仓库无 Grok”作为完成标准。新增迁移中不再安装 Grok 作业/复核/更正对象，负向回归测试仍会引用其名称。

## 3. 路由和鉴权边界

- 新创建意图只处理原生视频操作，不再接管原有 `/videos/generations` 或 Grok 平台请求。
- Composite JSON 先确定目标，再对 OpenAI 原生路径领取意图；本地已有原生任务仍优先按原任务重放，不因当前路由变化重新生成。
- Composite multipart 在解析后先分派，只有原生路径领取 task/hold 创建意图；旧网关转发不创建新 Grok 作业。
- 共享认证对原生视频延后付费检查时，转向原有 Grok 网关的分支仍必须检查有效 Key/额度，不能因剥离恢复逻辑而放开旧入口。
- 创建意图不再声明 Grok 历史状态；未知状态仍按默认分支拒绝，不实现历史兼容、自动重放或退款。

## 4. 数据库边界

用户确认生产环境没有执行本次视频平台迁移或处理，本地开发环境可以直接调整。因此直接整理尚未发布的迁移集合，不增加前向退役、数据归档、历史修复或 checksum 兼容机制。

剥离时视频平台保留 238-254 与 264；只有 254 合并了原生用户保护并剥离 Grok 字段和分支，其他保留 SQL 不改写。后续另行新增 265_video_failed_auto_release.sql 与 266_video_task_provider_url.sql，分别用于失败任务自动释放和视频 URL 存储，不是恢复同编号的旧 Grok SQL。编号空档无需补齐或重排，迁移框架继续按现有文件排序执行。新数据库不再建立 Grok 专属表、公共实体上的 Grok 删除限制或全局 effect archive 触发器。

验证使用集成测试自动创建的隔离 PostgreSQL/Redis，不接入生产，也不删除用户现有本地数据库。已运行旧版本的个人开发库不能直接视为已清理；重建开发库后使用新迁移集合，禁止绕过全局 checksum 校验。此前“238-269 字节不变、保留 effect archive”的说明仅适用于第 5 节旧检查点，已被本轮决定替代。

## 5. 前轮回归与证据

本节为前轮运行时剥离的历史结果，保留追溯，不代表本轮 SQL 和残留代码清理后的验证。

剥离前检查点：

`/Users/gedebin/Documents/Code/sub2api-checkpoints/video-grok-removal-2026-09-06T04-28-52-936Z`

`before.tar.gz`、`before.json` 和 `staged.patch` 保留剥离前源码、逐文件哈希和原暂存区。最终源码及删除/修改清单另存，不覆盖初始证据。

| 验证 | 前轮结果 | 证据 |
|---|---|---|
| 前端定向 API/UI/翻译 | 4 文件、19 项通过 | 检查点 `frontend-targeted.log` |
| 最终完整仓储集成 | 788 个顶层用例通过；保留 1 个既有子测试 skip | 检查点 `backend-integration-final.jsonl` |
| 生成 Wire | 已重新生成，无新增 Grok 视频 Worker | 检查点 `wire.log` |
| 完整 Go 默认/unit | 通过；最终 unit 使用 `-p 2` 限制并发 | 检查点 `backend-default.log`、`backend-unit-final.log` |
| Go vet | 通过 | 检查点 `backend-vet.log` |
| 完整前端测试/构建 | 328 文件 / 2404 Vitest + 4 privacy 通过；构建通过 | 检查点 `frontend-tests-final.log`、`frontend-privacy.log`、`frontend-build-final.log` |
| 最后差异复验 | 原生零预算重放通过；5 个包的 Video/Grok 定向 race 通过 | 检查点 `backend-integration-final.jsonl`、`backend-race-final.log` |
| 源码边界/暂存区/归档 | 36 文件删除；原有核心链路与 HEAD 一致，SQL/暂存区不变 | 检查点 `scope-audit.json`、最终源码清单及校验文件 |

新增保护包括：已删除管理入口返回 404、前端不再导出 Grok 管理命令、页面不再含 Grok/legacy/派发复核工具、Wire 不启动新增 Worker、原有 Grok 请求不进入原生意图，以及原生任务在余额和 Key 额度耗尽后仍可按原任务重放。

中间编译错误和旧测试断言不作为最终通过证据；同步删除功能专属测试，不删除原生视频或项目原有 Grok 的正确性要求。

资源竞争下的首轮完整前端有 2 个未修改的 Radar 路由测试超时；降低为 2 个 Vitest worker 后 2404 项全部通过，没有修改无关测试或提高超时时限。首轮并行 unit 和构建为降低资源占用主动终止，退出码 143 的日志保留，不记为通过；以上表格只引用最终成功记录。

仓储子测试 `TestConcurrencyCacheSuite/TestGetAccountsLoadBatch` 是原代码中的显式 skip，本次未新增或掩盖跳过。Go race 为指定包/名称范围，不是整个项目的 race 全覆盖；前端构建保留既有 chunk size 等非阻塞警告。所有本次执行的测试和构建进程均在收尾前结束。

## 6. 完成边界

本次交付新增 Grok 运行时及未发布迁移精简，不继续实现可信上传计量、全系统硬 reservation、自动 principal 验真或任何 Grok 迁移/更正能力。真实 Provider、浏览器人工验收及生产发布没有执行，不扩大宣称为生产上线通过。

## 7. 前阶段残留清理验证

范围限定为 SQL 精简、原生创建意图、响应缓冲、multipart 哈希与测试/文档校正。前端、原有 Grok 网关和迁移执行框架未修改。

- 已增加新库“不安装被删除的表、函数、触发器和字段”的负向检查。
- 保留并验证原生 task/hold 原子性、冻结绑定、并发领取、租约代次和 untracked 封锁；原生未知提交/计费复核不删除。
- multipart 校验测试转至实际上传解析入口；响应测试覆盖正常发布、完成检查失败、panic、超限及未绑定成功的隔离。

| 本轮验证 | 最终结果 |
|---|---|
| `CI=true go test -p 2 -tags=integration ./internal/repository -count=1 -timeout=5m` | 退出码 0；隔离新库迁移、全量仓储集成通过，仓储包耗时 31.716 秒 |
| `go test -p 2 -tags=unit ./... -count=1 -timeout=5m` | 退出码 0；完整后端通过，含新增缓冲/上传校验，服务包耗时 181.541 秒 |
| `go vet -p 1 ./...` | 退出码 0 |
| `git diff --check HEAD` | 退出码 0 |
| 清理前快照对比 | 删除 14 个 SQL，仅修改保留 SQL 中的 254；其余 319 个保留 SQL 字节不变 |
| 原有 Grok 核心链路核对 | `grok_media.go` 两处、`openai_gateway_usage.go`、`gateway_cache.go` 与 HEAD 无差异 |

首轮定向测试有两个新增测试预期不准确（MIME 示例可被解析、标量上限错误码应为 400），已修正测试而未改变业务准入规则，最终完整 unit 已覆盖通过。本轮未修改前端，不重复前端构建；未重跑 race、真实 Provider 或浏览器人工验收。集成套件保留前轮已说明的既有显式 skip，不以退出码 0 代表没有跳过。所有本轮测试进程均已结束。

## 8. 审查问题修复与验证（2026-09-06）

本阶段只修复完整审查发现的四项问题，不增加 Grok 兼容、迁移、历史更正或新的后台处理系统，也不修改迁移 SQL。

| 问题 | 修复内容 |
|---|---|
| 角色删除调用错误接口且缺少持久化恢复 | 复用原生任务删除流程，角色任务调用 `DeleteCharacter`；删除成功时，在同一个带租约校验的事务中更新任务、角色资源与事件；失败由既有 Worker 重试 |
| 角色在结算完成前可读取、披露或复用 | 校验角色与源任务的用户、账户、Provider、操作及上游 ID 绑定；读取、披露、复用要求源任务 completed/captured 且未进入删除；普通任务披露入口同步拦截未结算角色 |
| 回调排队期间租约过期、旧领取者仍可发送或写入 | 每次只领取一个回调、使用独立领取令牌，发送前与执行期间续租，续租失败取消 HTTP；续租和结果写入必须匹配未过期租约；修复隔离 SQL 的未定型参数 |
| 管理页面异步响应覆盖当前页面或重开旧详情 | 概览、列表、详情分别跟踪请求序号；关闭、切换和卸载使旧响应失效；操作完成后只刷新仍对应同一次选择的详情 |

接口行为变化：角色创建在扣费尚未完成时只返回本地 ID、对象类型、创建时间和 `status: creating`，不披露上游 ID、名称或元数据；此时读取、复用仍受结算检查限制。角色 DELETE 持久化提交后返回 HTTP 409 和 `Retry-After: 3`，由既有 Worker 完成删除；删除完成后重复 DELETE 返回成功，跨用户请求仍拒绝。

新增回归覆盖：角色结算/披露/复用与源任务绑定、删除端点和重试、任务与资源删除的事务回滚、回调过期/旧领取者写入拒绝、慢 HTTP 续租及失租取消，以及前端六类请求顺序场景。

| 本阶段验证 | 最终结果 |
|---|---|
| `CI=true go test -p 1 -tags=integration ./internal/repository -count=1 -timeout=5m` | 通过；全量仓储集成耗时 36.549 秒，使用隔离测试数据库，保留既有显式 skip |
| 前端定向 Vitest（单 Worker） | 8 个文件、33 项测试通过，含新增的 6 项请求顺序测试 |
| `npm run typecheck`、`npm run build` | 通过；构建入口隐私检查通过，保留既有构建警告 |
| `go vet -p 1 ./...` | 通过 |
| `go test -p 2 -tags=unit ./... -count=1 -timeout=5m` | 通过；完整后端 unit，服务包耗时 183.492 秒 |
| `go test -p 1 -race ./internal/service -run 'TestVideoCallback(Claims\|LostLease\|Heartbeat)\|TestVideoCharacter' -count=1 -timeout=3m` | 通过；本次角色和回调租约定向 race，非全项目 race |
| `git diff --check HEAD`、本次 Go 文件 `gofmt -l` | 通过；格式检查无输出 |
| 原有 Grok 核心链路核对 | `grok_media.go` 两处、`openai_gateway_usage.go`、`gateway_cache.go` 与 HEAD 无差异 |

首次定向仓储验证发现回调隔离 SQL 的 `$3` 参数类型错误，已修复并由最终全量仓储集成覆盖；首次失败不计为通过。所有本阶段测试和构建进程均已结束。

本阶段未执行生产迁移、现有开发库重建、真实 Provider 请求或浏览器端到端验收；本地自动化通过不等于生产发布通过。回调仍为至少一次投递，不承诺外部接收方仅收到一次。

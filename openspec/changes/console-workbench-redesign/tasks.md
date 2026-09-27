## 0. 准备（0.5 天）

- [ ] 0.1 从 `test` 切出 `feat/console-workbench`；把 `design-mockups/console-d.html` 作为视觉基线，截图 1440 / 1920 / 400 × 浅 / 深归档到 `design-mockups/baseline/`
- [ ] 0.2 建立核对表：从 `components/layout/AppSidebar.vue` 抄出全部条目（路径、i18n key、图标、featureFlag、hideInSimpleMode、exact、tour 锚点），作为 `consoleNav.spec` 的断言来源
- [ ] 0.3 记录当前 `pnpm test:run` / `typecheck` / `lint:check` 基线结果

## 1. 令牌与全局样式（1.5 天）

- [ ] 1.1 `tailwind.config.js`：新增 `colors.ground`，删除 `backgroundImage.mesh-gradient`
- [ ] 1.2 `style.css` `@layer base`：新增 `--zt-*` 语义变量（浅 / 深两套），`body` 背景改 `ground`
- [ ] 1.3 `style.css` `@layer components`：按 design.md §1 重写 `.btn*`、`.input*`、`.badge*`、`.card*`、`.table*`、`.tabs/.tab`、`.dropdown`、`.modal-*`、`.dialog-*`、`.toast`、`.progress`、`.switch`、`.code*`；新增 `.btn-danger`、`.badge-plat`
- [ ] 1.4 新增 `styles/console.css`：`.page-head .kpis .sec .panel .toolbar .statrow .meter .cell-meter .split .form .hbars .health .tile .plan .amounts .pay .code .steps .feed .plist .ops-log .lat` 与响应式断点（从设计稿 CSS 迁入，去掉 mockup 专用类）
- [ ] 1.5 `main.ts` 引入 `console.css`；保留 `effects.css`
- [ ] 1.6 截图核对 `BaseDialog`、`ConfirmDialog`、`CreateAccountModal`、`EditAccountModal`、`GroupsView` 编辑弹窗、`KeysView` 创建弹窗在浅 / 深主题下的表现

## 2. 导航模型（1 天）

- [ ] 2.1 新建 `navigation/consoleNav.ts`：`NavItem / NavGroup / NavArea` 类型、`buildConsoleNav(ctx)` 与 `buildAdminNav(ctx)`（ctx 注入 t、featureFlags、isSimpleMode、backendModeEnabled、customMenuItems、purchaseNavLabel）
- [ ] 2.2 迁移 `AppSidebar.vue` 中全部条目到分组结构（design.md §3）：条目集合、父子结构（`children` / `expandOnly`）、顺序原样保留，只加域分组
- [ ] 2.3 实现 `filterNav(area, ctx)`（featureFlag `!== false` 宽容语义、简单模式过滤、空分组隐藏、自定义菜单项追加到「更多」）与 `resolveCrumb(route, nav)`（exact / 前缀高亮规则一致）
- [ ] 2.4 `stores/app.ts`：新增 `navCollapsedGroups` 持久化与 setter
- [ ] 2.5 `navigation/__tests__/consoleNav.spec.ts`：按 0.2 核对表逐条断言；覆盖 payment / subscription / availableChannels / channelMonitor / affiliate / riskControl / pluginManagement / ops / batchImage / videoPlayground 开关、简单模式、后端模式、自定义菜单、`/admin/users` 与 VIP 对账高亮、`/purchase` 文案随计费模式
- [ ] 2.6 i18n：`locales/zh|en/common.ts` 新增 `nav.area.*`、`nav.group.*`、`console.*`；跑 `pnpm check:i18n`

## 3. 壳层组件（2.5 天）

- [ ] 3.1 `components/layout/SideRail.vue`：品牌区、`AreaSwitch`（仅管理员）、分组导航（域折叠、可展开子组、tour 锚点、`iconSvg` 经 `sanitizeSvg`）、收起态（64px + title）、移动抽屉、滚动位置持久化；不做搜索入口与计数徽标
- [ ] 3.2 `components/layout/RailUserMenu.vue`：用户卡 + 弹层（资料、密钥、联系客服 / `CustomerSupportDialog`、重放引导、退出、移动端余额）；主题切换按钮
- [ ] 3.3 `components/layout/ContextBar.vue`：面包屑、余额胶囊（冻结悬浮）、模型广场、文档、`SubscriptionProgressMini`、`AnnouncementBell`、`LocaleSwitcher`、移动端菜单按钮
- [ ] 3.4 `components/console/PageHead.vue`；`AppLayout.vue` 重写为新壳：默认按 `resolveRouteMetaKeys` 渲染 `PageHead`，支持 `#actions` 插槽与 `:page-head="false"`；`useOnboardingTour` 挂载点不变
- [ ] 3.5 删除 `AppSidebar.vue`、`AppHeader.vue`；`main.ts` 成为主题初始化唯一入口
- [ ] 3.6 测试：`SideRail.spec`（迁移 AppSidebar.spec 十项）、`ContextBar.spec`（迁移 AppHeader.spec 三项）、`AppLayout.spec`（PageHead 默认渲染与插槽）
- [ ] 3.7 `TablePageLayout.vue` 改为 panel + toolbar + table-wrap(max-height) + pager；`TablePageLayout.spec` 现有断言通过，新增「桌面端 table-wrap 有 max-height 且 overflow auto」断言；用 `admin/UsageView` 1000 行验证虚拟滚动

## 4. 共享组件与图表主题（2 天）

- [ ] 4.1 `components/console/`：`KpiRow / KpiItem / Sparkline / SectionBlock / Panel / Toolbar / TabStrip / StatPills / MeterCell / HorizontalBars / FormSection / FormRow / SplitView / HealthCard / Tile / TileGrid / CodeBlock / Steps / PlanCard / AmountPicker / PayMethodPicker / ActivityFeed`，各带最小 spec
- [ ] 4.2 重样式 `DataTable`、`Pagination`、`StatusBadge`、`GroupBadge`、`Select`、`Input`、`SearchInput`、`TextArea`、`Toggle`、`EmptyState`、`Skeleton`、`LoadingSpinner`、`HelpTooltip`、`Toast`；`StatCard` 标记 `@deprecated`
- [ ] 4.3 `composables/useChartTheme.ts`：Chart.js defaults 补丁 + 浅 / 深类别色 + `version` 触发更新
- [ ] 4.4 接入 `TokenUsageTrend`、`ModelDistributionChart`、`GroupDistributionChart`、`EndpointDistributionChart`、`UserDashboardCharts`、`ops/components/Ops*Chart.vue`；移除硬编码调色板；深色截图核对

## 5. 用户端核心页（3 天）

- [ ] 5.1 仪表盘：`UserDashboardStats` 八项指标改两行 `KpiRow`、平台拆分改列表版式（指标与配额条不变）；`UserDashboardCharts` 时间范围 / 粒度 / 刷新控件原样移到页头；`UserDashboardRecentUsage` → `ActivityFeed`；`UserDashboardQuickActions` 保留、改页头操作组样式；附改版前后控件清单；更新 `DashboardView.spec`
- [ ] 5.2 API 密钥：`KeysView.vue` 拆出 `components/keys/KeyList.vue`、`KeyDetail.vue`；`SplitView` 与现有 DataTable「列表视图」可切换；创建 / 编辑 / 删除 / 启用禁用 / 使用说明 / 批量编辑 / 列设置 / 分组切换 / 复制 / 导入 CCS / 端点复制测速全部保留且接线不变；`data-tour="keys-create-btn"` 随按钮移到页头主操作；附控件清单；更新 `KeysView.spec`
- [ ] 5.3 使用记录（用户 / 管理）：`UsageStatsCards` → `KpiRow`（指标不变）；现有分布图组件换主题（指标 / 来源切换保留）；现有页签换 `TabStrip` 样式；更新两份 `UsageView.spec`

## 6. 用户端其余页（2.5 天）

- [ ] 6.1 可用渠道 `AvailableChannelsView` → `StatPills` + `TileGrid`
- [ ] 6.2 渠道状态 `ChannelStatusV1View / V2View` → `StatPills` + `HealthCard` + 事件列表
- [ ] 6.3 我的订阅 `SubscriptionsView` → 订阅卡 + 空态
- [ ] 6.4 充值 / 订阅 `PaymentView` → `TabStrip` + `AmountPicker` + `PayMethodPicker` + 订单摘要 + `PlanCard`；`paymentUx.spec`、`PaymentView.spec` 通过
- [ ] 6.5 我的订单 `UserOrdersView` 列与徽章精修
- [ ] 6.6 兑换 `RedeemView` → 两栏 Panel + 记录表
- [ ] 6.7 邀请返利 `AffiliateView` → `KpiRow` + 链接 Panel + `TabStrip`
- [ ] 6.8 个人资料 `ProfileView` → `FormSection` 五段；`ProfileView.spec` 通过
- [ ] 6.9 批量生图 `BatchImageGuideView` → `Steps` + `CodeBlock` + 任务 Panel
- [ ] 6.10 视频 Playground `VideoPlaygroundView` → 左表单 Panel + 右任务列表
- [ ] 6.11 自定义页面 `CustomPageView`、支付结果 / 二维码页只换壳，核对面包屑回退

## 7. 管理端概览类页（2.5 天）

- [ ] 7.1 管理仪表盘 `admin/DashboardView` → 两行 `KpiRow`（现有统计卡全部保留）+ 现有图表区换主题 + 现有列表区换版式；`DashboardView.spec` 通过
- [ ] 7.2 运维监控 `ops/OpsDashboard` → `StatPills` + 吞吐趋势 + 错误分布 / 延迟分位 + 告警规则 / 系统日志 Panel；Ops 子组件 spec 通过
- [ ] 7.3 支付概览 `AdminPaymentDashboardView` → `KpiRow` + 收入趋势 + 支付方式横条 + 最近订单
- [ ] 7.4 内容审核 `RiskControlView` → `KpiRow` + 规则 / 命中记录 Panel；`RiskControlView.spec` 通过
- [ ] 7.5 渠道监控 `ChannelMonitorView` → `StatPills` + `HealthCard`；三份 spec 通过
- [ ] 7.6 插件管理 `PluginsView` → `TileGrid`；`PluginsView.spec` 通过
- [ ] 7.7 系统设置 `SettingsView` → 页头保存 + 现有分区导航换 `TabStrip` 样式 + `FormSection`；分区与字段不增不减；`SettingsView.spec`、`SettingsViewBranding.spec` 通过

## 8. 管理端表格页精修（2 天）

- [ ] 8.1 用户、VIP 对账（含页头 `KpiRow(3)`）、分组（页头 `StatPills`）、订阅管理
- [ ] 8.2 账号管理（页头 `StatPills`、卡片 / 列表切换、并发与窗口用量 `MeterCell`）、渠道定价、模型价格、IP 管理
- [ ] 8.3 视频任务、提示词审计、操作日志
- [ ] 8.4 订单管理、订阅套餐、兑换码、优惠码、邀请返利三页（邀请记录 / 返利记录 / 提取记录）、公告
- [ ] 8.5 每页：主操作按钮原样移到 `PageHead #actions`（功能与 tour 锚点不变），工具栏保留其余全部按钮；附改版前后控件清单；对应 spec 通过

## 9. 清理与验收（1 天）

- [ ] 9.1 删除 `style.css` 中 `.sidebar*`、`.page-header`、`.glass`、`.stat-*`、`.tabs` 旧胶囊样式；删除 `StatCard.vue` 与 `components/common/index.ts` 导出；全库 grep 确认无引用
- [ ] 9.2 新手引导三条流程（分组、账号、密钥）手动走通；`Guide/steps.ts` 锚点全部命中
- [ ] 9.3 1440 / 1920 / 400 × 浅 / 深截图抽样 12 页，与 `design-mockups/baseline/` 对照
- [ ] 9.4 `pnpm typecheck`、`pnpm lint:check`、`pnpm test:run`、`pnpm build` 全绿；`README` 截图与文档页「控制台」相关截图更新
- [ ] 9.5 合入 `test` 分支验证后再进 `main`；发布说明列出壳与导航分组变化

## 里程碑

| 里程碑 | 内容 | 预估 |
|---|---|---|
| M1 | 0–3：令牌、导航模型、新壳、TablePageLayout（全部页面自动换壳） | 5.5 天 |
| M2 | 4–5：共享组件、图表主题、用户端核心三页 | 5 天 |
| M3 | 6：用户端其余十页 | 2.5 天 |
| M4 | 7：管理端概览类七页 | 2.5 天 |
| M5 | 8：管理端表格页精修 | 2 天 |
| M6 | 9：清理与验收 | 1 天 |
| 合计 | | 约 18.5 人日 |

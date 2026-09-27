## Context

动机见 proposal.md，视觉基线是 `design-mockups/console-d.html`（可用 `open design-mockups/console-d.html` 预览，右下角切换用户 / 管理员视角与浅深主题）。与方案相关的现状：

- **壳**：`components/layout/AppLayout.vue`（52 行）渲染 `bg-mesh-gradient` 背景、`AppSidebar`、`AppHeader` 与 `p-4/6/8` 的内容区；40 个视图直接用 `<AppLayout>` 包裹，无 props / 具名插槽。
- **侧栏**：`AppSidebar.vue`（1126 行）内联声明 `NavItem`（`featureFlag`、`hideInSimpleMode`、`exact`、`children`、`expandOnly`、`iconSvg`），用户端 `buildSelfNavItems()`、管理端 `adminNavItems`，自定义菜单项来自 `appStore.cachedPublicSettings.custom_menu_items`（user）与 `adminSettingsStore.customMenuItems`（admin）；简单模式下管理端追加「API 密钥 / 系统设置」；导出新手引导锚点 `#sidebar-group-manage`、`#sidebar-channel-manage`、`#sidebar-wallet`、`[data-tour="sidebar-my-keys"]`；负责主题初始化、滚动位置持久化（`appStore.sidebarScrollTop`）与分组展开覆盖。
- **顶栏**：`AppHeader.vue`（431 行）从 `resolveRouteMetaKeys(route)` 取页标题与描述（`/purchase` 随计费模式切换文案），右侧有模型广场、文档、`LocaleSwitcher`、`SubscriptionProgressMini`、`AnnouncementBell`、余额胶囊（冻结金额悬浮）、用户下拉（资料 / 密钥 / 联系客服 `CustomerSupportDialog` / 重放引导 / 退出）。
- **表格页**：`TablePageLayout.vue` 提供 `actions / filters / table / pagination` 四个插槽，桌面端整页固定高度 `calc(100vh - 64px - 4rem)`，表体在容器内滚动；36 个视图使用。`DataTable` 在行数超过 `virtualizeThreshold`（默认 100）时启用 `@tanstack/vue-virtual`，滚动元素是表格自身的 wrapper。
- **全局类**：`style.css` 的 `btn btn-primary` 被 107 个文件引用，`input-label` 70，`card p-4` 25，`badge badge-*` 15，`stat-card` 6，`page-header` 5；`Select` 组件 237 处，`Pagination` 83，`DataTable` 57，`EmptyState` 40。
- **图表**：`components/charts/{TokenUsageTrend,ModelDistributionChart,GroupDistributionChart,EndpointDistributionChart}.vue` 与 `components/user/dashboard/UserDashboardCharts.vue` 用 vue-chartjs，调色板硬编码（`#3b82f6,#10b981,#f59e0b…`）。
- **主题**：`main.ts` 与 `AppSidebar.vue` 都做 `localStorage.theme` 初始化；公开页各自有切换按钮。
- **令牌**：`tailwind.config.js` 已有 `primary`（indigo）、`accent`（cyan）、`dark`（slate）色阶与 `shadow-glow`；`styles/effects.css` 有 `tech-grid`、`text-gradient` 等品牌动效。

## Goals / Non-Goals

**Goals**
- 导航结构适配「管理端多且平、用户端少、无二级页」：侧栏完整平铺，分组只做视觉分区。
- 视觉与首页 / 登录页一致，且不再让人联想到 sub2api 的壳与卡片网格。
- 页面数据流、API、路由、功能开关、简单模式、后端模式、自定义菜单、新手引导全部保持行为不变。
- 改动集中在壳与全局样式，页面层按「版式模式」批量处理，单页改动尽量只换外层结构。
- 浅色 / 深色同等完成度；1440、1920、400 三档宽度可用，宽屏内容随窗口填满。

**Non-Goals**
- 不改后端；不改 `router/index.ts` 的路径与 meta 语义；不改任何功能开关。
- 不做 ⌘K 搜索、侧栏计数徽标等需要新增数据或逻辑的 UI。
- 不做 DataTable 的功能改造（排序、虚拟滚动、列设置逻辑不动）。

## 约束：功能零改动

- 每个页面改版前后的「功能清单」必须一致：指标集合、按钮与弹窗、筛选与列设置、快捷入口、分页与排序、导航条目与父子结构。改版任务只替换容器、排布与样式类。
- 设计稿中的以下元素判定为功能变化，不实施：侧栏 ⌘K 搜索入口；侧栏计数徽标；管理端邀请返利三页合并为一个入口；仪表盘快捷操作移除；仪表盘 / 管理仪表盘指标由八项裁为四五项。
- 冲突处理规则：设计稿与现有功能冲突时以现有功能为准，用设计稿的版式承载现有功能（例如指标多于设计稿时排成两行 KpiRow）。
- 实施验收以「功能清单核对表」为准：每页在改版 PR 中附改版前后控件清单对照。

## 1. 设计令牌与全局样式

**决策**：保留 Tailwind 色阶，新增语义 CSS 变量作为「表面 / 墨色」令牌；既有全局组件类保留类名、按新令牌重写；新增版式类。

- `tailwind.config.js`：`colors.ground = '#f5f6fb'`（略带靛蓝偏色的浅底，替代 `gray-50`），其余沿用；`backgroundImage.mesh-gradient` 删除。
- `style.css` `@layer base`：`:root` 与 `.dark` 定义 `--zt-ground / --zt-surface / --zt-surface-2 / --zt-surface-3 / --zt-border / --zt-border-2 / --zt-ink / --zt-ink-2 / --zt-ink-3`，取值见设计稿 `:root` 与 `[data-theme="dark"]` 两段；`body` 背景改 `var(--zt-ground)`。
- `@layer components` 重写（类名不变）：
  - `.btn` 高 34px、圆角 9px、13px；`.btn-primary` 保持靛蓝渐变，阴影收敛为 `0 4px 14px rgba(99,102,241,.3)`；新增 `.btn-danger`（浅红底红字）。
  - `.input` 高 34px、圆角 9px、边框 `--zt-border-2`；`.input-label` 12px 中等字重。
  - `.badge` 高 22px 胶囊，新增内部圆点 `i`；`.badge-success/warning/danger/gray/primary` 映射到 good/warn/bad/gray/accent 语义色；新增 `.badge-plat`（分组徽章：浅底细边 + 平台色点 + 等宽倍率）。
  - `.card` 圆角 12px、`--zt-border` 细边、仅 `shadow-card`；`.card-header/body/footer` 内边距同步。
  - `.table` 表头 `--zt-surface-2` 底、11.5px 半粗、行高 40px、行 hover `--zt-surface-2`。
  - `.tabs/.tab` 改为下划线式（渐变 2px 指示条），废弃灰底胶囊式。
  - `.sidebar*`、`.page-header`、`.glass`、`.stat-*` 标记为废弃，在 M6 删除。
- 新增版式类（对应设计稿）：`.page-head`、`.kpis/.kpi`、`.sec/.sec-h`、`.panel/.panel-h/.panel-b`、`.toolbar`、`.statrow/.statpill`、`.meter/.cell-meter`、`.split/.kl/.kd`、`.form/.fsec/.frow`、`.hbars/.hbar`、`.health/.hcard`、`.tile`、`.plan`、`.amounts/.amt`、`.pay`、`.code`、`.steps/.step`、`.feed`、`.plist`、`.ops-log`、`.lat`。这些类只在 `components/console/*` 内使用，视图通过组件而非裸类消费。
- 字体沿用系统栈；数字统一 `font-mono tabular-nums`（表格、KPI、进度）。

## 2. 壳层结构

**决策**：`AppLayout.vue` 文件与导出名保留（40 个视图零改动），内部替换为新壳；旧 `AppSidebar.vue`、`AppHeader.vue` 删除。

```
AppLayout.vue（壳）
├─ SideRail.vue（240px 深色侧栏，fixed）
│  ├─ RailBrand：Logo + 站点名 + 副标题（AI GATEWAY）
│  ├─ AreaSwitch：控制台 / 管理后台（仅管理员显示）
│  ├─ RailNav：分组 → 条目（useConsoleNav）
│  └─ RailFoot：用户卡（头像 / 用户名 / 余额 · 冻结）→ RailUserMenu；主题切换；收起
├─ ContextBar.vue（48px sticky）
│  ├─ Breadcrumb：区域 / 分组 / 页面（由 nav 模型按当前路由派生）
│  └─ 右侧：余额胶囊（含冻结悬浮）、模型广场、文档、SubscriptionProgressMini、AnnouncementBell、LocaleSwitcher
└─ <main class="body">
   ├─ PageHead（默认由路由 meta 渲染标题 / 描述；视图可用 #actions 插槽放主操作，或 :page-head="false" 自绘）
   └─ <slot/>
```

- **宽度**：侧栏 240px，收起 64px（图标 + title 提示）；内容区 `padding: 24px 28px 72px`，不设最大宽度（宽屏填满，网格类版块用 `auto-fill` 增列）。
- **移动端（<1024px）**：侧栏 off-canvas + 遮罩，复用 `appStore.mobileOpen`；上下文条左侧出现菜单按钮；余额、文档等收进用户菜单。
- **用户菜单**：从顶栏下拉迁到侧栏底部弹层，内容与现有一致（资料、密钥、联系客服 / 二维码、重放引导（标准模式管理员）、退出）。
- **主题**：初始化只保留 `main.ts` 一处；`SideRail` 只负责切换与写 `localStorage.theme`。
- **收起状态**：沿用 `appStore.sidebarCollapsed / toggleSidebar`；滚动位置沿用 `sidebarScrollTop`。

## 3. 导航模型

**决策**：新建 `frontend/src/navigation/consoleNav.ts`，把 `AppSidebar.vue` 中的 `NavItem` 声明抽为纯数据 + 过滤函数，侧栏与面包屑共用。

```ts
interface NavItem {
  path: string; labelKey: string; icon: IconName | null; iconSvg?: string
  featureFlag?: () => boolean | undefined; hideInSimpleMode?: boolean; exact?: boolean
  children?: NavItem[]; expandOnly?: boolean   // 原可折叠父项语义原样保留
  tourId?: string; tourAttr?: string
}
interface NavGroup { key: string; labelKey: string; items: NavItem[] }
interface NavArea { key: 'console' | 'admin'; groups: NavGroup[] }
```

- **用户端（console）**：工作台（仪表盘、API 密钥、使用记录）· 生成（批量生图、视频 Playground）· 渠道（可用渠道、渠道状态）· 账单（我的订阅、充值 / 订阅、我的订单、兑换、邀请返利）· 账户（个人资料）· 自定义菜单项追加到「更多」分组。
- **管理端（admin）**：总览（仪表盘、运维监控）· 运营（用户管理、VIP 对账、分组管理、订阅管理）· 渠道与模型（渠道管理 ▸ 渠道定价 / 渠道监控、模型价格、账号管理、IP 管理、插件管理）· 监控与审计（使用记录、视频任务、安全审计 ▸ 内容审核 / 提示词审计、操作日志）· 财务（订单管理 ▸ 支付概览 / 订单管理 / 订阅套餐、兑换码、优惠码、邀请返利 ▸ 邀请记录 / 返利记录 / 提取记录）· 系统（公告、系统设置）· 我的账户（现有 `personalNavItems` 全部条目）· 管理端自定义菜单项追加到「更多」。「▸」表示现有可折叠父项及其子项原样保留。
- **现有语义保留**：
  - `featureFlag` 仍通过 `makeSidebarFlag`（`channelMonitor / payment / availableChannels / subscription / affiliate / riskControl / pluginManagement`）与 `adminSettingsStore.opsMonitoringEnabled / paymentEnabled`、`useBatchImageAccess`、`useVideoPlaygroundAccess`；过滤后为空的分组整组隐藏。
  - `hideInSimpleMode` 与简单模式下管理端追加「API 密钥 / 系统设置」的行为保留；后端模式（`appStore.backendModeEnabled`）下普通用户不渲染 console 区域。
  - 四个可折叠父项（渠道管理、安全审计、订单管理、邀请返利）及其子项、`expandOnly` 与「父项在子路由激活且未展开时高亮」的规则原样保留，只是放进所属域分组；条目集合与改版前逐一对应，不合并、不平铺、不新增。
  - `/admin/users` 与 `/admin/users/vip-reconcile` 的 `exact` 高亮规则保留。
  - 引导锚点：`tourId` 输出为元素 `id`（`sidebar-group-manage`、`sidebar-channel-manage`、`sidebar-wallet`），`tourAttr` 输出 `data-tour="sidebar-my-keys"`；`Guide/steps.ts` 不改。
- **区域判定**：`area = route.path.startsWith('/admin') ? 'admin' : 'console'`；管理员点击区域切换时跳转该区域首页（`/admin/dashboard` 或 `/dashboard`）。非管理员不渲染切换控件。
- **分组折叠**：`appStore.navCollapsedGroups: Record<string, boolean>` 持久化到 `localStorage`（键 `zt.nav.collapsed`），默认全部展开；当前路由所在分组不可被自动折叠掉（手动折叠后仍高亮分组标题）。
- **不做计数徽标**：设计稿侧栏上的「12」「2 异常」为装饰，需要新增数据请求，超出「只改样式」范围。

## 4. 上下文条与页头

- 面包屑：`区域 / 分组 / 页面`，页面名沿用 `resolveRouteMetaKeys` 的 `titleKey`（保证 `/purchase` 的计费模式文案与 `document.title` 一致）；自定义页面用菜单项 `label`。
- `PageHead`：标题 22px 粗体、描述 12.5px 淡色、右侧 `actions` 插槽。默认由 `AppLayout` 根据 `meta.titleKey/descriptionKey` 渲染，视图提供 `<template #actions>` 放主操作（如「创建密钥」）；需要自定义描述（如带日期范围）的视图传 `:page-head="false"` 并自行使用 `<PageHead>`。
- 原 `AppHeader` 中的「移动端余额」「联系客服」「重放引导」等逻辑全部迁到 `RailUserMenu`，`CustomerSupportDialog` 挂载点随之迁移。

## 5. 表格页布局

**决策**：`TablePageLayout` API 不变，内部结构改为设计稿的面板模式；虚拟滚动依赖的固定高度改为受限最大高度。

```
<div class="panel">
  <div class="toolbar">  ← filters 插槽（左）+ actions 插槽（右）
  <div class="table-wrap" style="max-height: calc(100vh - 320px)">  ← table 插槽（DataTable 的滚动元素仍是自身 wrapper）
  <div class="pager">  ← pagination 插槽
</div>
```

- 去掉 `height: calc(100vh - 64px - 4rem)` 的整页固定高度；`table-wrap` 保留 `overflow: auto` 并给 `max-height`，`DataTable` 的 `getScrollElement` 不变，虚拟化不受影响。
- 移动端 `overflow-x-auto` 行为保留（`TablePageLayout.spec` 现有断言仍成立）。
- 主操作按钮位置：M1 保持在工具栏右侧；M5 逐页把「创建 / 新建」类主操作上移到 `PageHead #actions`，工具栏只留刷新、列设置、导出。

## 6. 共享组件

新增 `frontend/src/components/console/`（全部为纯展示组件，无 store 依赖）：

| 组件 | 用途 | 对应设计稿 |
|---|---|---|
| `PageHead` | 标题 / 描述 / 操作插槽 | `.page-head` |
| `KpiRow` / `KpiItem` | 细线分隔的大数字行，可带 `Sparkline`、颜色、副文案 | `.kpis` |
| `Sparkline` | 12 点迷你面积折线（inline SVG，`currentColor`） | `.spark` |
| `SectionBlock` | 标题 + 副标题 + 右侧插槽 + 细线 | `.sec` |
| `Panel` | 细边框面板，头部标题 / 副标题 / 右侧插槽 | `.panel` |
| `Toolbar` | 搜索 / 筛选 / 计数 / 操作一行 | `.toolbar` |
| `TabStrip` | 下划线页签，可带计数 | `.tabs` |
| `StatPills` | 状态摘要胶囊行 | `.statrow` |
| `MeterCell` | 「已用 / 上限」+ 细进度条，≥80% 变警告色 | `.cell-meter` |
| `HorizontalBars` | 横条排行（模型 / 分组 / 错误类别 / 支付方式） | `.hbars` |
| `FormSection` / `FormRow` | 左标题说明、右字段的设置表单 | `.fsec/.frow` |
| `SplitView` | 左列表右详情分栏（密钥页） | `.split` |
| `HealthCard` | 平台健康卡（状态、三指标、延迟迷你图） | `.hcard` |
| `Tile` / `TileGrid` | 卡片网格（可用渠道、插件），`auto-fill` 列 | `.tile/.g3` |
| `CodeBlock` | 深色代码块 + 复制 | `.code` |
| `Steps` | 编号步骤列表 | `.steps` |
| `PlanCard` | 套餐卡（含推荐角标） | `.plan` |
| `AmountPicker` / `PayMethodPicker` | 充值金额档位 / 支付方式 | `.amounts/.pay` |
| `ActivityFeed` | 最近请求 / 最近订单的行式列表 | `.feed` |

重样式（不改 API）：`DataTable`（表头、行高、hover、勾选框强调色）、`Pagination`（等宽页码、当前页浅靛蓝底）、`StatusBadge`（映射到 good/warn/bad/gray + 圆点）、`GroupBadge`（`badge-plat`）、`Select`/`Input`/`SearchInput`/`TextArea`、`Toggle`、`EmptyState`、`BaseDialog`/`ConfirmDialog`（圆角 14px、令牌化边框）、`Toast`、`Skeleton`、`LoadingSpinner`、`HelpTooltip`。`StatCard` 标记废弃，由 `KpiItem` 取代（4 处调用迁移）。

## 7. 图表主题

- 新增 `composables/useChartTheme.ts`：读取当前主题，返回 Chart.js `defaults` 补丁（字体等宽 11px、刻度色 `--zt-ink-3`、网格色 `--zt-border`、线宽 2、点半径 0/悬停 4、面积填充 alpha .10、tooltip 用表面色与细边）与类别色数组。
- 类别色（已用 dataviz 校验器验证相邻色盲可分）：浅色 `#4f46e5 #d97706 #0891b2 #e11d48 #7c3aed #059669`，深色 `#6366f1 #d97706 #0891b2 #f43f5e #8b5cf6 #059669`；「其他」固定灰 `--s-other`。
- `TokenUsageTrend`、`ModelDistributionChart`、`GroupDistributionChart`、`EndpointDistributionChart`、`UserDashboardCharts`、Ops 图表组件统一接入；主题切换时通过 `watch` 重设 `chart.options` 并 `update()`。
- 环形图 / 饼图改为横条排行（`HorizontalBars`）优先；保留环形图的页面（模型分布 Token 占比）用 2px 表面色间隙分片。

## 8. 页面版式映射

| 页面（路由） | 视图文件 | 目标版式 | 主要改动 |
|---|---|---|---|
| 仪表盘 `/dashboard` | `user/DashboardView.vue` + `components/user/dashboard/*` | 概览：两行 KpiRow（八项指标不变）→ 趋势 SectionBlock → 两栏（模型横条 / 平台与配额列表）→ 最近请求 ActivityFeed + 快捷操作 | `UserDashboardStats` 余额 / 密钥 / 今日请求 / 今日消费、今日 Token / 累计 Token / RPM·TPM / 平均响应八项与平台拆分（含配额条）全部保留，只换版式；`UserDashboardCharts` 的时间范围 / 粒度 / 刷新控件原样保留、移到页头；`RecentUsage` 改 ActivityFeed；`QuickActions` 保留，改为页头右侧操作组样式 |
| API 密钥 `/keys` | `user/KeysView.vue`（2392 行） | SplitView：左列表（现有搜索 / 分组 / 状态筛选）右详情（现有各列信息与操作）；「列表视图」切换回现有 DataTable | 现有功能全部保留：创建 / 编辑 / 删除 / 启用禁用 / 使用说明 / 批量编辑 / 列设置 / 分组切换 / 复制 / 导入 CCS / 端点复制与测速；分栏只是把同一份数据换一种排布，详情区的字段与操作与表格列一一对应；`EndpointPopover` 只换样式 |
| 使用记录 `/usage`、`/admin/usage` | `user/UsageView.vue`、`admin/UsageView.vue` | KpiRow（`UsageStatsCards` 指标集合不变）→ 现有筛选控件 → 趋势 → 分布图（模型 / 分组 / 端点，现有的指标切换与来源切换保留）→ Panel + TabStrip（现有页签）+ 表 | 只换容器与图表主题；`UserErrorRequestsTable`、列设置、导出逻辑不动 |
| 批量生图 `/batch-image` | `user/BatchImageGuideView.vue` | 两栏（Steps / CodeBlock）+ 最近任务 Panel | 文案与任务说明沿用 |
| 视频 Playground `/video-playground` | `user/VideoPlaygroundView.vue` | 左表单 Panel（模型 / 提示词 / 参考图 / 分辨率 / 时长 / 预估）+ 右任务列表 | 表单字段沿用现有 `components/video/*` |
| 可用渠道 `/available-channels` | `user/AvailableChannelsView.vue` | StatPills + TileGrid（分组卡：平台徽章、倍率、说明、模型 chips、并发、「用此分组创建密钥」） | 数据来自现有 `userChannelsAPI` |
| 渠道状态 `/monitor` | `user/ChannelStatusV1View.vue` / `V2View.vue` | StatPills + HealthCard 网格 + 近期事件列表 | V1/V2 模式切换保留，只换外层 |
| 我的订阅 `/subscriptions` | `user/SubscriptionsView.vue` | 订阅卡（左信息 + 右三条额度 Meter）+ 空态 Tile | 续费 / 查看用量动作沿用 |
| 充值 / 订阅 `/purchase` | `user/PaymentView.vue` | Panel + TabStrip（充值余额 / 订阅套餐）；充值：AmountPicker + 自定义 + PayMethodPicker + 订单摘要；套餐：PlanCard 网格 | 支付流程（微信 / 支付宝 / Stripe / Airwallex）与 `paymentUx.ts` 不动 |
| 我的订单 `/orders` | `user/UserOrdersView.vue` | TablePageLayout（自动） | 列：订单号（等宽）、类型徽章、内容、金额、方式、状态、时间、操作 |
| 兑换 `/redeem` | `user/RedeemView.vue` | 两栏 Panel（输入 / 说明 Steps）+ 兑换记录 Panel | |
| 邀请返利 `/affiliate` | `user/AffiliateView.vue` | KpiRow(4) + 邀请链接 Panel + Panel/TabStrip 三类记录 | |
| 个人资料 `/profile` | `user/ProfileView.vue` + `components/user/profile/*` | FormSection：基本信息 / 安全 / 通知 / 偏好 / 危险操作 | Passkey、2FA、属性表单沿用 |
| 管理仪表盘 `/admin/dashboard` | `admin/DashboardView.vue` | 两行 KpiRow（现有全部统计卡，含启用时的上游余额）→ 现有图表区（换主题）→ 现有列表区（换版式） | 指标与图表集合不变 |
| 运维监控 `/admin/ops` | `admin/ops/OpsDashboard.vue` + `components/*` | StatPills → 吞吐趋势 → 两栏（错误分布横条 / 延迟分位 Meter）→ 两栏 Panel（告警规则表 / 系统日志） | 现有 Ops 子组件只换容器与图表主题 |
| 支付概览 `/admin/orders/dashboard` | `admin/orders/AdminPaymentDashboardView.vue` | KpiRow(4) → 收入趋势 → 两栏（支付方式横条 / 最近订单 Feed） | |
| 内容审核 `/admin/risk-control` | `admin/RiskControlView.vue` | KpiRow(4) + 两栏 Panel（规则表 / 命中记录） | |
| 渠道监控 `/admin/channels/monitor` | `admin/ChannelMonitorView.vue` | StatPills + HealthCard 网格 | `MonitorFormDialog` 不动 |
| 插件管理 `/admin/plugins` | `admin/PluginsView.vue` | TileGrid（开关在卡片右上） | |
| 系统设置 `/admin/settings` | `admin/SettingsView.vue` | 页头「保存更改」+ 现有分区导航（换页签样式）+ FormSection | 分区、字段、默认值与保存逻辑不增不减 |
| 其余管理端表格页（用户、VIP 对账、分组、订阅管理、账号、渠道定价、模型价格、IP、视频任务、提示词审计、操作日志、订单、套餐、兑换码、优惠码、邀请返利记录、公告） | 对应 `admin/*View.vue` | TablePageLayout（自动）+ 页头 StatPills（账号、VIP 对账、视频任务）+ 主操作上移 | M1 自动换壳，M5 逐页精修列与工具栏 |

## 9. 状态与持久化

- `stores/app.ts` 新增 `navCollapsedGroups`（持久化）与 `setNavGroupCollapsed(key, collapsed)`；`sidebarCollapsed`、`mobileOpen`、`sidebarScrollTop` 沿用。
- 区域不入 store，由路由派生。
- 主题：`localStorage.theme` 单一来源，`main.ts` 初始化。

## 10. 路由与面包屑

- 路由表不改。面包屑分组名由 nav 模型反查当前路径（`exact` 与前缀规则与高亮一致）；未在导航中的路由（支付回调、自定义页、二级详情）回退为「区域 / 页面」两级。
- `document.title` 逻辑不变。

## 11. i18n

- `locales/{zh,en}/common.ts` 新增：`nav.area.console` / `nav.area.admin`、`nav.group.workspace / generate / channels / billing / account / overview / operations / channelsModels / monitoring / finance / system / myAccount / more`、`console.search.placeholder`、`console.rail.collapse / expand`、`console.crumb.*`（如需）。`check:i18n`（`localeKeyCompleteness.spec`）保证中英同步。
- 页面级新增文案（KPI 标签、空态、说明）随各页任务补充。

## 12. 响应式

- ≥1181：侧栏 240 + 内容自适应；网格 `auto-fill(minmax(300px,1fr))`。
- 1024–1180：KPI 两列、两栏版块单列、`SplitView` 左列 260px。
- <1024：侧栏抽屉、内容 16px 内边距、`SplitView` 上下堆叠、表单单列。
- <640：KPI 单列、金额档位两列。

## 13. 新手引导与兼容

- `Guide/steps.ts` 的侧栏锚点通过 `tourId / tourAttr` 保留；`useOnboardingTour` 在 `AppLayout` 的挂载点不变。
- `AppHeader.spec` 三项断言（无 GitHub 链接、文档链接经 `sanitizeUrl`、模型广场按运行时开关显示）迁到 `ContextBar.spec`。
- `AppSidebar.spec` 十项断言迁到 `SideRail.spec` / `consoleNav.spec`：自定义 SVG 不覆色、滚动位置持久化、可折叠分组、品牌区无版本徽章、文档不作为菜单项、VIP 对账不激活父项、充值入口只受 payment 开关控制、模型价格独立入口、订阅开关门控、充值入口文案随计费模式。

## 风险与对策

| 风险 | 对策 |
|---|---|
| 全局类重写影响 100+ 文件中的弹窗、表单 | 类名与盒模型语义不变，只改颜色 / 圆角 / 高度；M1 完成后按 `BaseDialog`、`ConfirmDialog`、账号 / 分组 / 密钥三大弹窗做浅深主题截图核对 |
| 去掉整页固定高度后长表格性能 | `table-wrap` 保留 `max-height` 滚动容器，`DataTable` 虚拟化滚动元素不变；用 `admin/UsageView` 1000 行数据验证 |
| 导航模型抽离遗漏功能开关 / 简单模式分支 | `consoleNav.spec` 逐开关断言可见性；对照 `AppSidebar.vue` 现有条目做一对一核对表 |
| 图表主题切换时 Chart.js 实例不刷新 | `useChartTheme` 暴露 `version`，各图表 `watch` 后 `chart.update()`；深色下截图验收 |
| 密钥页从表格改分栏引起习惯变化 | 保留「列表视图」切换与列设置；分栏为默认 |
| 自定义菜单项 SVG 颜色 | 沿用 `sanitizeSvg` 与 `.sidebar-svg-icon` 不覆色规则 |

## 验收标准

- 用户 / 管理员两种视角、简单模式、后端模式、各功能开关组合下，侧栏条目集合与旧版一致（由 `consoleNav.spec` 覆盖）。
- 1440 / 1920 / 400 三档宽度、浅色 / 深色，抽样页面（仪表盘、密钥、使用记录、账号管理、系统设置、充值）与设计稿一致；1920 下内容填满、无右侧空白。
- `pnpm typecheck`、`pnpm lint:check`、`pnpm test:run`、`pnpm build`（含 `check:i18n`）全部通过。
- 新手引导三条流程（分组、账号、密钥）可完整走通。

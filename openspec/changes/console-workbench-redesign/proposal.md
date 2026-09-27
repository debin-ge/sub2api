## Why

首页、登录页、模型广场、文档页已完成 ZenTok 品牌改版（靛蓝 / 青蓝、深色品牌面板 + 浅色内容），控制台仍是 sub2api 上游的原样壳：白色长侧栏 + 毛玻璃顶栏 + 彩色图标小卡片网格。用户进入控制台后视觉断层明显，且一眼能认出原项目。

前两轮设计（顶栏页签式、图标栏主从式、页内子导航式）都因导航结构不匹配被否：管理端约 27 个平铺页面、用户端约 13 个，绝大多数页面没有二级页。最终选定「分域侧栏工作台」（设计稿 `design-mockups/console-d.html`）：侧栏承载完整平铺菜单并按域分组，顶部只留上下文条，内容区改为编辑式版式。本提案把该设计稿落到 Vue 代码。

## 约束（最高优先级）

- **原有功能与业务逻辑零改动**：每个页面现有的指标、按钮、弹窗、筛选、列设置、快捷入口、页面集合与导航条目一个不少、一个不并；只改主题、样式与版式。
- **后端、API、路由、功能开关一律不动**：路由路径与 meta、公开设置字段、`featureFlags` 注册、简单模式 / 后端模式分支全部沿用。
- **设计稿与现有功能冲突时以现有功能为准**。据此从设计稿中剔除的元素：侧栏 ⌘K 搜索入口、侧栏计数徽标（「12」「2 异常」）、管理端邀请返利三页合并、仪表盘快捷操作移除与指标裁剪。
- **不新增需要数据请求的 UI**。

## What Changes

- **壳层重构**：`AppLayout` / `AppSidebar` / `AppHeader` 三件套替换为 `ConsoleShell` + `SideRail` + `ContextBar` + `PageHead`。侧栏改为 240px 深色品牌栏，菜单按域分组（仅视觉分组，条目仍是一级页面），管理员在侧栏顶部切换「控制台 / 管理后台」；顶栏改为 48px 上下文条（面包屑 + 余额 + 全局入口）；页面标题与主操作下沉到内容区页头；用户菜单与主题切换移到侧栏底部。
- **全局样式重定义**：`style.css` 中 `btn / input / badge / table / card / pager / tabs` 等既有组件类按新令牌重写（保留类名，避免改动 100+ 个使用文件），新增 `kpis / sec / panel / toolbar / meter / split / form` 等新版式类；移除 `bg-mesh-gradient`、毛玻璃顶栏与旧侧栏类。
- **表格页布局统一**：`TablePageLayout` 保持 `actions / filters / table / pagination` 插槽 API，内部改为「面板 + 工具栏 + 表 + 分页脚」，去掉整页固定高度；36 个表格页无需改动即换新版式。
- **共享组件**：新增 `components/console/` 一组版式组件（`PageHead`、`KpiRow`、`SectionBlock`、`Panel`、`Toolbar`、`TabStrip`、`StatPills`、`MeterCell`、`FormSection`、`SplitView`、`HealthCard`、`Tile`、`CodeBlock`、`Steps`、`PlanCard`、`AmountPicker`、`Sparkline`）；重样式 `DataTable`、`Pagination`、`StatusBadge`、`GroupBadge`、`Select`、`Input`、`Toggle`、`EmptyState`、`BaseDialog`、`Toast`。
- **图表主题统一**：新增 `useChartTheme()`，Chart.js 全局默认改为细线 2px、面积填充 10%、发丝网格、等宽刻度字体，类别色使用已通过色盲校验的六色（浅色 / 深色各一套）；替换 `UserDashboardCharts` 等处硬编码的调色板。
- **页面改版**：用户端 13 页、管理端 25 页按设计稿逐页调整版式（详见 design.md 映射表）；页面内所有既有指标、控件、弹窗与数据流不变，只换容器、排布与样式。
- **导航模型抽离**：侧栏条目声明从 `AppSidebar.vue` 抽到 `navigation/consoleNav.ts`，条目集合、父子结构（可展开子组）、featureFlag、hideInSimpleMode、exact、自定义菜单项与新手引导选择器语义原样保留，只在其上加域分组与区域（console / admin）两层视觉分区；面包屑由同一模型派生。

## Capabilities

### New Capabilities
- `console-workbench`：新的控制台壳（分域侧栏、区域切换、上下文条、页头）、导航模型与共享版式组件的行为规格。

### Modified Capabilities
<!-- openspec/specs 目前为空，没有既有能力需要修改。 -->

## Impact

- **前端**：`components/layout/*`、`style.css`、`tailwind.config.js`、`components/common/*` 重样式、`components/charts/*` 与 `components/user/dashboard/*` 主题、40 个包裹 `<AppLayout>` 的视图、`i18n/locales/{zh,en}/common.ts` 新增导航分组与区域文案、`stores/app.ts` 新增分组折叠持久化、`components/Guide/steps.ts` 选择器核对。
- **后端 / API / 路由**：无变更。路由路径、`meta.titleKey/descriptionKey`、公开设置字段、功能开关均沿用。
- **测试**：`components/layout/__tests__` 三个 spec 重写为新组件；`AppSidebar.spec` 的 10 项断言迁移到 `SideRail.spec` 与 `consoleNav.spec`；受版式影响的视图 spec（`DashboardView`、`KeysView`、`UsageView`、`ProfileView` 等）按新结构调整；`check:i18n` 锁定新增文案的中英完整性。
- **发布方式**：在 `feat/console-workbench` 分支完成后一次性切换，不保留旧壳的运行时开关；上线前用 1440 / 1920 / 400 三档宽度截图验收浅色与深色。

## Non-goals

- 不改动任何接口、数据模型、权限与功能开关语义。
- 不新增、删除、合并或移动任何页面与功能入口；不实现 ⌘K 搜索与侧栏计数徽标。
- 不重做登录页、首页、模型广场、文档页。
- 不引入新的 UI 框架或组件库；仍用 Tailwind + 手写组件 + Chart.js。

## ADDED Requirements

### Requirement: 控制台壳采用分域侧栏工作台
控制台（`/dashboard` 等用户路由与 `/admin/*` 管理路由）SHALL 使用同一个壳：左侧 240px 深色侧栏承载当前区域的完整菜单，顶部 48px 上下文条显示「区域 / 分组 / 页面」面包屑与全局入口，页面标题与主操作位于内容区页头。内容区 SHALL 随窗口宽度填满，不设最大宽度。

#### Scenario: 管理员进入管理路由
- **WHEN** 管理员访问 `/admin/accounts`
- **THEN** 侧栏区域切换处于「管理后台」，菜单显示管理端分组，面包屑为「管理后台 / 渠道与模型 / 账号管理」，页头标题为「账号管理」

#### Scenario: 普通用户不显示区域切换
- **WHEN** 非管理员登录
- **THEN** 侧栏不渲染「控制台 / 管理后台」切换控件，菜单只包含用户端分组

#### Scenario: 宽屏填满
- **WHEN** 视口宽度为 1920px
- **THEN** 表格、KPI 行、图表与卡片网格延伸到内容区右边距，卡片网格按 `auto-fill` 增加列数

### Requirement: 功能零改动
改版 SHALL 只改变主题、样式与版式。每个页面改版前后的指标集合、按钮与弹窗、筛选与列设置、快捷入口、分页与排序、导航条目与父子结构 SHALL 一致；后端、API、路由与功能开关 SHALL 不变。

#### Scenario: 控件清单对照
- **WHEN** 任一页面完成改版
- **THEN** 该页改版前后的控件清单逐项对应，无新增、删除或合并

### Requirement: 菜单集合与旧侧栏一致
新侧栏 SHALL 在功能开关、简单模式、后端模式、自定义菜单项的任意组合下，渲染与改版前 `AppSidebar` 相同的条目集合与父子结构；域分组仅为视觉分区。

#### Scenario: 可折叠父项保留
- **WHEN** 管理员查看「渠道与模型」分组
- **THEN** 「渠道管理」仍为可展开父项，展开后显示「渠道定价 / 渠道监控」，子路由激活且未展开时父项高亮

#### Scenario: 功能开关关闭
- **WHEN** 公开设置 `payment_enabled=false`
- **THEN** 用户端「充值 / 订阅」「我的订单」与管理端「支付概览 / 订单管理 / 订阅套餐」不渲染；分组内无剩余条目时整组隐藏

#### Scenario: 简单模式管理员
- **WHEN** 管理员处于简单模式
- **THEN** 管理端分组隐藏所有 `hideInSimpleMode` 条目，并在「系统」分组前追加「API 密钥」，「系统设置」保留

#### Scenario: 自定义菜单项
- **WHEN** 公开设置含 `visibility=user` 的自定义菜单项
- **THEN** 该项出现在用户端「更多」分组，图标使用经 `sanitizeSvg` 处理的 SVG 且不覆盖其自带颜色

#### Scenario: 新手引导锚点
- **WHEN** 新手引导执行到「分组管理 / 账号管理 / 我的密钥」步骤
- **THEN** 页面上存在 `#sidebar-group-manage`、`#sidebar-channel-manage`、`[data-tour="sidebar-my-keys"]` 对应元素并可被高亮

### Requirement: 分组折叠与收起态
侧栏分组 SHALL 可手动折叠并持久化到浏览器；侧栏 SHALL 可收起为 64px 图标栏，收起时仅显示图标并以 title 提示名称。

#### Scenario: 折叠状态持久化
- **WHEN** 用户折叠「财务」分组后刷新页面
- **THEN** 「财务」分组保持折叠，其余分组展开

#### Scenario: 折叠分组内的当前页
- **WHEN** 当前路由属于已折叠的分组
- **THEN** 分组标题呈高亮态，展开后当前条目高亮

### Requirement: 表格页布局
`TablePageLayout` SHALL 保持 `actions / filters / table / pagination` 插槽，渲染为面板：工具栏（筛选在左、操作在右）、可滚动表区（桌面端 `max-height` 限高、`overflow: auto`）、分页脚；`DataTable` 的虚拟滚动 SHALL 继续以表区为滚动元素。

#### Scenario: 长表格
- **WHEN** 表格数据超过 `virtualizeThreshold`
- **THEN** 表区内滚动且仅渲染可见行，页面主体不出现整页固定高度

### Requirement: 图表主题
控制台图表 SHALL 使用统一主题：线宽 2px、面积填充 10%、发丝网格、等宽刻度字体；类别色按固定顺序取自已验证色盲可分的六色，浅色与深色各一套；主题切换后图表 SHALL 立即更新。

#### Scenario: 深色切换
- **WHEN** 用户在侧栏切换为深色
- **THEN** 已渲染的趋势图与分布图在不刷新页面的情况下换用深色类别色与网格色

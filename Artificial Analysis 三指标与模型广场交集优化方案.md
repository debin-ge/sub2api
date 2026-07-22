# Artificial Analysis 三指标与模型广场交集优化方案

## 1. 数据来源与刷新策略

- 使用官方 Free API：`/api/v2/language/models/free?page=n`。
- 获取并展示：
  - `artificial_analysis_intelligence_index`
  - `artificial_analysis_coding_index`
  - `artificial_analysis_agentic_index`
- Free API 是 AA 当前发布快照，不是历史时间序列；官方没有承诺每日更新，也没有提供明确的数据更新时间 SLA。
- 每 6 小时自动获取一次：
  - 服务启动时立即获取。
  - 当前约 3 页，即每天约 12 次请求，低于 Free tier 每日 100 次额度。
  - 保留管理员手动刷新。
- Redis 只保存最新完整快照：
  - Key：`radar:degradation:aa`
  - TTL：7 天。
  - 全部分页成功后才覆盖旧快照。
  - 任意分页失败时保留旧数据，只更新来源失败状态。
  - 不保存历史序列。
- 页面展示“项目获取时间”，不得将其描述为 AA 发布时间。

## 2. 官方接口抓取与校验

- 动态遍历 `pagination.total_pages`，不写死 3 页。
- 校验：
  - `tier=free`
  - `page` 连续。
  - 每页 `data` 非空，避免异常空快照覆盖最后可用快照。
  - `has_more` 与总页数一致。
  - 所有页面的 `intelligence_index_version` 一致。
  - 单页及合并响应不超过大小限制。
  - 跨页不存在重复 AA slug。
  - slug、指标、价格、发布日期等字段格式有效。
- 给总页数设置安全上限，防止异常分页无限请求。
- 每页继续使用现有超时、重试和错误分类机制。
- 合并并验证全部页面后，一次性向 Radar Runner 返回完整 payload。
- 保留旧扁平缓存结构的读取兼容，确保部署切换期间旧 Redis 数据仍可使用。
- AA slug 校验允许大小写字母，以及安全的 `. _ -` 字符。

## 3. 模型广场交集和模糊匹配

### 交集来源

- 使用 `ModelCatalogService.ListPublicPassive`。
- 只读取模型广场现有的公开、启用、非独占模型。
- 不触发上游模型发现。
- 在读取 Radar 数据时计算交集，使模型广场变化不需要等待下一次 AA 抓取。

只有同时满足以下条件的 AA 模型才能展示：

- Intelligence、Coding、Agentic 三项指标均非空。
- 至少与一个模型广场模型匹配。
- 最佳匹配结果唯一，不存在歧义。

### 匹配优先级

由强到弱：

1. 原始字符串完全一致。
2. 忽略大小写。
3. 统一点号、下划线、空格和横线。
4. 去除模型广场 namespace。
5. 去除完整的末尾发布日期。
6. 双侧语义后缀模糊匹配。

语义后缀白名单初始定义为：

- `high`
- `low`
- `medium`
- `xhigh`
- `thinking`
- `reasoning`
- `pro`
- `mini`
- `max`
- `ultra`

规则：

- AA slug 和模型广场 ID 两侧都可以临时删除白名单中的末尾后缀。
- 只生成匹配别名，不修改原始数据。
- 支持逐级删除连续的末尾后缀，例如：
  - `gpt-5-pro-high` → `gpt-5-pro` → `gpt-5`
- 每侧最多连续删除两个后缀，且一次匹配的两侧合计最多删除两个后缀，避免过度模糊。
- 删除更少后缀的匹配优先。
- 原始/规范化精确匹配始终优先于任何删除后缀的匹配。
- `preview`、`latest`、`instruct`、`chat`、`code`、`codex`、`vision`、`audio` 等不在白名单中的产品、版本或能力后缀不得删除。
- 同一优先级命中多个 AA 模型时，不自动匹配并排除该模型广场项。

### 展示规则

- 页面始终按照 AA 返回的原始完整 `name` 和 `slug` 展示，不对 AA 展示名称删除任何后缀。
- 如果 AA 基础模型同时匹配模型广场的 `xxx-high`、`xxx-low`、`xxx-xhigh`：
  - 只生成一个 AA 展示项。
  - 不为每个模型广场变体复制一份相同指标。
  - 雷达图、选择器和 URL 都使用 AA slug。
- 匹配到的模型广场原始名称保存在 `catalog_matches` 中，内容不得被改写；可以作为次要信息展示，但不作为雷达图主模型名称。
- 若 AA 本身返回 `gpt-5-pro`，展示时必须保留完整的 `gpt-5-pro`。

### 排序

- `available_models` 按三项指标算术平均值降序。
- 平均值相同则按 AA slug 升序，保证结果稳定。
- 默认选择排序后的 Top 6。

## 4. 公共接口与前端

### 接口调整

扩展 `GET /api/v1/public/radar/degradation/latest`：

```json
{
  "models": [],
  "available_models": [],
  "default_model_slugs": [],
  "intelligence_index_version": 4.1,
  "sources_last_updated": {
    "aa": "项目成功获取时间"
  },
  "stale": false
}
```

模型结构增加：

```json
{
  "slug": "AA 原始 slug",
  "name": "AA 原始名称",
  "catalog_matches": [
    {
      "platform": "openai",
      "model_id": "xxx-high"
    },
    {
      "platform": "openai",
      "model_id": "xxx-low"
    }
  ]
}
```

- `available_models`：完整交集模型。
- `models`：默认 Top 6，保留旧客户端兼容。
- `default_model_slugs`：Top 6 的 AA slug。
- `intelligence_index_version`：官方响应中的版本。
- Free API 没有模型更新时间时，现有 `last_updated_at` 保持 `null`，不伪造时间。
- 模型广场读取失败时不得回退展示未交集的 AA 全量数据；返回空模型集合、标记 stale，并且不缓存此次降级结果。
- AA 刷新失败但 Redis 有旧快照时继续展示旧交集，并通过来源状态和 stale 提示。

### 前端选择器

- 增加可搜索多选器。
- 默认选择后端 Top 6。
- 最多同时选择 10 个 AA 模型。
- 搜索范围包括：
  - AA name
  - AA slug
  - AA vendor
  - 模型广场 platform
  - 完整的模型广场 model ID
- 雷达图和卡片主标题使用 AA 原始名称。
- 模型广场匹配项作为次要标签或详情显示，保留完整后缀。
- URL 使用 `?models=slug1,slug2`：
  - 使用 AA slug。
  - 保留顺序。
  - 自动去重。
  - 过滤不存在的 slug。
  - 最多接受 10 个。
  - 没有有效选择时回退 Top 6。
  - 使用路由 replace 更新，避免产生大量浏览历史。
- 图表颜色扩展到 10 组。
- 页面显示三指标的官方字段含义、Intelligence Index 版本、项目获取时间及 stale 状态。
- 公共 HTTP 缓存由 1 小时调整为约 5 分钟；服务内缓存继续保持 1 分钟。

## 5. 移除趋势链路、测试与上线

### 删除无效趋势能力

- 删除 `/public/radar/degradation/trend`。
- 删除趋势 Handler、服务接口、DTO、前端 API、composable 状态和趋势图。
- 删除 AA Performance fetcher 和 `aa_perf:*` 调度。
- 删除以下配置：
  - `artificial_analysis_model_slugs`
  - `artificial_analysis_performance_interval_minutes`
  - `artificial_analysis_performance_stale_threshold_minutes`
- 清理管理员来源列表、监控指标、告警和国际化文案中的 AA Performance。
- 旧 `aa_perf:*` Redis 数据不主动删除，按 TTL 自然过期。
- 实施前先收敛当前工作区内未完成的 Radar 改动，保留已验证的嵌套响应解析和大写 slug 修复，不覆盖其他无关未提交文件。

### 测试重点

后端：

- 官方 Free 单页、多页、分页异常和中途失败。
- 分页失败时旧 Redis 快照不被覆盖。
- tier、版本、页码、重复 slug、响应大小校验。
- 三指标缺失模型被排除。
- 双侧后缀删除和连续后缀删除。
- 双侧合计删除超过两个后缀时拒绝匹配。
- 删除更少后缀者优先。
- 精确匹配优先于模糊匹配。
- 同级歧义匹配被排除。
- 多个 high/low/xhigh 变体只生成一个 AA 展示项。
- AA 原始 name、slug 和 `catalog_matches` 均不被改写。
- Top 6 排序稳定。
- 趋势路由返回 404，AA Performance 不再调度。

前端：

- 默认 Top 6、最大 10 个、搜索和取消选择。
- URL 初始化、去重、过滤、顺序保持和默认回退。
- 雷达图使用 AA 原始名称。
- 模型广场完整后缀只作为匹配信息展示。
- 三项指标、版本、获取时间和 stale 状态正确。
- 页面不再请求旧趋势接口。
- 移动端、深色模式和键盘操作可用。

上线：

- 运行后端全量测试、前端类型检查、单元测试和生产构建。
- 重建 Docker 服务并执行一次 AA 手动刷新。
- 验证 AA 来源 healthy、Redis 为完整合并快照。
- 验证页面只展示三指标完整的模型广场交集。
- 检查模糊匹配数量、歧义排除数量、最终交集数量及 AA 请求额度。

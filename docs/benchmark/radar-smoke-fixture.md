# Benchmark Radar Smoke Fixture

## 目的

这份 smoke fixture 用于本地快速验证 `benchmark/radar` 子系统的关键链路：

- 创建 suite
- 创建两个以上 target
- 创建多种 task type
- 创建 profile
- preview profile
- create run
- 产出 scored 与 invalid result
- 构建 score snapshot
- 发布 public snapshot
- 验证“样本不足仍参与排名”

对应自动化测试位于：

- `backend/internal/service/benchmark_fixture_test.go`

## Fixture 数据结构

测试夹具固定使用以下数据形态：

- `suite`
  - 名称：`Smoke Radar Suite`
- `targets`
  - `gpt-4.1 · Relay A`
  - `claude-sonnet-4 · Relay B`
- `tasks`
  - `reasoning`
  - `coding`
  - `writing`（用于验证“多任务类型存在，但 profile 可按 task_types 过滤”）
- `profile`
  - task scale：`medium`
  - task types：`reasoning`, `coding`
  - targets：两个 target 全部纳入
- `run results`
  - Target A：`reasoning=scored`，`coding=parse_error`
  - Target B：`reasoning=scored`，`coding=scored`

这组数据刻意制造两个结果：

- Target A 能力分更高，但 coverage 只有 `0.5`，因此会被标记为“样本不足”
- Target B coverage 为 `1.0`，能力分较低，但样本完整

这样可以同时验证：

- 无效样本不进入能力分分母
- 样本不足 target 仍保留在排名中

## 后台创建步骤

如果需要手工在后台页面复现，可按下面顺序操作：

1. 在 `Benchmark Targets` 中创建两个 target
   - 同一个模型名在不同渠道必须拆成不同 target
2. 在 `Benchmark Tasks` 中至少创建两种 task type
   - 推荐 `reasoning` 与 `coding`
3. 在 `Benchmark Profiles` 中创建一个 profile
   - 选中两个 target
   - task type 选 `reasoning` + `coding`
   - scale 选 `medium`
4. 在 `Benchmark Runs` 中基于该 profile 创建 run
5. 在结果层为其中一个 target 保留部分 invalid result
   - 例如 `parse_error`
6. 生成 score snapshot 后进入 run detail 检查排行榜与 invalid reason breakdown

## 发布 Public Snapshot 步骤

1. 确保系统设置中：
   - `benchmark_enabled = true`
   - `benchmark_public_enabled = true`
2. 在 `Benchmark Run Detail` 页面打开一个 `completed` run
3. 点击 `发布 public snapshot`
4. 后端会把当前 run 的脱敏聚合结果写入 public snapshot
5. 首页与 dashboard 会读取最新 published snapshot

## 首页观察点

当 `home_content` 为空且 `benchmark_home_enabled = true` 时，首页应观察到：

- 默认首页切换为 Radar
- 排名对象按“模型 + 渠道组合”展示
- `overall_score` 只表示能力分
- 成功率、延迟、token、成本单独展示
- 最新 public snapshot 发布时间可见

## 样本不足与无效样本的预期展示

- 样本不足 target：
  - 仍参与排名
  - 显示“样本不足”标记
- 无效样本：
  - 进入 `invalid reason breakdown`
  - 进入 coverage / success rate 统计
  - 不进入能力分分母

对于这份 fixture，预期现象是：

- `gpt-4.1 · Relay A` 排名第一，但带“样本不足”标记
- `claude-sonnet-4 · Relay B` 排名第二，coverage 完整
- public radar 与后台 run detail 的排序保持一致

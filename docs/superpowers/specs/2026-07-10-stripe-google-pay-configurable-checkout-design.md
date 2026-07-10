# Stripe Google Pay 可配置支付设计

日期：2026-07-10
状态：产品设计已确认，待书面规格复核

本规格取代 `2026-07-10-stripe-google-pay-express-checkout-design.md`，并保留已完成实现中关于单一 PaymentIntent、共享 Elements、共享提交锁及 Webhook 权威性的有效部分。

## 背景

现有实现已经在 Stripe 支付面板顶部挂载 Express Checkout Element，并只允许 Google Pay。Google Pay 当前由 Stripe 自动判断是否可用，但 Sub2API 管理端没有 Google Pay 开关；不可用时整个快捷支付区域静默隐藏；本地订单也无法区分最终使用的是普通 Stripe 卡还是 Google Pay 钱包。

新的产品目标是把 Google Pay 提升为 Stripe 服务商下的一项正式可配置能力：管理员可以显式启用，用户可以看到并选择，成功后由现有 Stripe Webhook 完成幂等入账，并在 Sub2API 订单中显示 Google Pay。

Stripe 官方 Web 指南：<https://docs.stripe.com/google-pay?platform=web>

官方要求包括：

- 在 Stripe Dashboard 的 Payment Methods 中启用 Google Pay。
- 使用带 TLS 域名验证证书的 HTTPS 页面。
- 注册所有展示 Google Pay 按钮的生产、测试、顶级和子域名。
- 浏览器和设备满足 Google Pay 要求，Google Wallet 中存在可用银行卡。
- 使用测试卡套件时，钱包中仍必须先添加一张真实银行卡，Google Pay 才会显示。

## 产品目标

- Stripe 服务商实例的“支持支付方式”增加 Google Pay 选项。
- 现有和新建 Stripe 实例均默认关闭 Google Pay，必须由管理员主动启用。
- Google Pay 必须依赖 `card`；缺少 `card` 时不能保存配置。
- 管理员启用后，Google Pay 区域同时出现在 `/purchase` 内嵌面板和 `/payment/stripe` 独立页。
- Stripe 判断可用时显示真实 Google Pay Express Checkout 按钮。
- Stripe 判断不可用或加载失败时显示禁用占位按钮和通用诊断，而不是隐藏整个区域。
- Google Pay 与普通 Stripe 支付共享本地订单、PaymentIntent、Stripe、Elements、提交锁和结果页。
- 成功交易只由验签后的 `payment_intent.succeeded` Webhook 入账。
- Google Pay 成功后，Sub2API 订单列表和支付统计显示 Google Pay。
- 不新增数据库字段或数据库迁移。

## 非目标

- 不把 Google Pay 做成购买页与 Stripe 并列的顶层服务商入口。
- 不创建独立 Google Pay Provider、第二张订单或第二个 PaymentIntent。
- 不直接集成 Google Pay Web API。
- 不把 Google Pay Merchant ID 或 Stripe Payment Method Domain ID 保存到 Sub2API 配置、数据库、接口或日志。
- 不通过自定义可点击按钮绕过 Stripe 的钱包可用性判断。
- 不根据前端成功动画、回跳参数或浏览器自报结果发放余额或订阅。
- 不让自动化测试创建真实 Stripe 付款或声称验证了真实钱包。
- 不重构无关支付服务商或无关订单流程。

## 方案比较

### 方案一：Stripe `supported_types` 增加 `google_pay` 子方式（采用）

`google_pay` 作为 Stripe 服务商实例的能力开关写入现有 `supported_types`。它在创建 PaymentIntent 时映射为 `card`，并与显式 `card` 去重。该方案复用现有管理端、服务商实例、限额和订单模型，改动集中。

### 方案二：Stripe Config 增加独立布尔字段（不采用）

该方案能保持 `supported_types` 只包含 Stripe API PaymentMethod 类型，但管理端“支持支付方式”与实际存储位置分离，校验、序列化和迁移语义更复杂。

### 方案三：新增顶层 `google_pay` 服务商类型（不采用）

该方案需要新增用户入口、负载均衡、限额、退款、恢复和 Provider Registry 分支，并会与 Stripe 入口重复，不符合已确认的方案 A。

## 配置模型

### 管理端

Stripe 可选子方式从：

```text
card, alipay, wxpay, link
```

扩展为：

```text
card, alipay, wxpay, link, google_pay
```

Google Pay 只加入“可选列表”，不加入默认选中列表。因此：

- 升级前已存在的 Stripe 实例保持关闭。
- 升级后新建的 Stripe 实例也默认关闭。
- 管理员必须主动选择 Google Pay。

前端在选择 Google Pay 但未选择 `card` 时立即显示依赖提示。后端创建和更新服务商实例时执行相同校验，防止绕过界面提交无效配置。

管理端说明文字必须明确：Sub2API 开关不能替代 Stripe Dashboard 的 Payment Methods 启用、HTTPS 证书、支付域名注册和钱包配置。

### PaymentIntent 类型映射

Stripe 子方式映射规则：

| Sub2API 子方式 | Stripe `payment_method_types` |
| --- | --- |
| `card` | `card` |
| `google_pay` | `card` |
| `alipay` | `alipay` |
| `wxpay` | `wechat_pay` |
| `link` | `link` |

映射结果必须去重。`card + google_pay` 只能生成一个 `card`，不得向 Stripe 发送不存在的 `google_pay` PaymentMethod 类型。

## 后端接口与实例一致性

`/payment/checkout-info` 增加：

```json
{
  "stripe_google_pay_enabled": true
}
```

该值与公开 Publishable Key 来自同一个已启用 Stripe 实例，用于支付页初始化和旧流程回退。

创建 Stripe 订单后，响应增加从实际选中服务商实例派生的公开信息：

```json
{
  "stripe_publishable_key": "pk_...",
  "google_pay_enabled": true
}
```

订单响应中的值是当前订单的权威值，避免多个 Stripe 实例配置不同时使用错误开关。Publishable Key 本身是公开参数；Secret Key 和 Webhook Secret 不得进入响应。

支付恢复快照增加 `stripePublishableKey` 和 `googlePayEnabled`，刷新后继续使用原订单选中的 Stripe 实例能力。独立支付页优先读取匹配订单的恢复快照；旧链接没有新字段时才回退到 Checkout 配置。

## 用户界面状态

管理员未启用 Google Pay 时：

- 不创建 Express Checkout Element。
- 不显示 Google Pay 区域、占位按钮或分隔线。
- Payment Element 行为保持不变。

管理员已启用 Google Pay 时，支付面板顶部有四种状态：

| 状态 | 用户界面 |
| --- | --- |
| 检测中 | 禁用 Google Pay 占位按钮和“正在检测可用性” |
| 可用 | Stripe 渲染的真实 Google Pay Express Checkout 按钮 |
| 不可用 | 禁用占位按钮和通用环境诊断 |
| 加载失败 | 禁用占位按钮和通用环境诊断；记录非敏感 Stripe 错误类型和代码 |

通用诊断提示：

> 当前环境无法使用 Google Pay，请检查 HTTPS、Stripe 支付域名和 Google Wallet，或改用其他 Stripe 支付方式。

禁用占位按钮只能传达状态，不得发起付款、Google 登录或自定义钱包流程。可用状态下只能由 Stripe Express Checkout Element 提供可点击按钮。

Express Checkout 配置保持：

```ts
paymentMethods: {
  googlePay: 'auto',
  applePay: 'never',
  link: 'never',
  amazonPay: 'never',
  paypal: 'never',
  klarna: 'never',
}
```

Payment Element 始终保留在快捷区域下方。内嵌面板和独立页使用相同状态语义、提示和可访问性标签。

## 支付数据流

1. 管理员为 Stripe 实例启用 `card + google_pay`。
2. 用户在购买页选择 Stripe 并创建本地订单。
3. 后端选择 Stripe 实例，把 `card + google_pay` 映射并去重为 `card`，创建一个 PaymentIntent。
4. 后端返回 `clientSecret`、实际实例的 Publishable Key 和 `google_pay_enabled`。
5. 前端创建一个 Stripe 实例和一个 Elements 实例。
6. Google Pay 已启用时，在同一 Elements 实例上挂载 Express Checkout Element 与 Payment Element。
7. Stripe 可用性事件控制真实按钮或禁用占位状态。
8. 用户点击真实 Google Pay 按钮后取得共享提交锁并调用 `stripe.confirmPayment`。
9. 即时失败时释放锁并保留订单；无即时错误时保留恢复快照并进入结果页。
10. 结果页显示处理中并轮询本地订单，不提前发放。
11. Stripe 发送 `payment_intent.succeeded`。
12. 后端验签、确认实际钱包类型、幂等更新订单并完成充值或订阅发放。
13. 结果页读取本地终态并显示成功。

## Webhook、Google Pay 标识与入账

### 权威来源

Webhook 必须继续验证 Stripe 签名。前端的 Express Checkout `confirm` 事件只能发起付款，不能决定本地订单支付方式或成功状态。

对于成功的 PaymentIntent，Stripe Provider 使用真实 Stripe SDK 获取 PaymentMethod，并检查：

```text
payment_method.card.wallet.type == google_pay
```

如果确认为 Google Pay，在同一入账事务中把现有订单字段：

```text
payment_type: stripe -> google_pay
```

同时完成订单状态、余额或订阅发放。普通银行卡或其他 Stripe 支付保持 `payment_type=stripe`。

### 无数据库迁移

`payment_orders.payment_type` 已是可容纳 `google_pay` 的字符串字段。Provider 身份继续由现有字段固定：

- `provider_key = stripe`
- `provider_instance_id = <原 Stripe 实例>`
- 原 Provider Snapshot

内部基础类型映射增加 `google_pay -> stripe`，确保退款、查询、历史兼容和 Provider 路由仍使用 Stripe。`google_pay` 只作为支付完成后的实际渠道标识，不加入购买页顶层 Provider 列表。

管理端订单列表和支付方式统计直接使用 `payment_type=google_pay` 显示“Google Pay”。

### Stripe 查询失败

成功 Webhook 中获取 PaymentMethod 暂时失败时：

- 不把浏览器声明作为替代证据。
- 不提前入账。
- 返回可重试错误，让 Stripe 重投 Webhook。
- 结果页继续显示处理中。

重复 Webhook 继续由现有幂等机制保护，不得重复充值或重复发放订阅。

## 共享状态与生命周期

- Google Pay 与 Payment Element 共用一个提交锁。
- 任一确认进行中时，另一个入口不能确认同一个 PaymentIntent。
- 取消钱包不取消 PaymentIntent，也不清除父流程持有的锁。
- 页面退出时移除 Express Checkout、Payment Element 和 popup message 监听器并销毁 Element。
- 无即时错误后不显示本地终态成功；必须进入结果页等待 Webhook。

## 安全边界

允许进入前端的 Stripe 参数：

- Publishable Key。
- 当前订单的 PaymentIntent Client Secret。
- `google_pay_enabled` 布尔能力标识。

不得进入前端、接口、日志或持久化配置：

- Stripe Secret Key。
- Webhook Secret。
- Google Pay Merchant ID。
- Stripe Payment Method Domain ID。
- 钱包支付载荷或银行卡信息。

错误日志仅记录非敏感的 Stripe 错误类型、代码、订单 ID 和 Provider 实例标识。

## 错误处理

| 场景 | 行为 |
| --- | --- |
| 配置 Google Pay 但未配置 `card` | 前后端拒绝保存 |
| Google Pay 未由管理员启用 | 不创建或展示快捷区域 |
| Stripe 正在检测 | 显示禁用检测中占位 |
| 浏览器、钱包、HTTPS 或域名不满足 | 显示禁用占位与通用诊断 |
| Express Checkout 加载失败 | 显示禁用占位；记录非敏感错误 |
| 用户关闭钱包 | 保持订单待支付，允许重试或切换方式 |
| 即时确认错误 | 显示错误并释放本组件取得的锁 |
| Webhook 延迟 | 结果页保持处理中并继续轮询 |
| PaymentMethod 查询暂时失败 | 返回可重试错误，不提前入账 |
| Webhook 重复投递 | 幂等处理，不重复发放 |

## 测试策略

### 管理端与配置

- Stripe 可选子方式包含 Google Pay。
- 新建和现有实例默认不选中 Google Pay。
- Google Pay 缺少 `card` 时前端阻止保存。
- 绕过前端提交相同无效配置时后端拒绝。
- 有效 `card + google_pay` 可以保存和读取。

### PaymentIntent 与接口

- `google_pay` 映射为 `card`。
- `card + google_pay` 去重为单个 `card`。
- Checkout Info、创建订单响应和恢复快照正确传播开关及公开密钥。
- 多实例场景以实际选中订单实例的响应为权威。

### 前端行为

- 未启用时不创建 Express Checkout。
- 已启用时覆盖检测中、可用、不可用和加载失败状态。
- 可用状态使用真实 `StripeGooglePayExpress` Vue 子组件，只替换 Stripe SDK 网络边界。
- 不可用占位不可点击。
- 只有 Google Pay 为 `auto`，其余 Express 钱包均为 `never`。
- 两个支付界面共享 Stripe、Elements、PaymentIntent 和提交锁。
- 两个提交方向均有并发阻止测试。
- 确认后保留快照并进入结果页轮询。
- 组件卸载正确销毁监听器和 Element。

### Webhook、入账与退款

- 已验签成功事件通过 Stripe SDK 识别 Google Pay。
- Google Pay 在同一事务内更新 `payment_type=google_pay` 并完成入账。
- 普通 Stripe 卡保持 `payment_type=stripe`。
- PaymentMethod 查询失败不入账并产生可重试结果。
- 重复 Webhook 只入账一次。
- 管理端订单和统计显示 Google Pay。
- Google Pay 订单退款仍路由到原 Stripe Provider 实例。

自动化测试不得声称验证真实 Stripe 网络、真实 Google Wallet 或真实支付成功。

## 人工 HTTPS 验收

上线前必须在符合官方要求的环境完成：

1. Stripe Dashboard 已启用 Google Pay。
2. 当前生产或预发布域名已注册到匹配密钥的 Stripe 账户。
3. 页面使用受信任的 TLS HTTPS 证书。
4. Chrome 已登录 Google，Wallet 中存在可用银行卡。
5. 管理员未启用时不显示 Google Pay。
6. 管理员启用但环境不可用时显示禁用诊断。
7. 支持环境显示真实 Google Pay 按钮。
8. 钱包取消或失败后仍可使用 Payment Element。
9. 成功交易在 Stripe 中显示为 Google Pay 钱包。
10. Sub2API 订单显示 Google Pay，余额或订阅只发放一次。
11. Google Pay 订单可以通过原 Stripe 退款流程退款。

Live Stripe.js 不得通过 `http://localhost` 作为最终验收环境；真实收款必须使用已注册的 HTTPS 域名。

## 上线与回滚

- 数据库无需迁移，现有 Stripe 实例默认关闭，因此升级后不会自动改变用户界面。
- 管理员可以逐实例启用 Google Pay。
- 先在注册的预发布 HTTPS 域名完成验收，再启用生产实例。
- 监控 Stripe Webhook 重试、PaymentMethod 查询失败、Google Pay 订单量和入账延迟。
- 回滚时取消 Stripe 实例中的 `google_pay` 子方式即可停止新入口；已有订单、Payment Element、Webhook 和退款流程继续工作。

## 验收标准

- 管理端 Stripe 子方式可以选择 Google Pay，默认关闭且强制依赖 `card`。
- 启用后两个 Stripe 支付界面都显示 Google Pay 区域。
- 支持环境显示真实 Stripe Google Pay 按钮。
- 不支持环境显示禁用占位和明确的通用诊断。
- Google Pay 与其他 Stripe 方式共用一个订单和 PaymentIntent。
- 确认后结果页等待 Webhook，不提前入账。
- 已验签 Webhook 从 Stripe 识别真实钱包类型。
- Google Pay 成功订单使用现有字段显示 `payment_type=google_pay`，无数据库迁移。
- Provider 身份、退款和查询继续固定到原 Stripe 实例。
- 重复 Webhook 不重复入账。
- Merchant ID 和 Domain ID 不进入 Sub2API 运行时。
- 真实 HTTPS 环境完成官方要求的人工验收。

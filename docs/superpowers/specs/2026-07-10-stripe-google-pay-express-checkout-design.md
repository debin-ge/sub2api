# Stripe Google Pay Express Checkout 设计

日期：2026-07-10
状态：已被 `2026-07-10-stripe-google-pay-configurable-checkout-design.md` 取代

> 后续确认的产品需求增加了 Stripe 服务商内的 Google Pay 管理开关、不可用占位入口，以及支付完成后在本地订单中标识 Google Pay。新的权威规格见同目录的 `2026-07-10-stripe-google-pay-configurable-checkout-design.md`。

## 背景

Sub2API 已经通过 Stripe PaymentIntent、Stripe Payment Element 和 Webhook 支持银行卡、支付宝、微信支付及 Link。现在需要在 Stripe 支付面板顶部增加 Google Pay 快捷支付入口，并继续使用现有订单、入账、退款和恢复机制。

商户已经取得 Google Pay Merchant ID，并在 Stripe 中取得已注册域名对应的 Domain ID。采用 Stripe Express Checkout Element 后，这两个标识不作为浏览器端或 Sub2API 后端请求参数传递：Google Pay Merchant ID 由 Google 侧保留，Stripe Domain ID 用于确认支付域名已注册在当前 Stripe 账户。

## 目标

- 在 Stripe 支付面板顶部展示 Google Pay 快捷支付按钮。
- 快捷支付区域暂时只允许 Google Pay，不展示 Apple Pay、Link、Amazon Pay、PayPal 或 Klarna。
- 只有 Stripe 判断当前浏览器、设备、账户、币种和域名满足条件时才展示 Google Pay。
- Google Pay 不可用时静默隐藏快捷支付区域，并保留下方现有 Payment Element。
- 复用现有 PaymentIntent、Elements 实例、Webhook、订单恢复和退款流程。
- 在内嵌 Stripe 支付面板和独立 Stripe 支付页中保持一致行为。

## 非目标

- 不直接集成 Google Pay Web API。
- 不新增 `google_pay` 后端支付类型。
- 不创建第二个 PaymentIntent 或第二张本地订单。
- 不把 Google Pay Merchant ID 或 Stripe Domain ID 存入 Sub2API 配置、接口响应、前端代码或日志。
- 不改变 Stripe Webhook 的订单入账权威性。
- 不在本次工作中重构其他支付服务商或无关支付页面。
- 不要求自动化测试打开 Google Pay 钱包、选择卡片或完成付款；这类操作依赖已登录 Google 且配置钱包的真实 Chrome 用户环境。
- 不新增 Playwright 或其他外部浏览器测试框架，不由测试脚本调用 Stripe Test Mode 或 Live Mode API。

## 方案比较

### 方案一：复用现有 PaymentIntent 和 Elements 实例（采用）

订单创建后，前端继续使用现有 `clientSecret` 初始化单个 Elements 实例，并在同一实例上挂载 Express Checkout Element 与 Payment Element。该方案保持现有订单生命周期不变，改动范围最小。

### 方案二：为快捷支付创建独立 Elements 实例（不采用）

两个 Elements 实例会分别管理支付状态和提交过程，容易出现重复入口、错误状态不同步和并发确认，不符合单一支付状态源的要求。

### 方案三：点击 Google Pay 后再创建 PaymentIntent（不采用）

该方案符合 Stripe 的延迟创建示例，但需要重构当前“先创建订单和 PaymentIntent，再进入支付面板”的流程。现有架构没有为 Google Pay 单独承担这项复杂度的必要。

### 自动化验证边界（采用行为测试与人工外部验收）

Task 2 和 Task 3 的生产代码始终动态加载真实 `@stripe/stripe-js`，并把真实 Stripe/Elements 实例传给 `StripeGooglePayExpress`。自动化验证使用 Vitest 挂载真实 Vue 子组件，只在 Stripe SDK 边界使用测试替身，以确定性验证共享提交锁、成功状态、错误恢复和支付宝/微信支付分支隔离。

不新增 Playwright 外部测试，也不让自动化脚本创建 Stripe Test Mode 或 Live Mode PaymentIntent。测试替身只能证明应用内部的实例传递和事件处理，不能作为真实 Stripe 网络或钱包可用性的证据。真实 Stripe.js 加载、域名注册、Google 钱包可用性和成功交易来源在已注册 HTTPS 环境中人工验收。

## 总体架构

Stripe 支付面板按以下顺序组织：

1. 订单金额和订单摘要。
2. Google Pay Express Checkout 区域，仅在可用时显示。
3. 可选分隔提示。
4. 现有 Payment Element，提供银行卡、支付宝、微信支付和 Link 等回退方式。
5. 取消订单及错误提示。

Express Checkout Element 与 Payment Element 共享：

- 同一个 Stripe.js 实例。
- 同一个 Elements 实例。
- 同一个 PaymentIntent `clientSecret`。
- 同一个提交互斥状态。
- 同一个 `return_url` 生成规则。
- 同一个 Webhook 入账流程。

## 组件设计

### `StripeGooglePayExpress`

新增一个聚焦 Google Pay 快捷支付的可复用前端组件。该组件负责：

- 接收已初始化的 Stripe 和 Elements 实例。
- 挂载及卸载 Express Checkout Element。
- 限制 Express Checkout 只显示 Google Pay。
- 监听 `availablepaymentmethodschange` 并向父组件报告可用状态。
- 监听 `confirm` 并触发现有 PaymentIntent 确认。
- 报告提交中、即时错误和确认完成状态。

父组件继续负责：

- 获取订单和 `clientSecret`。
- 初始化 Stripe.js 与 Elements。
- 渲染金额、Payment Element、成功状态和取消入口。
- 查询或轮询本地订单状态。
- 在页面退出时销毁定时器及挂载对象。

该边界使 Google Pay 快捷支付逻辑可以同时复用于：

- `frontend/src/components/payment/StripePaymentInline.vue`
- `frontend/src/views/user/StripePaymentView.vue`

### Express Checkout 配置

Express Checkout Element 使用以下支付方式策略：

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

`googlePay: 'auto'` 表示只有 Stripe 判断当前环境适用时才展示按钮。初版不使用 `always`，避免向未登录或未配置 Google Pay 的用户主动发起登录流程。

Express Checkout Element 与 Payment Element 挂载在同一个 Elements 实例上后，由 Stripe 避免在两个组件中重复展示钱包入口；项目不再额外渲染自定义 Google Pay 按钮。Payment Element 的其余现有支付方式保持当前配置。

## 支付数据流

1. 用户选择 Stripe 并创建本地订单。
2. 后端根据订单金额和币种创建 PaymentIntent，`payment_method_types` 继续包含 `card`。
3. 后端返回订单信息与 PaymentIntent `clientSecret`。
4. 前端使用 Stripe publishable key 和 `clientSecret` 创建 Elements 实例。
5. 前端在同一个 Elements 实例上挂载 Express Checkout Element 与 Payment Element。
6. Express Checkout 触发 `availablepaymentmethodschange`：
   - `googlePay.available === true`：显示 Google Pay 区域。
   - 其他情况：隐藏 Google Pay 区域及分隔提示。
7. 用户点击 Google Pay 后，组件进入共享提交锁并调用 `stripe.confirmPayment`。
8. 如果 Stripe 返回即时错误，前端解除提交锁并显示错误；用户可以重试或改用下方支付方式。
9. 如果确认完成且无需跳转，前端显示处理中状态；需要跳转时使用现有 `return_url`。
10. Stripe 投递 `payment_intent.succeeded`，后端验签、幂等处理并完成充值或订阅发放。
11. 前端通过现有订单查询、轮询或结果页显示最终状态。

前端成功动画或回跳参数不作为发放余额或订阅的依据，Webhook 始终是支付成功的权威来源。

## 并发与幂等

- Google Pay 和普通 Payment Element 共用一个提交锁。
- 任一确认流程开始后，两个入口都暂时禁止重复提交。
- 后端继续使用当前 PaymentIntent 幂等键与订单元数据。
- Webhook 继续使用现有幂等入账机制，重复事件不得重复发放余额或订阅。
- 用户取消 Google Pay 钱包弹窗不取消 PaymentIntent，也不将本地订单标记为失败。

## 错误处理

| 场景 | 行为 |
| --- | --- |
| Google Pay 不可用 | 静默隐藏快捷支付区，显示现有 Payment Element |
| 域名未注册或 HTTPS 不合格 | 快捷支付不可用；保留其他方式，并记录非敏感诊断信息 |
| 用户关闭 Google Pay | 保持订单待支付；未取得提交锁时不得解除其他支付流程持有的锁；允许重试或改用其他方式 |
| Stripe 即时确认错误 | 在快捷支付区域下方显示本地化错误，解除提交锁 |
| 需要跳转或验证 | 使用现有 `return_url` 与结果页恢复机制 |
| 前端显示成功但 Webhook 延迟 | 显示处理中并继续查询本地订单，不提前发放 |
| 页面刷新或重新进入 | 使用现有订单和恢复令牌机制恢复支付状态 |
| Webhook 重复投递 | 后端幂等处理，不重复入账 |

错误消息不得包含 `clientSecret`、Secret Key、Webhook Secret、Google Pay Merchant ID 或 Stripe Domain ID。

## 参数与安全边界

现有 Stripe 参数继续使用：

- 后端：Stripe Secret Key、Webhook Secret、收款币种。
- 前端：Stripe Publishable Key、当前订单的 PaymentIntent `clientSecret`。
- 外部配置：已启用 Google Pay 的 Stripe 账户、已注册的 HTTPS 支付域名。

以下参数不进入 Sub2API 运行时：

- Google Pay Merchant ID。
- Stripe Payment Method Domain ID。

支付域名必须与使用当前 Publishable Key 的 Stripe 账户匹配。测试和生产密钥、Webhook Secret 及域名注册状态必须保持环境一致。自动化测试不读取 Stripe Secret Key，不创建外部 PaymentIntent，也不接触 Live Mode 数据。

## 测试策略

### 前端单元测试

- Express Checkout 配置中只有 Google Pay 为 `auto`，其他快捷支付方式均为 `never`。
- `googlePay.available=true` 时显示快捷支付区。
- Google Pay 不可用或事件返回 `undefined` 时隐藏区域和分隔提示。
- `confirm` 成功时调用一次 `stripe.confirmPayment`。
- 即时错误、用户取消和异常时解除提交锁并显示预期状态。
- Google Pay 与普通 Payment Element 不能并发提交。
- 组件卸载时正确销毁监听和 Element。
- Task 2 和 Task 3 的行为测试必须挂载真实 `StripeGooglePayExpress` Vue 子组件，不得用 Vue stub 替代该组件。
- 行为测试中的 Stripe SDK 测试替身只用于触发无法在无人值守浏览器中稳定操作的钱包事件；它们不作为真实 Stripe 集成成功的证据。

### 现有流程回归

- 银行卡、Link、支付宝和微信支付仍按现有流程工作。
- 独立 Stripe 支付页与内嵌支付面板行为一致。
- 页面刷新、结果页回跳、订单取消和恢复令牌流程不回归。
- Webhook 成功、失败和重复投递测试继续通过。
- Stripe 退款流程不因 Google Pay 钱包来源发生变化。

### 自动化覆盖范围

- `StripeGooglePayExpress` 测试验证仅 Google Pay 为 `auto`、可用性事件、确认、错误、取消、外部锁和销毁。
- `StripePaymentInline` 与 `StripePaymentView` 测试必须挂载真实 Vue 子组件，验证父子共享同一 Stripe/Elements 引用及同一提交锁。
- 两个提交方向都必须有并发阻止测试：Google Pay 持锁时普通表单不可提交，普通表单持锁时 Google Pay 不得二次确认。
- 独立支付页的支付宝和微信支付直连分支不得挂载 Express Checkout Element。
- 现有后端 Stripe provider、Webhook、入账和退款回归继续运行。
- 自动化结果不得声称已验证真实 Stripe 网络、Google 钱包可用性或真实支付成功。

### 人工端到端验收

- 在已注册的公网 HTTPS 测试域名上使用与之匹配的 Stripe 测试账户。
- 在已登录 Google 且配置测试钱包的 Chrome/Android 环境验证 Google Pay 可用、取消和确认场景。
- 成功交易在 Stripe 中应能识别为 `card.wallet.type = google_pay`。
- 验证支付完成后仅由 Webhook 触发一次入账。

## 上线与回滚

1. 先在测试环境启用并完成端到端验证。
2. 确认生产域名在与生产 Publishable Key 对应的 Stripe 账户中处于启用状态。
3. 部署前确认 Google Pay 已在 Stripe Payment Methods 设置中启用。
4. 上线后监控 PaymentIntent 失败率、Webhook 延迟和支付方式分布。
5. 如出现异常，可隐藏或移除 `StripeGooglePayExpress` 挂载点；现有 Payment Element 和后端支付流程不受影响。

## 验收标准

- 支持环境中，Stripe 支付面板顶部只显示 Google Pay 快捷按钮。
- 不支持环境中，不显示空白快捷支付容器或错误提示，下方支付方式可正常使用。
- Google Pay 与普通支付方式共享同一个订单和 PaymentIntent。
- Google Pay 成功后只入账一次，并能通过现有结果页看到最终状态。
- Google Pay 取消或失败后可以重试或切换支付方式。
- Google Pay Merchant ID 和 Stripe Domain ID 不出现在前端、接口响应、日志或持久化配置中。
- 现有 Stripe 支付、Webhook、恢复和退款测试均通过。
- Task 2 和 Task 3 的生产代码调用真实 `@stripe/stripe-js`；行为测试挂载真实 `StripeGooglePayExpress` Vue 子组件并只替换 SDK 边界。
- 自动化测试不读取 Stripe Secret Key、不创建 Test/Live Mode PaymentIntent，也不声明完成真实外部 Stripe 验证。
- 上线前在已注册 HTTPS 环境完成人工钱包验收，并确认成功交易为 `card.wallet.type = google_pay`。

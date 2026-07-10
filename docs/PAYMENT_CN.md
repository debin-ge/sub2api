# 支付系统配置指南

Sub2API 内置支付系统，支持用户自助充值，无需部署独立的支付服务。

---

## 目录

- [支持的支付方式](#支持的支付方式)
- [快速开始](#快速开始)
- [系统设置](#系统设置)
- [服务商配置](#服务商配置)
- [服务商实例管理](#服务商实例管理)
- [Webhook 配置](#webhook-配置)
- [支付流程](#支付流程)
- [从 Sub2ApiPay 迁移](#从-sub2apipay-迁移)

---

## 支持的支付方式

| 服务商 | 支付方式 | 说明 |
|--------|---------|------|
| **EasyPay（易支付）** | 支付宝、微信支付 | 兼容易支付协议的第三方聚合支付 |
| **支付宝官方** | 桌面二维码扫码、移动端支付宝跳转 | 直接对接支付宝开放平台，桌面端返回二维码，移动端返回 WAP/唤起链接 |
| **微信官方** | Native 扫码、H5、公众号/JSAPI 支付 | 直接对接微信支付 APIv3，按终端环境自动分流 |
| **Stripe** | 银行卡、支付宝、微信支付、Link 等 | 国际支付，支持多币种 |
| **Wise** | Wise Quick Pay / bank transfer | 国际收款，v1 仅自动处理到账金额等于订单金额的 Wise balance / bank transfer |

> 支付宝官方 / 微信官方与易支付可以同时作为后台服务商实例存在，但前台始终只展示 `支付宝`、`微信支付` 两个可见按钮。管理员需要分别为这两个按钮选择唯一支付来源：官方或易支付。官方渠道直接对接 API，资金直达商户账户，手续费更低；易支付通过第三方平台聚合，接入门槛更低。

> 支付渠道的安全性、稳定性及合规性请自行鉴别，本项目不对任何第三方支付服务商做担保或背书。

---

## 快速开始

1. 进入管理后台 → **设置** → **支付设置** 标签页
2. 开启 **启用支付**
3. 配置基本参数（金额范围、超时时间等）
4. 在 **服务商管理** 中添加至少一个服务商实例
5. 用户即可在前端页面进行充值

---

## 系统设置

在管理后台 **设置 → 支付设置** 中配置以下参数：

### 基本设置

| 设置项 | 说明 | 默认值 |
|--------|------|--------|
| **启用支付** | 启用或禁用支付系统 | 关闭 |
| **商品名前缀** | 支付页面显示的商品名前缀 | - |
| **商品名后缀** | 商品名后缀（如"元"） | - |
| **最低金额** | 单笔最低充值金额 | 1 |
| **最高金额** | 单笔最高充值金额（留空表示不限制） | - |
| **每日限额** | 每用户每日累计充值上限（留空表示不限制） | - |
| **订单超时时间** | 订单超时分钟数，至少 1 分钟 | 30 |
| **最大待支付订单数** | 同一用户最大并行待支付订单数 | 3 |
| **负载均衡策略** | 多服务商实例时的选择策略 | 轮询 |

### 前台可见支付方式路由

当前版本对用户统一展示支付方式，不区分官方渠道还是易支付：

- **支付宝**：后台启用后，需要额外指定该按钮路由到 `支付宝官方` 或 `易支付支付宝`
- **微信支付**：后台启用后，需要额外指定该按钮路由到 `微信官方` 或 `易支付微信`
- 同一个可见支付方式在同一时刻只能路由到一个来源
- 支付来源未选择时，即使对应按钮被开启，前台也不会暴露该支付方式

### 负载均衡策略

| 策略 | 说明 |
|------|------|
| **轮询（round-robin）** | 按顺序轮流分配到各服务商实例 |
| **最少金额（least-amount）** | 优先分配到当日累计金额最少的实例 |

### 取消频率限制

防止用户频繁创建并取消订单：

| 设置项 | 说明 |
|--------|------|
| **启用限制** | 开关 |
| **窗口模式** | 滚动窗口 / 固定窗口 |
| **时间窗口** | 窗口长度 |
| **窗口单位** | 分钟 / 小时 |
| **最大次数** | 窗口内允许的最大取消次数 |

### 帮助信息

| 设置项 | 说明 |
|--------|------|
| **帮助图片** | 充值页面显示的客服二维码等图片（支持上传） |
| **帮助文本** | 充值页面显示的说明文字 |

---

## 服务商配置

每种服务商需要不同的凭证和参数。在 **服务商管理 → 添加服务商** 中选择类型后填写。

> **回调地址自动生成**：添加服务商时，异步回调地址（Notify URL）和同步跳转地址（Return URL）由系统根据你的站点域名自动拼接，无需手动填写。管理员只需确认域名正确即可。

### EasyPay（易支付）

兼容任何 EasyPay 协议的支付服务商。

| 参数 | 说明 | 必填 |
|------|------|------|
| **商户 ID（PID）** | EasyPay 商户 ID | 是 |
| **商户密钥（PKey）** | EasyPay 商户密钥 | 是 |
| **API 地址** | EasyPay API 基础地址 | 是 |
| **支付宝通道 ID** | 指定支付宝通道（可选） | 否 |
| **微信通道 ID** | 指定微信通道（可选） | 否 |

### 支付宝官方

直接对接支付宝开放平台。移动端走支付宝手机网站支付跳转；桌面端优先使用当面付返回扫码串，若商户未开通当面付则回退到电脑网站支付，并将收银台链接同时返回给前端用于渲染二维码或直接打开支付页。

| 参数 | 说明 | 必填 |
|------|------|------|
| **AppID** | 支付宝应用 AppID | 是 |
| **应用私钥** | RSA2 应用私钥 | 是 |
| **支付宝公钥** | 支付宝公钥 | 是 |

### 微信官方

直接对接微信支付 APIv3，支持 Native 扫码支付、H5 支付，以及在微信环境内的公众号/JSAPI 支付。

| 参数 | 说明 | 必填 |
|------|------|------|
| **AppID** | 微信支付 AppID | 是 |
| **商户号（MchID）** | 微信支付商户号 | 是 |
| **商户 API 私钥** | 商户 API 私钥（PEM 格式） | 是 |
| **APIv3 密钥** | 32 位 APIv3 密钥 | 是 |
| **微信支付公钥** | 微信支付公钥（PEM 格式） | 是 |
| **微信支付公钥 ID** | 微信支付公钥 ID | 是 |
| **商户证书序列号** | 商户证书序列号 | 是 |

### Stripe

国际支付平台，支持多种支付方式和币种。

| 参数 | 说明 | 必填 |
|------|------|------|
| **Secret Key** | Stripe 密钥（`sk_live_...` 或 `sk_test_...`） | 是 |
| **Publishable Key** | Stripe 可公开密钥（`pk_live_...` 或 `pk_test_...`） | 是 |
| **Webhook Secret** | Stripe Webhook 签名密钥（`whsec_...`） | 是 |

#### Google Pay Express Checkout

Google Pay 是每个 Stripe 服务商实例的可选子方式；现有实例和新建实例均默认关闭。管理员必须在服务商对话框中同时启用 Card 与 Google Pay，Sub2API 会拒绝没有 Card 的 Google Pay 配置。订单实际选中的实例是权威来源：后端把 `card + google_pay` 去重映射为一个 `card` PaymentIntent，并把该实例的能力和 Publishable Key 返回给结账页。

Sub2API 开关不能替代 Stripe Dashboard 配置。必须在 Stripe Payment Methods 中启用 Google Pay，把每个生产和预发布主机名（包括各个子域名）注册到匹配 Stripe 账户的 Payment Method Domains，通过受信任 TLS HTTPS 提供结账页面，并在受支持的 Chrome/Android 环境中使用 Google Wallet 内的可用银行卡测试。Google Pay Merchant ID 和 Stripe Payment Method Domain ID 不是 Sub2API 输入，禁止加入配置或日志。`http://localhost + live key` 不能作为真实 Google Pay 验收证据。

启用后，Stripe 面板具有四种状态：Stripe 检测可用性时显示禁用状态占位；受支持环境显示 Stripe 真实的 Express Checkout Google Pay 按钮；域名、浏览器或钱包不支持时保留禁用占位和诊断；Element 加载失败时也保留禁用占位和诊断。只有 Stripe 真实 Express Checkout Element 可以点击，禁用占位不能发起支付、Google 登录或自定义钱包流程；在不可用和错误状态下，Payment Element 始终保持可用。

Google Pay 与 Payment Element 复用同一个本地订单、PaymentIntent、Stripe/Elements 实例、提交锁和结果页。确认后结果页等待 Webhook，绝不提前发放余额或订阅。验签后的 `payment_intent.succeeded` Webhook 使用 stripe-go 获取 PaymentMethod；`card.wallet.type=google_pay` 会在同一次有条件的已支付状态更新中把现有订单的 `payment_type` 更新为 `google_pay`，重复投递不会重复发放。Provider 身份、查询和退款始终绑定原 Stripe 实例，无需数据库迁移。

自动化测试只验证 Stripe SDK 边界内的应用行为；不得访问 Stripe Test/Live API、创建真实 PaymentIntent，也不得把自动化结果声称为真实 Google Wallet 交易证据。生产上线前，操作员必须在已注册 HTTPS 主机名、匹配的 Stripe 账户以及真实受支持的 Chrome/Google Wallet 环境中逐项完成并记录以下 11 项：

1. Stripe Dashboard 的 Payment Methods 已启用 Google Pay。
2. 当前生产或预发布主机名已注册到与配置密钥匹配的 Stripe 账户。
3. 页面使用公开受信任的 TLS HTTPS 证书。
4. Chrome 已登录 Google，Google Wallet 中有可用银行卡。
5. 管理员未对订单所选 Stripe 实例启用 Google Pay 时，不显示 Google Pay 区域。
6. 管理员启用但环境不支持时，面板显示禁用占位和诊断。
7. 受支持环境中由 Stripe 渲染真实 Google Pay Express Checkout 按钮。
8. 钱包取消或失败后仍可使用 Payment Element。
9. 成功交易在 Stripe 中显示 `card.wallet.type=google_pay`，同时结果页等待 Webhook。
10. Sub2API 订单显示 Google Pay，余额或订阅只发放一次。
11. Google Pay 订单可通过原 Stripe 服务商实例退款。

如果当前不具备上述已注册 HTTPS 与钱包环境，应把清单记录为 `NOT EXECUTED — requires operator on registered HTTPS environment`；localhost、mock、`httptest` 和代码检查均不能使人工验收通过。上线时应先逐实例完成预发布验收，再逐个启用，并监控 Stripe Webhook 重试、PaymentMethod 获取失败、入账延迟和 Google Pay 订单量。回滚时取消该服务商实例中的 `google_pay` 子方式即可停止新入口；已有订单、Payment Element、Webhook 处理和退款继续使用原 Stripe 实例。

### Wise

Wise 接入采用 hosted redirect + profile-level webhook subscription + 自动对账模式。系统会生成 Wise Quick Pay/payment link 跳转地址，并追加 `amount`、`currency`、`description=纯字母数字订单 reference`。该 Wise description 由本地 `out_trade_no` 去除非字母数字字符后生成，避免 Wise Quick Pay hosted 页面因特殊字符拒绝访问。

Wise 自动创建 webhook subscription 还需要当前 Sub2API 实例的公网 HTTPS 基础地址。请在系统设置中配置 **API 端点地址** 或 **前端基础 URL**，也可以在部署环境中设置 `API_BASE_URL`、`FRONTEND_URL`、`SERVER_FRONTEND_URL`、`SITE_URL`、`BASE_URL` 或 `APP_URL`。系统会向 Wise 注册 `<base-url>/api/v1/payment/webhook/wise`。

v1 仅自动入账 Wise balance / bank transfer 中到账金额等于订单金额的交易。card、Apple Pay、Google Pay 等可能扣除收款手续费的交易不会自动发放余额或订阅，系统会进入人工审核。Wise 实付币种与订单 / provider 币种不一致时会保留 `currency_mismatch`，不会自动换汇。

Wise 退款为人工流程。管理员发起 Wise 退款时，系统不会把网关退款伪装成成功，而是提示需要先在 Wise 后台人工退款。操作员在 Wise 完成退款后，需要回到 Sub2API 填写 Wise 退款参考并确认，本地才会更新退款状态、按需执行余额 / 订阅扣减，并写入审计日志。

Wise 对账会把明确引用本地订单且 Wise 侧已失败或已取消的 activity 映射为本地失败 / 取消。金额不一致、手续费扣减、元数据不一致、币种不一致仍进入人工审核，不会自动入账。

| 参数 | 说明 | 必填 |
|------|------|------|
| **Quick Pay 基础链接** | Wise Quick Pay/payment link，不带订单参数 | 是 |
| **环境** | Wise 环境：`production` 或 `sandbox`，默认 `production` | 否 |
| **API Base** | Wise API 地址，生产环境通常为 `https://api.wise.com` | 是 |
| **API Token** | Wise user/API token，用于创建 profile-level subscription 和查询 statement/activity；不需要 `clientKey` 或 client credentials access token | 是 |
| **Profile ID** | Wise business profile ID | 是 |
| **Balance ID** | Wise balance ID | 是 |
| **币种** | 当前实例收款币种 | 是 |
| **Webhook Public Key** | Wise webhook RSA 公钥的可选覆盖项；通常由系统按环境使用内置公钥 | 否 |
| **Settlement Strategy** | v1 固定为 `exact_only` | 是 |

---

## 服务商实例管理

同一种服务商可以创建**多个实例**，实现负载均衡和风控：

- **多实例负载均衡** — 按轮询或最少金额策略分流订单
- **独立限额** — 每个实例可独立配置单笔最小/最大金额和每日限额
- **独立启停** — 可单独启用/禁用某个实例，不影响其他实例
- **退款控制** — 每个实例可单独开启或关闭退款功能
- **支付方式** — 每个实例可选择支持的支付方式子集
- **排序** — 拖拽调整实例顺序
- **站点名前缀订单号** — 新支付订单的 `out_trade_no` 使用系统站点名称生成纯字母数字前缀；中文站点名会转为近似拼音首字母，无法生成时回退 `Sub2API`。历史 `sub2_` 订单号继续兼容。

### 实例限额配置

每个实例支持以下限额：

| 限额项 | 说明 |
|--------|------|
| **单笔最小金额** | 该实例接受的最小订单金额 |
| **单笔最大金额** | 该实例接受的最大订单金额 |
| **每日限额** | 该实例每日累计交易上限 |

> 负载均衡时，系统会自动跳过超出限额的实例。

---

## Webhook 配置

支付回调是支付系统的核心环节，必须正确配置：

### 回调地址格式

添加服务商时，系统会自动根据站点域名拼接回调地址，格式如下：

| 服务商 | 回调路径 |
|--------|---------|
| **EasyPay** | `https://your-domain.com/api/v1/payment/webhook/easypay` |
| **支付宝官方** | `https://your-domain.com/api/v1/payment/webhook/alipay` |
| **微信官方** | `https://your-domain.com/api/v1/payment/webhook/wxpay` |
| **Stripe** | `https://your-domain.com/api/v1/payment/webhook/stripe` |
| **Wise** | `https://your-domain.com/api/v1/payment/webhook/wise` |

> 将 `your-domain.com` 替换为你的实际域名。EasyPay / 支付宝 / 微信的回调地址在添加服务商时自动填入，无需手动配置。

### Stripe Webhook 设置

1. 登录 [Stripe Dashboard](https://dashboard.stripe.com/)
2. 进入 **Developers → Webhooks**
3. 添加端点，填写回调地址
4. 订阅事件：`payment_intent.succeeded`、`payment_intent.payment_failed`
5. 将生成的 Webhook Secret（`whsec_...`）填入服务商配置

### Wise Webhook 设置

管理员无需手动在 Wise 后台创建 webhook。保存并启用 Wise provider 时，系统会使用配置的 Wise user/API token 调用 `POST /v3/profiles/{profileId}/subscriptions`，自动创建 profile-level `balances#credit` subscription。

1. 确认站点域名可公网访问，Wise Webhook URL 必须是公网 **HTTPS** 地址：`https://your-domain.com/api/v1/payment/webhook/wise`。
2. 保存并启用 Wise provider，系统自动创建或复用 profile-level `balances#credit` subscription。
3. Wise 创建订阅时会发送 test notification；系统会快速 ACK，但不会触发对账或订单入账。
4. 正式 webhook 到达后，系统只在请求内完成快速验签、幂等记录和异步对账触发，然后立即 ACK；完整对账由后台任务查询 Wise statement/activity 完成。

禁用或删除 Wise provider 时，系统会先调用 Wise API 删除远端 webhook subscription。Wise 返回 404/410 时视为已删除；其他 Wise API 错误会阻止本地禁用 / 删除，避免远端继续投递到已禁用 provider 后产生拒签和错误日志。启用状态下 Wise profile/API base 变化时，系统会先创建新 subscription，再删除旧的远端 subscription。

### 注意事项

- 回调地址必须是 **HTTPS**（Stripe 强制要求，Wise 要求公网 HTTPS，其他服务商强烈推荐）
- 确保服务器防火墙允许支付平台的回调请求
- 系统会自动进行签名验证，防止伪造回调
- 支付成功并通过服务商验签 / 对账后自动完成余额充值；Wise v1 只自动入账到账金额等于订单金额的 Wise balance / bank transfer，手续费扣减、金额不一致、币种不一致或 card / Apple Pay / Google Pay 等交易需要人工审核

---

## 支付流程

```
用户选择充值金额和支付方式
       │
       ▼
  创建订单 (PENDING)
  ├─ 校验金额范围、待支付订单数、每日限额
  ├─ 负载均衡选择服务商实例
  └─ 调用服务商获取支付信息
       │
       ▼
  用户完成支付
  ├─ EasyPay    → 扫码 / H5 跳转
  ├─ 支付宝官方  → 桌面扫码单（当面付优先，电脑网站支付回退）/ 移动端支付宝跳转
  ├─ 微信官方    → 桌面 Native 扫码 / 非微信 H5 / 微信内 JSAPI
  ├─ Stripe     → Payment Element（银行卡/支付宝/微信等）
  └─ Wise       → Hosted Quick Pay/payment link 跳转
       │
       ▼
  支付回调验签
  └─ Wise       → 快速 ACK，幂等记录并触发异步对账
       │
       ▼
  Wise 后台对账
  └─ 查询 statement/activity，并仅自动确认到账金额等于订单金额的 balance / bank transfer
       │
       ▼
  订单 PAID
       │
       ▼
  自动充值到用户余额 → 订单 COMPLETED
```

### 订单状态说明

| 状态 | 说明 |
|------|------|
| `PENDING` | 待支付，等待用户完成支付 |
| `PAID` | 已支付，等待充值到账 |
| `COMPLETED` | 已完成，余额已到账 |
| `EXPIRED` | 已过期，超时未支付 |
| `CANCELLED` | 已取消，用户主动取消 |
| `FAILED` | 充值失败，可管理员重试 |
| `REFUND_REQUESTED` | 已申请退款 |
| `REFUNDING` | 退款处理中 |
| `REFUNDED` | 已退款 |

### 超时与兜底

- 订单超时后，后台任务会先查询上游支付状态再标记过期
- 如果用户实际已支付但回调延迟，系统会通过查询补单
- 后台任务每 60 秒执行一次超时检查

---

## 从 Sub2ApiPay 迁移

如果你之前使用 [Sub2ApiPay](https://github.com/touwaeriol/sub2apipay) 作为外部支付系统，现在可以迁移到内置支付：

### 主要差异

| 对比项 | Sub2ApiPay | 内置支付 |
|--------|-----------|---------|
| 部署方式 | 独立服务（Next.js + PostgreSQL） | 内置于 Sub2API，无需额外部署 |
| 支付方式 | EasyPay、支付宝、微信、Stripe | 相同 |
| 配置方式 | 环境变量 + 独立管理后台 | Sub2API 管理后台内统一配置 |
| 充值对接 | 通过 Admin API 回调 | 内部直接处理，更可靠 |
| 订阅套餐 | 支持 | 暂不支持（计划中） |
| 订单管理 | 独立管理界面 | 集成在 Sub2API 管理后台 |

### 迁移步骤

1. 在 Sub2API 管理后台启用支付并配置服务商（使用相同的支付凭证）
2. 更新 Webhook 回调地址为 Sub2API 的回调地址
3. 确认新订单通过内置支付正常处理
4. 停用 Sub2ApiPay 服务

> **注意**：Sub2ApiPay 中的历史订单数据不会自动迁移。建议保留 Sub2ApiPay 一段时间以便查询历史记录。

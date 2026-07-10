# Stripe Google Pay Configurable Checkout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Google Pay 作为 Stripe 服务商实例下默认关闭、依赖 `card` 的可配置子方式，在两个 Stripe 支付界面展示真实 Express Checkout 按钮，并由验签 Webhook 识别实际钱包、幂等入账和标记订单。

**Architecture:** 继续复用一个本地订单、一个 Stripe PaymentIntent、一个 Stripe/Elements 实例和一个提交锁；`supported_types` 只承担能力开关，创建 PaymentIntent 时把 `google_pay` 映射并去重为 `card`。订单创建响应携带实际选中 Stripe 实例的公开密钥和开关，前端快照负责刷新恢复；成功 Webhook 使用 stripe-go 查询 PaymentMethod，并在现有条件更新中同时写入 `payment_type=google_pay`，Provider 身份仍固定为 Stripe。

**Tech Stack:** Go 1.24、Ent、stripe-go v85.0.0、Gin、Vue 3、TypeScript 5.6、Stripe.js 9.8、Vitest、Vue Test Utils、vue-i18n。

## Global Constraints

- 不新增数据库字段、Ent schema 变更或 migration；`payment_orders.payment_type` 继续使用现有字符串字段。
- Google Pay 不是购买页顶层 Provider；创建订单时仍使用 `payment_type=stripe`，成功识别后才更新为 `google_pay`。
- `provider_key`、`provider_instance_id` 和 Provider Snapshot 始终保持原 Stripe 实例。
- 现有和新建 Stripe 实例默认关闭 Google Pay；管理员必须显式选中 `card + google_pay`。
- 前端和后端都拒绝 `google_pay` 已选但 `card` 未选的配置。
- Google Pay、Payment Element 共用同一 PaymentIntent、Stripe、Elements、提交锁和结果页。
- `payment_intent.succeeded` 是唯一入账权威；浏览器事件、成功动画和回跳参数不得发放余额或订阅。
- 成功 Webhook 必须使用真实 stripe-go SDK 查询 PaymentMethod；测试只能替换 SDK 的网络边界。
- Merchant ID、Stripe Payment Method Domain ID、Secret Key、Webhook Secret、钱包载荷和卡信息不得进入新接口、日志或持久化配置。
- Express Checkout 只允许 `googlePay: 'auto'`；Apple Pay、Link、Amazon Pay、PayPal、Klarna 均为 `never`。
- 自动化测试不得调用 Stripe Test/Live API 创建 PaymentIntent，也不得声称验证了真实 Google Wallet。
- Live Google Pay 只能在已注册、受信任 TLS HTTPS 域名人工验收；`http://localhost` 不能作为最终验收环境。
- 后端聚焦测试使用 `-tags=unit`；全量前端测试以当前已知 34 个失败/14 个文件为基线，变更不得增加失败。

---

## File Structure

- `frontend/src/components/payment/providerConfig.ts`：维护 Stripe 可选子方式、默认子方式及 Google Pay/card 依赖纯函数。
- `frontend/src/components/payment/PaymentProviderDialog.vue`：管理端即时依赖提示和保存拦截。
- `backend/internal/payment/types.go`：定义 `google_pay` 渠道常量、通知元数据键和基础 Provider 映射。
- `backend/internal/service/payment_config_providers.go`：后端创建/更新 Stripe 实例的依赖校验。
- `backend/internal/payment/provider/stripe.go`：子方式映射去重、stripe-go PaymentMethod 查询和钱包识别。
- `backend/internal/service/payment_config_service.go`、`backend/internal/handler/payment_handler.go`：同一 Stripe 实例的 Checkout 回退能力。
- `backend/internal/service/payment_service.go`、`backend/internal/service/payment_order.go`：订单所选实例的权威公开能力响应。
- `frontend/src/types/payment.ts`、`frontend/src/components/payment/paymentFlow.ts`：API 和恢复快照契约。
- `frontend/src/components/payment/StripeGooglePayExpress.vue`：检测中、可用、不可用、加载失败四态及真实 Express Checkout 确认。
- `frontend/src/components/payment/StripePaymentInline.vue`、`frontend/src/views/user/StripePaymentView.vue`、`frontend/src/views/user/PaymentView.vue`：两个 Stripe 页面按实例能力挂载并共享状态。
- `backend/internal/service/payment_fulfillment.go`：在幂等状态更新中同时记录实际 Google Pay 渠道。
- `frontend/src/components/admin/payment/AdminOrderTable.vue`、`frontend/src/components/admin/payment/PaymentMethodChart.vue`、`frontend/src/views/admin/orders/AdminPaymentDashboardView.vue`：订单筛选与统计展示。
- `frontend/src/i18n/locales/en.ts`、`frontend/src/i18n/locales/zh.ts`：管理说明、支付状态、渠道名称双语文案。
- `docs/PAYMENT.md`、`docs/PAYMENT_CN.md`：Stripe Dashboard、HTTPS 域名、Wallet、Webhook、验收和回滚说明。

### Task 1: 管理端 Google Pay 子方式、默认关闭与 card 依赖

**Files:**
- Modify: `frontend/src/components/payment/providerConfig.ts`
- Modify: `frontend/src/components/payment/PaymentProviderDialog.vue`
- Modify: `frontend/src/components/payment/__tests__/providerConfig.spec.ts`
- Modify: `frontend/src/components/payment/__tests__/PaymentProviderDialog.spec.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Modify: `frontend/src/i18n/locales/zh.ts`

**Interfaces:**
- Consumes: 现有 `PROVIDER_SUPPORTED_TYPES`、`getAvailableTypes()`、`PaymentProviderDialog.reset()` 和 `handleSave()`。
- Produces: `PROVIDER_DEFAULT_SUPPORTED_TYPES: Record<string, string[]>`；`isValidStripeGooglePaySelection(providerKey: string, supportedTypes: string[]): boolean`；i18n 键 `admin.settings.payment.googlePayRequiresCard`、`admin.settings.payment.googlePaySetupHint`、`payment.methods.google_pay`。

- [ ] **Step 1: 写可选列表、默认列表和依赖校验的失败测试**

在 `providerConfig.spec.ts` 增加：

```ts
import {
  PROVIDER_DEFAULT_SUPPORTED_TYPES,
  PROVIDER_SUPPORTED_TYPES,
  isValidStripeGooglePaySelection,
} from '@/components/payment/providerConfig'

describe('Stripe Google Pay provider configuration', () => {
  it('offers Google Pay without enabling it by default', () => {
    expect(PROVIDER_SUPPORTED_TYPES.stripe).toContain('google_pay')
    expect(PROVIDER_DEFAULT_SUPPORTED_TYPES.stripe).toEqual(['card', 'alipay', 'wxpay', 'link'])
  })

  it('requires card whenever Google Pay is selected', () => {
    expect(isValidStripeGooglePaySelection('stripe', ['google_pay'])).toBe(false)
    expect(isValidStripeGooglePaySelection('stripe', ['card', 'google_pay'])).toBe(true)
    expect(isValidStripeGooglePaySelection('airwallex', ['google_pay'])).toBe(true)
  })
})
```

先给测试 `messages` 增加：

```ts
'admin.settings.payment.googlePayRequiresCard': 'Google Pay requires Card',
'admin.settings.payment.googlePaySetupHint': 'Configure Stripe Dashboard and HTTPS domains.',
'payment.methods.google_pay': 'Google Pay',
```

再在 `PaymentProviderDialog.spec.ts` 增加：

```ts
it('offers Google Pay but leaves it off for a new Stripe provider', async () => {
  const wrapper = mountDialog()
  ;(wrapper.vm as unknown as { reset: (key: string) => void }).reset('stripe')
  await nextTick()

  const googlePay = wrapper.findAll('button')
    .find(button => button.text() === 'Google Pay')
  expect(googlePay).toBeDefined()
  expect(googlePay!.classes()).not.toContain('bg-primary-500')
})

it('blocks Stripe Google Pay without card', async () => {
  const provider = providerFactory({
    provider_key: 'stripe',
    name: 'Stripe',
    supported_types: ['google_pay'],
    config: { publishableKey: 'pk_test_123', currency: 'USD' },
  })
  const wrapper = mountDialog({ editing: provider })
  ;(wrapper.vm as unknown as { loadProvider: (value: ProviderInstance) => void })
    .loadProvider(provider)
  await nextTick()
  await wrapper.find('form').trigger('submit.prevent')

  expect(wrapper.emitted('save')).toBeUndefined()
  expect(wrapper.text()).toContain('Google Pay requires Card')
})
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```bash
pnpm --dir frontend exec vitest run src/components/payment/__tests__/providerConfig.spec.ts src/components/payment/__tests__/PaymentProviderDialog.spec.ts
```

Expected: FAIL，提示 `PROVIDER_DEFAULT_SUPPORTED_TYPES`、`isValidStripeGooglePaySelection` 未导出，且默认表单行为仍会选中全部 Stripe 子方式。

- [ ] **Step 3: 实现可选/默认列表分离和纯函数**

在 `providerConfig.ts` 将 Stripe 可选项扩展，并增加默认表：

```ts
export const PROVIDER_SUPPORTED_TYPES: Record<string, string[]> = {
  easypay: ['alipay', 'wxpay'],
  alipay: ['alipay'],
  wxpay: ['wxpay'],
  stripe: ['card', 'alipay', 'wxpay', 'link', 'google_pay'],
  airwallex: ['airwallex'],
  wise: ['wise'],
}

export const PROVIDER_DEFAULT_SUPPORTED_TYPES: Record<string, string[]> = {
  easypay: ['alipay', 'wxpay'],
  alipay: ['alipay'],
  wxpay: ['wxpay'],
  stripe: ['card', 'alipay', 'wxpay', 'link'],
  airwallex: ['airwallex'],
  wise: ['wise'],
}

export function isValidStripeGooglePaySelection(
  providerKey: string,
  supportedTypes: string[],
): boolean {
  return providerKey !== 'stripe'
    || !supportedTypes.includes('google_pay')
    || supportedTypes.includes('card')
}
```

- [ ] **Step 4: 接入管理端即时提示、默认值和保存拦截**

在 `PaymentProviderDialog.vue` 导入新符号，新增计算属性：

```ts
const stripeGooglePayRequiresCard = computed(() =>
  !isValidStripeGooglePaySelection(form.provider_key, form.supported_types),
)
```

在支持方式按钮组下加入：

```vue
<p
  v-if="form.provider_key === 'stripe'"
  class="mt-2 text-xs text-gray-500 dark:text-gray-400"
>
  {{ t('admin.settings.payment.googlePaySetupHint') }}
</p>
<p
  v-if="stripeGooglePayRequiresCard"
  role="alert"
  class="mt-2 text-xs text-red-600 dark:text-red-400"
>
  {{ t('admin.settings.payment.googlePayRequiresCard') }}
</p>
```

把 `onKeyChange()` 与 `reset()` 中的默认赋值改为：

```ts
form.supported_types = [...(PROVIDER_DEFAULT_SUPPORTED_TYPES[providerKey] || [])]
```

其中 `onKeyChange()` 使用 `form.provider_key`，`reset()` 使用 `defaultKey`。`loadProvider()` 继续原样读取存量 `supported_types`，因此升级不会替存量实例开启 Google Pay。在 `handleSave()` 名称校验之后加入：

```ts
if (stripeGooglePayRequiresCard.value) {
  emitValidationError(t('admin.settings.payment.googlePayRequiresCard'))
  return
}
```

- [ ] **Step 5: 增加双语管理说明和支付方式名称**

在中英文 locale 对应 `admin.settings.payment` 与 `payment.methods` 节点加入：

```ts
// en.ts
googlePayRequiresCard: 'Google Pay requires Card to be enabled for this Stripe provider.',
googlePaySetupHint: 'Also enable Google Pay in Stripe Dashboard and register every HTTPS payment domain. Merchant ID and Domain ID are not entered in Sub2API.',
// payment.methods
google_pay: 'Google Pay',

// zh.ts
googlePayRequiresCard: '启用 Google Pay 时必须同时启用此 Stripe 服务商的银行卡支付。',
googlePaySetupHint: '还需在 Stripe Dashboard 启用 Google Pay，并注册所有 HTTPS 支付域名；Merchant ID 和 Domain ID 无需填写到 Sub2API。',
// payment.methods
google_pay: 'Google Pay',
```

- [ ] **Step 6: 运行聚焦测试和类型检查并确认 GREEN**

Run:

```bash
pnpm --dir frontend exec vitest run src/components/payment/__tests__/providerConfig.spec.ts src/components/payment/__tests__/PaymentProviderDialog.spec.ts
pnpm --dir frontend run typecheck
```

Expected: 两个测试文件全部 PASS；`vue-tsc` exit 0。

- [ ] **Step 7: 提交管理端配置能力**

```bash
git add frontend/src/components/payment/providerConfig.ts frontend/src/components/payment/PaymentProviderDialog.vue frontend/src/components/payment/__tests__/providerConfig.spec.ts frontend/src/components/payment/__tests__/PaymentProviderDialog.spec.ts frontend/src/i18n/locales/en.ts frontend/src/i18n/locales/zh.ts
git commit -m "feat(payments): configure Google Pay for Stripe"
```

### Task 2: 后端配置校验与 Stripe PaymentIntent 类型去重

**Files:**
- Modify: `backend/internal/payment/types.go`
- Modify: `backend/internal/payment/registry_test.go`
- Modify: `backend/internal/service/payment_config_providers.go`
- Modify: `backend/internal/service/payment_config_providers_test.go`
- Modify: `backend/internal/payment/provider/stripe.go`
- Create: `backend/internal/payment/provider/stripe_test.go`

**Interfaces:**
- Consumes: `validateProviderRequest(providerKey, name, supportedTypes string) error`；`resolveStripeMethodTypes(instanceSubMethods string) []string`；`payment.GetBasePaymentType(string) string`。
- Produces: `payment.TypeGooglePay = "google_pay"`；`payment.NotificationMetadataPaymentType = "payment_type"`；`google_pay -> stripe` 基础 Provider 映射；`card + google_pay -> []string{"card"}`。

- [ ] **Step 1: 写后端依赖校验、基础映射和去重的失败测试**

在 `payment_config_providers_test.go` 的 `validateProviderRequest` 表格加入：

```go
{
    name: "stripe google pay requires card", providerKey: payment.TypeStripe,
    providerName: "Stripe", supportedTypes: payment.TypeGooglePay,
    wantErr: true, errContains: "google_pay requires card",
},
{
    name: "stripe google pay with card", providerKey: payment.TypeStripe,
    providerName: "Stripe", supportedTypes: payment.TypeCard + "," + payment.TypeGooglePay,
},
```

在 `registry_test.go` 增加：

```go
func TestGooglePayPaymentTypeUsesStripeProvider(t *testing.T) {
    t.Parallel()
    require.Equal(t, TypeStripe, GetBasePaymentType(TypeGooglePay))
}
```

新建 `stripe_test.go`：

```go
func TestResolveStripeMethodTypesMapsGooglePayToDeduplicatedCard(t *testing.T) {
    t.Parallel()
    require.Equal(t, []string{"card"}, resolveStripeMethodTypes("card,google_pay"))
    require.Equal(t, []string{"card", "link"}, resolveStripeMethodTypes("google_pay,card,link"))
}
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```bash
cd backend && go test -tags=unit ./internal/payment ./internal/payment/provider ./internal/service -run 'Test(GooglePayPaymentTypeUsesStripeProvider|ResolveStripeMethodTypesMapsGooglePayToDeduplicatedCard|ValidateProviderRequest)' -count=1
```

Expected: FAIL，`TypeGooglePay` 未定义，且 `resolveStripeMethodTypes("card,google_pay")` 未满足去重断言。

- [ ] **Step 3: 增加渠道常量和基础 Provider 映射**

在 `backend/internal/payment/types.go` 增加：

```go
const (
    TypeGooglePay PaymentType = "google_pay"
    NotificationMetadataPaymentType = "payment_type"
)
```

并把 `GetBasePaymentType()` 的 Stripe 分支改为：

```go
case t == TypeStripe || t == TypeCard || t == TypeLink || t == TypeGooglePay:
    return TypeStripe
```

- [ ] **Step 4: 创建和更新请求都执行 Google Pay/card 校验**

在 `validateProviderRequest()` 中加入：

```go
if providerKey == payment.TypeStripe {
    selected := make(map[string]bool)
    for _, supportedType := range splitTypes(supportedTypes) {
        selected[supportedType] = true
    }
    if selected[payment.TypeGooglePay] && !selected[payment.TypeCard] {
        return infraerrors.BadRequest("VALIDATION_ERROR", "stripe google_pay requires card")
    }
}
```

`CreateProviderInstance()` 已调用该函数；在 `UpdateProviderInstance()` 计算 `nextSupportedTypes` 后、可见方式冲突校验前加入：

```go
nextName := current.Name
if req.Name != nil {
    nextName = *req.Name
}
if err := validateProviderRequest(current.ProviderKey, nextName, nextSupportedTypes); err != nil {
    return nil, err
}
```

- [ ] **Step 5: 将 Google Pay 映射为 card 并保持首次出现顺序去重**

在 `stripePaymentMethodTypes` 增加：

```go
payment.TypeGooglePay: {"card"},
```

将 `resolveStripeMethodTypes()` 的收集部分改为：

```go
methods := make([]string, 0)
seen := make(map[string]struct{})
for _, paymentType := range strings.Split(instanceSubMethods, ",") {
    paymentType = strings.TrimSpace(paymentType)
    for _, method := range stripePaymentMethodTypes[paymentType] {
        if _, exists := seen[method]; exists {
            continue
        }
        seen[method] = struct{}{}
        methods = append(methods, method)
    }
}
```

保留空结果回退 `[]string{"card"}`；绝不能向 Stripe 发送 `google_pay` 作为 PaymentMethod 类型。

- [ ] **Step 6: 运行后端聚焦测试并确认 GREEN**

Run:

```bash
cd backend && go test -tags=unit ./internal/payment ./internal/payment/provider ./internal/service -run 'Test(GooglePayPaymentTypeUsesStripeProvider|ResolveStripeMethodTypesMapsGooglePayToDeduplicatedCard|ValidateProviderRequest)' -count=1
```

Expected: PASS，且测试过程中没有外部 Stripe 请求。

- [ ] **Step 7: 提交配置校验与映射**

```bash
git add backend/internal/payment/types.go backend/internal/payment/registry_test.go backend/internal/service/payment_config_providers.go backend/internal/service/payment_config_providers_test.go backend/internal/payment/provider/stripe.go backend/internal/payment/provider/stripe_test.go
git commit -m "feat(payments): map Stripe Google Pay to card"
```

### Task 3: 传播实际 Stripe 实例的公开能力并支持刷新恢复

**Files:**
- Modify: `backend/internal/service/payment_config_service.go`
- Modify: `backend/internal/service/payment_config_service_test.go`
- Modify: `backend/internal/handler/payment_handler.go`
- Modify: `backend/internal/handler/payment_handler_resume_test.go`
- Modify: `backend/internal/service/payment_service.go`
- Modify: `backend/internal/service/payment_order.go`
- Modify: `backend/internal/service/payment_order_result_test.go`
- Modify: `frontend/src/types/payment.ts`
- Modify: `frontend/src/components/payment/paymentFlow.ts`
- Modify: `frontend/src/components/payment/__tests__/paymentFlow.spec.ts`
- Modify: `frontend/src/components/payment/StripePaymentInline.vue`
- Modify: `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts`
- Modify: `frontend/src/views/user/PaymentView.vue`
- Modify: `frontend/src/views/user/__tests__/PaymentView.spec.ts`

**Interfaces:**
- Consumes: `payment.InstanceSelection{InstanceID, ProviderKey, Config, SupportedTypes}`；`buildCreateOrderResponse(...)`；`PaymentRecoverySnapshot`；`decidePaymentLaunch()`。
- Produces: Checkout 字段 `stripe_google_pay_enabled: boolean`；订单响应字段 `stripe_publishable_key: string`、`google_pay_enabled: boolean`；快照字段 `stripePublishableKey?: string`、`googlePayEnabled?: boolean`。

- [ ] **Step 1: 写 Checkout 与订单所选实例能力的失败测试**

在 `payment_config_service_test.go` 增加同实例配对测试：

```go
func TestGetStripeCheckoutCapabilitiesUsesOneOrderedInstance(t *testing.T) {
    ctx := context.Background()
    client := newPaymentConfigServiceTestClient(t)
    for _, item := range []struct {
        name, config, supported string
        sortOrder int
    }{
        {"primary", `{"publishableKey":"pk_primary"}`, "card", 1},
        {"secondary", `{"publishableKey":"pk_secondary"}`, "card,google_pay", 2},
    } {
        _, err := client.PaymentProviderInstance.Create().
            SetProviderKey(payment.TypeStripe).
            SetName(item.name).
            SetConfig(item.config).
            SetSupportedTypes(item.supported).
            SetSortOrder(item.sortOrder).
            SetEnabled(true).
            Save(ctx)
        if err != nil {
            t.Fatal(err)
        }
    }
    svc := &PaymentConfigService{entClient: client}
    key, enabled := svc.getStripeCheckoutCapabilities(ctx)
    if key != "pk_primary" || enabled {
        t.Fatalf("capabilities = (%q, %v), want (pk_primary, false)", key, enabled)
    }
}
```

在 `payment_order_result_test.go` 增加完整响应测试：

```go
func TestBuildCreateOrderResponseUsesSelectedStripeCapabilities(t *testing.T) {
    t.Parallel()
    order := &dbent.PaymentOrder{
        ID: 42, Amount: 10, PayAmount: 10, OutTradeNo: "sub2_42",
        ExpiresAt: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC),
    }
    sel := &payment.InstanceSelection{
        InstanceID: "42", ProviderKey: payment.TypeStripe,
        Config: map[string]string{payment.ConfigKeyPublishableKey: "pk_selected"},
        SupportedTypes: "card,google_pay",
    }
    resp := buildCreateOrderResponse(
        order,
        CreateOrderRequest{PaymentType: payment.TypeStripe},
        10,
        sel,
        &payment.CreatePaymentResponse{ClientSecret: "pi_secret"},
        payment.CreatePaymentResultOrderCreated,
    )
    if resp.StripePublishableKey != "pk_selected" || !resp.GooglePayEnabled {
        t.Fatalf("Stripe capabilities = (%q, %v)", resp.StripePublishableKey, resp.GooglePayEnabled)
    }
}
```

同时增加不含 `google_pay` 和非 Stripe selection 的 false/空值用例。Handler 测试断言 `/payment/checkout-info` JSON 包含 `stripe_google_pay_enabled`。

- [ ] **Step 2: 写前端 API 与恢复快照的失败测试**

在 `paymentFlow.spec.ts` 的 Stripe 决策输入加入：

```ts
stripe_publishable_key: 'pk_selected',
google_pay_enabled: true,
```

并断言：

```ts
expect(decision.recovery.stripePublishableKey).toBe('pk_selected')
expect(decision.recovery.googlePayEnabled).toBe(true)
```

增加旧快照缺少两字段仍可读取、且返回值保持 `undefined` 的兼容用例。在 `PaymentView.spec.ts` 把 Stub props 增加 `googlePayEnabled`，并验证新订单优先使用响应中的 `pk_selected`/`true`，即使 Checkout 回退是另一个 key/false。

- [ ] **Step 3: 运行聚焦测试并确认 RED**

Run:

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler -run 'Test.*(Stripe|CheckoutInfo|CreateOrderResponse)' -count=1
pnpm --dir frontend exec vitest run src/components/payment/__tests__/paymentFlow.spec.ts src/components/payment/__tests__/StripePaymentInline.spec.ts src/views/user/__tests__/PaymentView.spec.ts
```

Expected: FAIL，API/Go/TypeScript 类型中尚无三个能力字段，快照断言失败。

- [ ] **Step 4: 让 Checkout 回退 key 与开关来自同一实例**

在 `PaymentConfig` 增加：

```go
StripeGooglePayEnabled bool `json:"stripe_google_pay_enabled"`
```

用以下 helper 取代只返回 key 的 `getStripePublishableKey()`：

```go
func (s *PaymentConfigService) getStripeCheckoutCapabilities(ctx context.Context) (string, bool) {
    if s.entClient == nil {
        return "", false
    }
    inst, err := s.entClient.PaymentProviderInstance.Query().
        Where(
            paymentproviderinstance.EnabledEQ(true),
            paymentproviderinstance.ProviderKeyEQ(payment.TypeStripe),
        ).
        Order(paymentproviderinstance.BySortOrder(), paymentproviderinstance.ByID()).
        First(ctx)
    if err != nil {
        return "", false
    }
    cfg, err := s.decryptConfig(inst.Config)
    if err != nil {
        return "", false
    }
    return cfg[payment.ConfigKeyPublishableKey], containsPaymentType(inst.SupportedTypes, payment.TypeGooglePay)
}

func containsPaymentType(raw string, target string) bool {
    for _, value := range splitTypes(raw) {
        if value == target {
            return true
        }
    }
    return false
}
```

在 `GetPaymentConfig()` 中一次性赋值：

```go
cfg.StripePublishableKey, cfg.StripeGooglePayEnabled = s.getStripeCheckoutCapabilities(ctx)
```

在 `checkoutInfoResponse` 和响应构造中加入：

```go
StripeGooglePayEnabled bool `json:"stripe_google_pay_enabled"`
```

- [ ] **Step 5: 从实际订单 selection 构造权威公开能力**

在 `CreateOrderResponse` 增加：

```go
StripePublishableKey string `json:"stripe_publishable_key,omitempty"`
GooglePayEnabled     bool   `json:"google_pay_enabled"`
```

在 `payment_order.go` 增加：

```go
func stripeSelectionPublicCapabilities(sel *payment.InstanceSelection) (string, bool) {
    if sel == nil || sel.ProviderKey != payment.TypeStripe {
        return "", false
    }
    return sel.Config[payment.ConfigKeyPublishableKey],
        containsPaymentType(sel.SupportedTypes, payment.TypeGooglePay)
}
```

把 `buildCreateOrderResponse()` 的直接 return 改为先取能力再构造响应：

```go
stripePublishableKey, googlePayEnabled := stripeSelectionPublicCapabilities(sel)
return &CreateOrderResponse{
    OrderID:              order.ID,
    Amount:               order.Amount,
    PayAmount:            payAmount,
    FeeRate:              order.FeeRate,
    Status:               OrderStatusPending,
    ResultType:           resultType,
    PaymentType:          req.PaymentType,
    OutTradeNo:           order.OutTradeNo,
    PayURL:               pr.PayURL,
    QRCode:               pr.QRCode,
    ClientSecret:         pr.ClientSecret,
    IntentID:             pr.IntentID,
    Currency:             pr.Currency,
    CountryCode:          pr.CountryCode,
    PaymentEnv:           pr.PaymentEnv,
    OAuth:                pr.OAuth,
    JSAPI:                pr.JSAPI,
    JSAPIPayload:         pr.JSAPI,
    ExpiresAt:            order.ExpiresAt,
    PaymentMode:          sel.PaymentMode,
    StripePublishableKey: stripePublishableKey,
    GooglePayEnabled:     googlePayEnabled,
}
```

只返回 Publishable Key；不要复制 `secretKey`、`webhookSecret` 或整个 Config。

- [ ] **Step 6: 扩展前端类型和向后兼容恢复快照**

在 `PaymentConfig`、`CheckoutInfoResponse` 增加 `stripe_google_pay_enabled: boolean`，在 `CreateOrderResult` 增加：

```ts
stripe_publishable_key?: string
google_pay_enabled?: boolean
```

在 `PaymentRecoverySnapshot` 增加：

```ts
stripePublishableKey?: string
googlePayEnabled?: boolean
```

`decidePaymentLaunch()` 创建基础快照时写入：

```ts
stripePublishableKey: result.stripe_publishable_key,
googlePayEnabled: result.google_pay_enabled,
```

`readPaymentRecoverySnapshot()` 仅在字段存在时检查类型，并在返回对象保留两字段：

```ts
|| (parsed.stripePublishableKey != null && typeof parsed.stripePublishableKey !== 'string')
|| (parsed.googlePayEnabled != null && typeof parsed.googlePayEnabled !== 'boolean')
```

- [ ] **Step 7: 让 `/purchase` 使用订单能力而非错误的全局实例**

给 `StripePaymentInline` 的 props 增加 `googlePayEnabled: boolean`（Task 4 使用），并把 `StripePaymentInline.spec.ts` 的 `mountInline()` 默认 props 增加 `googlePayEnabled: false`：

```ts
const props = defineProps<{
  orderId: number
  amount: number
  clientSecret: string
  orderType?: 'balance' | 'subscription'
  publishableKey: string
  googlePayEnabled: boolean
  payAmount: number
  currency?: string
}>()
```

在 `PaymentView.vue` 传入：

```vue
:publishable-key="paymentState.stripePublishableKey || checkout.stripe_publishable_key"
:google-pay-enabled="paymentState.googlePayEnabled ?? checkout.stripe_google_pay_enabled"
```

把 `emptyPaymentState()` 保持为不设置可选字段；Checkout 初值增加 `stripe_google_pay_enabled: false`。新建订单响应会写入明确的 `false`，因此不会错误回退到另一 Stripe 实例的 `true`；只有旧快照缺字段时才使用 Checkout 回退。

- [ ] **Step 8: 运行聚焦测试与类型检查并确认 GREEN**

Run:

```bash
cd backend && go test -tags=unit ./internal/service ./internal/handler -run 'Test.*(Stripe|CheckoutInfo|CreateOrderResponse)' -count=1
pnpm --dir frontend exec vitest run src/components/payment/__tests__/paymentFlow.spec.ts src/components/payment/__tests__/StripePaymentInline.spec.ts src/views/user/__tests__/PaymentView.spec.ts
pnpm --dir frontend run typecheck
```

Expected: 全部 PASS；多实例用例证明订单 response 优先，旧快照用例仍兼容。

- [ ] **Step 9: 提交能力传播**

```bash
git add backend/internal/service/payment_config_service.go backend/internal/service/payment_config_service_test.go backend/internal/handler/payment_handler.go backend/internal/handler/payment_handler_resume_test.go backend/internal/service/payment_service.go backend/internal/service/payment_order.go backend/internal/service/payment_order_result_test.go frontend/src/types/payment.ts frontend/src/components/payment/paymentFlow.ts frontend/src/components/payment/__tests__/paymentFlow.spec.ts frontend/src/components/payment/StripePaymentInline.vue frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts frontend/src/views/user/PaymentView.vue frontend/src/views/user/__tests__/PaymentView.spec.ts
git commit -m "feat(payments): propagate Stripe Google Pay capability"
```

### Task 4: 两个 Stripe 页面挂载真实 Express Checkout 四态组件

**Files:**
- Modify: `frontend/src/components/payment/StripeGooglePayExpress.vue`
- Modify: `frontend/src/components/payment/StripePaymentInline.vue`
- Modify: `frontend/src/views/user/StripePaymentView.vue`
- Modify: `frontend/src/components/payment/__tests__/StripeGooglePayExpress.spec.ts`
- Modify: `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts`
- Modify: `frontend/src/views/user/__tests__/StripePaymentView.spec.ts`
- Modify: `frontend/src/i18n/locales/en.ts`
- Modify: `frontend/src/i18n/locales/zh.ts`

**Interfaces:**
- Consumes: Task 3 的 `googlePayEnabled`、`stripePublishableKey`；真实 `StripeGooglePayExpress` 子组件；父组件现有 `Stripe`、`StripeElements`、`submitting`、结果页路由。
- Produces: `GooglePayAvailability = 'checking' | 'available' | 'unavailable' | 'error'`；禁用占位 `data-testid="stripe-google-pay-placeholder"`；真实 Element mount `data-testid="stripe-google-pay-mount"`。

- [ ] **Step 1: 写四态、不可点击占位和安全日志的失败测试**

在 `StripeGooglePayExpress.spec.ts` 保留真实 Vue 组件挂载，只把 `elements.create()` 返回的 Stripe SDK 网络边界替换为测试 double。覆盖：

```ts
expect(wrapper.get('[data-testid="stripe-google-pay-state"]').text())
  .toContain('payment.googlePayChecking')
expect(wrapper.get('[data-testid="stripe-google-pay-placeholder"]').attributes('disabled'))
  .toBeDefined()
```

依次触发：

```ts
handlers.get('ready')?.({ availablePaymentMethods: { googlePay: true } })
handlers.get('availablepaymentmethodschange')?.({ paymentMethods: { googlePay: { available: false } } })
handlers.get('loaderror')?.({ error: { type: 'invalid_request_error', code: 'payment_method_domain_invalid', payment_method: { card: 'must-not-log' } } })
```

断言四态分别显示检测文案、真实 mount、通用诊断、通用诊断；`console.warn` 只收到 `type` 和 `code`。

- [ ] **Step 2: 写两个父页面的能力关闭、真实挂载和双向提交锁失败测试**

在 `StripePaymentInline.spec.ts` 与 `StripePaymentView.spec.ts`：

- 传 `googlePayEnabled: false`，断言 `elements.create('expressCheckout', ...)` 从未发生。
- 传 `true`，不要 stub `StripeGooglePayExpress`，断言真实子组件通过同一 `elements` 创建 Express Checkout。
- 先触发 Payment Element 提交，再触发 Google Pay `confirm`，断言第二次 `confirmPayment` 被阻止。
- 先保持 Google Pay confirm Promise pending，再点击普通支付按钮，断言只有一次 `confirmPayment`。
- Google Pay confirm 无即时错误后，断言进入 `/payment/result`，且没有把本地订单标记成功。
- 卸载两个页面，断言 Express Checkout 和 Payment Element 都先 `off()` 再 `destroy()`，内嵌页的 popup `message` listener 也被移除。

- [ ] **Step 3: 运行三个前端测试文件并确认 RED**

Run:

```bash
pnpm --dir frontend exec vitest run src/components/payment/__tests__/StripeGooglePayExpress.spec.ts src/components/payment/__tests__/StripePaymentInline.spec.ts src/views/user/__tests__/StripePaymentView.spec.ts
```

Expected: FAIL；当前组件会静默隐藏检测/不可用/失败状态，父页面也尚未按 `googlePayEnabled` 控制挂载。

- [ ] **Step 4: 将 Express 组件改为明确四态且仅真实按钮可点击**

核心状态和事件实现为：

```ts
type GooglePayAvailability = 'checking' | 'available' | 'unavailable' | 'error'
const availability = ref<GooglePayAvailability>('checking')

function handleReady(event: StripeExpressCheckoutElementReadyEvent) {
  availability.value = event.availablePaymentMethods?.googlePay ? 'available' : 'unavailable'
}

function handleAvailabilityChange(
  event: StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent,
) {
  availability.value = event.paymentMethods?.googlePay?.available ? 'available' : 'unavailable'
}

function handleLoadError(event: { error: { type: string; code?: string } }) {
  console.warn('[StripeGooglePayExpress] load failed', {
    type: event.error.type,
    code: event.error.code,
  })
  availability.value = 'error'
}
```

模板保持 mount 节点始终存在供 Stripe SDK 挂载，仅用 `v-show` 切换可见性；非可用状态显示原生禁用按钮：

```vue
<div data-testid="stripe-google-pay-state" class="space-y-3" aria-live="polite">
  <div
    ref="mountTarget"
    data-testid="stripe-google-pay-mount"
    v-show="availability === 'available'"
    :class="{ 'pointer-events-none opacity-60': disabled || confirming }"
  />
  <template v-if="availability !== 'available'">
    <button
      data-testid="stripe-google-pay-placeholder"
      type="button"
      disabled
      :aria-label="t('payment.googlePayUnavailableLabel')"
      class="w-full rounded-md bg-black px-4 py-3 font-medium text-white opacity-50"
    >Google Pay</button>
    <p class="text-sm text-gray-500 dark:text-gray-400">
      {{ availability === 'checking' ? t('payment.googlePayChecking') : t('payment.googlePayUnavailable') }}
    </p>
  </template>
  <p v-if="errorMessage" class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
</div>
```

保留当前真实 `elements.create('expressCheckout', options)`、`stripe.confirmPayment()`、事件 `on/off` 和 `destroy()`；禁止用模拟按钮调用付款。

- [ ] **Step 5: 仅在实例启用时挂载，并保持分隔线与 Payment Element**

使用 Task 3 已加入 `StripePaymentInline` 的 prop：

```ts
googlePayEnabled: boolean
```

两个页面均改为：

```vue
<StripeGooglePayExpress
  v-if="googlePayEnabled && stripeInstance && elementsInstance"
  :stripe="stripeInstance"
  :elements="elementsInstance"
  :return-url="returnUrl"
  :disabled="submitting"
  @submitting-change="submitting = $event"
  @confirmed="handleGooglePayConfirmed"
/>
<div
  v-if="googlePayEnabled"
  class="my-5 border-t border-gray-200 dark:border-dark-600"
  aria-hidden="true"
/>
```

删除父页面仅以 `googlePayAvailable` 控制分隔线的状态。独立页把恢复快照变量提升到 `onMounted()` 函数作用域，并只保留匹配当前订单的快照：

```ts
const googlePayEnabled = ref(false)

// onMounted 内
let restored: PaymentRecoverySnapshot | null = null
if (typeof window !== 'undefined') {
  const candidate = readPaymentRecoverySnapshot(
    window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
    { resumeToken },
  )
  if (candidate?.orderId === orderId) {
    restored = candidate
    currency.value = normalizePaymentCurrency(candidate.currency)
  }
}

// fetchConfig() 后：
const publishableKey = restored?.stripePublishableKey
  || paymentStore.config?.stripe_publishable_key
googlePayEnabled.value = restored?.googlePayEnabled
  ?? paymentStore.config?.stripe_google_pay_enabled
  ?? false
```

未启用时完全不创建 Express Checkout；Payment Element 始终保留。

- [ ] **Step 6: Google Pay 确认后直接进入现有结果页等待 Webhook**

内嵌页继续 `emit('confirmed')`，由 `PaymentView.onStripePaymentConfirmed()` 保留快照并导航结果页。独立页将 `handleGooglePayConfirmed()` 改为：

```ts
async function handleGooglePayConfirmed() {
  await router.push({
    path: '/payment/result',
    query: {
      order_id: String(route.query.order_id || ''),
      resume_token: typeof route.query.resume_token === 'string'
        ? route.query.resume_token
        : undefined,
    },
  })
}
```

不要设置 `stripeSuccess=true`，不要提前显示“支付成功”，不要清除恢复快照。即时 Stripe 错误仍释放本组件取得的锁；钱包取消不释放父组件拥有的锁。

- [ ] **Step 7: 增加双语状态文案**

```ts
// en.ts
googlePayChecking: 'Checking Google Pay availability…',
googlePayUnavailable: 'Google Pay is unavailable in this environment. Check HTTPS, Stripe payment domains, and Google Wallet, or use another Stripe payment method.',
googlePayUnavailableLabel: 'Google Pay unavailable',

// zh.ts
googlePayChecking: '正在检测 Google Pay 可用性…',
googlePayUnavailable: '当前环境无法使用 Google Pay，请检查 HTTPS、Stripe 支付域名和 Google Wallet，或改用其他 Stripe 支付方式。',
googlePayUnavailableLabel: 'Google Pay 当前不可用',
```

- [ ] **Step 8: 运行聚焦测试、类型检查和目标 lint 并确认 GREEN**

Run:

```bash
pnpm --dir frontend exec vitest run src/components/payment/__tests__/StripeGooglePayExpress.spec.ts src/components/payment/__tests__/StripePaymentInline.spec.ts src/views/user/__tests__/StripePaymentView.spec.ts src/views/user/__tests__/PaymentView.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend exec eslint src/components/payment/StripeGooglePayExpress.vue src/components/payment/StripePaymentInline.vue src/views/user/StripePaymentView.vue src/views/user/PaymentView.vue src/components/payment/__tests__/StripeGooglePayExpress.spec.ts src/components/payment/__tests__/StripePaymentInline.spec.ts src/views/user/__tests__/StripePaymentView.spec.ts
```

Expected: 全部 PASS；ESLint exit 0；测试挂载真实 `StripeGooglePayExpress`，但无外部 Stripe 网络调用。

- [ ] **Step 9: 提交用户支付界面**

```bash
git add frontend/src/components/payment/StripeGooglePayExpress.vue frontend/src/components/payment/StripePaymentInline.vue frontend/src/views/user/StripePaymentView.vue frontend/src/components/payment/__tests__/StripeGooglePayExpress.spec.ts frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts frontend/src/views/user/__tests__/StripePaymentView.spec.ts frontend/src/i18n/locales/en.ts frontend/src/i18n/locales/zh.ts
git commit -m "feat(payments): gate Google Pay Express Checkout"
```

### Task 5: Webhook 使用真实 Stripe SDK 识别 Google Pay

**Files:**
- Modify: `backend/internal/payment/provider/stripe.go`
- Modify: `backend/internal/payment/provider/stripe_test.go`
- Modify: `backend/internal/handler/payment_webhook_handler.go`
- Modify: `backend/internal/handler/payment_webhook_handler_test.go`

**Interfaces:**
- Consumes: stripe-go `Client.V1PaymentMethods.Retrieve(ctx, id, nil)`；已验签 `payment_intent.succeeded`；`PaymentNotification.Metadata`。
- Produces: `stripePaymentMethodRetriever.Retrieve(context.Context, string, *stripe.PaymentMethodRetrieveParams) (*stripe.PaymentMethod, error)`；成功通知元数据 `payment_type=stripe|google_pay`。

- [ ] **Step 1: 写真实 SDK 适配边界和钱包识别的失败测试**

在 `stripe_test.go` 定义只替换网络边界的 fake：

```go
type fakeStripePaymentMethodRetriever struct {
    paymentMethod *stripe.PaymentMethod
    err error
    requestedID string
}

func (f *fakeStripePaymentMethodRetriever) Retrieve(
    _ context.Context,
    id string,
    _ *stripe.PaymentMethodRetrieveParams,
) (*stripe.PaymentMethod, error) {
    f.requestedID = id
    return f.paymentMethod, f.err
}
```

为成功事件写三类用例：Google Pay PaymentMethod 返回 `payment.TypeGooglePay`；普通 card 返回 `payment.TypeStripe`；Retrieve 返回错误时 `VerifyNotification()` 返回错误且 notification 为 nil。事件 JSON 必须包含 `payment_method: "pm_123"`，签名使用固定 `webhookSecret` 生成，证明查询只发生在验签通过后。另在 Handler 测试写入 Stripe 事件 `data.object.metadata.orderId=sub2_selected_42`，断言 `extractOutTradeNo()` 返回该值，以便多实例 Webhook 先定位原订单实例再验签。

- [ ] **Step 2: 运行 Provider/Webhook 测试并确认 RED**

Run:

```bash
cd backend && go test -tags=unit ./internal/payment/provider ./internal/handler -run 'Test.*Stripe.*(GooglePay|PaymentMethod|Webhook)' -count=1
```

Expected: FAIL；当前 `VerifyNotification()` 不查询 PaymentMethod，也不会写实际渠道元数据。

- [ ] **Step 3: 添加 stripe-go PaymentMethod 查询适配器**

在 `stripe.go` 增加：

```go
type stripePaymentMethodRetriever interface {
    Retrieve(context.Context, string, *stripe.PaymentMethodRetrieveParams) (*stripe.PaymentMethod, error)
}

type stripeSDKPaymentMethodRetriever struct {
    client *stripe.Client
}

func (r stripeSDKPaymentMethodRetriever) Retrieve(
    ctx context.Context,
    id string,
    params *stripe.PaymentMethodRetrieveParams,
) (*stripe.PaymentMethod, error) {
    return r.client.V1PaymentMethods.Retrieve(ctx, id, params)
}
```

给 `Stripe` 增加 `paymentMethods stripePaymentMethodRetriever`，并在 `ensureInit()` 创建真实 SDK client 后赋值：

```go
s.sc = stripe.NewClient(s.config["secretKey"])
s.paymentMethods = stripeSDKPaymentMethodRetriever{client: s.sc}
```

测试可在调用前设置 `initialized=true` 和 fake retriever；生产代码始终走 stripe-go SDK。

- [ ] **Step 4: 只在已验签成功事件中检索并识别钱包**

把 `VerifyNotification` 的 context 参数恢复为 `ctx context.Context`。解析成功 PaymentIntent 后调用：

```go
func (s *Stripe) resolvedPaymentType(
    ctx context.Context,
    pi *stripe.PaymentIntent,
) (string, error) {
    if pi.PaymentMethod == nil || strings.TrimSpace(pi.PaymentMethod.ID) == "" {
        return "", fmt.Errorf("stripe succeeded payment intent missing payment method")
    }
    method, err := s.paymentMethods.Retrieve(ctx, pi.PaymentMethod.ID, nil)
    if err != nil {
        errorType, errorCode := "unknown", ""
        var stripeErr *stripe.Error
        if errors.As(err, &stripeErr) {
            errorType = string(stripeErr.Type)
            errorCode = string(stripeErr.Code)
        }
        slog.Warn("stripe payment method lookup failed",
            "orderID", pi.Metadata["orderId"],
            "providerInstanceID", s.instanceID,
            "type", errorType,
            "code", errorCode,
        )
        return "", errors.New("stripe retrieve payment method failed")
    }
    if method.Card != nil && method.Card.Wallet != nil &&
        method.Card.Wallet.Type == stripe.PaymentMethodCardWalletTypeGooglePay {
        return payment.TypeGooglePay, nil
    }
    return payment.TypeStripe, nil
}
```

相应增加标准库 imports `errors` 与 `log/slog`。不要把原始 SDK error、PaymentMethod ID、Webhook body 或 PaymentMethod 对象写入日志/返回错误。

将解析 helper 改为明确返回 PaymentIntent：

```go
func parseStripePaymentIntent(
    event *stripe.Event,
    status string,
    rawBody string,
) (*payment.PaymentNotification, *stripe.PaymentIntent, error) {
    var pi stripe.PaymentIntent
    if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
        return nil, nil, fmt.Errorf("stripe parse payment_intent: %w", err)
    }
    currency := stripeIntentCurrency(pi.Currency, payment.DefaultPaymentCurrency)
    notification := &payment.PaymentNotification{
        TradeNo: pi.ID,
        OrderID: pi.Metadata["orderId"],
        Amount: payment.MinorUnitToAmount(pi.Amount, currency),
        Status: status,
        RawData: rawBody,
        Metadata: map[string]string{"currency": currency},
    }
    return notification, &pi, nil
}
```

`VerifyNotification()` 的 switch 使用以下完整分支：

```go
switch event.Type {
case stripeEventPaymentSuccess:
    notification, pi, err := parseStripePaymentIntent(
        &event, payment.ProviderStatusSuccess, rawBody,
    )
    if err != nil {
        return nil, err
    }
    resolvedType, err := s.resolvedPaymentType(ctx, pi)
    if err != nil {
        return nil, err
    }
    notification.Metadata[payment.NotificationMetadataPaymentType] = resolvedType
    return notification, nil
case stripeEventPaymentFailed:
    notification, _, err := parseStripePaymentIntent(
        &event, payment.ProviderStatusFailed, rawBody,
    )
    return notification, err
default:
    return nil, nil
}
```

成功分支写入的实际渠道键为：

```go
notification.Metadata[payment.NotificationMetadataPaymentType] = resolvedType
```

失败事件保持原行为，不查询 PaymentMethod。检索失败返回错误，使 Handler 返回可重试状态，让 Stripe 重投；不得从浏览器或回跳参数补齐。

在 `extractOutTradeNo()` 增加 Stripe 分支，只提取路由所需的订单号，不信任它完成支付：

```go
case payment.TypeStripe:
    var payload struct {
        Data struct {
            Object struct {
                Metadata map[string]string `json:"metadata"`
            } `json:"object"`
        } `json:"data"`
    }
    if err := json.Unmarshal([]byte(rawBody), &payload); err == nil {
        return strings.TrimSpace(payload.Data.Object.Metadata["orderId"])
    }
```

该值仅供 `GetWebhookProviders()` 找回订单固定的 Stripe instance；随后仍必须由该 instance 的 Webhook Secret 验签，未验签数据不能进入 fulfillment。

- [ ] **Step 5: 验证日志和 Handler 重试语义**

在 `payment_webhook_handler_test.go` 增加 `verifyNotificationWithProviders()` 的错误传播用例：测试 Provider 返回 `stripe retrieve payment method failed`，断言返回 notification 为 nil 且 error 非空。再为以下纯函数增加断言，确保 Stripe 验证/SDK 查询错误绝不记录原始 webhook body：

```go
func shouldLogWebhookVerifyFailureBody(providerKey string) bool {
    return providerKey != payment.TypeStripe
}
```

在 `handleNotify()` 现有 `slog.Debug(... "rawBody", truncatedBody)` 外增加该 guard。Stripe SDK Retrieve 失败时，从 `*stripe.Error` 只提取 `Type` 和 `Code`，以 `orderID=pi.Metadata["orderId"]`、`providerInstanceID=s.instanceID` 记录非敏感 warning，然后向 Handler 返回不含 SDK message、PaymentMethod ID 或 payload 的通用 `stripe retrieve payment method failed`。测试错误中放入 `must-not-log-card-payload` 并断言捕获日志和返回错误均不包含它。

- [ ] **Step 6: 运行后端测试并确认 GREEN**

Run:

```bash
cd backend && go test -tags=unit ./internal/payment/provider ./internal/handler -run 'Test.*Stripe.*(GooglePay|PaymentMethod|Webhook)' -count=1
```

Expected: PASS；测试 double 只替换 Retrieve 网络边界，生产适配器明确调用 `V1PaymentMethods.Retrieve`。

- [ ] **Step 7: 提交真实钱包识别**

```bash
git add backend/internal/payment/provider/stripe.go backend/internal/payment/provider/stripe_test.go backend/internal/handler/payment_webhook_handler.go backend/internal/handler/payment_webhook_handler_test.go
git commit -m "feat(payments): identify Google Pay from Stripe webhooks"
```

### Task 6: 同一幂等更新记录 Google Pay、保持退款路由并展示统计

**Files:**
- Modify: `backend/internal/service/payment_fulfillment.go`
- Modify: `backend/internal/service/payment_fulfillment_test.go`
- Modify: `backend/internal/service/payment_wise_reconcile_test.go`
- Modify: `backend/internal/service/payment_refund_test.go`
- Modify: `backend/internal/service/payment_stats_test.go`
- Modify: `frontend/src/components/admin/payment/AdminOrderTable.vue`
- Modify: `frontend/src/components/admin/payment/PaymentMethodChart.vue`
- Modify: `frontend/src/views/admin/orders/AdminPaymentDashboardView.vue`
- Modify: `frontend/src/components/admin/payment/__tests__/orderCurrencyDisplay.spec.ts`

**Interfaces:**
- Consumes: Task 5 的 `notification.Metadata[payment.NotificationMetadataPaymentType]`；Task 2 的 `GetBasePaymentType(google_pay) == stripe`；现有条件状态更新、退款和统计查询。
- Produces: `resolvedNotificationPaymentType(currentType, providerKey string, metadata map[string]string) string`；Google Pay 订单在状态更新时同步写 `payment_type=google_pay`。

- [ ] **Step 1: 写入账渠道、幂等和普通卡回归失败测试**

在 `payment_fulfillment_test.go` 增加：

```go
notification := &payment.PaymentNotification{
    OrderID: order.OutTradeNo, TradeNo: "pi_google_pay", Amount: order.PayAmount,
    Status: payment.NotificationStatusSuccess,
    Metadata: map[string]string{
        payment.NotificationMetadataPaymentType: payment.TypeGooglePay,
        "currency": "USD",
    },
}
require.NoError(t, svc.HandlePaymentNotification(ctx, notification, payment.TypeStripe))
updated := client.PaymentOrder.GetX(ctx, order.ID)
require.Equal(t, payment.TypeGooglePay, updated.PaymentType)
require.Equal(t, payment.TypeStripe, updated.ProviderKey)
```

再次处理同一通知，断言余额/订阅只发放一次。另写普通 Stripe card 元数据用例，断言保持 `payment_type=stripe`。写伪造 `providerKey=alipay` + `payment_type=google_pay` 用例，断言不会改成 Google Pay。

- [ ] **Step 2: 写退款路由、统计和管理展示失败测试**

在 `payment_refund_test.go` 创建 `payment_type=google_pay`、`provider_key=stripe`、原 Stripe instance 的已完成订单，断言退款使用该实例的 Stripe Provider。在 `payment_stats_test.go` 加入 Google Pay 订单并断言 `buildMethodDistribution()` 独立返回 `TypeGooglePay`。

在 `orderCurrencyDisplay.spec.ts` 增加 Google Pay 行，断言表格显示 `Google Pay` 且筛选器含 `value: 'google_pay'`。为 `PaymentMethodChart` 挂载增加 `google_pay` 数据，断言使用专用颜色而不是灰色回退。

- [ ] **Step 3: 运行测试并确认 RED**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'Test.*(GooglePay|MethodDistribution|Refund)' -count=1
pnpm --dir frontend exec vitest run src/components/admin/payment/__tests__/orderCurrencyDisplay.spec.ts
```

Expected: FAIL；订单仍保留 `stripe`，管理筛选和颜色映射没有 `google_pay`。

- [ ] **Step 4: 仅接受 Stripe SDK 产生的实际渠道并传入条件更新**

在 `payment_fulfillment.go` 增加：

```go
func resolvedNotificationPaymentType(
    currentType string,
    providerKey string,
    metadata map[string]string,
) string {
    if payment.GetBasePaymentType(providerKey) != payment.TypeStripe {
        return currentType
    }
    if metadata[payment.NotificationMetadataPaymentType] == payment.TypeGooglePay {
        return payment.TypeGooglePay
    }
    return currentType
}
```

`confirmPayment()` 调用 `toPaid()` 时增加最后一个参数：

```go
actualPaymentType := resolvedNotificationPaymentType(o.PaymentType, pk, metadata)
return s.toPaid(ctx, o, tradeNo, paid, pk, actualPaymentType)
```

修改签名：

```go
func (s *PaymentService) toPaid(
    ctx context.Context,
    o *dbent.PaymentOrder,
    tradeNo string,
    paid float64,
    pk string,
    actualPaymentType string,
) error
```

在现有带状态谓词的同一个 Ent Update 链加入：

```go
SetPaymentType(actualPaymentType)
```

它必须与 `SetStatus(PAID)`、`SetPayAmount()`、`SetPaymentTradeNo()`、`SetPaidAt()` 同属一次条件更新；不要先做单独更新。更新 `payment_wise_reconcile_test.go` 对 `toPaid()` 的直接调用，最后传入 `payment.TypeWise`。

- [ ] **Step 5: 保持 Provider 身份与退款路由**

不得修改 `ProviderKey`、`ProviderInstanceID` 或 Snapshot。Task 2 的基础映射使现有退款代码：

```go
paymentType := payment.GetBasePaymentType(strings.TrimSpace(o.PaymentType))
```

对 Google Pay 得到 Stripe。现有 `payment_refund.go` 已统一通过该基础映射选择实例，因此生产退款代码保持不变；测试必须确认实例查找、`Refund()` 与 `QueryRefund()` 都使用原 Stripe instance，不新增 Google Pay Provider 分支。

- [ ] **Step 6: 增加管理筛选和统计颜色**

在 `AdminOrderTable.vue` 的 filter options 加入：

```ts
{ value: 'google_pay', label: t('payment.methods.google_pay') },
```

在 `PaymentMethodChart.vue` 两个 map 和 `AdminPaymentDashboardView.vue` 的 `methodColor()` map 增加：

```ts
google_pay: 'bg-sky-500',
```

统计后端已经按 `PaymentType` 分组，不合并回 Stripe。订单表和详情现有 `payment.methods.<type>` 翻译会使用 Task 1 的 Google Pay 文案。

- [ ] **Step 7: 运行聚焦测试并确认 GREEN**

Run:

```bash
cd backend && go test -tags=unit ./internal/service -run 'Test.*(GooglePay|MethodDistribution|Refund)' -count=1
pnpm --dir frontend exec vitest run src/components/admin/payment/__tests__/orderCurrencyDisplay.spec.ts
pnpm --dir frontend run typecheck
```

Expected: PASS；重复通知只发放一次，Google Pay 退款仍命中原 Stripe 实例。

- [ ] **Step 8: 提交入账、退款与展示**

```bash
git add backend/internal/service/payment_fulfillment.go backend/internal/service/payment_fulfillment_test.go backend/internal/service/payment_wise_reconcile_test.go backend/internal/service/payment_refund_test.go backend/internal/service/payment_stats_test.go frontend/src/components/admin/payment/AdminOrderTable.vue frontend/src/components/admin/payment/PaymentMethodChart.vue frontend/src/views/admin/orders/AdminPaymentDashboardView.vue frontend/src/components/admin/payment/__tests__/orderCurrencyDisplay.spec.ts
git commit -m "feat(payments): record fulfilled Google Pay orders"
```

### Task 7: 运维文档、全量验证与 HTTPS 人工验收清单

**Files:**
- Modify: `docs/PAYMENT.md`
- Modify: `docs/PAYMENT_CN.md`

**Interfaces:**
- Consumes: Tasks 1–6 的最终行为和 Stripe 官方文档 `https://docs.stripe.com/google-pay?platform=web`。
- Produces: 中英文配置、诊断、安全、上线、回滚和人工验收说明；不产生运行时配置。

- [ ] **Step 1: 先运行文档契约扫描并确认旧文案不符合规格**

Run:

```bash
rg -n 'does not add a `google_pay` backend payment type|不新增 `google_pay` 后端支付类型|hidden silently|静默隐藏' docs/PAYMENT.md docs/PAYMENT_CN.md
```

Expected: 能命中已被新规格取代的旧说明，证明文档尚未描述可配置开关、禁用占位和订单渠道标识。

- [ ] **Step 2: 更新英文 Stripe Google Pay 章节**

用完整段落明确写入以下事实：

```markdown
Google Pay is an optional sub-method of each Stripe provider instance. It is off by default for both existing and newly created instances. Enable both Card and Google Pay in the provider dialog; Sub2API rejects Google Pay without Card.

The Sub2API switch does not replace Stripe Dashboard setup. Enable Google Pay under Stripe Payment Methods, register every production and staging hostname under Payment Method Domains, serve checkout over trusted TLS HTTPS, and test in a supported Chrome/Android environment with a usable card in Google Wallet. Google Pay Merchant ID and Stripe Payment Method Domain ID are not Sub2API inputs and must not be added to its configuration or logs.

When enabled, the Stripe panel shows a disabled status placeholder while Stripe checks availability. A supported environment receives Stripe's real Express Checkout Google Pay button. An unsupported domain, browser, wallet, or failed Element load keeps a disabled placeholder and diagnostic while the Payment Element remains available.

A signed `payment_intent.succeeded` webhook retrieves the PaymentMethod with stripe-go. `card.wallet.type=google_pay` updates the existing order's `payment_type` to `google_pay` in the same conditional paid-state update. Provider identity and refunds remain attached to the original Stripe instance. No database migration is required.
```

再加入 11 项人工验收和“取消实例中的 `google_pay` 即停止新入口”的回滚说明。

- [ ] **Step 3: 更新中文 Stripe Google Pay 章节**

写入与英文完全对称的内容，必须明确：默认关闭、`card` 依赖、四态 UI、Webhook SDK 识别、现有字段标记、原实例退款、无迁移、Merchant ID/Domain ID 不录入，以及 `http://localhost + live key` 不能验收真实 Google Pay。

- [ ] **Step 4: 运行文档契约扫描确认新边界**

Run:

```bash
rg -n "off by default|默认关闭|payment_type.*google_pay|Merchant ID|Domain ID|HTTPS|http://localhost|disabled placeholder|禁用占位" docs/PAYMENT.md docs/PAYMENT_CN.md
```

Expected: 中英文均命中默认关闭、实际渠道、敏感参数边界、HTTPS 和禁用占位说明；旧的“静默隐藏/不新增后端支付类型”文案无匹配。

- [ ] **Step 5: 运行后端全量 unit 验证**

Run:

```bash
cd backend && go test -tags=unit ./...
```

Expected: PASS。该命令不得要求 Stripe Test/Live key，不得创建真实 PaymentIntent。

- [ ] **Step 6: 运行前端目标测试、类型、lint 和构建**

Run:

```bash
pnpm --dir frontend exec vitest run src/components/payment/__tests__/providerConfig.spec.ts src/components/payment/__tests__/PaymentProviderDialog.spec.ts src/components/payment/__tests__/paymentFlow.spec.ts src/components/payment/__tests__/StripeGooglePayExpress.spec.ts src/components/payment/__tests__/StripePaymentInline.spec.ts src/views/user/__tests__/StripePaymentView.spec.ts src/views/user/__tests__/PaymentView.spec.ts src/components/admin/payment/__tests__/orderCurrencyDisplay.spec.ts
pnpm --dir frontend run typecheck
pnpm --dir frontend run lint:check
pnpm --dir frontend run build
```

Expected: 目标测试、typecheck、lint、build 全部 exit 0。

- [ ] **Step 7: 比较前端全量测试基线**

Run:

```bash
pnpm --dir frontend run test:run
```

Expected: 最佳结果为全部 PASS；若仍存在仓库既有失败，保存输出并确认不超过已知基线 34 个失败/14 个文件，且本计划涉及的所有测试文件均 PASS。若失败数增加，停止并修复后重跑。

- [ ] **Step 8: 检查无 schema/migration 和敏感参数泄漏**

Run:

```bash
git diff --name-only 76477927c...HEAD
rg -n "merchant.?id|domain.?id" backend frontend --glob '!**/node_modules/**'
git diff -- backend/ent/schema backend/migrations
```

Expected: 变更列表不含 `backend/ent/schema/*` 或 `backend/migrations/*`；运行时代码没有新增 Google Pay Merchant ID/Domain ID 字段；schema/migration diff 为空。

- [ ] **Step 9: 在注册 HTTPS 环境执行人工验收**

按顺序记录结果：Stripe Dashboard 启用 Google Pay；目标顶级/子域注册到同一 Stripe 账户；可信 TLS；Chrome 登录 Google 且 Wallet 有可用卡；管理员关闭时无快捷区域；启用但环境不支持时显示禁用诊断；支持时显示 Stripe 真实按钮；取消后仍可使用 Payment Element；成功后结果页等待 Webhook；Stripe 显示 `card.wallet.type=google_pay`；Sub2API 显示 Google Pay 且只发放一次；从原 Stripe 实例退款成功。

- [ ] **Step 10: 提交文档和最终验证结果**

```bash
git add -f docs/PAYMENT.md docs/PAYMENT_CN.md
git commit -m "docs(payments): document configurable Google Pay"
```

---

## Final Specification Coverage Check

- 管理端可选、默认关闭、card 依赖：Task 1、Task 2。
- PaymentIntent `google_pay -> card` 且去重：Task 2。
- Checkout 回退与实际订单实例权威能力：Task 3。
- `/purchase` 与 `/payment/stripe` 四态真实组件、共享资源和锁：Task 4。
- 确认后结果页等待 Webhook，不提前入账：Task 4。
- 验签后使用真实 Stripe SDK 获取 PaymentMethod：Task 5。
- 同一条件更新标记 `payment_type=google_pay`、幂等发放：Task 6。
- Provider 身份、查询与退款保持原 Stripe 实例：Task 2、Task 6。
- 管理订单和统计显示 Google Pay：Task 1、Task 6。
- Merchant ID/Domain ID 不进入运行时、无数据库迁移：Global Constraints、Task 7。
- 注册 HTTPS 域名人工验收与回滚：Task 7。

# Stripe Google Pay Express Checkout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在两个 Stripe 支付界面的 Payment Element 上方增加仅显示 Google Pay 的 Express Checkout Element，并复用现有 PaymentIntent、订单状态与 Webhook 流程。

**Architecture:** 前端升级到支持 `availablepaymentmethodschange` 的 `@stripe/stripe-js` 9.8.x，在一个可复用的 `StripeGooglePayExpress` 组件中封装 Express Checkout 的挂载、可用性、确认和清理。`StripePaymentInline.vue` 与 `StripePaymentView.vue` 继续各自创建唯一的 Stripe/Elements 实例，再把同一个实例传给快捷支付组件和现有 Payment Element；后端不新增支付类型或参数。

**Tech Stack:** Vue 3 Composition API、TypeScript 5.6、`@stripe/stripe-js` 9.8.x、Vitest 2、Vue Test Utils、pnpm、Stripe PaymentIntent/Webhook。

## Global Constraints

- 快捷支付区域只允许 Google Pay：`googlePay: 'auto'`，`applePay`、`link`、`amazonPay`、`paypal`、`klarna` 均为 `never`。
- Google Pay Merchant ID 与 Stripe Payment Method Domain ID 不进入代码、配置、接口响应或日志。
- 不新增 `google_pay` 后端支付类型；PaymentIntent 继续通过 `card` 支持 Google Pay。
- 每个支付界面只能创建一个 Stripe 实例、一个 Elements 实例和一个 PaymentIntent。
- Google Pay 不可用或组件加载失败时静默隐藏快捷区域，并保留现有 Payment Element。
- 前端确认结果不触发入账；`payment_intent.succeeded` Webhook 继续是充值和订阅发放的唯一权威来源。
- Google Pay 与普通 Payment Element 共用提交锁，不能并发确认同一个 PaymentIntent。
- 保留工作区中与本功能无关的现有改动；每次只暂存本任务列出的文件。
- 不升级 Stripe 以外的依赖。

---

## File Map

- Create `frontend/src/components/payment/StripeGooglePayExpress.vue`: 封装 Google Pay Express Checkout Element 的完整生命周期。
- Create `frontend/src/components/payment/__tests__/StripeGooglePayExpress.spec.ts`: 验证仅 Google Pay、动态可用性、确认、错误、取消和销毁。
- Modify `frontend/package.json`: 将 `@stripe/stripe-js` 下限升级到 `^9.8.0`。
- Modify `frontend/pnpm-lock.yaml`: 锁定与 package.json 一致的 Stripe.js 版本。
- Create `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts`: 验证内嵌支付面板共享 Elements 和提交锁。
- Modify `frontend/src/components/payment/StripePaymentInline.vue`: 在 Payment Element 上方集成复用组件。
- Modify `frontend/src/views/user/__tests__/StripePaymentView.spec.ts`: 验证独立支付页共享 Elements、确认和非通用支付分支。
- Modify `frontend/src/views/user/StripePaymentView.vue`: 在通用 Stripe 支付页集成复用组件。
- Modify `docs/PAYMENT_CN.md`: 补充中文 Google Pay Express Checkout 配置与参数边界。
- Modify `docs/PAYMENT.md`: 补充英文 Google Pay Express Checkout 配置与参数边界。

---

### Task 1: Build the reusable Google Pay Express Checkout component

**Files:**
- Create: `frontend/src/components/payment/StripeGooglePayExpress.vue`
- Create: `frontend/src/components/payment/__tests__/StripeGooglePayExpress.spec.ts`
- Modify: `frontend/package.json`
- Modify: `frontend/pnpm-lock.yaml`

**Interfaces:**
- Consumes: `Stripe`, `StripeElements`, `StripeExpressCheckoutElementConfirmEvent` and `StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent` from `@stripe/stripe-js`.
- Props: `stripe: Stripe`, `elements: StripeElements`, `returnUrl: string`, `disabled?: boolean`.
- Emits: `availabilityChange(available: boolean)`, `submittingChange(submitting: boolean)`, `confirmed()`.
- Produces: a component that owns exactly one Express Checkout Element and never creates a Stripe, Elements, PaymentIntent or local order.

- [ ] **Step 1: Write the failing component test**

Create `frontend/src/components/payment/__tests__/StripeGooglePayExpress.spec.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import type { Stripe, StripeElements } from '@stripe/stripe-js'
import StripeGooglePayExpress from '../StripeGooglePayExpress.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

type Handler = (event: any) => unknown

const handlers = new Map<string, Handler>()
const expressElement = {
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn((event: string, handler: Handler) => {
    handlers.set(event, handler)
    return expressElement
  }),
  off: vi.fn((event: string) => {
    handlers.delete(event)
    return expressElement
  }),
}
const elements = {
  create: vi.fn(() => expressElement),
} as unknown as StripeElements
const confirmPayment = vi.fn()
const stripe = { confirmPayment } as unknown as Stripe

function mountComponent(disabled = false) {
  return mount(StripeGooglePayExpress, {
    props: {
      stripe,
      elements,
      returnUrl: 'https://example.com/payment/result?order_id=42',
      disabled,
    },
  })
}

describe('StripeGooglePayExpress', () => {
  beforeEach(() => {
    handlers.clear()
    vi.clearAllMocks()
    confirmPayment.mockResolvedValue({})
  })

  it('creates an Express Checkout Element that only allows Google Pay', () => {
    mountComponent()

    expect(elements.create).toHaveBeenCalledWith('expressCheckout', {
      paymentMethods: {
        googlePay: 'auto',
        applePay: 'never',
        link: 'never',
        amazonPay: 'never',
        paypal: 'never',
        klarna: 'never',
      },
    })
    expect(expressElement.mount).toHaveBeenCalledOnce()
  })

  it('shows only when Google Pay becomes available', async () => {
    const wrapper = mountComponent()

    expect(wrapper.get('[data-testid="stripe-google-pay-express"]').isVisible()).toBe(false)
    handlers.get('availablepaymentmethodschange')?.({
      elementType: 'expressCheckout',
      paymentMethods: { googlePay: { available: true } },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-express"]').isVisible()).toBe(true)
    expect(wrapper.emitted('availabilityChange')).toEqual([[true]])
  })

  it('confirms the existing PaymentIntent and shares submitting state', async () => {
    const wrapper = mountComponent()
    const paymentFailed = vi.fn()

    await handlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })
    await flushPromises()

    expect(confirmPayment).toHaveBeenCalledWith({
      elements,
      confirmParams: {
        return_url: 'https://example.com/payment/result?order_id=42',
      },
      redirect: 'if_required',
    })
    expect(paymentFailed).not.toHaveBeenCalled()
    expect(wrapper.emitted('submittingChange')).toEqual([[true], [false]])
    expect(wrapper.emitted('confirmed')).toEqual([[]])
  })

  it('reports an immediate Stripe error without losing the fallback form', async () => {
    confirmPayment.mockResolvedValue({ error: { message: 'wallet declined' } })
    const wrapper = mountComponent()
    const paymentFailed = vi.fn()

    await handlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed,
    })
    await flushPromises()

    expect(wrapper.text()).toContain('wallet declined')
    expect(paymentFailed).toHaveBeenCalledWith({
      reason: 'fail',
      message: 'wallet declined',
    })
    expect(wrapper.emitted('confirmed')).toBeUndefined()
    expect(wrapper.emitted('submittingChange')).toEqual([[true], [false]])
  })

  it('clears submitting state on cancel and destroys the Stripe Element', () => {
    const wrapper = mountComponent()

    handlers.get('cancel')?.({ elementType: 'expressCheckout' })
    expect(wrapper.emitted('submittingChange')).toEqual([[false]])

    wrapper.unmount()
    expect(expressElement.destroy).toHaveBeenCalledOnce()
  })

  it('hides a failed Element and records only sanitized diagnostics', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined)
    const wrapper = mountComponent()

    handlers.get('availablepaymentmethodschange')?.({
      elementType: 'expressCheckout',
      paymentMethods: { googlePay: { available: true } },
    })
    handlers.get('loaderror')?.({
      elementType: 'expressCheckout',
      error: { type: 'invalid_request_error', code: 'payment_method_domain_invalid' },
    })
    await nextTick()

    expect(wrapper.get('[data-testid="stripe-google-pay-express"]').isVisible()).toBe(false)
    expect(warn).toHaveBeenCalledWith('[StripeGooglePayExpress] load failed', {
      type: 'invalid_request_error',
      code: 'payment_method_domain_invalid',
    })
    warn.mockRestore()
  })
})
```

- [ ] **Step 2: Run the new test and verify the missing component failure**

Run:

```bash
cd frontend
pnpm test:run -- src/components/payment/__tests__/StripeGooglePayExpress.spec.ts
```

Expected: FAIL because `StripeGooglePayExpress.vue` does not exist.

- [ ] **Step 3: Upgrade Stripe.js to the event-capable version**

Run:

```bash
cd frontend
pnpm add @stripe/stripe-js@^9.8.0
```

Expected: `package.json` contains `"@stripe/stripe-js": "^9.8.0"`; the lockfile resolves 9.8.x. Do not update unrelated dependencies.

- [ ] **Step 4: Implement the reusable component**

Create `frontend/src/components/payment/StripeGooglePayExpress.vue`:

```vue
<template>
  <div
    v-show="available"
    data-testid="stripe-google-pay-express"
    class="space-y-3"
    :aria-busy="confirming"
  >
    <div
      ref="mountTarget"
      :class="{ 'pointer-events-none opacity-60': disabled || confirming }"
    />
    <p
      v-if="errorMessage"
      data-testid="stripe-google-pay-error"
      class="text-sm text-red-600 dark:text-red-400"
    >
      {{ errorMessage }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { extractI18nErrorMessage } from '@/utils/apiError'
import type {
  Stripe,
  StripeElements,
  StripeExpressCheckoutElement,
  StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent,
  StripeExpressCheckoutElementConfirmEvent,
  StripeExpressCheckoutElementOptions,
  StripeExpressCheckoutElementReadyEvent,
} from '@stripe/stripe-js'

const props = withDefaults(defineProps<{
  stripe: Stripe
  elements: StripeElements
  returnUrl: string
  disabled?: boolean
}>(), {
  disabled: false,
})

const emit = defineEmits<{
  availabilityChange: [available: boolean]
  submittingChange: [submitting: boolean]
  confirmed: []
}>()

const { t } = useI18n()
const mountTarget = ref<HTMLElement | null>(null)
const available = ref(false)
const confirming = ref(false)
const errorMessage = ref('')
let expressCheckoutElement: StripeExpressCheckoutElement | null = null

const options = {
  paymentMethods: {
    googlePay: 'auto',
    applePay: 'never',
    link: 'never',
    amazonPay: 'never',
    paypal: 'never',
    klarna: 'never',
  },
} satisfies StripeExpressCheckoutElementOptions

function setAvailable(next: boolean) {
  if (available.value === next) return
  available.value = next
  emit('availabilityChange', next)
}

function setSubmitting(next: boolean) {
  confirming.value = next
  emit('submittingChange', next)
}

function handleReady(event: StripeExpressCheckoutElementReadyEvent) {
  setAvailable(Boolean(event.availablePaymentMethods?.googlePay))
}

function handleAvailabilityChange(
  event: StripeExpressCheckoutElementAvailablePaymentMethodsChangeEvent,
) {
  setAvailable(Boolean(event.paymentMethods?.googlePay?.available))
}

function handleCancel() {
  setSubmitting(false)
}

function handleLoadError(event: {
  error: { type: string; code?: string }
}) {
  console.warn('[StripeGooglePayExpress] load failed', {
    type: event.error.type,
    code: event.error.code,
  })
  setAvailable(false)
  setSubmitting(false)
}

async function handleConfirm(event: StripeExpressCheckoutElementConfirmEvent) {
  if (props.disabled || confirming.value) {
    event.paymentFailed({ reason: 'fail', message: t('common.processing') })
    return
  }

  setSubmitting(true)
  errorMessage.value = ''
  try {
    const { error } = await props.stripe.confirmPayment({
      elements: props.elements,
      confirmParams: { return_url: props.returnUrl },
      redirect: 'if_required',
    })
    if (error) {
      errorMessage.value = error.message || t('payment.result.failed')
      event.paymentFailed({ reason: 'fail', message: errorMessage.value })
      return
    }
    emit('confirmed')
  } catch (error: unknown) {
    errorMessage.value = extractI18nErrorMessage(
      error,
      t,
      'payment.errors',
      t('payment.result.failed'),
    )
    event.paymentFailed({ reason: 'fail', message: errorMessage.value })
  } finally {
    setSubmitting(false)
  }
}

onMounted(() => {
  if (!mountTarget.value) return
  expressCheckoutElement = props.elements.create('expressCheckout', options)
  expressCheckoutElement.on('ready', handleReady)
  expressCheckoutElement.on('availablepaymentmethodschange', handleAvailabilityChange)
  expressCheckoutElement.on('confirm', handleConfirm)
  expressCheckoutElement.on('cancel', handleCancel)
  expressCheckoutElement.on('loaderror', handleLoadError)
  expressCheckoutElement.mount(mountTarget.value)
})

onBeforeUnmount(() => {
  if (!expressCheckoutElement) return
  expressCheckoutElement.off('ready', handleReady)
  expressCheckoutElement.off('availablepaymentmethodschange', handleAvailabilityChange)
  expressCheckoutElement.off('confirm', handleConfirm)
  expressCheckoutElement.off('cancel', handleCancel)
  expressCheckoutElement.off('loaderror', handleLoadError)
  expressCheckoutElement.destroy()
  expressCheckoutElement = null
})
</script>
```

- [ ] **Step 5: Run component tests and type checking**

Run:

```bash
cd frontend
pnpm test:run -- src/components/payment/__tests__/StripeGooglePayExpress.spec.ts
pnpm typecheck
```

Expected: the new test file passes; typecheck recognizes `availablepaymentmethodschange` and exits 0.

- [ ] **Step 6: Commit the reusable component**

```bash
git add frontend/package.json frontend/pnpm-lock.yaml \
  frontend/src/components/payment/StripeGooglePayExpress.vue \
  frontend/src/components/payment/__tests__/StripeGooglePayExpress.spec.ts
git commit -m "feat(payments): add Google Pay express checkout component"
```

---

### Task 2: Integrate Google Pay into the inline Stripe payment panel

**Files:**
- Create: `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts`
- Modify: `frontend/src/components/payment/StripePaymentInline.vue`

**Interfaces:**
- Consumes: `StripeGooglePayExpress` props and events from Task 1.
- Produces: the inline panel passes the same `Stripe` and `StripeElements` instances to both Elements, shares `submitting`, and handles `confirmed` exactly like the existing card success path.

- [ ] **Step 1: Write failing inline integration tests**

Create `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts` with mocks matching the existing component dependencies. The core assertions must be:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import StripePaymentInline from '../StripePaymentInline.vue'

const loadStripe = vi.hoisted(() => vi.fn())
const paymentElement = vi.hoisted(() => ({
  mount: vi.fn(),
  on: vi.fn((event: string, callback: (event?: any) => void) => {
    if (event === 'ready') callback()
    if (event === 'change') callback({ value: { type: 'card' } })
  }),
}))
const expressHandlers = vi.hoisted(() => new Map<string, (event: any) => unknown>())
const expressElement = vi.hoisted(() => ({
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn((event: string, handler: (event: any) => unknown) => {
    expressHandlers.set(event, handler)
    return expressElement
  }),
  off: vi.fn((event: string) => {
    expressHandlers.delete(event)
    return expressElement
  }),
}))
const elements = vi.hoisted(() => ({
  create: vi.fn((type: string) => (
    type === 'expressCheckout' ? expressElement : paymentElement
  )),
}))
const stripe = vi.hoisted(() => ({
  elements: vi.fn(() => elements),
  confirmPayment: vi.fn(),
}))

vi.mock('@stripe/stripe-js', () => ({ loadStripe }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ resolve: vi.fn(), push: vi.fn() }),
}))
vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError: vi.fn() }),
}))
vi.mock('@/api/payment', () => ({
  paymentAPI: { cancelOrder: vi.fn() },
}))

function mountInline() {
  return mount(StripePaymentInline, {
    props: {
      orderId: 42,
      amount: 100,
      clientSecret: 'pi_secret_42',
      publishableKey: 'pk_test',
      payAmount: 103,
      currency: 'USD',
    },
    global: { stubs: { Icon: true } },
  })
}

describe('StripePaymentInline Google Pay integration', () => {
  beforeEach(() => {
    expressHandlers.clear()
    vi.clearAllMocks()
    loadStripe.mockResolvedValue(stripe)
    stripe.confirmPayment.mockResolvedValue({})
  })

  it('creates Payment and Express Checkout Elements from one Elements instance', async () => {
    const wrapper = mountInline()
    await flushPromises()
    await nextTick()

    expect(stripe.elements).toHaveBeenCalledTimes(1)
    expect(elements.create).toHaveBeenCalledWith('payment', expect.any(Object))
    expect(elements.create).toHaveBeenCalledWith('expressCheckout', expect.objectContaining({
      paymentMethods: expect.objectContaining({ googlePay: 'auto' }),
    }))
    expect(paymentElement.mount).toHaveBeenCalledOnce()
    expect(expressElement.mount).toHaveBeenCalledOnce()
    wrapper.unmount()
  })

  it('shares the submitting lock and handles a real child confirmation', async () => {
    let resolveConfirmation!: (value: object) => void
    stripe.confirmPayment.mockReturnValue(new Promise(resolve => {
      resolveConfirmation = resolve
    }))
    const wrapper = mountInline()
    await flushPromises()

    const confirmPromise = expressHandlers.get('confirm')?.({
      expressPaymentType: 'google_pay',
      paymentFailed: vi.fn(),
    })
    await nextTick()
    expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeDefined()

    resolveConfirmation({})
    await confirmPromise
    await flushPromises()
    expect(wrapper.emitted('success')).toEqual([[]])
    expect(wrapper.text()).toContain('payment.result.success')
    wrapper.unmount()
  })
})
```

- [ ] **Step 2: Run the inline tests and verify the missing child failure**

Run:

```bash
cd frontend
pnpm test:run -- src/components/payment/__tests__/StripePaymentInline.spec.ts
```

Expected: FAIL because `StripePaymentInline.vue` does not render `StripeGooglePayExpress`.

- [ ] **Step 3: Integrate the child component and shared state**

In `StripePaymentInline.vue`:

1. Import `shallowRef` and `StripeGooglePayExpress`.
2. Replace the non-reactive SDK variables with:

```ts
const stripeInstance = shallowRef<Stripe | null>(null)
const elementsInstance = shallowRef<StripeElements | null>(null)
const googlePayAvailable = ref(false)
const returnUrl = computed(() => (
  window.location.origin + '/payment/result?order_id=' + props.orderId + '&status=success'
))
```

3. Assign `stripeInstance.value = stripe` and `elementsInstance.value = elements` during initialization; update `handlePay` to read `.value`.
4. Add the following before the existing Payment Element mount target inside the card:

```vue
<StripeGooglePayExpress
  v-if="stripeInstance && elementsInstance"
  :stripe="stripeInstance"
  :elements="elementsInstance"
  :return-url="returnUrl"
  :disabled="submitting"
  @availability-change="googlePayAvailable = $event"
  @submitting-change="submitting = $event"
  @confirmed="handleGooglePayConfirmed"
/>
<div
  v-if="googlePayAvailable"
  class="my-5 border-t border-gray-200 dark:border-dark-600"
  aria-hidden="true"
/>
```

5. Use `returnUrl.value` in the existing card/Link `confirmPayment` call and add:

```ts
function handleGooglePayConfirmed() {
  success.value = true
  emit('success')
}
```

- [ ] **Step 4: Run inline and reusable component tests**

Run:

```bash
cd frontend
pnpm test:run -- \
  src/components/payment/__tests__/StripeGooglePayExpress.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts
pnpm typecheck
```

Expected: both test files pass and typecheck exits 0.

- [ ] **Step 5: Commit the inline integration**

```bash
git add frontend/src/components/payment/StripePaymentInline.vue \
  frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts
git commit -m "feat(payments): show Google Pay in inline Stripe checkout"
```

---

### Task 3: Integrate Google Pay into the standalone Stripe payment page

**Files:**
- Modify: `frontend/src/views/user/StripePaymentView.vue`
- Modify: `frontend/src/views/user/__tests__/StripePaymentView.spec.ts`

**Interfaces:**
- Consumes: `StripeGooglePayExpress` from Task 1.
- Produces: the generic `/payment/stripe` page renders Google Pay above the Payment Element, while `method=alipay` and `method=wechat_pay` keep their existing direct flows.

- [ ] **Step 1: Extend the existing view test with failing Google Pay assertions**

In `StripePaymentView.spec.ts`, replace `shallowMount` with `mount`, add an Express Checkout test double alongside the existing Payment Element double, and route `stripeElements.create(type)` to the matching double. The Express double must store real child event handlers in `expressHandlers`, using the same shape as Task 2. Then add:

```ts
it('creates Payment and Express Checkout Elements from one Elements instance', async () => {
  getOrder.mockResolvedValue({ data: orderFactory({ currency: 'USD' }) })

  const wrapper = mountView()
  await flushPromises()
  await flushPromises()

  expect(stripeInstance.elements).toHaveBeenCalledTimes(1)
  expect(stripeElements.create).toHaveBeenCalledWith('payment', expect.any(Object))
  expect(stripeElements.create).toHaveBeenCalledWith(
    'expressCheckout',
    expect.objectContaining({
      paymentMethods: expect.objectContaining({ googlePay: 'auto' }),
    }),
  )
  wrapper.unmount()
})

it('shares submitting state and completes the existing success flow through the real child', async () => {
  let resolveConfirmation!: (value: object) => void
  stripeInstance.confirmPayment.mockReturnValue(new Promise(resolve => {
    resolveConfirmation = resolve
  }))
  getOrder.mockResolvedValue({ data: orderFactory() })
  const wrapper = mountView()
  await flushPromises()
  await flushPromises()

  const confirmPromise = expressHandlers.get('confirm')?.({
    expressPaymentType: 'google_pay',
    paymentFailed: vi.fn(),
  })
  await nextTick()
  expect(wrapper.get('button.btn-stripe').attributes('disabled')).toBeDefined()

  resolveConfirmation({})
  await confirmPromise
  await flushPromises()
  expect(wrapper.text()).toContain('payment.stripeSuccessProcessing')
  wrapper.unmount()
})

it('does not render Google Pay in the direct Alipay flow', async () => {
  routeState.query = {
    order_id: '42',
    client_secret: 'pi_secret_42',
    method: 'alipay',
  }
  getOrder.mockResolvedValue({ data: orderFactory() })
  stripeInstance.confirmAlipayPayment.mockResolvedValue({})

  const wrapper = mountView()
  await flushPromises()

  expect(stripeElements.create).not.toHaveBeenCalledWith(
    'expressCheckout',
    expect.anything(),
  )
  expect(wrapper.find('[data-testid="stripe-google-pay-express"]').exists()).toBe(false)
  wrapper.unmount()
})
```

Also add `nextTick` to the Vue import used by the test. Clear `expressHandlers` and reset the Express double in `beforeEach`, so each test mounts the real `StripeGooglePayExpress` component against a fresh external Stripe boundary.

- [ ] **Step 2: Run the view test and verify the generic page failure**

Run:

```bash
cd frontend
pnpm test:run -- src/views/user/__tests__/StripePaymentView.spec.ts
```

Expected: the new generic checkout tests FAIL because the child component is absent; the existing currency test still passes.

- [ ] **Step 3: Integrate Google Pay into the generic Payment Element branch**

In `StripePaymentView.vue`:

1. Import `shallowRef` and `StripeGooglePayExpress`.
2. Replace SDK variables with:

```ts
const stripeInstance = shallowRef<Stripe | null>(null)
const elementsInstance = shallowRef<StripeElements | null>(null)
const googlePayAvailable = ref(false)
const returnUrl = computed(() => (
  window.location.origin
  + '/payment/result?order_id='
  + String(route.query.order_id || '')
  + '&status=success'
))
```

3. Assign `.value` during initialization and use `.value` in `handleGenericPay`.
4. Inside the `showPaymentElement` card, insert before `#stripe-payment-element`:

```vue
<StripeGooglePayExpress
  v-if="stripeInstance && elementsInstance"
  :stripe="stripeInstance"
  :elements="elementsInstance"
  :return-url="returnUrl"
  :disabled="stripeSubmitting"
  @availability-change="googlePayAvailable = $event"
  @submitting-change="stripeSubmitting = $event"
  @confirmed="handleGooglePayConfirmed"
/>
<div
  v-if="googlePayAvailable"
  class="my-5 border-t border-gray-200 dark:border-dark-600"
  aria-hidden="true"
/>
```

5. Reuse `returnUrl.value` in `handleGenericPay` and add:

```ts
function handleGooglePayConfirmed() {
  stripeSuccess.value = true
  scheduleClose()
}
```

The Alipay and WeChat branches remain unchanged and never set `showPaymentElement`, so the Google Pay child is not created for those routes.

- [ ] **Step 4: Run both page integration suites and type checking**

Run:

```bash
cd frontend
pnpm test:run -- \
  src/components/payment/__tests__/StripePaymentInline.spec.ts \
  src/views/user/__tests__/StripePaymentView.spec.ts
pnpm typecheck
```

Expected: all selected tests pass and typecheck exits 0.

- [ ] **Step 5: Commit the standalone page integration**

```bash
git add frontend/src/views/user/StripePaymentView.vue \
  frontend/src/views/user/__tests__/StripePaymentView.spec.ts
git commit -m "feat(payments): add Google Pay to Stripe payment page"
```

---

### Task 4: Document configuration, verify the full feature, and prepare rollout

**Files:**
- Modify: `docs/PAYMENT_CN.md`
- Modify: `docs/PAYMENT.md`

**Interfaces:**
- Consumes: the final frontend behavior from Tasks 1-3.
- Produces: operator instructions that distinguish required Stripe settings from unused Google/Stripe identifiers.

- [ ] **Step 1: Add the Chinese operator documentation**

After the Stripe parameter table in `docs/PAYMENT_CN.md`, add:

```markdown
#### Google Pay Express Checkout

Google Pay 通过 Stripe Express Checkout Element 展示在 Stripe 支付面板顶部，并作为 `card` 钱包处理。Sub2API 不需要新增 `google_pay` 支付类型，也不需要保存 Google Pay Merchant ID 或 Stripe Payment Method Domain ID。

启用前请确认：

1. Stripe Dashboard 的 Payment Methods 中已启用 Google Pay。
2. Stripe 服务商实例的支持类型包含 `card`。
3. 所有展示支付面板的生产和测试域名（包括 `www` 和其他子域）都已在 Stripe Payment Method Domains 中注册。
4. 支付页面使用有效的公网 HTTPS 证书。
5. 测试和生产环境分别使用匹配的 Publishable Key、Secret Key、Webhook Secret 和域名注册状态。

Google Pay 不可用时，快捷按钮会自动隐藏，银行卡、支付宝、微信支付和 Link 等现有 Stripe 支付方式仍可使用。支付成功后的余额或订阅发放仍以 Stripe `payment_intent.succeeded` Webhook 为准。
```

- [ ] **Step 2: Add the equivalent English documentation**

After the Stripe parameter table in `docs/PAYMENT.md`, add:

```markdown
#### Google Pay Express Checkout

Google Pay is displayed above the Stripe Payment Element through Stripe's Express Checkout Element and is processed as a `card` wallet. Sub2API does not add a `google_pay` backend payment type and does not store the Google Pay Merchant ID or Stripe Payment Method Domain ID.

Before enabling it, confirm that:

1. Google Pay is enabled under Payment Methods in the Stripe Dashboard.
2. The Stripe provider instance includes `card` in its supported types.
3. Every production and test hostname that displays checkout, including `www` and other subdomains, is registered under Stripe Payment Method Domains.
4. Checkout is served from a publicly trusted HTTPS origin.
5. Test and live environments use matching publishable keys, secret keys, webhook secrets, and domain registrations.

When Google Pay is unavailable, the express button is hidden automatically and the existing Stripe methods remain available. Balance or subscription fulfillment continues to depend on the signed `payment_intent.succeeded` webhook.
```

- [ ] **Step 3: Run focused frontend verification**

Run:

```bash
cd frontend
pnpm test:run -- \
  src/components/payment/__tests__/StripeGooglePayExpress.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts \
  src/views/user/__tests__/StripePaymentView.spec.ts
pnpm typecheck
pnpm exec eslint \
  src/components/payment/StripeGooglePayExpress.vue \
  src/components/payment/StripePaymentInline.vue \
  src/views/user/StripePaymentView.vue \
  src/components/payment/__tests__/StripeGooglePayExpress.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts \
  src/views/user/__tests__/StripePaymentView.spec.ts
pnpm build
```

Expected: all focused tests pass; typecheck, lint, and production build exit 0.

- [ ] **Step 4: Run payment backend regression verification**

Run:

```bash
cd backend
go test ./internal/payment/provider ./internal/handler ./internal/service -run 'Stripe|PaymentWebhook' -count=1
```

Expected: selected Stripe and payment webhook tests pass; no backend file changes are required.

- [ ] **Step 5: Perform manual HTTPS validation**

On a registered staging hostname:

1. Open generic Stripe checkout in a Google Pay-capable Chrome/Android environment.
2. Verify that only Google Pay appears in the express region.
3. Verify that no empty express container or divider appears when the wallet is unavailable.
4. Cancel the wallet and confirm that the local order remains pending and the Payment Element remains usable.
5. Complete a test payment and verify one `payment_intent.succeeded` event and one local fulfillment.
6. Confirm in Stripe that the successful charge reports `card.wallet.type = google_pay`.
7. Re-test card, Link, Alipay, WeChat Pay, result-page recovery, and refund flows.

Expected: every acceptance criterion in `docs/superpowers/specs/2026-07-10-stripe-google-pay-express-checkout-design.md` is satisfied.

- [ ] **Step 6: Commit documentation and verification-ready state**

```bash
git add docs/PAYMENT_CN.md docs/PAYMENT.md
git commit -m "docs(payments): document Google Pay express checkout"
```

---

## Final Review Checklist

- [ ] `git diff --check` reports no whitespace errors.
- [ ] `git status --short` contains no unintended files from this feature.
- [ ] The changed Google Pay frontend files contain no `merchantId`, `domainId`, Google Pay Merchant ID value, or Stripe `pmd_...` Domain ID value.
- [ ] The Express Checkout options keep every non-Google wallet at `never`.
- [ ] Both parents pass the exact same Elements instance to Google Pay and Payment Element.
- [ ] All automated verification commands in Task 4 passed with fresh output.
- [ ] Manual HTTPS verification is recorded before enabling production traffic.

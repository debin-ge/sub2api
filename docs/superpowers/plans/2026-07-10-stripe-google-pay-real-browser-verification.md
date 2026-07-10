# Stripe Google Pay Real Browser Verification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成独立 Stripe 支付页的 Google Pay Express Checkout 集成，并用真实 Stripe Test Mode PaymentIntent、真实 Stripe.js 和真实浏览器证明 Task 2/3 的外部集成有效。

**Architecture:** 保留 Vitest 行为测试对共享锁、成功状态及支付宝/微信分支的确定性覆盖；这些测试挂载真实 `StripeGooglePayExpress` Vue 子组件，只替换 Stripe SDK 边界。新增独立 Playwright 外部集成层：Node 进程直接创建并清理 Stripe Test Mode PaymentIntent，Vite 测试页面加载两个真实生产支付组件，浏览器不得拦截或替换 Stripe 网络请求。

**Tech Stack:** Vue 3、TypeScript 5.6、`@stripe/stripe-js` 9.8.x、Vitest 2、Vue Test Utils、Playwright、Vite、pnpm、Stripe Test Mode REST API。

## Global Constraints

- 快捷支付区域只允许 Google Pay：`googlePay: 'auto'`，`applePay`、`link`、`amazonPay`、`paypal`、`klarna` 均为 `never`。
- Google Pay Merchant ID 与 Stripe Payment Method Domain ID 不进入代码、配置、接口响应、测试参数或日志。
- 不新增 `google_pay` 后端支付类型；PaymentIntent 继续通过 `card` 支持 Google Pay。
- 每个支付界面只能创建一个 Stripe 实例、一个 Elements 实例和一个 PaymentIntent。
- Google Pay 不可用或组件加载失败时静默隐藏快捷区域，并保留现有 Payment Element。
- 前端确认结果不触发入账；`payment_intent.succeeded` Webhook 继续是充值和订阅发放的唯一权威来源。
- Google Pay 与普通 Payment Element 共用提交锁，不能并发确认同一个 PaymentIntent。
- Task 2/3 的 Vitest 行为测试必须挂载真实 `StripeGooglePayExpress` Vue 子组件；不得用 Vue stub 代替该组件。
- Playwright 外部测试不得 mock、route、代理或本地替换 `@stripe/stripe-js`、`js.stripe.com` 或 `api.stripe.com`。
- 外部测试不自动打开 Google Pay 钱包、不选择卡片、不完成付款，也不要求 `googlePay.available=true`。
- `STRIPE_E2E_SECRET_KEY` 只允许 Node 测试进程读取，禁止进入 Vite 环境、浏览器、URL、trace、截图、视频或日志。
- `STRIPE_E2E_PUBLISHABLE_KEY` 必须与 Secret Key 属于同一个 Stripe Test Mode 账户。
- 显式运行 Stripe E2E 时缺少必需环境变量必须在启动浏览器前失败，不能跳过或退化为模拟测试。
- 测试创建 `amount=100`、`currency=usd`、`payment_method_types=[card]` 的 PaymentIntent，并用 `metadata[test_suite]=sub2api_google_pay_e2e` 标记；测试结束后取消未完成的 PaymentIntent。
- 不升级 Stripe.js 和 Playwright 以外的依赖，不修改后端生产代码。
- 保留工作区中的无关改动；每次只暂存当前任务列出的文件。

## Execution State

- 原计划 Task 1 已完成：`5e7e20ecb`、`2abe8be98`。
- 原计划 Task 2 生产集成已完成：`03e8f6ea1`。
- 补充设计已提交：`888dac255`。
- 本计划从 Task 3 继续；Task 4 是 Task 2/3 的强制真实 Stripe 验收层。

---

## File Map

- Modify `frontend/src/views/user/StripePaymentView.vue`: 在通用 Payment Element 分支集成真实子组件，共享 Stripe/Elements/锁/返回地址。
- Modify `frontend/src/views/user/__tests__/StripePaymentView.spec.ts`: 挂载真实子组件，验证通用分支与支付宝直连隔离。
- Modify `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts`: 补齐普通支付持锁时 Google Pay 不得二次确认的反向锁测试。
- Modify `frontend/package.json`: 增加 Playwright 开发依赖与显式 Stripe E2E 命令。
- Modify `frontend/pnpm-lock.yaml`: 只记录 Playwright 及其传递依赖。
- Modify `.gitignore`: 忽略 Playwright 临时结果目录。
- Create `frontend/e2e/stripe-google-pay/playwright.config.ts`: 真实 Stripe 浏览器测试配置，关闭敏感制品。
- Create `frontend/e2e/stripe-google-pay/stripe-test-api.ts`: Node-only Stripe Test API 创建、校验和清理。
- Create `frontend/e2e/stripe-google-pay/index.html`: 仅供 Vite 测试服务器使用的入口。
- Create `frontend/e2e/stripe-google-pay/fixture.ts`: 用真实 Pinia、Router、i18n 挂载两个生产支付组件。
- Create `frontend/e2e/stripe-google-pay/stripe-google-pay.external.spec.ts`: 观察真实 Stripe.js、Elements Session 和 iframe 挂载。
- Modify `docs/PAYMENT_CN.md`: 中文配置、真实 E2E 环境变量和运行说明。
- Modify `docs/PAYMENT.md`: 英文等价说明。

---

### Task 3: Complete the standalone Stripe page integration and reciprocal lock coverage

**Files:**
- Modify: `frontend/src/views/user/StripePaymentView.vue`
- Modify: `frontend/src/views/user/__tests__/StripePaymentView.spec.ts`
- Modify: `frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts`

**Interfaces:**
- Consumes: `StripeGooglePayExpress` props `stripe`, `elements`, `returnUrl`, `disabled`; events `availabilityChange`, `submittingChange`, `confirmed`.
- Produces: generic `StripePaymentView` shares one `Stripe`, one `StripeElements`, one PaymentIntent and one lock; direct `method=alipay` and `method=wechat_pay` branches never create Express Checkout.

- [ ] **Step 1: Finish the failing real-child behavior tests**

Keep the existing Stripe SDK test doubles, but use Vue Test Utils `mount` so `StripeGooglePayExpress` itself is never stubbed. Ensure the Express test double stores the handlers registered by the real child:

```ts
const expressHandlers = vi.hoisted(() => new Map<string, (event: any) => unknown>())
const stripeExpressElement = vi.hoisted(() => ({
  mount: vi.fn(),
  destroy: vi.fn(),
  on: vi.fn((event: string, handler: (event: any) => unknown) => {
    expressHandlers.set(event, handler)
    return stripeExpressElement
  }),
  off: vi.fn((event: string) => {
    expressHandlers.delete(event)
    return stripeExpressElement
  }),
}))

stripeElements.create.mockImplementation((type: string) => (
  type === 'expressCheckout' ? stripeExpressElement : stripePaymentElement
))
```

Add these assertions to `StripePaymentView.spec.ts`:

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
      paymentMethods: {
        googlePay: 'auto',
        applePay: 'never',
        link: 'never',
        amazonPay: 'never',
        paypal: 'never',
        klarna: 'never',
      },
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

it('does not let Google Pay confirm while the generic form owns the lock', async () => {
  let resolveCardConfirmation!: (value: object) => void
  stripeInstance.confirmPayment.mockReturnValue(new Promise(resolve => {
    resolveCardConfirmation = resolve
  }))
  getOrder.mockResolvedValue({ data: orderFactory() })

  const wrapper = mountView()
  await flushPromises()
  await flushPromises()

  const cardPromise = wrapper.get('button.btn-stripe').trigger('click')
  await nextTick()
  const paymentFailed = vi.fn()
  await expressHandlers.get('confirm')?.({
    expressPaymentType: 'google_pay',
    paymentFailed,
  })

  expect(stripeInstance.confirmPayment).toHaveBeenCalledTimes(1)
  expect(paymentFailed).toHaveBeenCalledWith({
    reason: 'fail',
    message: 'common.processing',
  })

  resolveCardConfirmation({})
  await cardPromise
  await flushPromises()
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

  expect(stripeElements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
  expect(wrapper.find('[data-testid="stripe-google-pay-express"]').exists()).toBe(false)
  wrapper.unmount()
})

it('does not render Google Pay in the direct WeChat Pay flow', async () => {
  routeState.query = {
    order_id: '42',
    client_secret: 'pi_secret_42',
    method: 'wechat_pay',
  }
  getOrder.mockResolvedValue({ data: orderFactory() })
  stripeInstance.confirmWechatPayPayment.mockResolvedValue({
    error: { message: 'stop after direct branch selection' },
  })

  const wrapper = mountView()
  await flushPromises()

  expect(stripeElements.create).not.toHaveBeenCalledWith('expressCheckout', expect.anything())
  expect(wrapper.find('[data-testid="stripe-google-pay-express"]').exists()).toBe(false)
  wrapper.unmount()
})
```

Add the reciprocal lock assertion to `StripePaymentInline.spec.ts`:

```ts
it('does not let Google Pay confirm while the Payment Element owns the lock', async () => {
  let resolveCardConfirmation!: (value: object) => void
  stripe.confirmPayment.mockReturnValue(new Promise(resolve => {
    resolveCardConfirmation = resolve
  }))
  const wrapper = mountInline()
  await flushPromises()

  const cardPromise = wrapper.get('button.btn-stripe').trigger('click')
  await nextTick()
  const paymentFailed = vi.fn()
  await expressHandlers.get('confirm')?.({
    expressPaymentType: 'google_pay',
    paymentFailed,
  })

  expect(stripe.confirmPayment).toHaveBeenCalledTimes(1)
  expect(paymentFailed).toHaveBeenCalledWith({
    reason: 'fail',
    message: 'common.processing',
  })

  resolveCardConfirmation({})
  await cardPromise
  await flushPromises()
  wrapper.unmount()
})
```

- [ ] **Step 2: Run the behavior tests and prove the standalone page is RED**

Run:

```bash
cd frontend
pnpm test:run \
  src/components/payment/__tests__/StripeGooglePayExpress.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts \
  src/views/user/__tests__/StripePaymentView.spec.ts
```

Expected: reusable and inline tests pass; the new standalone generic tests fail because `StripePaymentView.vue` does not create `expressCheckout` or render `StripeGooglePayExpress`.

- [ ] **Step 3: Integrate the child only into the generic standalone branch**

In `StripePaymentView.vue`, import `shallowRef` and the real child:

```ts
import { ref, shallowRef, computed, nextTick, onMounted, onUnmounted } from 'vue'
import StripeGooglePayExpress from '@/components/payment/StripeGooglePayExpress.vue'
```

Replace the non-reactive SDK variables and add shared state:

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

During initialization and `mountPaymentElement`, assign the exact shared objects:

```ts
stripeInstance.value = stripe

function mountPaymentElement(stripe: Stripe, clientSecret: string) {
  const isDark = document.documentElement.classList.contains('dark')
  const elements = stripe.elements({
    clientSecret,
    appearance: { theme: isDark ? 'night' : 'stripe', variables: { borderRadius: '8px' } },
  })
  elementsInstance.value = elements
  const paymentElement = elements.create('payment', {
    layout: 'tabs',
    paymentMethodOrder: ['alipay', 'wechat_pay', 'card', 'link'],
  } as Record<string, unknown>)
  paymentElement.mount('#stripe-payment-element')
  paymentElement.on('ready', () => { stripeReady.value = true })
}
```

Insert this before `#stripe-payment-element`, inside the existing `showPaymentElement` card only:

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
<div id="stripe-payment-element" class="min-h-[200px]"></div>
```

Use the same return URL and `.value` SDK references in the generic confirmation path:

```ts
async function handleGenericPay() {
  if (!stripeInstance.value || !elementsInstance.value || stripeSubmitting.value) return
  stripeSubmitting.value = true
  stripeError.value = ''
  try {
    const { error } = await stripeInstance.value.confirmPayment({
      elements: elementsInstance.value,
      confirmParams: { return_url: returnUrl.value },
      redirect: 'if_required',
    })
    if (error) {
      stripeError.value = error.message || t('payment.result.failed')
    } else {
      stripeSuccess.value = true
      scheduleClose()
    }
  } catch (err: unknown) {
    stripeError.value = extractI18nErrorMessage(
      err,
      t,
      'payment.errors',
      t('payment.result.failed'),
    )
  } finally {
    stripeSubmitting.value = false
  }
}

function handleGooglePayConfirmed() {
  stripeSuccess.value = true
  scheduleClose()
}
```

Do not modify `confirmAlipay` or `confirmWechatPay`; their branches never set `showPaymentElement`.

- [ ] **Step 4: Run focused tests, type checking and lint**

Run:

```bash
cd frontend
pnpm test:run \
  src/components/payment/__tests__/StripeGooglePayExpress.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts \
  src/views/user/__tests__/StripePaymentView.spec.ts
pnpm typecheck
pnpm exec eslint \
  src/views/user/StripePaymentView.vue \
  src/views/user/__tests__/StripePaymentView.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts
```

Expected: all focused tests pass; typecheck and focused ESLint exit 0 with no new warning.

- [ ] **Step 5: Commit Task 3**

```bash
git add frontend/src/views/user/StripePaymentView.vue \
  frontend/src/views/user/__tests__/StripePaymentView.spec.ts \
  frontend/src/components/payment/__tests__/StripePaymentInline.spec.ts
git commit -m "feat(payments): add Google Pay to Stripe payment page"
```

---

### Task 4: Add mandatory real Stripe Playwright verification for both payment surfaces

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/pnpm-lock.yaml`
- Modify: `.gitignore`
- Create: `frontend/e2e/stripe-google-pay/playwright.config.ts`
- Create: `frontend/e2e/stripe-google-pay/stripe-test-api.ts`
- Create: `frontend/e2e/stripe-google-pay/stripe-google-pay.external.spec.ts`
- Create: `frontend/e2e/stripe-google-pay/index.html`
- Create: `frontend/e2e/stripe-google-pay/fixture.ts`

**Interfaces:**
- Consumes: Task 2 `StripePaymentInline`, Task 3 `StripePaymentView`, environment keys `STRIPE_E2E_SECRET_KEY` and `STRIPE_E2E_PUBLISHABLE_KEY`.
- Produces: `pnpm test:e2e:stripe`, which uses one real Test Mode PaymentIntent per test, real Stripe.js and one real Elements Session per mounted page.

- [ ] **Step 1: Add the isolated Playwright dependency and command**

Run:

```bash
cd frontend
pnpm add -D @playwright/test@^1.54.1
pnpm exec playwright install chromium
```

Add this script to `frontend/package.json`:

```json
"test:e2e:stripe": "playwright test --config e2e/stripe-google-pay/playwright.config.ts"
```

Append to the repository `.gitignore`:

```gitignore
frontend/test-results/
frontend/playwright-report/
```

Expected: only Playwright-related dependency entries change; Chromium is installed outside the repository.

- [ ] **Step 2: Add the Node-only Stripe API boundary**

Create `frontend/e2e/stripe-google-pay/stripe-test-api.ts`:

```ts
export type StripeE2ETarget = 'inline' | 'view'

export interface StripeE2EEnvironment {
  secretKey: string
  publishableKey: string
}

export interface StripeTestIntent {
  id: string
  clientSecret: string
}

export function readStripeE2EEnvironment(): StripeE2EEnvironment {
  const secretKey = process.env.STRIPE_E2E_SECRET_KEY?.trim() || ''
  const publishableKey = process.env.STRIPE_E2E_PUBLISHABLE_KEY?.trim() || ''

  if (!secretKey || !publishableKey) {
    throw new Error(
      'Stripe E2E requires STRIPE_E2E_SECRET_KEY and STRIPE_E2E_PUBLISHABLE_KEY',
    )
  }
  if (!secretKey.startsWith('sk_test_') || !publishableKey.startsWith('pk_test_')) {
    throw new Error('Stripe E2E accepts only Stripe Test Mode keys')
  }
  return { secretKey, publishableKey }
}

async function stripePost(
  path: string,
  secretKey: string,
  body?: URLSearchParams,
): Promise<Response> {
  return fetch(`https://api.stripe.com${path}`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${secretKey}`,
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body,
  })
}

export async function createStripeTestIntent(
  target: StripeE2ETarget,
): Promise<StripeTestIntent> {
  const { secretKey } = readStripeE2EEnvironment()
  const form = new URLSearchParams()
  form.set('amount', '100')
  form.set('currency', 'usd')
  form.append('payment_method_types[]', 'card')
  form.set('metadata[test_suite]', 'sub2api_google_pay_e2e')
  form.set('metadata[target]', target)

  const response = await stripePost('/v1/payment_intents', secretKey, form)
  if (!response.ok) {
    throw new Error(`Stripe test PaymentIntent creation failed with HTTP ${response.status}`)
  }
  const payload = await response.json() as { id?: string; client_secret?: string }
  if (!payload.id || !payload.client_secret) {
    throw new Error('Stripe test PaymentIntent response is missing required fields')
  }
  return { id: payload.id, clientSecret: payload.client_secret }
}

export async function cancelStripeTestIntent(intentId: string): Promise<void> {
  const { secretKey } = readStripeE2EEnvironment()
  const response = await stripePost(
    `/v1/payment_intents/${encodeURIComponent(intentId)}/cancel`,
    secretKey,
  )
  if (!response.ok) {
    throw new Error(`Stripe test PaymentIntent cleanup failed with HTTP ${response.status}`)
  }
}
```

The helper must never read or print a Stripe response body on failure and must never export the Secret Key to browser code.

- [ ] **Step 3: Add the failing external acceptance test before the harness exists**

Create `frontend/e2e/stripe-google-pay/playwright.config.ts`:

```ts
import { defineConfig } from '@playwright/test'
import { readStripeE2EEnvironment } from './stripe-test-api'

readStripeE2EEnvironment()

const externalBaseURL = process.env.STRIPE_E2E_BASE_URL?.trim()
const localBaseURL = 'http://127.0.0.1:4173/e2e/stripe-google-pay/'

export default defineConfig({
  testDir: '.',
  testMatch: 'stripe-google-pay.external.spec.ts',
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  outputDir: '../../test-results/stripe-google-pay',
  use: {
    baseURL: externalBaseURL || localBaseURL,
    browserName: 'chromium',
    trace: 'off',
    screenshot: 'off',
    video: 'off',
  },
  webServer: externalBaseURL
    ? undefined
    : {
        command: 'pnpm exec vite --host 127.0.0.1 --port 4173',
        url: localBaseURL,
        reuseExistingServer: !process.env.CI,
        timeout: 120_000,
      },
})
```

Create `frontend/e2e/stripe-google-pay/stripe-google-pay.external.spec.ts`:

```ts
import { expect, test, type Page } from '@playwright/test'
import {
  cancelStripeTestIntent,
  createStripeTestIntent,
  readStripeE2EEnvironment,
  type StripeE2ETarget,
  type StripeTestIntent,
} from './stripe-test-api'

interface ObservedStripeTraffic {
  stripeJs: number
  elementsSessions: number
}

function observeStripeTraffic(page: Page): ObservedStripeTraffic {
  const observed = { stripeJs: 0, elementsSessions: 0 }
  page.on('response', response => {
    const url = new URL(response.url())
    if (response.ok() && url.hostname === 'js.stripe.com') {
      observed.stripeJs += 1
    }
    if (
      response.ok()
      && url.hostname === 'api.stripe.com'
      && url.pathname === '/v1/elements/sessions'
    ) {
      observed.elementsSessions += 1
    }
  })
  return observed
}

async function fulfillLocalOrder(page: Page): Promise<void> {
  await page.route(/\/api\/v1\/payment\/orders\/42(?:\?.*)?$/, route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({
      code: 0,
      data: {
        id: 42,
        user_id: 7,
        amount: 1,
        pay_amount: 1,
        currency: 'USD',
        fee_rate: 0,
        payment_type: 'stripe',
        out_trade_no: 'sub2_stripe_e2e_42',
        status: 'PENDING',
        order_type: 'balance',
        created_at: '2026-07-10T00:00:00Z',
        expires_at: '2026-07-10T00:30:00Z',
        refund_amount: 0,
      },
    }),
  }))
}

async function mountRealStripePage(
  page: Page,
  target: StripeE2ETarget,
  intent: StripeTestIntent,
): Promise<ObservedStripeTraffic> {
  const { publishableKey } = readStripeE2EEnvironment()
  const observed = observeStripeTraffic(page)
  await fulfillLocalOrder(page)
  await page.addInitScript(config => {
    ;(window as Window & { __STRIPE_E2E__?: typeof config }).__STRIPE_E2E__ = config
  }, {
    target,
    publishableKey,
    clientSecret: intent.clientSecret,
  })
  await page.goto('./', { waitUntil: 'domcontentloaded' })
  return observed
}

async function expectRealElementsMounted(
  page: Page,
  target: StripeE2ETarget,
  observed: ObservedStripeTraffic,
): Promise<void> {
  const harness = page.getByTestId(`stripe-${target}-harness`)
  const expressFrames = harness
    .getByTestId('stripe-google-pay-express')
    .locator('iframe')

  await expect.poll(() => observed.stripeJs, { timeout: 30_000 }).toBeGreaterThan(0)
  await expect.poll(() => observed.elementsSessions, { timeout: 30_000 }).toBe(1)
  await expect.poll(() => expressFrames.count(), { timeout: 30_000 }).toBeGreaterThan(0)

  if (target === 'view') {
    await expect.poll(
      () => harness.locator('#stripe-payment-element iframe').count(),
      { timeout: 30_000 },
    ).toBeGreaterThan(0)
  } else {
    await expect.poll(async () => {
      const allFrames = await harness.locator('iframe').count()
      const expressCount = await expressFrames.count()
      return allFrames - expressCount
    }, { timeout: 30_000 }).toBeGreaterThan(0)
  }

  await page.waitForTimeout(250)
  expect(observed.elementsSessions).toBe(1)
}

for (const target of ['inline', 'view'] as const) {
  test(`${target} mounts real Payment and Express Checkout Elements`, async ({ page }) => {
    const intent = await createStripeTestIntent(target)
    try {
      const observed = await mountRealStripePage(page, target, intent)
      await expectRealElementsMounted(page, target, observed)
    } finally {
      await cancelStripeTestIntent(intent.id)
    }
  })
}
```

Run with the two Test Mode environment variables already exported in the shell:

```bash
cd frontend
pnpm test:e2e:stripe
```

Expected RED: both tests fail because `frontend/e2e/stripe-google-pay/index.html` and `fixture.ts` do not exist, so neither production component nor Stripe iframe is mounted. Each created PaymentIntent is still canceled in `finally`.

- [ ] **Step 4: Add the test-only Vite harness using real production components**

Create `frontend/e2e/stripe-google-pay/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Stripe Google Pay E2E</title>
  </head>
  <body>
    <div id="app"></div>
    <script type="module" src="./fixture.ts"></script>
  </body>
</html>
```

Create `frontend/e2e/stripe-google-pay/fixture.ts`:

```ts
import { createApp, createVNode, defineComponent, h } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import StripePaymentInline from '@/components/payment/StripePaymentInline.vue'
import StripePaymentView from '@/views/user/StripePaymentView.vue'
import { usePaymentStore } from '@/stores/payment'
import i18n, { initI18n } from '@/i18n'
import type { PaymentConfig } from '@/types/payment'
import '@/style.css'

type HarnessTarget = 'inline' | 'view'

interface StripeE2EBrowserConfig {
  target: HarnessTarget
  publishableKey: string
  clientSecret: string
}

declare global {
  interface Window {
    __STRIPE_E2E__?: StripeE2EBrowserConfig
  }
}

function readBrowserConfig(): StripeE2EBrowserConfig {
  const config = window.__STRIPE_E2E__
  if (!config?.publishableKey || !config.clientSecret) {
    throw new Error('Stripe E2E browser configuration is missing')
  }
  if (config.target !== 'inline' && config.target !== 'view') {
    throw new Error('Stripe E2E target is invalid')
  }
  return config
}

function paymentConfig(publishableKey: string): PaymentConfig {
  return {
    payment_enabled: true,
    min_amount: 1,
    max_amount: 1000,
    daily_limit: 0,
    max_pending_orders: 5,
    order_timeout_minutes: 30,
    balance_disabled: false,
    balance_recharge_multiplier: 1,
    subscription_usd_to_cny_rate: 0,
    enabled_payment_types: ['stripe'],
    help_image_url: '',
    help_text: '',
    stripe_publishable_key: publishableKey,
  }
}

async function bootstrap(): Promise<void> {
  const config = readBrowserConfig()
  const pinia = createPinia()
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{
      path: '/:pathMatch(.*)*',
      component: { render: () => createVNode('div') },
    }],
  })
  const paymentStore = usePaymentStore(pinia)
  paymentStore.config = paymentConfig(config.publishableKey)
  paymentStore.configLoaded = true

  await router.push(config.target === 'view'
    ? {
        path: '/payment/stripe',
        query: {
          order_id: '42',
          client_secret: config.clientSecret,
          method: 'e2e_generic',
        },
      }
    : '/')

  await initI18n()

  const Root = defineComponent({
    name: 'StripeGooglePayE2EHarness',
    setup() {
      return () => config.target === 'inline'
        ? h('div', { 'data-testid': 'stripe-inline-harness' }, [
            h(StripePaymentInline, {
              orderId: 42,
              amount: 1,
              clientSecret: config.clientSecret,
              publishableKey: config.publishableKey,
              payAmount: 1,
              currency: 'USD',
            }),
          ])
        : h('div', { 'data-testid': 'stripe-view-harness' }, [
            h(StripePaymentView),
          ])
    },
  })

  const app = createApp(Root)
  app.use(pinia)
  app.use(router)
  app.use(i18n)
  await router.isReady()
  app.mount('#app')
}

void bootstrap()
```

The unknown `method=e2e_generic` selects the production generic branch while avoiding `AppLayout`; direct `alipay` and `wechat_pay` values remain untouched and are covered by Task 3 behavior tests.

- [ ] **Step 5: Prove the external tests are GREEN and secret-safe**

With matching Test Mode keys exported, run:

```bash
cd frontend
pnpm test:e2e:stripe
pnpm typecheck
pnpm exec eslint e2e/stripe-google-pay --ext .ts
```

Expected:

- Two Playwright tests pass: `inline` and `view`.
- Each test observes at least one successful `js.stripe.com` response and exactly one successful `/v1/elements/sessions` response.
- Each harness contains an Express Checkout iframe and a separate Payment Element iframe.
- No Stripe key or `client_secret` appears in command output, trace, screenshot, video or repository files.
- Typecheck and E2E ESLint exit 0.

Also prove missing credentials fail before Playwright starts a browser or Vite server:

```bash
cd frontend
env -u STRIPE_E2E_SECRET_KEY -u STRIPE_E2E_PUBLISHABLE_KEY \
  pnpm test:e2e:stripe --list
```

Expected: exit 1 with the missing-variable message from `readStripeE2EEnvironment`; no browser or Vite server starts.

Run this repository scan without printing environment values:

```bash
rg -n "sk_test_[A-Za-z0-9]{8,}|pk_test_[A-Za-z0-9]{8,}|pi_[A-Za-z0-9]+_secret_[A-Za-z0-9]{8,}|STRIPE_E2E_SECRET_KEY.*VITE" \
  frontend/e2e frontend/src frontend/package.json docs/PAYMENT.md docs/PAYMENT_CN.md
```

Expected: no hard-coded Stripe key/client-secret value and no Vite exposure of `STRIPE_E2E_SECRET_KEY`; documented environment variable names are allowed.

- [ ] **Step 6: Commit Task 4**

```bash
git add .gitignore frontend/package.json frontend/pnpm-lock.yaml \
  frontend/e2e/stripe-google-pay/playwright.config.ts \
  frontend/e2e/stripe-google-pay/stripe-test-api.ts \
  frontend/e2e/stripe-google-pay/stripe-google-pay.external.spec.ts \
  frontend/e2e/stripe-google-pay/index.html \
  frontend/e2e/stripe-google-pay/fixture.ts
git commit -m "test(payments): verify Google Pay with real Stripe"
```

---

### Task 5: Document real Stripe verification and run final regressions

**Files:**
- Modify: `docs/PAYMENT_CN.md`
- Modify: `docs/PAYMENT.md`

**Interfaces:**
- Consumes: the production behavior and `pnpm test:e2e:stripe` from Tasks 1-4.
- Produces: operator documentation and fresh evidence for frontend, backend and real Stripe acceptance.

- [ ] **Step 1: Add Chinese operator and test instructions**

After the Stripe parameter table in `docs/PAYMENT_CN.md`, add:

```markdown
#### Google Pay Express Checkout

Google Pay 通过 Stripe Express Checkout Element 展示在 Stripe 支付面板顶部，并作为 `card` 钱包处理。Sub2API 不新增 `google_pay` 支付类型，也不保存 Google Pay Merchant ID 或 Stripe Payment Method Domain ID。

启用前请确认：

1. Stripe Dashboard 的 Payment Methods 中已启用 Google Pay。
2. Stripe 服务商实例的支持类型包含 `card`。
3. 所有展示支付面板的生产和测试域名均已在 Stripe Payment Method Domains 中注册。
4. 生产支付页面使用受信任的 HTTPS 证书。
5. Test Mode 与 Live Mode 分别使用匹配的 Publishable Key、Secret Key、Webhook Secret 和域名注册状态。

Google Pay 不可用时，快捷区域会自动隐藏，现有 Payment Element 保持可用。充值或订阅发放仍以验签后的 `payment_intent.succeeded` Webhook 为准。

真实 Stripe 浏览器验证使用以下环境变量，值不得写入仓库：

- `STRIPE_E2E_SECRET_KEY`：Stripe Test Mode Secret Key，仅供 Playwright 的 Node 进程调用 Stripe API。
- `STRIPE_E2E_PUBLISHABLE_KEY`：同一测试账户的 Publishable Key。
- `STRIPE_E2E_BASE_URL`：可选；不设置时使用本地 Vite 测试页面。

安装 Chromium 后运行：

```bash
cd frontend
pnpm exec playwright install chromium
pnpm test:e2e:stripe
```

该命令会创建并清理测试 PaymentIntent，真实加载 Stripe.js 和 Elements，但不会打开 Google Pay 钱包或完成付款。
```

- [ ] **Step 2: Add the equivalent English documentation**

After the Stripe parameter table in `docs/PAYMENT.md`, add:

```markdown
#### Google Pay Express Checkout

Google Pay is displayed above the Stripe Payment Element through Stripe's Express Checkout Element and is processed as a `card` wallet. Sub2API does not add a `google_pay` payment type and does not store the Google Pay Merchant ID or Stripe Payment Method Domain ID.

Before enabling it, confirm that:

1. Google Pay is enabled under Payment Methods in the Stripe Dashboard.
2. The Stripe provider instance includes `card` in its supported types.
3. Every production and test hostname that displays checkout is registered under Stripe Payment Method Domains.
4. Production checkout uses a publicly trusted HTTPS origin.
5. Test Mode and Live Mode use matching publishable keys, secret keys, webhook secrets and domain registrations.

When Google Pay is unavailable, the express region is hidden and the existing Payment Element remains available. Fulfillment continues to depend on the signed `payment_intent.succeeded` webhook.

Real Stripe browser verification reads these environment variables; never commit their values:

- `STRIPE_E2E_SECRET_KEY`: Stripe Test Mode Secret Key, used only by Playwright's Node process.
- `STRIPE_E2E_PUBLISHABLE_KEY`: Publishable Key from the same test account.
- `STRIPE_E2E_BASE_URL`: optional; the local Vite harness is used when omitted.

Install Chromium and run:

```bash
cd frontend
pnpm exec playwright install chromium
pnpm test:e2e:stripe
```

The command creates and cleans up test PaymentIntents and loads real Stripe.js and Elements. It does not open the Google Pay wallet or complete a payment.
```

- [ ] **Step 3: Run focused frontend verification**

Run:

```bash
cd frontend
pnpm test:run \
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
  src/views/user/__tests__/StripePaymentView.spec.ts \
  e2e/stripe-google-pay --ext .ts
pnpm build
```

Expected: all focused tests pass; typecheck, lint and production build exit 0.

- [ ] **Step 4: Run the mandatory real Stripe browser gate**

With matching Test Mode keys exported in the shell, run:

```bash
cd frontend
pnpm test:e2e:stripe
```

Expected: both external tests pass with real Stripe API/Stripe.js traffic, and both test PaymentIntents are canceled during cleanup.

- [ ] **Step 5: Run backend payment regressions**

Run:

```bash
cd backend
go test ./internal/payment/provider ./internal/handler ./internal/service \
  -run 'Stripe|PaymentWebhook' -count=1
```

Expected: selected Stripe and webhook tests pass; `git diff -- backend` remains empty.

- [ ] **Step 6: Compare the full frontend suite to the recorded baseline**

Run:

```bash
cd frontend
pnpm test:run
```

Expected: no Google Pay or Playwright test fails. The pre-feature baseline contains 34 unrelated failures across 14 files; the task may proceed only if the failure set is unchanged or smaller and every changed payment test is green.

- [ ] **Step 7: Commit Task 5 documentation**

```bash
git add docs/PAYMENT_CN.md docs/PAYMENT.md
git commit -m "docs(payments): document Google Pay verification"
```

---

## Final Review Checklist

- [ ] `git diff --check` reports no whitespace errors.
- [ ] `git status --short` contains only ignored `.superpowers` artifacts and no unintended feature files.
- [ ] No source, config, test artifact, URL or log contains a Stripe Secret Key, PaymentIntent `client_secret`, Google Pay Merchant ID value or Stripe Domain ID value.
- [ ] Express Checkout keeps Google Pay at `auto` and every non-Google wallet at `never`.
- [ ] Inline and standalone parents pass the exact same `StripeElements` object to Payment Element and `StripeGooglePayExpress`.
- [ ] Both directions of the shared submission lock have deterministic behavior coverage.
- [ ] Direct Alipay and WeChat branches do not create Express Checkout.
- [ ] `pnpm test:e2e:stripe` observes real Stripe.js and exactly one successful Elements Session per page without intercepting Stripe traffic.
- [ ] Every E2E-created PaymentIntent has `metadata[test_suite]=sub2api_google_pay_e2e` and is canceled after the test.
- [ ] Focused tests, typecheck, lint, build, backend payment regressions and real Stripe browser tests pass with fresh output.
- [ ] Full frontend failures do not exceed or differ materially from the recorded 34-test/14-file baseline.
- [ ] Manual wallet completion remains a deployment gate for a registered HTTPS staging domain and is not claimed by this automation.

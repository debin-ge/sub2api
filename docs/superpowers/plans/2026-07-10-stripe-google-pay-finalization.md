# Stripe Google Pay Express Checkout Finalization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补齐 Google Pay Express Checkout 双语运维文档，并用确定性前端、构建和后端回归证据完成已实现功能的交付验证。

**Architecture:** 生产代码继续动态加载真实 `@stripe/stripe-js`；Vitest 挂载真实 `StripeGooglePayExpress` Vue 子组件，只替换 Stripe SDK 边界以验证应用内部行为。本计划不新增 Playwright，不调用 Stripe Test Mode 或 Live Mode API；真实钱包和 Stripe 网络验证保留为上线前人工 HTTPS 验收。

**Tech Stack:** Vue 3、TypeScript 5.6、`@stripe/stripe-js` 9.8.x、Vitest 2、Vue Test Utils、pnpm、Go、Stripe PaymentIntent/Webhook。

## Global Constraints

- 快捷支付区域只允许 Google Pay：`googlePay: 'auto'`，`applePay`、`link`、`amazonPay`、`paypal`、`klarna` 均为 `never`。
- Google Pay Merchant ID 与 Stripe Payment Method Domain ID 不进入代码、配置、接口响应或日志。
- 不新增 `google_pay` 后端支付类型；PaymentIntent 继续通过 `card` 支持 Google Pay。
- 每个支付界面只创建一个 Stripe 实例、一个 Elements 实例和一个 PaymentIntent。
- Google Pay 不可用或组件加载失败时静默隐藏快捷区域，并保留现有 Payment Element。
- 前端成功状态不触发入账；验签后的 `payment_intent.succeeded` Webhook 继续是充值和订阅发放的唯一权威来源。
- Google Pay 与普通 Payment Element 共用提交锁，不能并发确认同一个 PaymentIntent。
- Task 2/3 行为测试必须挂载真实 `StripeGooglePayExpress` Vue 子组件，只允许替换 Stripe SDK 边界。
- 不新增 Playwright 或其他外部浏览器测试框架，不由自动化脚本调用 Stripe Test Mode 或 Live Mode API。
- 自动化结果不得声称已验证真实 Stripe 网络、Google 钱包可用性或真实支付成功。
- 上线前人工 HTTPS 验收是部署门槛，不是本地自动化完成声明。
- 不修改后端生产代码，不升级任何依赖。
- 保留工作区无关改动；只暂存当前任务列出的文档文件。

## Execution State

- Task 1 reusable component complete: `5e7e20ecb`, `2abe8be98`.
- Task 2 inline integration complete: `03e8f6ea1`.
- Task 3 standalone integration and reciprocal locks complete: `cee45960c`.
- Final specification without Stripe mode automation: `2e2e43499`.

## File Map

- Modify `docs/PAYMENT_CN.md`: 中文 Google Pay 配置、参数边界、人工验收与故障回退说明。
- Modify `docs/PAYMENT.md`: 英文等价说明。
- No frontend, backend, dependency or test-source file changes are allowed in Task 4 unless task review finds a proven defect and the controller approves a separate fix cycle.

---

### Task 4: Document configuration and verify the completed feature

**Files:**
- Modify: `docs/PAYMENT_CN.md`
- Modify: `docs/PAYMENT.md`

**Interfaces:**
- Consumes: completed `StripeGooglePayExpress`, `StripePaymentInline`, `StripePaymentView`, existing Stripe provider and webhook flow.
- Produces: operator instructions that distinguish required Stripe settings from unused Merchant/Domain identifiers, plus fresh regression evidence.

- [ ] **Step 1: Prove the operator documentation is missing**

Run:

```bash
rg -n '^#### Google Pay Express Checkout$' docs/PAYMENT_CN.md docs/PAYMENT.md
```

Expected RED: exit 1 with no matches, proving neither operator guide documents the feature yet.

- [ ] **Step 2: Add the Chinese operator documentation**

Immediately after the Stripe parameter table in `docs/PAYMENT_CN.md`, add:

```markdown
#### Google Pay Express Checkout

Google Pay 通过 Stripe Express Checkout Element 展示在 Stripe 支付面板顶部，并作为现有 `card` PaymentIntent 的钱包方式处理。Sub2API 不新增 `google_pay` 后端支付类型，也不保存或传递 Google Pay Merchant ID、Stripe Payment Method Domain ID。

启用前请确认：

1. Stripe Dashboard 的 Payment Methods 中已启用 Google Pay。
2. Stripe 服务商实例的支持类型包含 `card`。
3. 所有展示支付面板的生产和预发布域名（包括 `www` 及其他子域）均已在 Stripe Payment Method Domains 中注册。
4. 支付页面使用受信任的公网 HTTPS 证书。
5. 当前域名、Publishable Key、Secret Key 和 Webhook Secret 属于同一个 Stripe 环境。

快捷支付区域暂时只允许 Google Pay；Apple Pay、Link、Amazon Pay、PayPal 和 Klarna 不会出现在该区域。Google Pay 不可用或加载失败时，快捷区域和分隔线会静默隐藏，下方 Payment Element 仍可用于银行卡、支付宝、微信支付和 Link。

Google Pay 与 Payment Element 复用同一个本地订单、PaymentIntent、Stripe/Elements 实例和提交锁。前端成功状态不发放余额或订阅；最终入账始终以验签后的 Stripe `payment_intent.succeeded` Webhook 为准。

自动化测试挂载真实 `StripeGooglePayExpress` Vue 子组件，但只验证应用内部行为，不代表真实 Google 钱包或 Stripe 网络已经可用。上线前必须在已注册 HTTPS 域名、已登录 Google 且配置钱包的真实 Chrome/Android 环境中人工确认：

- 只有 Google Pay 出现在快捷支付区域。
- 钱包取消或失败后仍可使用下方 Payment Element。
- 成功交易在 Stripe 中显示 `card.wallet.type = google_pay`。
- Webhook 只触发一次本地入账。
```

- [ ] **Step 3: Add the equivalent English documentation**

Immediately after the Stripe parameter table in `docs/PAYMENT.md`, add:

```markdown
#### Google Pay Express Checkout

Google Pay is displayed above the Stripe Payment Element through Stripe's Express Checkout Element and is processed as a wallet on the existing `card` PaymentIntent. Sub2API does not add a `google_pay` backend payment type and does not store or transmit the Google Pay Merchant ID or Stripe Payment Method Domain ID.

Before enabling it, confirm that:

1. Google Pay is enabled under Payment Methods in the Stripe Dashboard.
2. The Stripe provider instance includes `card` in its supported types.
3. Every production and staging hostname that displays checkout, including `www` and other subdomains, is registered under Stripe Payment Method Domains.
4. Checkout uses a publicly trusted HTTPS origin.
5. The hostname, publishable key, secret key and webhook secret belong to the same Stripe environment.

The express region currently permits only Google Pay. Apple Pay, Link, Amazon Pay, PayPal and Klarna are disabled in that region. When Google Pay is unavailable or fails to load, the express region and divider are hidden silently while the Payment Element remains available for card, Alipay, WeChat Pay and Link.

Google Pay and the Payment Element reuse the same local order, PaymentIntent, Stripe/Elements instances and submission lock. Frontend success state never grants balance or subscriptions; fulfillment remains authoritative only after the signed Stripe `payment_intent.succeeded` webhook.

Automated tests mount the real `StripeGooglePayExpress` Vue component but verify only application behavior; they do not prove that a real Google wallet or Stripe network is available. Before production rollout, validate manually on a registered HTTPS hostname in a real Chrome/Android environment signed into Google with a configured wallet:

- Only Google Pay appears in the express region.
- After wallet cancellation or failure, the Payment Element remains usable.
- A successful charge reports `card.wallet.type = google_pay` in Stripe.
- The webhook causes exactly one local fulfillment.
```

- [ ] **Step 4: Prove the documentation is GREEN and contains no runtime identifiers**

Run:

```bash
rg -n '^#### Google Pay Express Checkout$' docs/PAYMENT_CN.md docs/PAYMENT.md
rg -n 'Google Pay Merchant ID|Stripe Payment Method Domain ID|payment_intent\.succeeded|card\.wallet\.type = google_pay' \
  docs/PAYMENT_CN.md docs/PAYMENT.md
```

Expected: each heading appears exactly once; both guides contain the parameter boundary, webhook authority and manual wallet check.

Run the value/field scan:

```bash
rg -n 'merchantId|domainId|pmd_[A-Za-z0-9]{8,}' \
  frontend/src backend/internal docs/PAYMENT_CN.md docs/PAYMENT.md
```

Expected: no match. Human-readable names in the two operator guides are intentional; runtime field names and Domain ID values are absent.

- [ ] **Step 5: Run focused frontend behavior verification**

Run:

```bash
cd frontend
pnpm test:run \
  src/components/payment/__tests__/StripeGooglePayExpress.spec.ts \
  src/components/payment/__tests__/StripePaymentInline.spec.ts \
  src/views/user/__tests__/StripePaymentView.spec.ts
```

Expected: 3 test files and 15 tests pass. Tests mount the real Vue child and cover exact wallet options, availability, both submission-lock directions, success flow, cleanup, and Alipay/WeChat isolation.

- [ ] **Step 6: Run frontend type, lint and production-build gates**

Run:

```bash
cd frontend
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

Expected: typecheck, focused ESLint and production build exit 0. Existing pnpm override or Browserslist maintenance warnings may be recorded but must not become new errors.

- [ ] **Step 7: Run backend Stripe, Webhook and fulfillment regressions**

Run:

```bash
cd backend
go test ./internal/payment/provider ./internal/handler ./internal/service \
  -run 'Stripe|PaymentWebhook|HandlePaymentNotification' -count=1
```

Expected: selected backend tests pass and `git diff -- backend` remains empty.

- [ ] **Step 8: Compare the full frontend suite to the recorded baseline**

Run:

```bash
cd frontend
pnpm test:run
```

Expected: no changed Google Pay test fails. The pre-feature baseline contains 34 unrelated failures across 14 files; proceed only if the failure set is unchanged or smaller and every changed payment test remains green.

- [ ] **Step 9: Validate repository scope and commit the documentation**

Run:

```bash
git diff --check
git status --short
git diff --name-only -- backend frontend/package.json frontend/pnpm-lock.yaml
```

Expected: no whitespace errors; only `docs/PAYMENT_CN.md` and `docs/PAYMENT.md` are uncommitted task files; no backend or dependency file changed.

Commit:

```bash
git add docs/PAYMENT_CN.md docs/PAYMENT.md
git commit -m "docs(payments): document Google Pay express checkout"
```

---

## Final Review Checklist

- [ ] `git diff --check` reports no whitespace errors.
- [ ] Worktree contains no unintended tracked or untracked feature file.
- [ ] Stripe.js remains 9.8.x and no Playwright dependency or E2E harness was added.
- [ ] Express Checkout keeps Google Pay at `auto` and every non-Google wallet at `never`.
- [ ] Inline and standalone parents share one `StripeElements` object with the real Vue child.
- [ ] Both directions of the shared submission lock have passing behavior tests.
- [ ] Direct Alipay and WeChat branches do not create Express Checkout.
- [ ] Merchant ID and Domain ID do not appear as runtime fields, values, logs or persisted config.
- [ ] Webhook fulfillment remains unchanged and authoritative.
- [ ] Focused tests, typecheck, lint, build and backend payment regressions pass with fresh output.
- [ ] Full frontend failures do not exceed or materially differ from the recorded 34-test/14-file baseline.
- [ ] Manual wallet validation is documented as a deployment gate and is not claimed as automated evidence.

import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AdminOrdersView from '../AdminOrdersView.vue'
import type { PaymentOrder } from '@/types/payment'

const getOrders = vi.hoisted(() => vi.fn())
const getOrder = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const retryRecharge = vi.hoisted(() => vi.fn())
const refundOrder = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const showSuccess = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, fallback?: string) => fallback ?? key,
    }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    getOrders,
    getOrder,
    cancelOrder,
    retryRecharge,
    refundOrder,
  },
  default: {
    getOrders,
    getOrder,
    cancelOrder,
    retryRecharge,
    refundOrder,
  },
}))

const wiseOrder: PaymentOrder = {
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  currency: 'USD',
  fee_rate: 0,
  payment_type: 'wise',
  out_trade_no: 'sub2_wise_admin_detail_001',
  status: 'PENDING',
  order_type: 'balance',
  created_at: '2026-06-12T10:00:00Z',
  expires_at: '2026-06-12T10:30:00Z',
  refund_amount: 0,
}

function mountView() {
  return mount(AdminOrdersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /></div>',
        },
        Select: { template: '<div />' },
        Icon: { template: '<span />' },
        Pagination: { template: '<div />' },
        AdminRefundDialog: { template: '<div />' },
        OrderStatusBadge: {
          props: ['status'],
          template: '<span>{{ status }}</span>',
        },
        OrderTable: {
          props: ['orders', 'loading'],
          template: `
            <div>
              <div v-for="order in orders" :key="order.id">
                <slot name="actions" :row="order" />
              </div>
            </div>
          `,
        },
      },
    },
  })
}

describe('AdminOrdersView Wise reconciliation details', () => {
  beforeEach(() => {
    getOrders.mockReset()
    getOrder.mockReset()
    cancelOrder.mockReset()
    retryRecharge.mockReset()
    refundOrder.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('renders structured Wise reconciliation detail from audit log metadata', async () => {
    getOrders.mockResolvedValue({
      data: {
        items: [wiseOrder],
        total: 1,
      },
    })
    getOrder.mockResolvedValue({
      data: {
        order: wiseOrder,
        auditLogs: [
          {
            id: 7,
            action: 'PAYMENT_WISE_RECONCILE_MANUAL_REVIEW',
            detail: JSON.stringify({
              reason: 'amount_mismatch',
              reviewAction: 'manual_review',
              reconcile_decision: 'manual_review',
              wise_transaction_id: 'balance-tx-1',
              gross_amount: '90.00',
              fee_amount: '2.00',
              net_amount: '88.00',
              currency: 'USD',
              description: 'Payment for sub2_wise_admin_detail_001',
              reference: 'sub2_wise_admin_detail_001',
              transaction_status: 'completed',
              occurred_at: '2026-06-12T10:20:30Z',
            }),
            operator: 'wise',
            created_at: '2026-06-12T10:21:00Z',
          },
        ],
      },
    })

    const wrapper = mountView()
    await flushPromises()

    const viewButton = wrapper
      .findAll('button')
      .find(button => button.text().includes('common.view'))
    if (!viewButton) throw new Error('view button not found')
    await viewButton.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('payment.admin.wiseReconcile.title')
    expect(wrapper.text()).toContain('payment.admin.wiseReconcile.manualReviewWarning')
    expect(wrapper.text()).toContain('balance-tx-1')
    expect(wrapper.text()).toContain('amount_mismatch')
    expect(wrapper.text()).toContain('manual_review')
    expect(wrapper.text()).toContain('USD 88.00')
    expect(wrapper.text()).toContain('USD 90.00')
    expect(wrapper.text()).toContain('USD 2.00')
    expect(wrapper.text()).toContain('Payment for sub2_wise_admin_detail_001')
    expect(wrapper.text()).not.toContain('¥88.00')
  })
})

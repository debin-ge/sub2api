import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ModelDetailModal from '@/components/plaza/ModelDetailModal.vue'
import type { AggregatedModel } from '@/composables/useModelAggregation'
import type { VueWrapper } from '@vue/test-utils'

const messages: Record<string, string> = {
  'plaza.modal.close': 'Close',
  'plaza.modal.fullPricing': 'Full pricing',
  'plaza.modal.input': 'Input',
  'plaza.modal.output': 'Output',
  'plaza.modal.cacheWrite': 'Cache write',
  'plaza.modal.cacheRead': 'Cache read',
  'plaza.modal.imageOutput': 'Image output',
  'plaza.modal.perRequest': 'Per request',
  'plaza.modal.supportedChannels': 'Channels supporting this model',
  'plaza.modal.channelsCount': '{n} channels',
  'plaza.modal.tieredPricing': 'Tiered pricing',
  'plaza.modal.tierRange': '{min} - {max} tokens',
  'plaza.modal.tierRangeOpenEnded': '{min}+ tokens',
  'plaza.card.billingPerToken': 'Pay per token',
  'plaza.card.discountBadge': '{percent}% of reference',
  'plaza.card.recentCalls': '{count} calls in 7d',
  'plaza.price.unitPerMillion': '/1M',
  'plaza.price.unitPerRequest': '/request',
}

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const template = messages[key] ?? key
        return template.replace(/\{(\w+)\}/g, (_, name) => String(params?.[name] ?? ''))
      },
    }),
  }
})

const model: AggregatedModel = {
  model: 'gpt-5.5',
  displayName: 'gpt-5.5',
  platform: 'openai',
  minPricing: {
    input: 0.000003,
    output: 0.000012,
    cacheWrite: null,
    cacheRead: null,
    imageOutput: 0.00004,
    perRequest: 0.002
  },
  supportedGroups: [
    {
      channelName: 'OpenAI backup',
      channelDescription: 'secondary',
      group: {
        id: 3,
        name: 'backup',
        platform: 'openai',
        subscription_type: 'standard',
        rate_multiplier: 2,
        is_exclusive: false
      },
      pricing: null
    },
    {
      channelName: 'OpenAI direct',
      channelDescription: 'primary',
      group: {
        id: 2,
        name: 'public',
        platform: 'openai',
        subscription_type: 'standard',
        rate_multiplier: 1,
        is_exclusive: false
      },
      pricing: {
        billing_mode: 'token',
        input_price: 0.000003,
        output_price: 0.000012,
        cache_write_price: null,
        cache_read_price: null,
        image_output_price: null,
        per_request_price: null,
        intervals: [
          {
            min_tokens: 0,
            max_tokens: 100000,
            tier_label: 'Base',
            input_price: 0.000003,
            output_price: 0.000012,
            cache_write_price: 0.000004,
            cache_read_price: 0.000001,
            per_request_price: 0.002
          },
          {
            min_tokens: 100001,
            max_tokens: null,
            tier_label: 'Long context',
            input_price: 0.000004,
            output_price: 0.000016,
            cache_write_price: 0.000005,
            cache_read_price: 0.0000015,
            per_request_price: 0.003
          }
        ]
      }
    }
  ],
  bestRateMultiplier: 1,
  recentCalls: 0,
  recentCallWindowSeconds: 0
}

let wrapper: VueWrapper | null = null

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  document.body.innerHTML = ''
})

describe('ModelDetailModal', () => {
  it('renders full pricing, sorted channel list, and closes', async () => {
    document.body.innerHTML = '<button id="trigger">Open modal</button>'
    const trigger = document.querySelector<HTMLButtonElement>('#trigger')
    trigger?.focus()

    wrapper = mount(ModelDetailModal, {
      props: { open: true, model, multiplier: 1.5, rate: 6.8 },
      attachTo: document.body
    })
    await wrapper.vm.$nextTick()

    expect(document.body.textContent).toContain('gpt-5.5')
    expect(document.body.textContent).toContain('Full pricing')
    expect(document.body.textContent).toContain('Input')
    expect(document.body.textContent).toContain('Output')
    expect(document.body.textContent).toContain('Cache write')
    expect(document.body.textContent).toContain('Cache read')
    expect(document.body.textContent).toContain('Image output')
    expect(document.body.textContent).toContain('Per request')
    expect(document.body.textContent).toContain('$3')
    expect(document.body.textContent).toContain('9.8% of reference')
    expect(document.body.textContent).toContain('$12')
    expect(document.body.textContent).toContain('$40')
    expect(document.body.textContent).toContain('$0.002')
    expect(document.body.textContent).toContain('Channels supporting this model')
    expect(document.body.textContent).toContain('Tiered pricing')
    expect(document.body.textContent).toContain('Base')
    expect(document.body.textContent).toContain('0 - 100,000 tokens')
    expect(document.body.textContent).toContain('Long context')
    expect(document.body.textContent).toContain('100,001+ tokens')
    expect(document.body.textContent).toContain('$4')
    expect(document.body.textContent).toContain('$16')
    expect(document.body.textContent).toContain('$5')
    expect(document.body.textContent).toContain('$1.5')
    expect(document.body.textContent).toContain('$0.003')
    expect(document.body.textContent).not.toContain('完整定价')
    expect(document.body.textContent).not.toContain('支持该模型的渠道')

    const bodyText = document.body.textContent ?? ''
    expect(bodyText.indexOf('OpenAI direct')).toBeLessThan(bodyText.indexOf('OpenAI backup'))

    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-labelledby')).toBe('plaza-modal-title')
    expect(document.body.querySelector('#plaza-modal-title')?.textContent).toContain('gpt-5.5')

    const closeButton = document.body.querySelector<HTMLButtonElement>('[data-testid="plaza-modal-close"]')
    expect(closeButton).not.toBeNull()
    expect(document.activeElement).toBe(closeButton)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('close')).toBeTruthy()

    closeButton?.click()
    await wrapper.vm.$nextTick()

    expect(wrapper.emitted('close')).toHaveLength(2)
    await wrapper.setProps({ open: false })
    expect(document.activeElement).toBe(trigger)
  })
})

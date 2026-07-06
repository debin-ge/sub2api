import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import ModelDetailModal from '@/components/plaza/ModelDetailModal.vue'
import type { AggregatedModel } from '@/composables/useModelAggregation'
import type { VueWrapper } from '@vue/test-utils'

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
        intervals: []
      }
    }
  ]
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
    expect(document.body.textContent).toContain('完整定价')
    expect(document.body.textContent).toContain('输入')
    expect(document.body.textContent).toContain('输出')
    expect(document.body.textContent).toContain('缓存写入')
    expect(document.body.textContent).toContain('缓存读取')
    expect(document.body.textContent).toContain('图像输出')
    expect(document.body.textContent).toContain('按次计费')
    expect(document.body.textContent).toContain('$3')
    expect(document.body.textContent).toContain('$12')
    expect(document.body.textContent).toContain('$40')
    expect(document.body.textContent).toContain('$0.002')
    expect(document.body.textContent).toContain('支持该模型的渠道')

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

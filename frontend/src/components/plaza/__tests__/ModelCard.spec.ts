import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ModelCard from '@/components/plaza/ModelCard.vue'
import type { AggregatedModel } from '@/composables/useModelAggregation'

const model: AggregatedModel = {
  model: 'claude-opus-4-8',
  displayName: 'claude-opus-4-8',
  platform: 'anthropic',
  minPricing: {
    input: 0.000015,
    output: 0.000075,
    cacheWrite: 0.00001875,
    cacheRead: 0.0000015,
    imageOutput: null,
    perRequest: null
  },
  supportedGroups: [
    {
      channelName: 'Anthropic',
      channelDescription: '',
      group: {
        id: 1,
        name: 'public',
        platform: 'anthropic',
        subscription_type: 'standard',
        rate_multiplier: 1,
        is_exclusive: false
      },
      pricing: null
    }
  ]
}

describe('ModelCard', () => {
  it('renders four core pricing rows and emits open-detail', async () => {
    const wrapper = mount(ModelCard, {
      props: { model, multiplier: 1.5, rate: 6.8 }
    })

    expect(wrapper.text()).toContain('claude-opus-4-8')
    expect(wrapper.text()).toContain('$15')
    expect(wrapper.text()).toContain('$75')
    expect(wrapper.text()).toContain('$18.75')
    expect(wrapper.text()).toContain('$1.5')
    expect(wrapper.text()).toContain('¥102')
    expect(wrapper.text()).toContain('¥10')
    expect(wrapper.text()).toContain('10.2x')
    expect(wrapper.text()).toContain('支持渠道 · 1')
    expect(wrapper.text()).toContain('查看详情')

    await wrapper.trigger('click')

    expect(wrapper.emitted('open-detail')?.[0]).toEqual([model])
  })
})

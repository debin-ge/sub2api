import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import ModelCard from '@/components/plaza/ModelCard.vue'
import type { AggregatedModel } from '@/composables/useModelAggregation'

const messages: Record<string, string> = {
  'plaza.card.input': 'Input',
  'plaza.card.output': 'Output',
  'plaza.card.cacheWrite': 'Cache write',
  'plaza.card.cacheRead': 'Cache read',
  'plaza.card.imageOutput': 'Image output',
  'plaza.card.perRequest': 'Per request',
  'plaza.card.supportedChannels': '{n} channels',
  'plaza.card.billingPerToken': 'Pay per token',
  'plaza.card.billingPerRequest': 'Pay per request',
  'plaza.card.discountBadge': '{percent}% of reference',
  'plaza.card.notAvailable': 'N/A',
  'plaza.card.recentCalls': '{count} calls',
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
  ],
  bestRateMultiplier: 1,
  recentCalls: 0,
  recentCallWindowSeconds: 0
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
    expect(wrapper.text()).not.toContain('¥102')
    expect(wrapper.text()).toContain('¥10')
    expect(wrapper.text()).toContain('9.8% of reference')
    expect(wrapper.text()).toContain('1 channels')
    expect(wrapper.text()).not.toContain('View details')
    expect(wrapper.text()).not.toContain('plaza.card.viewDetails')
    expect(wrapper.text()).toContain('Pay per token')
    expect(wrapper.text()).not.toContain('支持渠道')
    expect(wrapper.text()).not.toContain('查看详情')
    expect(wrapper.text()).not.toContain('rose')
    expect(wrapper.html()).toContain('bg-sky-50')
    expect(wrapper.html()).toContain('bg-emerald-50')
    expect(wrapper.html()).toContain('text-2xl')
    expect(wrapper.html()).toContain('line-through')
    expect(wrapper.html()).not.toContain('bg-rose-50')

    await wrapper.trigger('click')

    expect(wrapper.emitted('open-detail')?.[0]).toEqual([model])
  })

  it('uses the model group multiplier when showing the model discount and recharged price', () => {
    const premiumModel: AggregatedModel = {
      ...model,
      bestRateMultiplier: 2,
      supportedGroups: model.supportedGroups.map((entry) => ({
        ...entry,
        group: { ...entry.group, rate_multiplier: 2 }
      }))
    }

    const wrapper = mount(ModelCard, {
      props: { model: premiumModel, multiplier: 1.5, rate: 6.8 }
    })

    expect(wrapper.text()).toContain('19.61% of reference')
    expect(wrapper.text()).toContain('¥20')
    expect(wrapper.text()).not.toContain('10.2x boost')
  })
})

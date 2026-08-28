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
  'plaza.card.standardDiscountBadge': 'Standard {percent}% of reference',
  'plaza.card.vipDiscountBadge': 'VIP {percent}% of reference',
  'plaza.card.peakPricing': 'Peak pricing applies',
  'plaza.card.deepSeekTimePricing': 'Peak / off-peak',
  'plaza.card.peakPrice': 'Peak',
  'plaza.card.offPeakPrice': 'Off-peak',
  'plaza.card.notAvailable': 'N/A',
  'plaza.card.recentCalls': '{count} calls',
  'plaza.price.unitPerMillion': '/1M',
  'plaza.price.unitPerRequest': '/request',
  'plaza.price.standardLabel': 'Standard',
  'plaza.price.vipLabel': 'VIP',
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
  standardPricing: {
    minPricing: {
      input: 0.000015,
      output: 0.000075,
      cacheWrite: 0.00001875,
      cacheRead: 0.0000015,
      imageOutput: null,
      perRequest: null
    },
    minPricingRateMultipliers: {
      input: 1,
      output: 1,
      cacheWrite: 1,
      cacheRead: 1,
      imageOutput: 1,
      perRequest: 1
    },
    displayRateMultiplier: 1
  },
  vipPricing: null,
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
  recentCalls: 0,
  recentCallWindowSeconds: 0
}

describe('ModelCard', () => {
  it('renders four core pricing rows and emits open-detail', async () => {
    const wrapper = mount(ModelCard, {
      props: { model }
    })

    expect(wrapper.text()).toContain('claude-opus-4-8')
    expect(wrapper.text()).toContain('$15')
    expect(wrapper.text()).toContain('$75')
    expect(wrapper.text()).toContain('$18.75')
    expect(wrapper.text()).toContain('$1.5')
    expect(wrapper.text()).toContain('¥15')
    expect(wrapper.text()).not.toContain('% of reference')
    expect(wrapper.text()).toContain('1 channels')
    expect(wrapper.text()).not.toContain('View details')
    expect(wrapper.text()).not.toContain('plaza.card.viewDetails')
    expect(wrapper.text()).toContain('Pay per token')
    expect(wrapper.text()).not.toContain('支持渠道')
    expect(wrapper.text()).not.toContain('查看详情')
    expect(wrapper.text()).not.toContain('rose')
    expect(wrapper.html()).toContain('divide-y')
    expect(wrapper.html()).toContain('tabular-nums')
    // the reference USD price is struck through against the recharged CNY price
    expect(wrapper.html()).toContain('line-through')
    expect(wrapper.html()).not.toContain('bg-rose-50')

    await wrapper.trigger('click')

    expect(wrapper.emitted('open-detail')?.[0]).toEqual([model])
  })

  it('uses the model group multiplier when showing the model discount and recharged price', () => {
    const premiumModel: AggregatedModel = {
      ...model,
      standardPricing: {
        ...model.standardPricing!,
        displayRateMultiplier: 2,
        minPricingRateMultipliers: {
          input: 2,
          output: 2,
          cacheWrite: 2,
          cacheRead: 2,
          imageOutput: 2,
          perRequest: 2
        }
      },
      supportedGroups: model.supportedGroups.map((entry) => ({
        ...entry,
        group: { ...entry.group, rate_multiplier: 2 }
      }))
    }

    const wrapper = mount(ModelCard, {
      props: { model: premiumModel }
    })

    expect(wrapper.text()).not.toContain('% of reference')
    expect(wrapper.text()).toContain('¥30')
    expect(wrapper.text()).not.toContain('10.2x boost')
  })

  it('shows a CNY original price and applies only the group multiplier to the final price', () => {
    const glmModel: AggregatedModel = {
      ...model,
      model: 'glm-5.1',
      displayName: 'glm-5.1',
      platform: 'zhipu',
      standardPricing: {
        minPricing: {
          input: 0.0000014,
          output: null,
          cacheWrite: null,
          cacheRead: null,
          imageOutput: null,
          perRequest: null
        },
        minPricingRateMultipliers: {
          input: 0.6,
          output: 0.6,
          cacheWrite: 0.6,
          cacheRead: 0.6,
          imageOutput: 0.6,
          perRequest: 0.6
        },
        minPricingCurrencies: {
          input: 'CNY',
          output: 'CNY',
          cacheWrite: 'CNY',
          cacheRead: 'CNY',
          imageOutput: 'CNY',
          perRequest: 'CNY'
        },
        displayRateMultiplier: 0.6
      }
    }

    const wrapper = mount(ModelCard, { props: { model: glmModel } })

    expect(wrapper.text()).toContain('¥1.4')
    expect(wrapper.text()).toContain('¥0.84')
    expect(wrapper.text()).not.toContain('¥5.712')
  })

  it('warns when a supported public group has peak token pricing', () => {
    const peakModel: AggregatedModel = {
      ...model,
      supportedGroups: model.supportedGroups.map((entry) => ({
        ...entry,
        group: {
          ...entry.group,
          subscription_type: 'subscription',
          peak_rate_enabled: true,
          peak_start: '14:00',
          peak_end: '18:00',
          peak_rate_multiplier: 2
        }
      }))
    }

    const wrapper = mount(ModelCard, {
      props: { model: peakModel }
    })

    expect(wrapper.text()).toContain('Peak pricing applies')
  })

  it('renders standard and VIP prices and discounts without one replacing the other', () => {
    const mixedModel: AggregatedModel = {
      ...model,
      vipPricing: {
        minPricing: { ...model.standardPricing!.minPricing },
        minPricingRateMultipliers: {
          input: 0.8,
          output: 0.8,
          cacheWrite: 0.8,
          cacheRead: 0.8,
          imageOutput: 0.8,
          perRequest: 0.8
        },
        displayRateMultiplier: 0.8
      },
      supportedGroups: [
        ...model.supportedGroups,
        {
          ...model.supportedGroups[0],
          group: {
            ...model.supportedGroups[0].group,
            id: 2,
            name: 'vip',
            rate_multiplier: 0.8,
            vip_only: true
          }
        }
      ]
    }

    const wrapper = mount(ModelCard, {
      props: { model: mixedModel }
    })

    expect(wrapper.text()).toContain('80% of reference')
    expect(wrapper.text()).toContain('¥15')
    expect(wrapper.text()).toContain('¥12')
    expect(wrapper.text()).toContain('Standard')
    expect(wrapper.text()).toContain('VIP')
  })

  it('shows DeepSeek peak and off-peak prices from the model-price schedule', () => {
    const deepseekModel: AggregatedModel = {
      ...model,
      model: 'deepseek-v4-flash',
      displayName: 'deepseek-v4-flash',
      platform: 'deepseek',
      standardPricing: {
        minPricing: {
          input: 0.000003,
          output: 0.000009,
          cacheWrite: null,
          cacheRead: 0.0000001,
          imageOutput: null,
          perRequest: null
        },
        minPricingRateMultipliers: {
          input: 1,
          output: 1,
          cacheWrite: 1,
          cacheRead: 1,
          imageOutput: 1,
          perRequest: 1
        },
        displayRateMultiplier: 1
      },
      // 目录价 / 管理端生效价对官方分时 SKU 存的是空闲价：高峰 ×2、空闲 ×1。
      timeSchedule: {
        kind: 'deepseek_official',
        timezone: 'Asia/Shanghai',
        peak_windows: ['09:00-12:00', '14:00-18:00'],
        peak_multiplier: 2,
        off_peak_multiplier: 1
      }
    }

    const wrapper = mount(ModelCard, {
      props: { model: deepseekModel }
    })

    expect(wrapper.text()).toContain('Peak / off-peak')
    expect(wrapper.text()).toContain('Peak')
    expect(wrapper.text()).toContain('Off-peak')
    // 原价行按「高峰 / 空闲」顺序渲染，倍率接反会变成 "$3 / $6"。
    expect(wrapper.text()).toContain('$6 / $3')
    expect(wrapper.text()).toContain('$18 / $9')
  })

  it('halves the price for schedules whose base price is the peak tier', () => {
    // 价格目录缺该 SKU 时生效价来自代码内官方兜底表，那份存的是高峰价。
    const deepseekModel: AggregatedModel = {
      ...model,
      model: 'deepseek-v4-pro',
      displayName: 'deepseek-v4-pro',
      platform: 'deepseek',
      standardPricing: {
        minPricing: {
          input: 0.000003,
          output: 0.000009,
          cacheWrite: null,
          cacheRead: 0.0000001,
          imageOutput: null,
          perRequest: null
        },
        minPricingRateMultipliers: {
          input: 1,
          output: 1,
          cacheWrite: 1,
          cacheRead: 1,
          imageOutput: 1,
          perRequest: 1
        },
        displayRateMultiplier: 1
      },
      timeSchedule: {
        kind: 'deepseek_official',
        timezone: 'Asia/Shanghai',
        peak_windows: ['09:00-12:00', '14:00-18:00'],
        peak_multiplier: 1,
        off_peak_multiplier: 0.5
      }
    }

    const wrapper = mount(ModelCard, {
      props: { model: deepseekModel }
    })

    expect(wrapper.text()).toContain('$3 / $1.5')
    expect(wrapper.text()).toContain('$9 / $4.5')
  })
})

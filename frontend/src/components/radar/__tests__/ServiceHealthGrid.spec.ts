import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ServiceHealthGrid from '@/components/radar/ServiceHealthGrid.vue'
import type { ServiceStatus } from '@/types/radar'
import { service } from './fixtures'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: ref('en'), t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

describe('ServiceHealthGrid', () => {
  it('renders only added model platforms and does not invent optional data', () => {
    const wrapper = mount(ServiceHealthGrid, {
      props: {
        services: [service('openai_api')],
        platforms: ['deepseek', 'openai', 'deepseek'],
      },
      global: { stubs: { Icon: true } },
    })

    const cards = wrapper.findAll('[data-service-key]')
    expect(cards.map((card) => card.attributes('data-service-key'))).toEqual(['deepseek', 'openai'])
    expect(cards.map((card) => card.get('h3').text())).toEqual(['DeepSeek', 'OpenAI'])
    expect(cards[0].text()).toContain('Status unknown')
    expect(cards[1].text()).toContain('Operational')
    expect(wrapper.text()).not.toContain('untrusted upstream name')
    expect(wrapper.text()).not.toContain('Source')
    expect(wrapper.find('[data-testid="service-uptime"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="service-incident"]').exists()).toBe(false)
  })

  it.each([
    ['operational', 'Operational'],
    ['degraded_performance', 'Degraded performance'],
    ['partial_outage', 'Partial outage'],
    ['major_outage', 'Major outage'],
    ['under_maintenance', 'Under maintenance'],
    ['unknown', 'Status unknown'],
  ] as const)('renders %s with text and an icon', (status: ServiceStatus, label: string) => {
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [service('claude_api', status)], platforms: ['anthropic'] },
      global: { stubs: { Icon: { template: '<svg data-testid="status-icon" />' } } },
    })

    const card = wrapper.get('[data-service-key="anthropic"]')
    expect(card.text()).toContain(label)
    expect(card.find('[data-testid="status-icon"]').exists()).toBe(true)
  })

  it('shows health details without upstream update time or stale warnings', () => {
    const item = service('claude_api')
    item.stale = true
    item.uptime_90d = 99.95
    item.last_incident = {
      name: 'API latency',
      status: 'resolved',
      impact: 'minor',
      created_at: '2026-07-12T08:00:00.000Z',
      resolved_at: '2026-07-12T09:00:00.000Z',
    }
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [item], platforms: ['anthropic'] },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.text()).not.toContain('Data may be outdated')
    expect(wrapper.text()).not.toContain('Updated')
    expect(wrapper.text()).toContain('99.95%')
    expect(wrapper.text()).toContain('API latency')
    expect(wrapper.text()).not.toMatch(/2026|Jul/)
  })

  it('uses the worst known service state for an added vendor platform', () => {
    const wrapper = mount(ServiceHealthGrid, {
      props: {
        services: [
          service('openai_api', 'operational'),
          service('codex_web', 'partial_outage'),
        ],
        platforms: ['openai'],
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.get('[data-service-key="openai"]').text()).toContain('Partial outage')
  })
})

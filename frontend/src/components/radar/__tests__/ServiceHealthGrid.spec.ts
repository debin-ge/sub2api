import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import ServiceHealthGrid from '@/components/radar/ServiceHealthGrid.vue'
import type { ServiceStatus } from '@/types/radar'
import { service, serviceHistory } from './fixtures'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ locale: ref('en'), t: (_key: string, fallback?: string) => fallback ?? _key }),
  }
})

describe('ServiceHealthGrid', () => {
  it('renders the platform list already validated by the page without source links', () => {
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
    expect(wrapper.find('[data-testid="service-source"]').exists()).toBe(false)
    expect(wrapper.find('a[href="https://status.claude.com"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="service-updated"]')).toHaveLength(1)
    expect(wrapper.find('[data-testid="service-uptime"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="service-incident"]').exists()).toBe(false)
    expect(cards[0].findAll('[data-history-date]')).toHaveLength(30)
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

  it('renders one shared history status legend above all service cards', () => {
    const wrapper = mount(ServiceHealthGrid, {
      props: {
        services: [service('deepseek'), service('openai_api')],
        platforms: ['deepseek', 'openai'],
      },
      global: { stubs: { Icon: true } },
    })

    const legends = wrapper.findAll('[data-testid="service-history-legend"]')
    expect(legends).toHaveLength(1)
    expect(legends[0].text()).toContain('Status key')
    expect(legends[0].findAll('[data-history-legend-status]')).toHaveLength(5)
    for (const label of [
      'Operational',
      'Degraded performance',
      'Partial outage',
      'Major outage',
      'Status unknown',
    ]) {
      expect(legends[0].text()).toContain(label)
    }
    expect(wrapper.findAll('[data-service-key]')).toHaveLength(2)
  })

  it('shows a 30-day strip and opens official incident details by hover, focus, or click', async () => {
    const item = service('claude_api')
    item.stale = true
    item.uptime_90d = 99.95
    item.history_30d = serviceHistory({
      '2026-07-12': {
        status: 'degraded_performance',
        incidents: [{
          name: 'API latency',
          status: 'resolved',
          impact: 'minor',
          created_at: '2026-07-12T08:00:00.000Z',
          resolved_at: '2026-07-12T09:00:00.000Z',
        }],
      },
    })
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [item], platforms: ['anthropic'] },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.text()).toContain('Data may be outdated')
    expect(wrapper.text()).toContain('Updated')
    expect(wrapper.text()).toContain('99.95%')
    expect(wrapper.findAll('[data-history-date]')).toHaveLength(30)
    const incidentDay = wrapper.get('[data-history-date="2026-07-12"]')
    expect(incidentDay.attributes('data-history-status')).toBe('degraded_performance')
    await incidentDay.trigger('mouseenter')
    expect(wrapper.get('[data-testid="service-history-tooltip"]').text()).toContain('API latency')
    expect(wrapper.get('[data-testid="service-history-tooltip"]').text()).toMatch(/2026|Jul/)
    expect(wrapper.get('[data-testid="service-history-tooltip"]').text()).toContain('8:00 AM')
    await incidentDay.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="service-history-tooltip"]').exists()).toBe(false)

    await incidentDay.trigger('click')
    await wrapper.get('[data-testid="service-history-region"]').trigger('mouseleave')
    expect(wrapper.get('[data-testid="service-history-tooltip"]').text()).toContain('API latency')
    await incidentDay.trigger('keydown', { key: 'Escape' })
    expect(wrapper.find('[data-testid="service-history-tooltip"]').exists()).toBe(false)
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

  it('shows the latest incident from the most recent seven history days', () => {
    const item = service('deepseek')
    item.last_incident = {
      name: 'Older upstream latest incident',
      status: 'resolved',
      impact: 'minor',
      created_at: '2026-07-05T08:00:00.000Z',
      resolved_at: '2026-07-05T09:00:00.000Z',
    }
    item.history_30d = serviceHistory({
      '2026-07-10': {
        incidents: [{
          name: 'Earlier recent incident',
          status: 'resolved',
          impact: 'minor',
          created_at: '2026-07-10T08:00:00.000Z',
          resolved_at: '2026-07-10T09:00:00.000Z',
        }],
      },
      '2026-07-12': {
        incidents: [{
          name: 'Latest recent incident',
          status: 'resolved',
          impact: 'minor',
          created_at: '2026-07-12T08:00:00.000Z',
          resolved_at: '2026-07-12T09:00:00.000Z',
        }],
      },
    })
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [item], platforms: ['deepseek'] },
      global: { stubs: { Icon: true } },
    })

    const incident = wrapper.get('[data-testid="service-incident"]')
    const incidentTime = incident.get('[data-testid="service-incident-time"]')
    expect(incident.text()).toContain('Latest recent incident')
    expect(incident.text()).not.toContain('Earlier recent incident')
    expect(incident.text()).not.toContain('Older upstream latest incident')
    expect(incidentTime.attributes('datetime')).toBe('2026-07-12T08:00:00.000Z')
    expect(incidentTime.text()).toMatch(/Jul 12, 2026/)
  })

  it('hides the latest incident when the recent seven history days contain none', () => {
    const item = service('deepseek')
    item.last_incident = {
      name: 'Older incident',
      status: 'resolved',
      impact: 'minor',
      created_at: '2026-07-06T08:00:00.000Z',
      resolved_at: '2026-07-06T09:00:00.000Z',
    }
    item.history_30d = serviceHistory({
      '2026-07-06': { incidents: [item.last_incident] },
    })
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [item], platforms: ['deepseek'] },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.find('[data-testid="service-incident"]').exists()).toBe(false)
  })

  it('merges multi-service platform history using the worst daily state', () => {
    const api = service('openai_api')
    api.history_30d = serviceHistory({ '2026-07-10': { status: 'degraded_performance' } })
    const web = service('codex_web')
    web.history_30d = serviceHistory({ '2026-07-10': { status: 'major_outage' } })
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [api, web], platforms: ['openai'] },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.get('[data-history-date="2026-07-10"]').attributes('data-history-status')).toBe('major_outage')
  })

  it('keeps partial source coverage and marks only missing days as unknown', () => {
    const item = service('deepseek')
    item.history_30d = [{
      date: '2026-07-10',
      status: 'degraded_performance',
      incidents: [],
    }]
    const wrapper = mount(ServiceHealthGrid, {
      props: { services: [item], platforms: ['deepseek'] },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.findAll('[data-history-date]')).toHaveLength(30)
    expect(wrapper.get('[data-history-date="2026-07-10"]').attributes('data-history-status')).toBe('degraded_performance')
    expect(wrapper.get('[data-history-date="2026-07-09"]').attributes('data-history-status')).toBe('unknown')
  })

  it('maps official platform cards supplied by the page', () => {
    const wrapper = mount(ServiceHealthGrid, {
      props: {
        services: [
          service('windsurf', 'operational'),
          service('deepseek', 'degraded_performance'),
          service('kimi', 'partial_outage'),
          service('minimax', 'major_outage'),
        ],
        platforms: ['windsurf', 'deepseek', 'kimi', 'minimax'],
      },
      global: { stubs: { Icon: true } },
    })

    expect(wrapper.findAll('[data-service-key]').map((card) => card.attributes('data-service-key'))).toEqual([
      'windsurf', 'deepseek', 'kimi', 'minimax',
    ])
    expect(wrapper.get('[data-service-key="deepseek"]').text()).toContain('Degraded performance')
    expect(wrapper.get('[data-service-key="minimax"]').text()).toContain('Major outage')
  })
})

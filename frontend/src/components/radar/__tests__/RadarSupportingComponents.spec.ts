import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RadarPageHeader from '@/components/radar/RadarPageHeader.vue'
import RadarHero from '@/components/radar/RadarHero.vue'
import RadarSectionState from '@/components/radar/RadarSectionState.vue'
import DataSourceFooter from '@/components/radar/DataSourceFooter.vue'
import { source } from './fixtures'

const { localeMock } = vi.hoisted(() => ({ localeMock: { value: 'en' } }))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: localeMock,
      t: (key: string, fallback?: string) => (
        key === 'radar.sources.every' && localeMock.value.startsWith('zh')
          ? '每隔'
          : key === 'radar.sources.aggregatedUsage' && localeMock.value.startsWith('zh')
            ? 'Sub2API 聚合用量'
          : fallback ?? key
      ),
    }),
  }
})

describe('radar supporting components', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    document.body.innerHTML = ''
    document.documentElement.classList.remove('dark')
    localeMock.value = 'en'
  })

  it('does not render section navigation in the page header', () => {
    const wrapper = mount(RadarPageHeader, {
      global: {
        stubs: {
          LocaleSwitcher: { template: '<button>locale</button>' },
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
          Icon: true,
        },
      },
    })

    expect(wrapper.find('nav').exists()).toBe(false)
    expect(wrapper.find('[data-radar-anchor]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Service health')
    expect(wrapper.text()).not.toContain('Quota radar')
    expect(wrapper.text()).not.toContain('Benchmark radar')
  })

  it('reuses the locale control and exposes working theme and login entries', async () => {
    const wrapper = mount(RadarPageHeader, {
      global: {
        stubs: {
          LocaleSwitcher: { template: '<button data-testid="locale-switcher">locale</button>' },
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
          Icon: true,
        },
      },
    })

    expect(wrapper.get('[data-testid="locale-switcher"]').exists()).toBe(true)
    expect(wrapper.get('a[href="/login"]').text()).toBe('Log in')
    const themeButton = wrapper.get('button[aria-label="Switch to dark theme"]')
    await themeButton.trigger('click')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(themeButton.attributes('aria-label')).toBe('Switch to light theme')
  })

  it('shows only the page fetch time without a stale warning or refresh action', () => {
    const wrapper = mount(RadarHero, {
      props: {
        lastFetchedAt: new Date('2026-07-13T08:00:00.000Z'),
      },
    })

    expect(wrapper.text()).toContain('Model Radar')
    expect(wrapper.text()).toContain('Page data fetched')
    expect(wrapper.text()).not.toContain('Data may be outdated')
    expect(wrapper.text()).toMatch(/2026|Jul/)
    expect(wrapper.find('button').exists()).toBe(false)
    expect(wrapper.emitted('refresh')).toBeUndefined()
  })

  it('keeps old content visible during errors and never renders raw errors', async () => {
    const wrapper = mount(RadarSectionState, {
      props: {
        loading: true,
        error: 'postgres://secret@internal/raw-stack',
        hasContent: true,
      },
      slots: { default: '<div data-testid="old-content">old content</div>' },
    })

    expect(wrapper.find('[data-testid="old-content"]').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('Data may be outdated')
    expect(wrapper.text()).toContain('Unable to load this section')
    expect(wrapper.text()).not.toContain('postgres')
    expect(wrapper.get('svg').classes()).toContain('motion-reduce:animate-none')
    expect(wrapper.find('[data-testid="radar-section-retry"]').exists()).toBe(false)
    expect(wrapper.get('[role="status"]').attributes('aria-live')).toBe('polite')
  })

  it('uses canonical allowlisted source links and exposes safe state metadata', () => {
    const wrapper = mount(DataSourceFooter, {
      props: {
        sources: [
          source({
            key: 'status_deepseek',
            name: 'DeepSeek Status',
            url: 'https://status.deepseek.com',
            platform: 'deepseek',
            platform_order: 1,
          }),
          source({
            key: 'unknown_source',
            name: 'Unknown',
            url: 'javascript:alert(1)',
            state: 'failed',
            is_healthy: false,
            stale: true,
            error: 'network_error',
          }),
        ],
      },
    })

    const link = wrapper.get('a')
    expect(link.attributes('href')).toBe('https://status.deepseek.com')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.findAll('a')).toHaveLength(1)
    expect(wrapper.text()).toContain('Every 6 hours')
    expect(wrapper.text()).toContain('Healthy')
    expect(wrapper.text()).toContain('Failed')
    expect(wrapper.text()).toContain('Data may be outdated')
    expect(wrapper.text()).toContain('statistical estimate')
    expect(wrapper.text()).not.toContain('javascript:')
  })

  it('lets long source metadata shrink and wrap without hiding any source details', () => {
    const longName = 'Artificial Analysis models with a deliberately long source label'
    const wrapper = mount(DataSourceFooter, {
      props: {
        sources: [source({
          key: 'aa',
          name: longName,
          next_fire_at: '2026-07-13T14:00:00.000Z',
        })],
      },
    })

    const article = wrapper.get('article')
    expect(article.classes()).toContain('min-w-0')

    const name = article.get('a span')
    expect(name.classes()).toContain('break-words')
    expect(name.classes()).toContain('[overflow-wrap:anywhere]')
    expect(name.text()).toBe(longName)

    const metadataRows = article.findAll('dl > div')
    expect(metadataRows).toHaveLength(3)
    for (const row of metadataRows) {
      expect(row.classes()).toContain('min-w-0')
      expect(row.classes()).toContain('grid-cols-1')
      expect(row.get('dt').classes()).toContain('min-w-0')
      expect(row.get('dd').classes()).toContain('min-w-0')
      expect(row.get('dd').classes()).toContain('break-words')
      expect(row.get('dd').text()).not.toBe('')
    }
    expect(wrapper.text()).toContain('Last attempt')
    expect(wrapper.text()).toContain('Last success')
    expect(wrapper.text()).toContain('Next scheduled run')
  })

  it.each([
    ['en', 'Sub2API Aggregated Usage'],
    ['zh', 'Sub2API 聚合用量'],
  ])('renders the internal quota source without inventing an external link in %s', (locale, expectedName) => {
    localeMock.value = locale
    const wrapper = mount(DataSourceFooter, {
      props: {
        sources: [source({
          key: 'quota_aggregator',
          name: 'Sub2API Aggregated Usage',
          url: '',
          interval: '15m',
          http_status: null,
        })],
      },
    })

    expect(wrapper.text()).toContain(expectedName)
    expect(wrapper.text()).toContain(locale === 'zh' ? '每隔 15分钟' : 'Every 15 minutes')
    expect(wrapper.find('a').exists()).toBe(false)
  })

  it('renders the quota aggregator failure as a bounded product message', () => {
    const wrapper = mount(DataSourceFooter, {
      props: {
        sources: [source({
          key: 'quota_aggregator',
          name: 'Sub2API Aggregated Usage',
          url: '',
          interval: '15m',
          state: 'failed',
          is_healthy: false,
          stale: true,
          error: 'aggregation_error',
        })],
      },
    })

    expect(wrapper.text()).toContain('Usage aggregation failed')
    expect(wrapper.text()).toContain('Data may be outdated')
    expect(wrapper.text()).not.toContain('redis')
  })

  it.each(['en', 'zh'])('locale-formats composite Go durations in %s and safely falls back', (locale) => {
    localeMock.value = locale
    const rawDuration = '12345h1m30s250ms'
    const invalidDuration = 'next-week'
    const wrapper = mount(DataSourceFooter, {
      props: {
        sources: [
          source({ interval: rawDuration }),
          source({ key: 'invalid', name: 'Invalid schedule', interval: invalidDuration }),
        ],
      },
    })
    const expectedDuration = locale === 'en'
      ? '12,345 hours, 1 minute, 30 seconds, and 250 milliseconds'
      : '12,345小时、1分钟、30秒钟和250毫秒'
    const prefix = locale === 'en' ? 'Every' : '每隔'

    expect(wrapper.text()).toContain(`${prefix} ${expectedDuration}`)
    expect(wrapper.text()).not.toContain(rawDuration)
    expect(wrapper.text()).toContain(invalidDuration)
  })

  it('does not claim source metadata is empty while it is loading', () => {
    const wrapper = mount(DataSourceFooter, {
      props: {
        sources: [],
        loading: true,
      },
    })

    expect(wrapper.text()).not.toContain('No source metadata available')
    expect(wrapper.text()).toContain('statistical estimates')
  })
})

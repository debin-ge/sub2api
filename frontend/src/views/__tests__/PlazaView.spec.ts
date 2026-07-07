import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'

import PlazaView from '../PlazaView.vue'

const fetchAll = vi.fn()
const open = vi.fn()
const close = vi.fn()

const channels = ref<any[]>([])
const pricingConfig = ref({
  multiplier: 1.5,
  rate: 6.8,
  paymentEnabled: true,
  balanceDisabled: false,
})
const loading = ref(false)
const error = ref('')
const isOpen = ref(false)
const currentModel = ref(null)

const messages: Record<string, string> = {
  'common.login': 'Login',
  'home.switchToLight': 'Switch to light mode',
  'home.switchToDark': 'Switch to dark mode',
  'plaza.header.label': 'Model Plaza',
  'plaza.header.dashboard': 'Dashboard',
  'plaza.header.register': 'Register',
  'plaza.hero.eyebrow': 'Public model catalog',
  'plaza.hero.title': 'Model Plaza',
  'plaza.hero.subtitle': 'Token prices are displayed per 1M tokens unless otherwise noted. CNY amounts are reference estimates based on public settings and do not change backend billing.',
  'plaza.hero.rateTag': '¥1 = ${rate}',
  'plaza.hero.boostValue': '{boost}x',
  'plaza.metrics.models': 'Models',
  'plaza.metrics.platforms': 'Providers',
  'plaza.metrics.boost': 'Recharge boost',
  'plaza.filters.title': 'Filters',
  'plaza.filters.subtitle': 'Filter models',
  'plaza.filters.search': 'Search models',
  'plaza.filters.searchPlaceholder': 'Search model name...',
  'plaza.filters.platform': 'Providers',
  'plaza.filters.allPlatforms': 'All',
  'plaza.filters.sort': 'Sort',
  'plaza.filters.sortPopularity': 'Trending',
  'plaza.filters.sortDefault': 'Name',
  'plaza.filters.sortInputAsc': 'Input price low to high',
  'plaza.filters.sortInputDesc': 'Input price high to low',
  'plaza.footer.scaleNote': 'Token prices are displayed per 1M tokens unless otherwise noted.',
  'plaza.footer.referenceRateDisclaimer': 'CNY amounts are reference estimates.',
  'plaza.footer.registerCta': 'Create account',
  'plaza.footer.rechargeCta': 'Recharge',
  'plaza.card.input': 'Input',
  'plaza.card.output': 'Output',
  'plaza.card.cacheWrite': 'Cache write',
  'plaza.card.cacheRead': 'Cache read',
  'plaza.card.imageOutput': 'Image output',
  'plaza.card.perRequest': 'Per request',
  'plaza.card.supportedChannels': '{n} channels',
  'plaza.card.viewDetails': 'View details',
  'plaza.card.billingPerToken': 'Pay per token',
  'plaza.card.billingPerRequest': 'Pay per request',
  'plaza.card.discountBadge': '{percent}% of reference',
  'plaza.card.notAvailable': 'N/A',
  'plaza.card.recentCalls': '{count} calls',
  'plaza.searchBar.total': '{total} models',
  'plaza.searchBar.filtered': '{visible}/{total} models',
  'plaza.infoBanner.text': 'Price varies by channel.',
  'plaza.modal.close': 'Close',
  'plaza.modal.fullPricing': 'Full pricing',
  'plaza.modal.input': 'Input',
  'plaza.modal.output': 'Output',
  'plaza.modal.cacheWrite': 'Cache write',
  'plaza.modal.cacheRead': 'Cache read',
  'plaza.modal.imageOutput': 'Image output',
  'plaza.modal.perRequest': 'Per request',
  'plaza.modal.supportedChannels': 'Channels supporting this model',
  'plaza.modal.tieredPricing': 'Tiered pricing',
  'plaza.modal.tierRange': '{min} - {max} tokens',
  'plaza.modal.tierRangeOpenEnded': '{min}+ tokens',
  'plaza.price.unitPerMillion': '/1M',
  'plaza.price.unitPerRequest': '/request',
  'plaza.platform.modelCount': '{n} models',
  'plaza.loading': 'Loading models...',
  'pagination.showing': 'Showing',
  'pagination.to': 'to',
  'pagination.of': 'of',
  'pagination.results': 'results',
  'pagination.previous': 'Previous',
  'pagination.next': 'Next',
  'pagination.pageOf': '{page} / {total}',
  'pagination.goToPage': 'Go to page {page}',
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
      locale: { value: 'en' },
    }),
  }
})

vi.mock('@/composables/usePlazaData', () => ({
  usePlazaData: () => ({
    channels,
    pricingConfig,
    loading,
    error,
    fetchAll,
  }),
}))

vi.mock('@/composables/useModelDetailModal', () => ({
  useModelDetailModal: () => ({
    isOpen,
    currentModel,
    open,
    close,
  }),
}))

function createPlazaChannel() {
  return {
    id: 1,
    name: 'Public Channel',
    description: 'Primary public channel',
    platforms: [
      {
        platform: 'openai',
        groups: [
          {
            id: 10,
            name: 'default',
            description: '',
            rate_multiplier: 1,
            daily_limit: null,
            weekly_limit: null,
            monthly_limit: null,
          },
        ],
        supported_models: [
          {
            name: 'gpt-4o-mini',
            pricing: {
              input_price: 0.15,
              output_price: 0.6,
              cache_write_price: null,
              cache_read_price: null,
              image_output_price: null,
              per_request_price: null,
            },
          },
        ],
      },
    ],
  }
}

function createManyModelChannel(count: number) {
  return {
    id: 2,
    name: 'Many Models',
    description: 'Channel with many public models',
    platforms: [
      {
        platform: 'anthropic',
        groups: [
          {
            id: 20,
            name: 'anthropic',
            description: '',
            rate_multiplier: 1,
            daily_limit: null,
            weekly_limit: null,
            monthly_limit: null,
          },
        ],
        supported_models: Array.from({ length: count }, (_, index) => ({
          name: `model-${String(index + 1).padStart(2, '0')}`,
          pricing: {
            input_price: 0.000001 * (index + 1),
            output_price: 0.000002 * (index + 1),
            cache_write_price: null,
            cache_read_price: null,
            image_output_price: null,
            per_request_price: null,
          },
          recent_call_count: count - index,
          recent_call_window_seconds: 604800,
        })),
      },
    ],
  }
}

async function mountPlaza() {
  const pinia = createPinia()
  setActivePinia(pinia)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/plaza', component: PlazaView },
      { path: '/login', component: { template: '<div>Login</div>' } },
      { path: '/register', component: { template: '<div>Register</div>' } },
      { path: '/dashboard', component: { template: '<div>Dashboard</div>' } },
      { path: '/purchase', component: { template: '<div>Purchase</div>' } },
    ],
  })
  router.push('/plaza')
  await router.isReady()

  const wrapper = mount(PlazaView, {
    global: {
      plugins: [router, pinia],
      stubs: {
        LocaleSwitcher: true,
        Teleport: true,
      },
    },
  })

  await nextTick()
  await nextTick()
  return wrapper
}

describe('PlazaView', () => {
  beforeEach(() => {
    fetchAll.mockReset()
    open.mockReset()
    close.mockReset()
    channels.value = [createPlazaChannel()]
    pricingConfig.value = {
      multiplier: 1.5,
      rate: 6.8,
      paymentEnabled: true,
      balanceDisabled: false,
    }
    loading.value = false
    error.value = ''
    isOpen.value = false
    currentModel.value = null
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders title, model, and recharge value boost', async () => {
    const wrapper = await mountPlaza()

    expect(fetchAll).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('Model Plaza')
    expect(wrapper.text()).toContain('¥1 = $1.5')
    expect(wrapper.text()).toContain('Token prices are displayed per 1M tokens unless otherwise noted.')
    expect(wrapper.text()).toContain('CNY amounts are reference estimates based on public settings and do not change backend billing.')
    expect(wrapper.text()).not.toContain('Browse public models without signing in')
    expect(wrapper.find('footer').exists()).toBe(false)
    expect(wrapper.text()).toContain('gpt-4o-mini')
    expect(wrapper.text()).toContain('10.2x')
    expect(wrapper.text()).not.toContain('750%')
    expect(wrapper.text()).toContain('1 models')
  })

  it('renders public models without available channels feature state', async () => {
    const wrapper = await mountPlaza()

    expect(wrapper.text()).toContain('gpt-4o-mini')
    expect(wrapper.text()).not.toContain('Model Plaza is unavailable')
  })

  it('renders search and filters above a four-column card grid', async () => {
    const wrapper = await mountPlaza()

    expect(wrapper.find('aside').exists()).toBe(false)
    expect(wrapper.find('input[type="search"]').exists()).toBe(true)
    expect(wrapper.find('select[aria-label="Sort"]').exists()).toBe(true)
    expect(wrapper.find('select[aria-label="Providers"]').exists()).toBe(true)
    expect(wrapper.find('.xl\\:grid-cols-4').exists()).toBe(true)
  })

  it('renders the localized loading state', async () => {
    loading.value = true

    const wrapper = await mountPlaza()

    expect(wrapper.text()).toContain('Loading models...')
    expect(wrapper.text()).not.toContain('模型加载中')
  })

  it('renders only the current page after sorting and filtering', async () => {
    channels.value = [createManyModelChannel(25)]

    const wrapper = await mountPlaza()

    expect(wrapper.text()).toContain('model-01')
    expect(wrapper.text()).toContain('model-20')
    expect(wrapper.text()).not.toContain('model-21')
    expect(wrapper.text()).toContain('Showing')
    expect(wrapper.text()).toContain('1')
    expect(wrapper.text()).toContain('20')
    expect(wrapper.text()).toContain('25')

    await wrapper.get('button[aria-label="Next"]').trigger('click')
    await nextTick()

    expect(wrapper.text()).toContain('model-21')
    expect(wrapper.text()).toContain('model-25')
    expect(wrapper.text()).not.toContain('model-01')
  })
})

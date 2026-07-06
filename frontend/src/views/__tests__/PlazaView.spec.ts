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
  multiplier: 1.25,
  rate: 6.8,
  paymentEnabled: true,
  balanceDisabled: false,
  availableChannelsEnabled: true,
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
  'plaza.hero.subtitle': 'Browse public models',
  'plaza.metrics.models': 'Models',
  'plaza.metrics.platforms': 'Platforms',
  'plaza.metrics.boost': 'Recharge value boost',
  'plaza.filters.search': 'Search models',
  'plaza.filters.searchPlaceholder': 'Search model or display name',
  'plaza.filters.platform': 'Platform',
  'plaza.filters.allPlatforms': 'All platforms',
  'plaza.filters.sort': 'Sort',
  'plaza.filters.sortDefault': 'Name',
  'plaza.filters.sortInputAsc': 'Input price low to high',
  'plaza.filters.sortInputDesc': 'Input price high to low',
  'plaza.footer.scaleNote': 'Token prices are displayed per 1M tokens unless otherwise noted.',
  'plaza.footer.referenceRateDisclaimer': 'CNY amounts are reference estimates.',
  'plaza.footer.registerCta': 'Create account',
  'plaza.footer.rechargeCta': 'Recharge',
}

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
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
      multiplier: 1.25,
      rate: 6.8,
      paymentEnabled: true,
      balanceDisabled: false,
      availableChannelsEnabled: true,
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
    expect(wrapper.text()).toContain('gpt-4o-mini')
    expect(wrapper.text()).toContain('750%')
  })
})

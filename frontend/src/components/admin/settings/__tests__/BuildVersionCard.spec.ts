import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import BuildVersionCard from '../BuildVersionCard.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const mocks = vi.hoisted(() => ({ getBuildInfo: vi.fn() }))
vi.mock('@/api/admin/system', () => mocks)

function setup(locale: 'en' | 'zh' = 'zh') {
  const i18n = createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { en, zh } })
  return mount(BuildVersionCard, { global: { plugins: [i18n] } })
}

describe('BuildVersionCard', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows the project tag and the merged upstream version', async () => {
    mocks.getBuildInfo.mockResolvedValue({
      version: '2.1.12',
      upstream_version: '0.2.23',
      commit: '0a45313260f1e2d3c4b5a69788796a5b4c3d2e1f',
      build_date: 'unknown',
      build_type: 'release'
    })
    const wrapper = setup()
    await flushPromises()

    expect(wrapper.text()).toContain('版本信息')
    expect(wrapper.get('[data-test="build-version"]').text()).toContain('项目版本')
    expect(wrapper.get('[data-test="build-version"] dd').text()).toBe('2.1.12')
    expect(wrapper.get('[data-test="build-upstream"] dd').text()).toBe('v0.2.23')
    expect(wrapper.get('[data-test="build-commit"] dd').text()).toBe('0a4531326')
    expect(wrapper.get('[data-test="build-date"] dd').text()).toBe('—')
  })

  it('never prefixes the project version with v, even if the build passed one', async () => {
    mocks.getBuildInfo.mockResolvedValue({
      version: 'v2.1.12',
      upstream_version: 'v0.2.23',
      commit: 'docker',
      build_date: '',
      build_type: 'release'
    })
    const wrapper = setup('en')
    await flushPromises()

    expect(wrapper.get('[data-test="build-version"] dd').text()).toBe('2.1.12')
    expect(wrapper.get('[data-test="build-upstream"] dd').text()).toBe('v0.2.23')
  })

  it('keeps non-numeric versions such as test builds as-is', async () => {
    mocks.getBuildInfo.mockResolvedValue({
      version: 'test',
      upstream_version: '',
      commit: 'docker',
      build_date: '',
      build_type: 'release'
    })
    const wrapper = setup('en')
    await flushPromises()

    expect(wrapper.get('[data-test="build-version"] dd').text()).toBe('test')
    expect(wrapper.get('[data-test="build-upstream"] dd').text()).toBe('—')
    expect(wrapper.get('[data-test="build-commit"] dd').text()).toBe('docker')
  })

  it('shows an error when the endpoint fails', async () => {
    mocks.getBuildInfo.mockRejectedValue(new Error('boom'))
    const wrapper = setup('en')
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toBe('Failed to load version info')
  })
})

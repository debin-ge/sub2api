import { mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAutoRefresh } from '@/composables/useAutoRefresh'

interface HarnessBindings {
  fetching: Readonly<{ value: boolean }>
  setEnabled: (value: boolean) => void
}

function mountHarness(onRefresh: () => Promise<void> | void): {
  wrapper: VueWrapper
  autoRefresh: HarnessBindings
} {
  let autoRefresh!: HarnessBindings
  const wrapper = mount(defineComponent({
    setup() {
      autoRefresh = useAutoRefresh({
        storageKey: 'auto-refresh-test',
        intervals: [1],
        defaultInterval: 1,
        onRefresh,
      })
      return () => null
    },
  }))
  return { wrapper, autoRefresh }
}

describe('useAutoRefresh', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it.each([
    {
      name: 'synchronous throws',
      firstFailure: () => {
        throw new Error('sync refresh failed')
      },
    },
    {
      name: 'asynchronous rejections',
      firstFailure: () => Promise.reject(new Error('async refresh failed')),
    },
  ])('contains $name at the timer boundary and continues refreshing', async ({ firstFailure }) => {
    const unhandled = vi.fn()
    window.addEventListener('unhandledrejection', unhandled)
    const onRefresh = vi.fn()
      .mockImplementationOnce(firstFailure)
      .mockResolvedValue(undefined)
    const { wrapper, autoRefresh } = mountHarness(onRefresh)

    autoRefresh.setEnabled(true)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(autoRefresh.fetching.value).toBe(false)
    expect(unhandled).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(1_000)
    expect(onRefresh).toHaveBeenCalledTimes(2)
    expect(autoRefresh.fetching.value).toBe(false)
    expect(unhandled).not.toHaveBeenCalled()

    wrapper.unmount()
    window.removeEventListener('unhandledrejection', unhandled)
  })
})

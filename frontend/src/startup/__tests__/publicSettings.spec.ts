import { afterEach, describe, expect, it, vi } from 'vitest'
import { initializePublicSettings } from '@/startup/publicSettings'

describe('initializePublicSettings', () => {
  afterEach(() => {
    delete window.__APP_CONFIG_STALE__
  })

  it('uses injected settings without making an API request', async () => {
    const store = {
      initFromInjectedConfig: vi.fn(() => true),
      fetchPublicSettings: vi.fn()
    }

    await initializePublicSettings(store)

    expect(store.initFromInjectedConfig).toHaveBeenCalledOnce()
    expect(store.fetchPublicSettings).not.toHaveBeenCalled()
  })

  it('force-refreshes last-known-good injected settings before mount', async () => {
    window.__APP_CONFIG_STALE__ = true
    const store = {
      initFromInjectedConfig: vi.fn(() => true),
      fetchPublicSettings: vi.fn().mockResolvedValue(undefined)
    }

    await initializePublicSettings(store)

    expect(store.fetchPublicSettings).toHaveBeenCalledOnce()
    expect(store.fetchPublicSettings).toHaveBeenCalledWith(true)
  })

  it('waits for public settings when no injected config is available', async () => {
    let resolveRequest!: () => void
    const request = new Promise<void>((resolve) => {
      resolveRequest = resolve
    })
    const store = {
      initFromInjectedConfig: vi.fn(() => false),
      fetchPublicSettings: vi.fn(() => request)
    }
    const initialized = vi.fn()

    const result = initializePublicSettings(store).then(initialized)
    await Promise.resolve()

    expect(store.fetchPublicSettings).toHaveBeenCalledOnce()
    expect(store.fetchPublicSettings).toHaveBeenCalledWith(false)
    expect(initialized).not.toHaveBeenCalled()

    resolveRequest()
    await result
    expect(initialized).toHaveBeenCalledOnce()
  })

  it('does not prevent bootstrap when loading settings fails unexpectedly', async () => {
    const error = new Error('network unavailable')
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    const store = {
      initFromInjectedConfig: vi.fn(() => false),
      fetchPublicSettings: vi.fn().mockRejectedValue(error)
    }

    await expect(initializePublicSettings(store)).resolves.toBeUndefined()
    expect(consoleError).toHaveBeenCalledWith(
      'Failed to initialize public settings before mount:',
      error
    )

    consoleError.mockRestore()
  })
})

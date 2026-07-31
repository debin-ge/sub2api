import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Router } from 'vue-router'
import { finishAuthenticatedNavigation } from '@/utils/authNavigation'
import { clearChunkReloadMarker } from '@/utils/chunkLoadRecovery'

describe('finishAuthenticatedNavigation', () => {
  beforeEach(() => {
    clearChunkReloadMarker()
  })

  it('shows success only after navigation completes', async () => {
    const order: string[] = []
    const router = {
      replace: vi.fn(async () => {
        order.push('navigate')
      })
    } as unknown as Router

    const result = await finishAuthenticatedNavigation({
      router,
      redirectTo: '/dashboard',
      onSuccess: () => order.push('success'),
      onNavigationFailure: () => order.push('failure')
    })

    expect(result).toBe('completed')
    expect(order).toEqual(['navigate', 'success'])
  })

  it('recovers a chunk failure without reporting an authentication failure', async () => {
    const onSuccess = vi.fn()
    const onNavigationFailure = vi.fn()
    const recoverChunkError = vi.fn(() => 'reloading' as const)
    const error = new Error("'text/html' is not a valid JavaScript MIME type.")
    const router = {
      replace: vi.fn().mockRejectedValue(error)
    } as unknown as Router

    const result = await finishAuthenticatedNavigation({
      router,
      redirectTo: '/dashboard',
      onSuccess,
      onNavigationFailure,
      recoverChunkError
    })

    expect(result).toBe('reloading')
    expect(recoverChunkError).toHaveBeenCalledWith(error)
    expect(onSuccess).not.toHaveBeenCalled()
    expect(onNavigationFailure).not.toHaveBeenCalled()
  })

  it('reports non-chunk navigation failures separately', async () => {
    const error = new Error('navigation guard failed')
    const onSuccess = vi.fn()
    const onNavigationFailure = vi.fn()
    const router = {
      replace: vi.fn().mockRejectedValue(error)
    } as unknown as Router

    const result = await finishAuthenticatedNavigation({
      router,
      redirectTo: '/dashboard',
      onSuccess,
      onNavigationFailure
    })

    expect(result).toBe('failed')
    expect(onSuccess).not.toHaveBeenCalled()
    expect(onNavigationFailure).toHaveBeenCalledWith(error)
  })

  it('requests a visible update warning when chunk loading still fails after reload', async () => {
    const onSuccess = vi.fn()
    const onNavigationFailure = vi.fn()
    const updateRequired = vi.fn()
    window.addEventListener('frontend-update-required', updateRequired)
    const router = {
      replace: vi.fn().mockRejectedValue(new Error('Importing a module script failed.'))
    } as unknown as Router
    sessionStorage.setItem('frontend_chunk_reload_attempted', String(Date.now()))

    const result = await finishAuthenticatedNavigation({
      router,
      redirectTo: '/dashboard',
      onSuccess,
      onNavigationFailure
    })

    expect(result).toBe('update-required')
    expect(updateRequired).toHaveBeenCalledOnce()
    expect(onSuccess).not.toHaveBeenCalled()
    expect(onNavigationFailure).not.toHaveBeenCalled()

    window.removeEventListener('frontend-update-required', updateRequired)
  })
})

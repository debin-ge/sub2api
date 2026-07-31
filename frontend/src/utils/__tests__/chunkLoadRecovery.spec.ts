import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  clearChunkReloadMarker,
  isChunkLoadError,
  isFrontendUpdateRequired,
  notifyFrontendUpdateRequired,
  recoverFromChunkLoadError
} from '@/utils/chunkLoadRecovery'

describe('chunkLoadRecovery', () => {
  beforeEach(() => {
    clearChunkReloadMarker()
  })

  it.each([
    new Error('Failed to fetch dynamically imported module: /assets/Dashboard.js'),
    new Error('Error loading dynamically imported module'),
    new Error('Importing a module script failed.'),
    new Error("'text/html' is not a valid JavaScript MIME type."),
    new Error(
      'Failed to load module script: Expected a JavaScript-or-Wasm module script but the server responded with a MIME type of "text/html".'
    ),
    Object.assign(new Error('chunk failed'), { name: 'ChunkLoadError' })
  ])('recognizes browser chunk loading errors', (error) => {
    expect(isChunkLoadError(error)).toBe(true)
  })

  it('does not classify ordinary application errors as chunk failures', () => {
    expect(isChunkLoadError(new Error('Request failed with status code 500'))).toBe(false)
  })

  it('reloads once for the first chunk failure', () => {
    const reload = vi.fn()

    expect(
      recoverFromChunkLoadError(
        new Error("'text/html' is not a valid JavaScript MIME type."),
        {
          now: () => 1_000,
          reload
        }
      )
    ).toBe('reloading')
    expect(reload).toHaveBeenCalledOnce()
  })

  it('deduplicates recovery handlers in the same document', () => {
    const reload = vi.fn()
    const error = new Error('Importing a module script failed.')

    expect(
      recoverFromChunkLoadError(error, {
        now: () => 1_000,
        reload
      })
    ).toBe('reloading')
    expect(
      recoverFromChunkLoadError(error, {
        now: () => 2_000,
        reload
      })
    ).toBe('reloading')
    expect(reload).toHaveBeenCalledOnce()
  })

  it('does not enter a reload loop when the reloaded document still cannot load the chunk', () => {
    const reload = vi.fn()
    sessionStorage.setItem('frontend_chunk_reload_attempted', '1000')

    expect(
      recoverFromChunkLoadError(new Error('Importing a module script failed.'), {
        now: () => 2_000,
        reload
      })
    ).toBe('already-reloaded')
    expect(reload).not.toHaveBeenCalled()
  })

  it('allows a later recovery attempt after the window expires', () => {
    const reload = vi.fn()
    sessionStorage.setItem('frontend_chunk_reload_attempted', '1000')

    expect(
      recoverFromChunkLoadError(new Error('Loading chunk 42 failed'), {
        now: () => 61_001,
        reload
      })
    ).toBe('reloading')
    expect(reload).toHaveBeenCalledOnce()
  })

  it('retains and deduplicates the update-required state for late listeners', () => {
    const updateRequired = vi.fn()
    window.addEventListener('frontend-update-required', updateRequired)

    notifyFrontendUpdateRequired()
    notifyFrontendUpdateRequired()

    expect(isFrontendUpdateRequired()).toBe(true)
    expect(updateRequired).toHaveBeenCalledOnce()

    window.removeEventListener('frontend-update-required', updateRequired)
  })
})

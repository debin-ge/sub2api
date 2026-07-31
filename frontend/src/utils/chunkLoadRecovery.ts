const CHUNK_RELOAD_KEY = 'frontend_chunk_reload_attempted'
const CHUNK_RELOAD_WINDOW_MS = 60_000
let reloadRequestedInCurrentDocument = false
let frontendUpdateRequired = false

const CHUNK_ERROR_PATTERNS = [
  'failed to fetch dynamically imported module',
  'error loading dynamically imported module',
  'importing a module script failed',
  'loading chunk',
  'loading css chunk',
  'unable to preload css',
  'is not a valid javascript mime type',
  'expected a javascript-or-wasm module script'
]

export const FRONTEND_UPDATE_REQUIRED_EVENT = 'frontend-update-required'

export type ChunkRecoveryResult =
  | 'not-chunk-error'
  | 'reloading'
  | 'already-reloaded'

interface ChunkRecoveryOptions {
  now?: () => number
  reload?: () => void
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }

  if (error && typeof error === 'object' && 'message' in error) {
    const message = (error as { message?: unknown }).message
    return typeof message === 'string' ? message : String(message ?? '')
  }

  return String(error ?? '')
}

export function isChunkLoadError(error: unknown): boolean {
  const name =
    error && typeof error === 'object' && 'name' in error
      ? String((error as { name?: unknown }).name ?? '')
      : ''
  const message = errorMessage(error).toLowerCase()

  return (
    name === 'ChunkLoadError' ||
    CHUNK_ERROR_PATTERNS.some((pattern) => message.includes(pattern))
  )
}

export function recoverFromChunkLoadError(
  error: unknown,
  options: ChunkRecoveryOptions = {}
): ChunkRecoveryResult {
  if (!isChunkLoadError(error)) {
    return 'not-chunk-error'
  }

  // A single failed dynamic import can be observed by Vite's preload handler,
  // Vue Router's global error handler, and the caller awaiting router.replace().
  // Treat all handlers in the same document as the same recovery attempt.
  if (reloadRequestedInCurrentDocument) {
    return 'reloading'
  }

  const now = options.now?.() ?? Date.now()
  const previousAttempt = Number(sessionStorage.getItem(CHUNK_RELOAD_KEY) || 0)

  if (!previousAttempt || now - previousAttempt > CHUNK_RELOAD_WINDOW_MS) {
    sessionStorage.setItem(CHUNK_RELOAD_KEY, String(now))
    reloadRequestedInCurrentDocument = true
    const reload = options.reload ?? (() => window.location.reload())
    reload()
    return 'reloading'
  }

  return 'already-reloaded'
}

export function clearChunkReloadMarker(): void {
  sessionStorage.removeItem(CHUNK_RELOAD_KEY)
  reloadRequestedInCurrentDocument = false
  frontendUpdateRequired = false
}

export function notifyFrontendUpdateRequired(): void {
  if (frontendUpdateRequired) {
    return
  }

  frontendUpdateRequired = true
  window.dispatchEvent(new CustomEvent(FRONTEND_UPDATE_REQUIRED_EVENT))
}

export function isFrontendUpdateRequired(): boolean {
  return frontendUpdateRequired
}

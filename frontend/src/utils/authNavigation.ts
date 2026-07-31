import type { RouteLocationRaw, Router } from 'vue-router'
import {
  notifyFrontendUpdateRequired,
  recoverFromChunkLoadError,
  type ChunkRecoveryResult
} from '@/utils/chunkLoadRecovery'

export type AuthNavigationResult =
  | 'completed'
  | 'reloading'
  | 'update-required'
  | 'failed'

interface FinishAuthenticatedNavigationOptions {
  router: Router
  redirectTo: RouteLocationRaw
  onSuccess: () => void
  onNavigationFailure: (error: unknown) => void
  recoverChunkError?: (error: unknown) => ChunkRecoveryResult
}

/**
 * Navigate after authentication without allowing route/chunk failures to be
 * mistaken for authentication failures.
 */
export async function finishAuthenticatedNavigation({
  router,
  redirectTo,
  onSuccess,
  onNavigationFailure,
  recoverChunkError = recoverFromChunkLoadError
}: FinishAuthenticatedNavigationOptions): Promise<AuthNavigationResult> {
  try {
    await router.replace(redirectTo)
    onSuccess()
    return 'completed'
  } catch (error) {
    const recovery = recoverChunkError(error)

    if (recovery === 'reloading') {
      return 'reloading'
    }

    if (recovery === 'already-reloaded') {
      notifyFrontendUpdateRequired()
      return 'update-required'
    }

    onNavigationFailure(error)
    return 'failed'
  }
}

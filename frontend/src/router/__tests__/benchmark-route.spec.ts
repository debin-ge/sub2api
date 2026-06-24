import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    customMenuItems: [],
    fetch: vi.fn(),
  }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

describe('benchmark admin routes', () => {
  it('registers all benchmark admin routes as admin-only', async () => {
    const { default: router } = await import('@/router')
    const expectedRoutes = [
      ['AdminBenchmarkDashboard', '/admin/benchmark/dashboard'],
      ['AdminBenchmarkTargets', '/admin/benchmark/targets'],
      ['AdminBenchmarkTasks', '/admin/benchmark/tasks'],
      ['AdminBenchmarkProfiles', '/admin/benchmark/profiles'],
      ['AdminBenchmarkRuns', '/admin/benchmark/runs'],
      ['AdminBenchmarkRunDetail', '/admin/benchmark/runs/:id'],
    ] as const

    for (const [name, path] of expectedRoutes) {
      const route = router.getRoutes().find((record) => record.name === name)
      expect(route?.path).toBe(path)
      expect(route?.meta.requiresAuth).toBe(true)
      expect(route?.meta.requiresAdmin).toBe(true)
    }
  })
})

describe('benchmark admin sidebar group', () => {
  it('defines a dedicated model radar group with benchmark children', () => {
    const componentPath = resolve(
      dirname(fileURLToPath(import.meta.url)),
      '../../components/layout/AppSidebar.vue'
    )
    const componentSource = readFileSync(componentPath, 'utf8')

    expect(componentSource).toContain("path: '/admin/benchmark'")
    expect(componentSource).toContain("label: t('nav.modelRadar')")
    expect(componentSource).toContain("path: '/admin/benchmark/dashboard'")
    expect(componentSource).toContain("path: '/admin/benchmark/targets'")
    expect(componentSource).toContain("path: '/admin/benchmark/tasks'")
    expect(componentSource).toContain("path: '/admin/benchmark/profiles'")
    expect(componentSource).toContain("path: '/admin/benchmark/runs'")
  })
})

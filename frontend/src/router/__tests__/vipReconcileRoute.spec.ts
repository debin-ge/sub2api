import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const routerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts')
const routerSource = readFileSync(routerPath, 'utf8')

describe('administrator VIP reconciliation route', () => {
  it('registers the protected, lazy-loaded management page', () => {
    expect(routerSource).toContain("path: '/admin/users/vip-reconcile'")
    expect(routerSource).toContain("name: 'AdminVIPReconcile'")
    expect(routerSource).toContain("component: () => import('@/views/admin/VIPReconcileView.vue')")
    expect(routerSource).toContain("titleKey: 'admin.vipReconcile.title'")
    expect(routerSource).toContain("descriptionKey: 'admin.vipReconcile.description'")
  })
})

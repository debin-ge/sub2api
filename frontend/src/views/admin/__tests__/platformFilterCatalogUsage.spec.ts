import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

function readSource(path: string): string {
  return readFileSync(resolve(path), 'utf8')
}

describe('admin platform filters', () => {
  it('uses the creatable (glm-excluded) group platform catalog on the subscriptions page', () => {
    const source = readSource('src/views/admin/SubscriptionsView.vue')
    expect(source).toContain("import { CREATABLE_GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'")
    expect(source).toMatch(/const platformFilterOptions[\s\S]*?\.\.\.CREATABLE_GROUP_PLATFORM_OPTIONS/)
  })

  it('uses the shared catalogs on the groups page', () => {
    const source = readSource('src/views/admin/GroupsView.vue')
    // The disabled edit-form platform Select must still show a legacy glm group's real value.
    expect(source).toMatch(/const platformOptions[\s\S]*?GROUP_PLATFORM_OPTIONS\.filter/)
    // The platform *filter* dropdown must not offer glm as a selectable choice —
    // glm is a legacy platform ID with no live entities left to find via the filter.
    expect(source).toContain('...CREATABLE_GROUP_PLATFORM_OPTIONS')
    // Composite route target platforms use the creatable (glm-excluded) catalog —
    // glm is a legacy platform ID that can no longer be assigned to new entities.
    expect(source).toContain('...CREATABLE_PLATFORM_OPTIONS')
  })

  it('excludes glm from the account and ops dashboard platform filters', () => {
    for (const path of [
      'src/components/admin/account/AccountTableFilters.vue',
      'src/views/admin/ops/components/OpsDashboardHeader.vue'
    ]) {
      const source = readSource(path)
      expect(source).toContain("import { CREATABLE_PLATFORM_OPTIONS } from '@/constants/platforms'")
      expect(source).toMatch(/platformOptions\s*=.*CREATABLE_PLATFORM_OPTIONS|pOpts.*\.\.\.CREATABLE_PLATFORM_OPTIONS/s)
    }
  })

  it('keeps the full concrete catalog (including glm) for the error-passthrough rule matcher', () => {
    // Error-passthrough rules match error *responses* by platform, not accounts/groups by
    // identity, and the backend places no restriction on which platform strings they may
    // reference — a rule may legitimately still want to match legacy glm-platform errors.
    const source = readSource('src/components/admin/ErrorPassthroughRulesModal.vue')
    expect(source).toContain("import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'")
    expect(source).toMatch(/platformOptions\s*=\s*CONCRETE_PLATFORM_OPTIONS/)
  })
})

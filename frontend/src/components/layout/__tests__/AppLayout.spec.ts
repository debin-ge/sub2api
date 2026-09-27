import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(resolve(here, '../AppLayout.vue'), 'utf8')

describe('AppLayout shell', () => {
  it('composes the rail, the context bar and the page head', () => {
    expect(source).toContain('<SideRail />')
    expect(source).toContain('<ContextBar />')
    expect(source).toContain('<PageHead v-if="pageHead"')
  })

  it('lets views opt out of the default page head and pass actions', () => {
    expect(source).toContain('withDefaults(defineProps<{ pageHead?: boolean }>(), { pageHead: true })')
    expect(source).toContain('<slot name="actions" />')
  })

  it('keeps the onboarding tour mount point', () => {
    expect(source).toContain('useOnboardingTour({')
    expect(source).toContain('onboardingStore.setReplayCallback(replayTour)')
  })

  it('does not paint the legacy mesh gradient', () => {
    expect(source).not.toContain('bg-mesh-gradient')
  })
})

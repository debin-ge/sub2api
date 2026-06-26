import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const benchmarkDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const targetsSource = readFileSync(resolve(benchmarkDir, 'BenchmarkTargetsView.vue'), 'utf8')
const tasksSource = readFileSync(resolve(benchmarkDir, 'BenchmarkTasksView.vue'), 'utf8')

describe('benchmark localization source coverage', () => {
  it('localizes benchmark targets view headings, actions, empty state, and toasts via t(...)', () => {
    expect(targetsSource).toContain('useI18n')
    expect(targetsSource).toContain("t('benchmark.admin.targets.title')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.refresh')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.emptyTitle')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.createSuccess')")
  })

  it('localizes benchmark tasks view headings, actions, empty state, and toasts via t(...)', () => {
    expect(tasksSource).toContain('useI18n')
    expect(tasksSource).toContain("t('benchmark.admin.tasks.title')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.refresh')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.emptyTitle')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.createSuccess')")
  })
})

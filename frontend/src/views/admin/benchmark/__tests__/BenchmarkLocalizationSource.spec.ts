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
    expect(targetsSource).toContain("t('benchmark.admin.targets.edit')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.updateSuccess')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.updateError')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.deleteSuccess')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.deleteError')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.deleteConfirm')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.fields.modelName')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.fields.channelId')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.fields.channelSnapshot')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.fields.publicVisible')")
    expect(targetsSource).toContain("t('benchmark.admin.targets.fields.sortOrder')")
  })

  it('localizes benchmark tasks view headings, actions, empty state, and toasts via t(...)', () => {
    expect(tasksSource).toContain('useI18n')
    expect(tasksSource).toContain("t('benchmark.admin.tasks.title')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.refresh')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.emptyTitle')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.createSuccess')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.edit')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.update')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.customType')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.updateSuccess')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.updateError')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.deleteSuccess')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.deleteError')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.deleteConfirm')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.jsonError',")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.customType')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.difficulty')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.inputPayload')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.expectedOutput')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.verifierConfig')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.publicPrompt')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.enabled')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.fields.sortOrder')")
    expect(tasksSource).toContain("t('benchmark.admin.tasks.columns.actions')")
  })
})

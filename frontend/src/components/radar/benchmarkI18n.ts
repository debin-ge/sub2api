import type { BenchmarkResultStatus, BenchmarkRunStatus, BenchmarkTaskScale } from '@/types/benchmark'

type BenchmarkTranslator = (key: string, params?: Record<string, unknown>) => string

const benchmarkTaskTypeKeys: Record<string, string> = {
  reasoning: 'benchmark.enums.taskType.reasoning',
  coding: 'benchmark.enums.taskType.coding',
  writing: 'benchmark.enums.taskType.writing',
}

const benchmarkRunStatusKeys: Record<BenchmarkRunStatus, string> = {
  pending: 'benchmark.enums.runStatus.pending',
  selecting: 'benchmark.enums.runStatus.selecting',
  queued: 'benchmark.enums.runStatus.queued',
  running: 'benchmark.enums.runStatus.running',
  scoring: 'benchmark.enums.runStatus.scoring',
  snapshotting: 'benchmark.enums.runStatus.snapshotting',
  completed: 'benchmark.enums.runStatus.completed',
  failed: 'benchmark.enums.runStatus.failed',
  canceled: 'benchmark.enums.runStatus.canceled',
}

const benchmarkResultStatusKeys: Record<BenchmarkResultStatus, string> = {
  pending: 'benchmark.enums.resultStatus.pending',
  running: 'benchmark.enums.resultStatus.running',
  scored: 'benchmark.enums.resultStatus.scored',
  failed: 'benchmark.enums.resultStatus.failed',
  timeout: 'benchmark.enums.resultStatus.timeout',
  channel_error: 'benchmark.enums.resultStatus.channel_error',
  parse_error: 'benchmark.enums.resultStatus.parse_error',
  rate_limited: 'benchmark.enums.resultStatus.rate_limited',
  verifier_error: 'benchmark.enums.resultStatus.verifier_error',
  skipped: 'benchmark.enums.resultStatus.skipped',
}

const benchmarkTaskScaleKeys: Record<BenchmarkTaskScale, string> = {
  small: 'benchmark.enums.taskScale.small',
  medium: 'benchmark.enums.taskScale.medium',
  full: 'benchmark.enums.taskScale.full',
  custom: 'benchmark.enums.taskScale.custom',
}

export function formatBenchmarkDateTime(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export function formatBenchmarkCompactDateTime(value: string, locale: string): string {
  return new Intl.DateTimeFormat(locale, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(new Date(value))
}

export function formatBenchmarkInteger(value: number, locale: string): string {
  return new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  }).format(value)
}

export function benchmarkTaskTypeLabel(taskType: string, t: BenchmarkTranslator): string {
  const key = benchmarkTaskTypeKeys[taskType]
  return key ? t(key) : taskType
}

export function benchmarkRunStatusLabel(status: BenchmarkRunStatus, t: BenchmarkTranslator): string {
  return t(benchmarkRunStatusKeys[status] || 'benchmark.enums.runStatus.pending')
}

export function benchmarkResultStatusLabel(status: BenchmarkResultStatus, t: BenchmarkTranslator): string {
  return t(benchmarkResultStatusKeys[status] || 'benchmark.enums.resultStatus.pending')
}

export function benchmarkTaskScaleLabel(scale: BenchmarkTaskScale, t: BenchmarkTranslator): string {
  return t(benchmarkTaskScaleKeys[scale] || 'benchmark.enums.taskScale.medium')
}

export function benchmarkEnabledLabel(enabled: boolean, t: BenchmarkTranslator): string {
  return enabled ? t('benchmark.enums.state.enabled') : t('benchmark.enums.state.disabled')
}

export function benchmarkVisibilityLabel(publicVisible: boolean, t: BenchmarkTranslator): string {
  return publicVisible ? t('benchmark.enums.visibility.public') : t('benchmark.enums.visibility.private')
}

export function benchmarkRunFallback(id: number, t: BenchmarkTranslator): string {
  return t('benchmark.fallback.run', { id })
}

export function benchmarkProfileFallback(id: number, t: BenchmarkTranslator): string {
  return t('benchmark.fallback.profile', { id })
}

export function benchmarkTargetFallback(id: number, t: BenchmarkTranslator): string {
  return t('benchmark.fallback.target', { id })
}

export function benchmarkChannelFallback(id: number, t: BenchmarkTranslator): string {
  return t('benchmark.fallback.channel', { id })
}

export function benchmarkResultFallback(id: number, t: BenchmarkTranslator): string {
  return t('benchmark.fallback.result', { id })
}

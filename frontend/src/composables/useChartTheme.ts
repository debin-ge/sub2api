/**
 * Chart.js 统一主题：细线 2px、面积填充 10%、发丝网格、等宽刻度字体、
 * 类别色使用已通过色盲相邻校验的六色（浅 / 深各一套）；主题切换时 theme 会变化，
 * 图表组件在 computed 里引用即可自动重算 options / datasets。
 *
 * 只改颜色与线型，不改任何图表的数据、轴数量与交互。
 */
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'

export const CHART_SERIES_LIGHT = ['#4f46e5', '#d97706', '#0891b2', '#e11d48', '#7c3aed', '#059669'] as const
export const CHART_SERIES_DARK = ['#6366f1', '#d97706', '#0891b2', '#f43f5e', '#8b5cf6', '#059669'] as const
export const CHART_SERIES_EXTENDED_LIGHT = [...CHART_SERIES_LIGHT, '#0ea5e9', '#f97316', '#14b8a6', '#a855f7', '#84cc16', '#ec4899'] as const
export const CHART_SERIES_EXTENDED_DARK = [...CHART_SERIES_DARK, '#38bdf8', '#fb923c', '#2dd4bf', '#c084fc', '#a3e635', '#f472b6'] as const
export const CHART_MONO = 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace'

const isDark = ref(typeof document !== 'undefined' && document.documentElement.classList.contains('dark'))
let observer: MutationObserver | null = null
let subscribers = 0

function startObserver() {
  if (observer || typeof MutationObserver === 'undefined' || typeof document === 'undefined') return
  observer = new MutationObserver(() => {
    isDark.value = document.documentElement.classList.contains('dark')
  })
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
}

function stopObserver() {
  observer?.disconnect()
  observer = null
}

export function withAlpha(hex: string, alpha: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(hex)
  if (!m) return hex
  const n = parseInt(m[1], 16)
  const r = (n >> 16) & 255
  const g = (n >> 8) & 255
  const b = n & 255
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

export function useChartTheme() {
  onMounted(() => {
    subscribers += 1
    isDark.value = document.documentElement.classList.contains('dark')
    startObserver()
  })
  onBeforeUnmount(() => {
    subscribers -= 1
    if (subscribers <= 0) {
      subscribers = 0
      stopObserver()
    }
  })

  const theme = computed(() => {
    const dark = isDark.value
    return {
      isDark: dark,
      series: (dark ? CHART_SERIES_DARK : CHART_SERIES_LIGHT) as readonly string[],
      seriesExtended: (dark ? CHART_SERIES_EXTENDED_DARK : CHART_SERIES_EXTENDED_LIGHT) as readonly string[],
      other: dark ? '#475569' : '#94a3b8',
      text: dark ? '#94a3b8' : '#475569',
      textMuted: dark ? '#64748b' : '#8b95a9',
      ink: dark ? '#f1f5f9' : '#0f172a',
      grid: dark ? '#1e293b' : '#e4e7f0',
      surface: dark ? '#0f172a' : '#ffffff',
      border: dark ? '#334155' : '#d2d8e6',
      mono: CHART_MONO,
    }
  })

  /** 第 i 个类别色（超过六个折到「其他」灰）。 */
  const colorAt = (index: number): string => theme.value.seriesExtended[index] ?? theme.value.other

  /** 与类别色等长的调色板；n 大于六时其余为「其他」灰。 */
  const palette = (n: number): string[] => Array.from({ length: n }, (_, i) => colorAt(i))

  /** 折线 / 面积数据集的默认样式。 */
  const lineDataset = (color: string, overrides: Record<string, unknown> = {}) => ({
    borderColor: color,
    backgroundColor: withAlpha(color, 0.1),
    fill: true,
    tension: 0.3,
    borderWidth: 2,
    pointRadius: 0,
    pointHitRadius: 12,
    pointHoverRadius: 4,
    pointHoverBackgroundColor: color,
    pointHoverBorderColor: theme.value.surface,
    pointHoverBorderWidth: 2,
    ...overrides,
  })

  const tickFont = computed(() => ({ family: theme.value.mono, size: 11 }))

  /** 笛卡尔坐标轴：x 无网格，y 发丝网格；刻度等宽字体、无轴线。 */
  const scales = computed(() => ({
    x: {
      grid: { display: false },
      border: { display: false },
      ticks: { color: theme.value.textMuted, font: tickFont.value, maxRotation: 0, autoSkipPadding: 12 },
    },
    y: {
      grid: { color: theme.value.grid },
      border: { display: false },
      ticks: { color: theme.value.textMuted, font: tickFont.value },
    },
  }))

  const tooltip = computed(() => ({
    backgroundColor: theme.value.surface,
    titleColor: theme.value.ink,
    bodyColor: theme.value.text,
    borderColor: theme.value.border,
    borderWidth: 1,
    padding: 10,
    cornerRadius: 8,
    displayColors: true,
    boxWidth: 8,
    boxHeight: 8,
    boxPadding: 4,
    usePointStyle: true,
    titleFont: { family: theme.value.mono, size: 11, weight: 'normal' as const },
    bodyFont: { size: 12 },
  }))

  const legend = computed(() => ({
    labels: {
      color: theme.value.text,
      usePointStyle: true,
      pointStyle: 'rectRounded' as const,
      boxWidth: 8,
      boxHeight: 8,
      padding: 14,
      font: { size: 11 },
    },
  }))

  return { theme, isDark, colorAt, palette, lineDataset, scales, tooltip, legend, withAlpha }
}

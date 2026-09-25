import type { PlazaBillingKind } from '@/composables/useModelAggregation'

export interface PlazaBillingKindStyle {
  labelKey: string
  /** 卡片顶部渐变条。 */
  accentBar: string
  /** 卡片悬停时的边框与光晕。 */
  hover: string
  /** 计费类型徽标。 */
  badge: string
  /** 侧栏圆点。 */
  dot: string
  /** 「详情 →」的颜色。 */
  link: string
}

export const PLAZA_BILLING_KINDS: PlazaBillingKind[] = ['text', 'image', 'video', 'per_request']

export const PLAZA_BILLING_KIND_STYLES: Record<PlazaBillingKind, PlazaBillingKindStyle> = {
  text: {
    labelKey: 'plaza.kind.text',
    accentBar: 'bar-fill',
    hover: 'hover:border-primary-300 hover:shadow-glow dark:hover:border-primary-500/40',
    badge: 'bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-300',
    dot: 'bg-primary-500',
    link: 'text-primary-600 dark:text-primary-400'
  },
  image: {
    labelKey: 'plaza.kind.image',
    accentBar: 'bg-gradient-to-r from-fuchsia-500 to-primary-500',
    hover: 'hover:border-fuchsia-300 hover:shadow-[0_0_20px_rgba(217,70,239,.2)] dark:hover:border-fuchsia-500/40',
    badge: 'bg-fuchsia-50 text-fuchsia-700 dark:bg-fuchsia-500/10 dark:text-fuchsia-300',
    dot: 'bg-fuchsia-500',
    link: 'text-fuchsia-600 dark:text-fuchsia-400'
  },
  video: {
    labelKey: 'plaza.kind.video',
    accentBar: 'bg-gradient-to-r from-amber-500 to-rose-500',
    hover: 'hover:border-amber-300 hover:shadow-[0_0_20px_rgba(245,158,11,.2)] dark:hover:border-amber-500/40',
    badge: 'bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-300',
    dot: 'bg-amber-500',
    link: 'text-amber-600 dark:text-amber-400'
  },
  per_request: {
    labelKey: 'plaza.kind.perRequest',
    accentBar: 'bg-gradient-to-r from-emerald-500 to-accent-400',
    hover: 'hover:border-emerald-300 hover:shadow-[0_0_20px_rgba(16,185,129,.18)] dark:hover:border-emerald-500/40',
    badge: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-300',
    dot: 'bg-emerald-500',
    link: 'text-emerald-600 dark:text-emerald-400'
  }
}

export function formatCallCount(count: number): string {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return String(count)
}

import type {
  BucketSnapshotDTO,
  QuotaTrendPointDTO,
  QuotaTrendWindowDTO,
  QuotaWindowDTO,
} from '@/types/radar'

export function quotaWindowsForBucket(bucket: BucketSnapshotDTO): QuotaWindowDTO[] {
  if (Array.isArray(bucket.windows) && bucket.windows.length > 0) {
    return bucket.windows.map((window) => ({
      ...window,
      model_windows: window.model_windows ?? [],
      model_breakdown: window.model_breakdown ?? [],
    }))
  }

  const windows: QuotaWindowDTO[] = []
  if (bucket.five_hour) {
    windows.push({
      key: '5h',
      label: '5H',
      duration_seconds: 5 * 60 * 60,
      currency: 'USD',
      stats: bucket.five_hour,
      model_windows: [],
      model_breakdown: bucket.model_breakdown_5h ?? [],
    })
  }
  if (bucket.seven_day) {
    windows.push({
      key: '7d',
      label: '7D',
      duration_seconds: 7 * 24 * 60 * 60,
      currency: 'USD',
      stats: bucket.seven_day,
      model_windows: [bucket.seven_day_sonnet, bucket.seven_day_fable].filter(
        (window): window is NonNullable<typeof window> => window !== null
      ),
      model_breakdown: bucket.model_breakdown_7d ?? [],
    })
  }
  return windows
}

export function quotaTrendWindow(
  point: QuotaTrendPointDTO,
  windowKey: string
): QuotaTrendWindowDTO | null {
  const generic = point.windows?.find((window) => window.key === windowKey)
  if (generic) return generic.stats
  if (windowKey === '5h') return point.five_hour
  if (windowKey === '7d') return point.seven_day
  return null
}

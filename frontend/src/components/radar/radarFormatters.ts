type DurationUnit = 'hour' | 'minute' | 'second' | 'millisecond'

interface CachedListFormatter {
  format(values: readonly string[]): string
}

interface ListFormatConstructor {
  new(locales?: string | string[], options?: { style?: 'long'; type?: 'conjunction' }): CachedListFormatter
}

const durationUnitMilliseconds: ReadonlyArray<readonly [DurationUnit, number]> = [
  ['hour', 60 * 60 * 1000],
  ['minute', 60 * 1000],
  ['second', 1000],
  ['millisecond', 1],
]

const goDurationUnitMilliseconds: Readonly<Record<string, number>> = {
  h: 60 * 60 * 1000,
  m: 60 * 1000,
  s: 1000,
  ms: 1,
  us: 1 / 1000,
  'µs': 1 / 1000,
  'μs': 1 / 1000,
  ns: 1 / 1_000_000,
}

const unitFormatterCache = new Map<string, Intl.NumberFormat>()
const listFormatterCache = new Map<string, CachedListFormatter>()

function parseGoDurationMilliseconds(value: string): number | null {
  if (value === '0') return 0

  const input = value.startsWith('+') ? value.slice(1) : value
  if (!input || value.startsWith('-')) return null

  const tokenPattern = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|μs|ms|s|m|h)/y
  let cursor = 0
  let total = 0

  while (cursor < input.length) {
    tokenPattern.lastIndex = cursor
    const match = tokenPattern.exec(input)
    if (!match) return null

    const amount = Number(match[1])
    const multiplier = goDurationUnitMilliseconds[match[2]]
    if (!Number.isFinite(amount) || amount < 0 || multiplier === undefined) return null

    total += amount * multiplier
    if (!Number.isFinite(total)) return null
    cursor = tokenPattern.lastIndex
  }

  return total
}

function getUnitFormatter(locale: string, unit: DurationUnit): Intl.NumberFormat {
  const key = `${locale}\u0000${unit}`
  let formatter = unitFormatterCache.get(key)
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, {
      style: 'unit',
      unit,
      unitDisplay: 'long',
      maximumFractionDigits: 6,
    })
    unitFormatterCache.set(key, formatter)
  }
  return formatter
}

function formatDurationParts(parts: string[], locale: string): string {
  if (parts.length <= 1) return parts[0] ?? ''

  try {
    let formatter = listFormatterCache.get(locale)
    if (!formatter) {
      const ListFormat = (Intl as typeof Intl & { ListFormat?: ListFormatConstructor }).ListFormat
      if (!ListFormat) return parts.join(', ')
      formatter = new ListFormat(locale, { style: 'long', type: 'conjunction' })
      listFormatterCache.set(locale, formatter)
    }
    return formatter.format(parts)
  } catch {
    return parts.join(', ')
  }
}

/** Format Go's time.Duration syntax without exposing machine-oriented unit abbreviations. */
export function formatGoDuration(value: string, locale: string): string {
  const milliseconds = parseGoDurationMilliseconds(value)
  if (milliseconds === null) return value

  try {
    if (milliseconds === 0) return getUnitFormatter(locale, 'second').format(0)

    let remainder = milliseconds
    const parts: string[] = []
    for (const [unit, unitMilliseconds] of durationUnitMilliseconds) {
      const amount = unit === 'millisecond'
        ? Math.round(remainder * 1_000_000) / 1_000_000
        : Math.floor(remainder / unitMilliseconds)
      if (amount <= 0) continue

      parts.push(getUnitFormatter(locale, unit).format(amount))
      remainder -= amount * unitMilliseconds
    }

    return formatDurationParts(parts, locale) || value
  } catch {
    return value
  }
}

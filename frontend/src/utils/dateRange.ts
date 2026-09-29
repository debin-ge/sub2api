/**
 * Date-range helpers shared by the usage/dashboard pages.
 *
 * Most presets ("last 7 days", "this month") really do mean whole calendar
 * days, and `start_date`/`end_date` express them exactly. "Last 24 hours" does
 * not: as a pair of dates it becomes [yesterday 00:00, tomorrow 00:00) — up to
 * 48 hours — which is why the usage page used to report roughly twice the
 * tokens the dashboard's "today" card showed.
 *
 * A rolling preset therefore also carries `startTime`/`endTime`, RFC3339
 * instants the backend prefers over the dates whenever both are present.
 */

export interface DateRangeSelection {
  start: string
  end: string
  /** Exact window bounds; only rolling presets set these. */
  startTime?: string
  endTime?: string
}

/** Format a date as YYYY-MM-DD in the browser timezone (never UTC). */
export const formatLocalDate = (date: Date): string =>
  `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`

const MINUTE_MS = 60 * 1000
const DAY_MS = 24 * 60 * MINUTE_MS

/**
 * The current calendar day in the browser timezone, as whole-day dates. The
 * backend widens `start_date`/`end_date` to the half-open local day using the
 * `timezone` query param every request carries, which is the same window the
 * admin dashboard's "today" cards are cut on, so the two pages agree.
 */
export const getTodayRange = (now: Date = new Date()): DateRangeSelection => {
  const today = formatLocalDate(now)
  return { start: today, end: today }
}

/**
 * The 24 hours ending at the next whole minute: [next minute − 24h, next
 * minute). Aligning to the minute keeps the window stable across repeated
 * loads within the same minute (so snapshot caches can hit) and lets the
 * picker show it as "MM-DD HH:mm ~ MM-DD HH:mm" without hiding seconds. The
 * dates are kept for granularity selection; the instants are what the query
 * actually runs on.
 */
export const getLast24HoursRange = (now: Date = new Date()): Required<DateRangeSelection> => {
  const end = new Date(Math.floor(now.getTime() / MINUTE_MS) * MINUTE_MS + MINUTE_MS)
  const start = new Date(end.getTime() - DAY_MS)
  return {
    start: formatLocalDate(start),
    // The end is exclusive: name the day its last covered minute falls on.
    end: formatLocalDate(new Date(end.getTime() - 1)),
    // toISOString() is UTC ("…Z"), so no "+" survives into the query string.
    startTime: start.toISOString(),
    endTime: end.toISOString()
  }
}

const pad2 = (n: number): string => String(n).padStart(2, '0')

/**
 * Render an instant range as "MM-DD HH:mm ~ MM-DD HH:mm" in the browser
 * timezone. The end is exclusive, so a minute-aligned end reads as the minute
 * the window stops at, matching how the rolling presets are described.
 */
export const formatInstantRange = (startTime: string, endTime: string): string => {
  const fmt = (iso: string): string => {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return ''
    return `${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  }
  const from = fmt(startTime)
  const to = fmt(endTime)
  return from && to ? `${from} ~ ${to}` : ''
}

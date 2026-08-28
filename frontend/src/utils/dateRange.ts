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

const DAY_MS = 24 * 60 * 60 * 1000

/**
 * The window ending now and starting exactly 24 hours ago. The dates are kept
 * for display and for granularity selection; the instants are what the query
 * actually runs on.
 */
export const getLast24HoursRange = (now: Date = new Date()): Required<DateRangeSelection> => {
  const start = new Date(now.getTime() - DAY_MS)
  return {
    start: formatLocalDate(start),
    end: formatLocalDate(now),
    // toISOString() is UTC ("…Z"), so no "+" survives into the query string.
    startTime: start.toISOString(),
    endTime: now.toISOString()
  }
}

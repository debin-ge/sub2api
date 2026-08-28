/**
 * Timezone helpers for usage/statistics queries.
 *
 * Date filters are sent to the backend as bare `YYYY-MM-DD` strings built from
 * the browser clock. Without an explicit `timezone` param the backend parses
 * them in the server-configured timezone, so a viewer in a different timezone
 * gets a window shifted by the offset — and the dashboard's "today" (computed
 * server-side) stops matching the usage page's "today" filter. Sending the
 * browser timezone with every usage query keeps both sides on the same day
 * boundary.
 */

/**
 * Resolve the browser's IANA timezone name (e.g. "Asia/Shanghai").
 * Returns undefined when unavailable, in which case the backend falls back to
 * the server-configured timezone.
 */
export function getBrowserTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined
  } catch {
    return undefined
  }
}

/**
 * Add the browser timezone to a query param object unless one is already set.
 * Explicit caller-supplied values always win.
 */
export function withTimezone<T extends object>(params?: T): T & { timezone?: string } {
  const resolved = { ...(params ?? ({} as T)) } as T & { timezone?: string }
  if (!resolved.timezone) {
    const tz = getBrowserTimezone()
    if (tz) {
      resolved.timezone = tz
    }
  }
  return resolved
}

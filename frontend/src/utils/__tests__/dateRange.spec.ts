import { describe, expect, it } from 'vitest'
import { formatInstantRange, formatLocalDate, getLast24HoursRange, getTodayRange } from '../dateRange'

describe('getLast24HoursRange', () => {
  it('ends at the next whole minute and spans exactly 24 hours', () => {
    const now = new Date(2026, 8, 28, 14, 37, 42, 123)
    const range = getLast24HoursRange(now)

    const end = new Date(range.endTime)
    const start = new Date(range.startTime)
    expect(end.getTime()).toBe(new Date(2026, 8, 28, 14, 38, 0, 0).getTime())
    expect(end.getTime() - start.getTime()).toBe(24 * 60 * 60 * 1000)
    expect(start.getSeconds()).toBe(0)
    expect(start.getMilliseconds()).toBe(0)
    expect(range.start).toBe('2026-09-27')
    expect(range.end).toBe('2026-09-28')
  })

  it('stays stable within the same minute', () => {
    const a = getLast24HoursRange(new Date(2026, 8, 28, 9, 5, 0, 0))
    const b = getLast24HoursRange(new Date(2026, 8, 28, 9, 5, 59, 999))
    expect(a).toEqual(b)
  })

  it('names the day of the last covered minute when the end lands on midnight', () => {
    const range = getLast24HoursRange(new Date(2026, 8, 28, 23, 59, 30))
    expect(new Date(range.endTime).getTime()).toBe(new Date(2026, 8, 29, 0, 0).getTime())
    expect(range.end).toBe('2026-09-28')
    expect(range.start).toBe('2026-09-28')
  })

  it('emits UTC instants', () => {
    const range = getLast24HoursRange(new Date(2026, 8, 28, 12, 0))
    expect(range.startTime.endsWith('Z')).toBe(true)
    expect(range.endTime.endsWith('Z')).toBe(true)
  })
})

describe('getTodayRange', () => {
  it('returns the local calendar day without instants', () => {
    const now = new Date(2026, 0, 5, 1, 2)
    expect(getTodayRange(now)).toEqual({ start: formatLocalDate(now), end: formatLocalDate(now) })
  })
})

describe('formatInstantRange', () => {
  it('renders both ends as local MM-DD HH:mm', () => {
    const start = new Date(2026, 8, 27, 14, 38).toISOString()
    const end = new Date(2026, 8, 28, 14, 38).toISOString()
    expect(formatInstantRange(start, end)).toBe('09-27 14:38 ~ 09-28 14:38')
  })

  it('returns an empty string for an invalid instant', () => {
    expect(formatInstantRange('nope', new Date().toISOString())).toBe('')
  })
})

import { afterEach, describe, expect, it, vi } from 'vitest'

import { formatDayKey, monthNames, parseDay, parseWhen, resetFormatters, weekdayNames } from './locale'
import { day, dayShort, daysAgo, isoDay } from './format'

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
  resetFormatters()
})

/**
 * These are the tests that matter, and they encode a bug rather than a preference.
 *
 * A date-only string is a calendar day, not an instant. `new Date("2024-08-12")`
 * parses it as UTC midnight, which is the previous evening anywhere west of
 * Greenwich — so every day in the interface displayed one day early for a driver in
 * the Americas. Nothing looks broken when this regresses; the dates are simply
 * wrong, consistently and plausibly.
 *
 * The suite runs with TZ=America/Chicago, set in package.json's test script, precisely
 * because a UTC test host cannot observe this class of fault at all.
 */
describe('calendar days do not shift across zones', () => {
  it('parses a date-only string as local midnight', () => {
    const d = parseDay('2024-08-12')
    expect(d).not.toBeNull()
    expect(d!.getFullYear()).toBe(2024)
    expect(d!.getMonth()).toBe(7)
    expect(d!.getDate()).toBe(12)
    expect(d!.getHours()).toBe(0)
  })

  it('renders a date-only string as its own day', () => {
    // The naive reading of this value is 11 Aug in any zone behind UTC.
    expect(dayShort('2024-08-12')).toContain('12')
    expect(day('2024-08-12')).toContain('12')
    expect(day('2024-08-12')).toContain('2024')
  })

  it('renders 1 January without slipping into the previous year', () => {
    // The worst case: an off-by-one day is also an off-by-one year.
    expect(day('2025-01-01')).toContain('2025')
    expect(day('2025-01-01')).toContain('1')
  })

  it('round-trips a day through parse and format unchanged', () => {
    for (const iso of ['2024-01-01', '2024-02-29', '2024-08-12', '2025-12-31']) {
      expect(formatDayKey(parseDay(iso)!)).toBe(iso)
    }
  })

  it('leaves an instant to be converted into the viewer’s zone', () => {
    // 19:22 UTC is still the same day in Chicago, so this must not move either.
    expect(dayShort('2026-08-04T19:22:28Z')).toContain('4')
  })

  it('distinguishes an instant from a calendar day', () => {
    const asDay = parseWhen('2024-08-12')
    const asInstant = parseWhen('2024-08-12T00:00:00Z')
    expect(asDay!.getTime()).not.toBe(asInstant!.getTime())
  })
})

/**
 * Relative filters are computed from the local calendar, not from UTC.
 *
 * toISOString at 20:00 in Chicago yields tomorrow's date, so "last 7 days" quietly
 * covered the wrong week for anyone looking at their data in the evening — the one
 * time of day a sim racer is most likely to be looking.
 */
describe('relative date filters use the local calendar', () => {
  it('treats an evening as today, not tomorrow', () => {
    vi.useFakeTimers()
    // 20:30 local on 4 August, which is 01:30 UTC on the 5th.
    vi.setSystemTime(new Date('2026-08-05T01:30:00Z'))
    expect(daysAgo(0)).toBe('2026-08-04')
    expect(daysAgo(7)).toBe('2026-07-28')
  })

  it('formats a local date without converting it', () => {
    // 23:00 local on 31 December is already the new year in UTC.
    const d = new Date(2025, 11, 31, 23, 0, 0)
    expect(isoDay(d)).toBe('2025-12-31')
  })
})

/** Names come from the locale so a calendar can label itself in any language. */
describe('locale-derived names', () => {
  it('returns twelve distinct months', () => {
    const names = monthNames()
    expect(names).toHaveLength(12)
    expect(new Set(names).size).toBe(12)
  })

  it('returns seven weekdays beginning on Sunday', () => {
    // Sunday first is a product decision, not the locale's first day: the driver
    // never races on one, so an empty top row on the heatmap is informative.
    const names = weekdayNames()
    expect(names).toHaveLength(7)
    const sunday = new Intl.DateTimeFormat(undefined, { weekday: 'narrow' }).format(
      new Date(2021, 7, 1),
    )
    expect(names[0]).toBe(sunday)
  })

  it('follows the resolved locale rather than a fixed one', () => {
    vi.stubGlobal('navigator', { language: 'de-DE' })
    resetFormatters()
    const german = monthNames()

    vi.stubGlobal('navigator', { language: 'en-US' })
    resetFormatters()
    const english = monthNames()

    // German abbreviates March as "Mär", English as "Mar"; if the locale were
    // ignored these lists would be identical.
    expect(german).not.toEqual(english)
  })
})

/** Malformed input is passed through rather than rendered as "Invalid Date". */
describe('unparseable values', () => {
  it('returns the input unchanged', () => {
    expect(day('not a date')).toBe('not a date')
    expect(parseDay('2024-8-1')).toBeNull()
    expect(parseDay('')).toBeNull()
  })
})

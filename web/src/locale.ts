/*
 * Locale resolution and date formatting.
 *
 * Dates follow the operating system rather than a fixed locale. The interface was
 * pinned to en-GB, so someone in the United States read "4 Aug 2026" where their
 * system would write "Aug 4, 2026" — and every number grouped the British way.
 *
 * Two distinct kinds of value arrive from the API and they must not be treated
 * alike:
 *
 *   - **Instants**, RFC 3339 with a Z: "2026-08-04T19:22:28Z". A moment in time.
 *     Converting these to the viewer's zone is correct and wanted.
 *   - **Calendar days**, date-only: "2026-08-04". A day as the database recorded
 *     it, already local to when the driving happened. These carry no time and no
 *     zone, and converting them is exactly wrong.
 *
 * Passing a date-only string to `new Date()` parses it as UTC midnight, which is
 * the previous evening anywhere west of Greenwich. Every calendar day therefore
 * displayed one day early for a driver in the Americas — 2024-08-12 rendered as
 * "11 Aug". parseDay exists to stop that.
 */

/**
 * locale returns the locale to format with, or undefined to let the runtime decide.
 *
 * Returning undefined is deliberate rather than lazy: Intl treats it as "use the
 * default", which is the browser's, which follows the operating system. Naming a
 * locale explicitly would re-introduce the pinning this replaces.
 *
 * navigator.language is read only so a test can assert what was resolved; the
 * formatters below pass undefined either way.
 */
export function locale(): string | undefined {
  if (typeof navigator === 'undefined') return undefined
  return navigator.language || undefined
}

/**
 * parseDay turns a date-only string into a Date at local midnight.
 *
 * The components are passed to the Date constructor individually, which builds a
 * local date. `new Date("2026-08-04")` would instead parse as UTC midnight and shift
 * the day backwards for anyone behind UTC.
 */
export function parseDay(iso: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso)
  if (!m) return null
  const [, y, mo, d] = m
  const date = new Date(Number(y), Number(mo) - 1, Number(d))
  return Number.isNaN(date.getTime()) ? null : date
}

/**
 * parseWhen accepts either kind of value and returns a Date to format.
 *
 * A date-only string becomes local midnight; anything else is left to the Date
 * constructor, which handles RFC 3339 instants correctly.
 */
export function parseWhen(iso: string): Date | null {
  const dayOnly = parseDay(iso)
  if (dayOnly) return dayOnly
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? null : d
}

/**
 * Formatters are built once and reused.
 *
 * Constructing an Intl.DateTimeFormat is comparatively expensive, and these are
 * called once per table row — a few thousand times on the laps page.
 */
const cache = new Map<string, Intl.DateTimeFormat>()

function fmt(key: string, opts: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  let f = cache.get(key)
  if (!f) {
    f = new Intl.DateTimeFormat(locale(), opts)
    cache.set(key, f)
  }
  return f
}

/** resetFormatters clears the cache, for tests that change the resolved locale. */
export function resetFormatters(): void {
  cache.clear()
}

/** dateFull formats a day with its year. */
export const dateFull = (d: Date): string =>
  fmt('full', { day: 'numeric', month: 'short', year: 'numeric' }).format(d)

/** dateCompact formats a day without its year, for dense tables. */
export const dateCompact = (d: Date): string =>
  fmt('compact', { day: 'numeric', month: 'short' }).format(d)

/** dateAndTime formats an instant as a day and a clock time. */
export const dateAndTime = (d: Date): string =>
  fmt('datetime', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  }).format(d)

/** monthAndYear formats a month, for the per-month chart and its table. */
export const monthAndYear = (d: Date): string =>
  fmt('monthyear', { month: 'short', year: 'numeric' }).format(d)

/**
 * monthNames returns the twelve month names in the resolved locale.
 *
 * ECharts' calendar takes either a built-in language code or an explicit list, and
 * it only ships English and Chinese. An explicit list is how a calendar labels its
 * months in the viewer's language.
 */
export function monthNames(): string[] {
  const f = fmt('monthshort', { month: 'short' })
  // 2021 chosen only because it is a common year; the day avoids month-end
  // rollover in any zone.
  return Array.from({ length: 12 }, (_, i) => f.format(new Date(2021, i, 15)))
}

/**
 * weekdayNames returns the seven weekday names, Sunday first.
 *
 * Sunday first because the heatmap starts its week on Sunday by explicit choice —
 * the driver never races on one, so an empty top row is informative. That is a
 * product decision and is deliberately not derived from the locale's own first day,
 * which would silently rotate the grid.
 */
export function weekdayNames(): string[] {
  const f = fmt('weekdayshort', { weekday: 'narrow' })
  // 2021-08-01 was a Sunday.
  return Array.from({ length: 7 }, (_, i) => f.format(new Date(2021, 7, 1 + i)))
}

/** numberGrouped formats an integer with the locale's thousands separator. */
export function numberGrouped(n: number): string {
  return n.toLocaleString(locale())
}

/**
 * todayLocal returns today's date as YYYY-MM-DD in the viewer's own zone.
 *
 * Not toISOString, which is UTC: at 20:00 in Chicago that returns tomorrow, so a
 * "last 7 days" filter silently covered the wrong week for anyone filtering in the
 * evening.
 */
export function todayLocal(): Date {
  const now = new Date()
  return new Date(now.getFullYear(), now.getMonth(), now.getDate())
}

/** formatDayKey renders a Date as the YYYY-MM-DD form the API filters on. */
export function formatDayKey(d: Date): string {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

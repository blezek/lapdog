/**
 * Formatting helpers. Kept pure and separate so they are unit-testable.
 *
 * Anything date- or number-shaped defers to ./locale, which follows the operating
 * system. These were pinned to en-GB.
 */

import {
  dateAndTime,
  dateCompact,
  dateFull,
  formatDayKey,
  monthAndYear,
  numberGrouped,
  parseWhen,
  todayLocal,
} from './locale'

/** hours renders a duration in hours with a fixed precision. */
export function hours(h: number, digits = 1): string {
  return `${h.toFixed(digits)} h`
}

/** hm renders seconds as h:mm, which is how session length reads naturally. */
export function hm(seconds: number): string {
  const total = Math.max(0, Math.round(seconds))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  return `${h}:${String(m).padStart(2, '0')}`
}

/**
 * hms renders seconds as m:ss, or h:mm:ss once it passes an hour.
 *
 * Separate from hm because the live totals are read at seconds resolution. The
 * observation the Live page exists for is 154 seconds in the car against zero
 * driving seconds, and hm renders that as "0:02" beside "0:00" — two numbers a
 * single digit apart for a difference of two and a half minutes.
 */
export function hms(seconds: number): string {
  const total = Math.max(0, Math.round(seconds))
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const ss = String(total % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${ss}` : `${m}:${ss}`
}

/**
 * lapTime renders a lap time as m:ss.mmm.
 *
 * Lap times are compared by eye down to thousandths, so the precision is not
 * negotiable and the seconds field is always two digits.
 */
export function lapTime(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds) || seconds <= 0) return '—'
  const m = Math.floor(seconds / 60)
  const s = seconds - m * 60
  return `${m}:${s.toFixed(3).padStart(6, '0')}`
}

/** delta renders a signed lap-time delta, where a negative value is an improvement. */
export function delta(seconds: number | null | undefined): string {
  if (seconds == null || !Number.isFinite(seconds)) return '—'
  const sign = seconds > 0 ? '+' : seconds < 0 ? '−' : ''
  return `${sign}${Math.abs(seconds).toFixed(3)}`
}

/** pct renders a ratio as a whole percentage. */
export function pct(ratio: number): string {
  if (!Number.isFinite(ratio)) return '—'
  return `${Math.round(ratio * 100)}%`
}

/** num groups thousands in the operating system's locale. */
export function num(n: number): string {
  return numberGrouped(n)
}

/**
 * day renders a date as a short readable date, with its year.
 *
 * It accepts either an instant or a date-only string. parseWhen is what keeps the
 * two apart: a date-only value is a calendar day and must not be shifted into
 * another one by being read as UTC midnight.
 */
export function day(iso: string): string {
  const d = parseWhen(iso)
  return d ? dateFull(d) : iso
}

/** dayShort renders a date without its year, for dense tables. */
export function dayShort(iso: string): string {
  const d = parseWhen(iso)
  return d ? dateCompact(d) : iso.slice(0, 10)
}

/**
 * monthLabel renders a "YYYY-MM" key in the viewer's locale.
 *
 * The key is a sort-stable database value, not something to show: "2026-08" reads
 * as a date to nobody. The day is fixed at the first so the month cannot roll over.
 *
 * Shared rather than private to a page. It began as a helper inside the dashboard
 * while the entity pages rendered the raw key beside it, so the same value appeared
 * as "Aug 2026" on one screen and "2026-08" on another. One definition is what
 * stops the two diverging again.
 */
export function monthLabel(key: string): string {
  const m = /^(\d{4})-(\d{2})$/.exec(key)
  if (!m) return key
  const [, y, mo] = m
  return monthAndYear(new Date(Number(y), Number(mo) - 1, 1))
}

/** dateTime renders an instant as a date and a clock time. */
export function dateTime(iso: string): string {
  const d = parseWhen(iso)
  return d ? dateAndTime(d) : iso
}

/** bytes renders a byte count in binary units. */
export function bytes(n: number): string {
  if (n <= 0) return 'unlimited'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v < 10 && i > 0 ? 1 : 0)} ${units[i]}`
}

/** position renders a finishing position, or an em dash when there is none. */
export function position(p: number | null | undefined): string {
  return p == null || p <= 0 ? '—' : `P${p}`
}

/**
 * label renders a session type and event context the way the interface shows it.
 *
 * This mirrors classify.Label in Go. It is duplicated rather than served because
 * the pair is what the API returns and the label is purely presentational — the
 * rules can change here without a data migration, which was the point of storing
 * the two fields separately.
 */
export function label(sessionType: string, eventContext: string): string {
  if (eventContext === 'Offline') return 'Offline Testing'
  if (sessionType === 'TimeTrial') return 'Time Trial'

  const base =
    sessionType === 'Practice'
      ? 'Practice'
      : sessionType === 'Qualify'
        ? 'Qualifying'
        : sessionType === 'Race'
          ? 'Race'
          : sessionType === 'Warmup'
            ? 'Warmup'
            : 'Unknown'

  switch (eventContext) {
    case 'League':
      return `League ${base}`
    case 'AI':
      return `AI ${base}`
    case 'Hosted':
      return `Hosted ${base}`
    case 'OfficialPractice':
      return sessionType === 'Practice' ? 'Public Practice' : base
    case 'OfficialRace':
      return sessionType === 'Practice' ? 'Race Practice' : base
    default:
      return base
  }
}

/**
 * labelForKey renders a "Type/Context" summary key as its display label.
 *
 * The fold bucket is named explicitly. Without that it fell through label()'s
 * unrecognised-type branch and rendered as "Unknown", which read as a data problem —
 * sessions that had failed to classify — when it was really several small categories
 * combined. "Unknown" is a real classification outcome, so it must not be borrowed
 * for anything else.
 */
export function labelForKey(key: string): string {
  if (key === OtherCategoryKey) return 'Other'
  const [type, context] = key.split('/')
  if (!type || !context) return key
  return label(type, context)
}

/**
 * OtherCategoryKey is the bucket small categories fold into.
 *
 * Defined here rather than only in the categories module so labelForKey can name it
 * without importing, and so the two cannot disagree about the spelling.
 */
export const OtherCategoryKey = 'Other/Other'

/** causeLabel renders a position-change cause in plain words. */
export function causeLabel(cause: string): string {
  switch (cause) {
    case 'OnTrack':
      return 'On track'
    case 'OpponentPit':
      return 'They pitted'
    case 'OpponentOffWorld':
      return 'They retired'
    default:
      return 'Unknown'
  }
}

/**
 * isoDay returns the YYYY-MM-DD form of a date, in the viewer's own zone.
 *
 * Deliberately not toISOString, which is UTC. At 20:00 in Chicago that returns
 * tomorrow's date, so every relative filter was off by a day for anyone looking at
 * their data in the evening — the range simply covered the wrong week.
 */
export function isoDay(d: Date): string {
  return formatDayKey(d)
}

/** daysAgo returns the YYYY-MM-DD date n days before today, locally. */
export function daysAgo(n: number): string {
  const d = todayLocal()
  d.setDate(d.getDate() - n)
  return isoDay(d)
}

/**
 * licenceLabel renders a Safety Rating as its class and value, e.g. "A 3.94".
 *
 * The class comes from the simulator's licence string but the number does not: it is
 * the same value the chart plots. Printing the string wholesale would let the
 * headline disagree with the last point of the line beside it whenever the two were
 * recorded at different moments, and a card that contradicts its own chart is worse
 * than one that shows only a number.
 */
export function licenceLabel(licString: string | null, sr: number | null): string {
  if (sr == null) return licString ?? '—'
  const cls = licString?.trim().match(/^([A-Za-z]+)/)?.[1]
  return cls ? `${cls.toUpperCase()} ${sr.toFixed(2)}` : sr.toFixed(2)
}

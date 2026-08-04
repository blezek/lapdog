/** Formatting helpers. Kept pure and separate so they are unit-testable. */

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

/** num groups thousands. */
export function num(n: number): string {
  return n.toLocaleString('en-GB')
}

/** day renders an ISO date as a short readable date. */
export function day(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' })
}

/** dayShort renders an ISO timestamp as a compact date. */
export function dayShort(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso.slice(0, 10)
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })
}

/** dateTime renders an ISO timestamp as date and time. */
export function dateTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('en-GB', {
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
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

/** labelForKey renders a "Type/Context" summary key as its display label. */
export function labelForKey(key: string): string {
  const [type, context] = key.split('/')
  if (!type || !context) return key
  return label(type, context)
}

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

/** isoDay returns the YYYY-MM-DD form of a date. */
export function isoDay(d: Date): string {
  return d.toISOString().slice(0, 10)
}

/** daysAgo returns the YYYY-MM-DD date n days before today. */
export function daysAgo(n: number): string {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return isoDay(d)
}

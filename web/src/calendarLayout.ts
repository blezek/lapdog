/** Bounds and cell sizing shared by the Dashboard calendar and date picker. */
export function calendarSpan(days: string[]): {
  range: [string, string]
  years: string[]
  weeks: number
} {
  const sorted = [...days].sort()
  const first = sorted[0] ?? '2020-01-01'
  const last = sorted.at(-1) ?? first
  const start = new Date(`${first}T00:00:00Z`)
  const end = new Date(`${last}T00:00:00Z`)
  start.setUTCDate(start.getUTCDate() - start.getUTCDay())
  end.setUTCDate(end.getUTCDate() + (6 - end.getUTCDay()))
  const startKey = start.toISOString().slice(0, 10)
  const endKey = end.toISOString().slice(0, 10)
  const weeks = Math.max(1, Math.round(((end.getTime() - start.getTime()) / 86_400_000 + 1) / 7))
  return {
    range: [startKey, endKey],
    years: [...new Set(sorted.map((day) => day.slice(0, 4)))],
    weeks,
  }
}

export function calendarCellSize(width: number, weeks: number, max: number, gutter: number): number {
  if (width <= 0) return max
  return Math.max(3, Math.min(max, Math.floor(Math.max(0, width - gutter) / weeks)))
}

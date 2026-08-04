/*
 * Category ordering and colour.
 *
 * Every chart that colours by session category shares this, so a category is the
 * same hue on the dashboard's category bar, the per-car stack and the per-track
 * stack. Colour follows the entity, never its rank: a filter that reorders the bars
 * must not repaint the survivors.
 */

import type { BreakdownRow, SummaryRow } from './api'

/**
 * MaxSlots is the categorical ceiling.
 *
 * Past this the tail folds into "Other" rather than generating another hue: a
 * generated colour is indistinguishable from an existing one under colour-vision
 * deficiency and would break every palette check.
 */
export const MaxSlots = 8

/** OtherKey is the fold-in bucket. */
export const OtherKey = 'Other/Other'

/**
 * categoryOrder returns the canonical category order for a dataset, folded to the
 * slot ceiling.
 *
 * Ordering is by total driving time descending, so the categories a driver actually
 * spends time in take the leading — and most distinguishable — colours.
 */
export function categoryOrder(totals: Map<string, number>): string[] {
  const keys = [...totals.entries()].sort((a, b) => b[1] - a[1]).map(([k]) => k)
  if (keys.length <= MaxSlots) return keys
  return [...keys.slice(0, MaxSlots - 1), OtherKey]
}

/** foldKey maps a category onto its slot key, folding the tail into "Other". */
export function foldKey(order: string[], key: string): string {
  return order.includes(key) ? key : OtherKey
}

/** slotOf returns a category's fixed colour slot, or the last slot for "Other". */
export function slotOf(order: string[], key: string): number {
  const i = order.indexOf(key)
  return i < 0 ? MaxSlots - 1 : i % MaxSlots
}

/** totalsFromSummary sums driving hours per category from a summary response. */
export function totalsFromSummary(rows: SummaryRow[]): Map<string, number> {
  const m = new Map<string, number>()
  for (const r of rows) m.set(r.key, (m.get(r.key) ?? 0) + r.drivingHours)
  return m
}

/** totalsFromBreakdown sums driving hours per category from a breakdown response. */
export function totalsFromBreakdown(rows: BreakdownRow[]): Map<string, number> {
  const m = new Map<string, number>()
  for (const r of rows) m.set(r.stack, (m.get(r.stack) ?? 0) + r.drivingHours)
  return m
}

/** GroupTotals is one outer group with its per-category split. */
export interface GroupTotals {
  group: string
  total: number
  /** byCategory is keyed by the folded category key. */
  byCategory: Map<string, number>
  sessions: number
  laps: number
}

/**
 * pivot turns breakdown rows into one entry per outer group, ordered by total
 * driving time descending so the most-used car or track leads.
 */
export function pivot(rows: BreakdownRow[], order: string[]): GroupTotals[] {
  const groups = new Map<string, GroupTotals>()
  for (const r of rows) {
    let g = groups.get(r.group)
    if (!g) {
      g = { group: r.group, total: 0, byCategory: new Map(), sessions: 0, laps: 0 }
      groups.set(r.group, g)
    }
    const key = foldKey(order, r.stack)
    g.byCategory.set(key, (g.byCategory.get(key) ?? 0) + r.drivingHours)
    g.total += r.drivingHours
    g.sessions += r.sessions
    g.laps += r.laps
  }
  return [...groups.values()].sort((a, b) => b.total - a.total)
}

/*
 * Category ordering and colour.
 *
 * Every chart that colours by session category shares this mapping. Hues are
 * assigned by category rank within the current result, so the most common
 * visible categories get distinct palette slots. Filtering may change a
 * category's rank and hue; chart legends must always name the categories.
 */

import type { BreakdownRow, SummaryRow } from './api'
import { OtherCategoryKey } from './format'

/**
 * MaxSlots is the categorical ceiling.
 *
 * Past this the tail folds into "Other" rather than generating another hue: a
 * generated colour is indistinguishable from an existing one under colour-vision
 * deficiency and would break every palette check.
 */
export const MaxSlots = 8

/** OtherKey is the fold-in bucket. Re-exported so callers need one import. */
export const OtherKey = OtherCategoryKey

/**
 * categoryOrder returns the canonical category order for a dataset, folded to the
 * slot ceiling.
 *
 * Ordering is by total driving time descending, so the categories a driver actually
 * spends time in take the leading — and most distinguishable — colours.
 */
export function categoryOrder(totals: Map<string, number>): string[] {
  const keys = categoryOrderAll(totals)
  if (keys.length <= MaxSlots) return keys
  return [...keys.slice(0, MaxSlots - 1), OtherKey]
}

/**
 * categoryOrderAll returns every category, largest first, with nothing folded.
 *
 * Table views use this so the fold is a property of the chart alone. Folding the
 * table too would mean the small categories appeared nowhere at all, which is a
 * silent cap — the reader would have no way to find out what "Other" contained.
 */
export function categoryOrderAll(totals: Map<string, number>): string[] {
  return [...totals.entries()].sort((a, b) => b[1] - a[1]).map(([k]) => k)
}

/** foldKey maps a category onto its slot key, folding the tail into "Other". */
export function foldKey(order: string[], key: string): string {
  return order.includes(key) ? key : OtherKey
}

/** slotOf returns a category's slot in the supplied result order. */
export function slotOf(order: string[], key: string): number {
  const i = order.indexOf(key)
  return i < 0 ? MaxSlots - 1 : i % MaxSlots
}

/**
 * categoryColour returns the colour for a category.
 *
 * The fold bucket takes the de-emphasis grey rather than a categorical hue. It landed
 * on the last slot before, which is red — so the residual was the loudest thing on
 * the chart, and red is close enough to the critical status colour to imply something
 * was wrong. A residual should recede.
 */
export function categoryColour(
  theme: { series: string[]; deEmphasis: string },
  order: string[],
  key: string,
): string {
  if (key === OtherKey) return theme.deEmphasis
  return theme.series[slotOf(order, key)] ?? theme.deEmphasis
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
  key: string
  group: string
  total: number
  /** byCategory is keyed by the folded category key. */
  byCategory: Map<string, number>
  sessions: number
  laps: number
}

/** Keep same-name cars and track layouts separate when their simulator IDs differ. */
export function breakdownGroupKey(row: BreakdownRow): string {
  return row.groupId == null
    ? `name:${JSON.stringify([row.group, row.groupConfig])}`
    : `id:${row.groupId}`
}

/** Add the layout, or a stable ID if two entities still have the same visible name. */
export function breakdownGroupLabel(row: BreakdownRow, ambiguous: Set<string>): string {
  const named = row.groupConfig ? `${row.group} · ${row.groupConfig}` : row.group
  return ambiguous.has(named) && row.groupId != null ? `${named} · #${row.groupId}` : named
}

export function ambiguousBreakdownNames(rows: BreakdownRow[]): Set<string> {
  const ids = new Map<string, Set<string>>()
  for (const row of rows) {
    const label = row.groupConfig ? `${row.group} · ${row.groupConfig}` : row.group
    const seen = ids.get(label) ?? new Set<string>()
    seen.add(breakdownGroupKey(row))
    ids.set(label, seen)
  }
  return new Set([...ids].filter(([, keys]) => keys.size > 1).map(([name]) => name))
}

/**
 * pivot turns breakdown rows into one entry per outer group, ordered by total
 * driving time descending so the most-used car or track leads.
 */
export function pivot(rows: BreakdownRow[], order: string[]): GroupTotals[] {
  const groups = new Map<string, GroupTotals>()
  const ambiguous = ambiguousBreakdownNames(rows)
  for (const r of rows) {
    const groupKey = breakdownGroupKey(r)
    let g = groups.get(groupKey)
    if (!g) {
      g = { key: groupKey, group: breakdownGroupLabel(r, ambiguous), total: 0, byCategory: new Map(), sessions: 0, laps: 0 }
      groups.set(groupKey, g)
    }
    const key = foldKey(order, r.stack)
    g.byCategory.set(key, (g.byCategory.get(key) ?? 0) + r.drivingHours)
    g.total += r.drivingHours
    g.sessions += r.sessions
    g.laps += r.laps
  }
  return [...groups.values()].sort((a, b) => b.total - a.total)
}

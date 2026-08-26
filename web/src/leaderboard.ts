import type { BreakdownRow } from './api'
import { foldKey } from './categories'

export type LeaderboardMetric = 'laps' | 'cleanLaps' | 'miles'

export interface LeaderboardGroup {
  group: string
  total: number
  byCategory: Map<string, number>
}

const MilesPerKilometre = 0.621371192237

/**
 * rankLeaderboard totals one fact per group and category, largest group first.
 *
 * Laps use the simulator's session counter, which includes timed, untimed and pit
 * laps. Distance follows the existing entity statistic: completed laps multiplied
 * by the track length recorded for that session. A missing track length therefore
 * contributes no invented distance.
 */
export function rankLeaderboard(
  rows: BreakdownRow[],
  metric: LeaderboardMetric,
  order: string[],
  limit = 10,
): LeaderboardGroup[] {
  const groups = new Map<string, LeaderboardGroup>()
  for (const row of rows) {
    const value =
      metric === 'laps'
        ? row.laps
        : metric === 'cleanLaps'
          ? row.cleanLaps
          : row.distanceKm * MilesPerKilometre
    let group = groups.get(row.group)
    if (!group) {
      group = { group: row.group, total: 0, byCategory: new Map() }
      groups.set(row.group, group)
    }
    const key = foldKey(order, row.stack)
    group.total += value
    group.byCategory.set(key, (group.byCategory.get(key) ?? 0) + value)
  }

  return [...groups.values()]
    .filter((group) => group.total > 0)
    .sort((a, b) => b.total - a.total || a.group.localeCompare(b.group))
    .slice(0, limit)
}

import { describe, expect, it } from 'vitest'

import type { BreakdownRow } from './api'
import { rankLeaderboard } from './leaderboard'

function row(
  group: string,
  stack: string,
  laps: number,
  distanceKm: number,
  cleanLaps = laps,
): BreakdownRow {
  return { group, groupId: null, groupConfig: null, stack, laps, cleanLaps, distanceKm, drivingHours: 1, sessions: 1 }
}

describe('rankLeaderboard', () => {
  it('orders by the selected fact and keeps only the top ten groups', () => {
    const rows = Array.from({ length: 12 }, (_, i) =>
      row(`Car ${String(i).padStart(2, '0')}`, 'Practice/Offline', i + 1, 100 - i),
    )

    const laps = rankLeaderboard(rows, 'laps', ['Practice/Offline'])
    expect(laps).toHaveLength(10)
    expect(laps[0]?.group).toBe('Car 11')
    expect(laps.at(-1)?.group).toBe('Car 02')

    const distance = rankLeaderboard(rows, 'distance', ['Practice/Offline'])
    expect(distance[0]?.group).toBe('Car 00')
    expect(distance.at(-1)?.group).toBe('Car 09')
  })

  it('includes every category in the group total and keeps distance in kilometres', () => {
    const rows = [
      row('Porsche', 'Practice/OfficialPractice', 7, 10, 6),
      row('Porsche', 'Race/OfficialRace', 3, 6.09344, 2),
    ]
    const order = ['Practice/OfficialPractice', 'Race/OfficialRace']

    const [laps] = rankLeaderboard(rows, 'laps', order)
    expect(laps?.total).toBe(10)
    expect(laps?.byCategory.get(order[0] ?? '')).toBe(7)
    expect(laps?.byCategory.get(order[1] ?? '')).toBe(3)

    const [clean] = rankLeaderboard(rows, 'cleanLaps', order)
    expect(clean?.total).toBe(8)
    expect(clean?.byCategory.get(order[0] ?? '')).toBe(6)
    expect(clean?.byCategory.get(order[1] ?? '')).toBe(2)

    const [distance] = rankLeaderboard(rows, 'distance', order)
    expect(distance?.total).toBeCloseTo(16.09344, 4)
  })

  it('keeps same-name track layouts separate by simulator ID', () => {
    const rows = [
      { ...row('Lime Rock Park', 'Race/OfficialRace', 8, 12), groupId: 1, groupConfig: 'Grand Prix' },
      { ...row('Lime Rock Park', 'Race/OfficialRace', 5, 8), groupId: 2, groupConfig: 'Chicanes' },
    ]
    const ranked = rankLeaderboard(rows, 'laps', ['Race/OfficialRace'])
    expect(ranked.map((group) => [group.key, group.group, group.total])).toEqual([
      ['id:1', 'Lime Rock Park · Grand Prix', 8],
      ['id:2', 'Lime Rock Park · Chicanes', 5],
    ])
  })
})

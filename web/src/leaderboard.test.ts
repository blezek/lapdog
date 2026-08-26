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
  return { group, stack, laps, cleanLaps, distanceKm, drivingHours: 1, sessions: 1 }
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

    const miles = rankLeaderboard(rows, 'miles', ['Practice/Offline'])
    expect(miles[0]?.group).toBe('Car 00')
    expect(miles.at(-1)?.group).toBe('Car 09')
  })

  it('includes every category in the group total and converts kilometres to miles', () => {
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

    const [miles] = rankLeaderboard(rows, 'miles', order)
    expect(miles?.total).toBeCloseTo(10, 4)
  })
})

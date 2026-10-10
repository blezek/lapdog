import { describe, expect, it } from 'vitest'
import type { BreakdownRow } from './api'
import { pivot } from './categories'

describe('breakdown grouping', () => {
  it('keeps same-name layouts as separate bars and names both when config is missing', () => {
    const rows: BreakdownRow[] = [
      { group: 'Virginia International Raceway', groupId: 12, groupConfig: null, stack: 'Race/OfficialRace', drivingHours: 3, sessions: 2, laps: 10, cleanLaps: 8, distanceKm: 40 },
      { group: 'Virginia International Raceway', groupId: 13, groupConfig: null, stack: 'Race/OfficialRace', drivingHours: 2, sessions: 1, laps: 7, cleanLaps: 6, distanceKm: 28 },
    ]
    expect(pivot(rows, ['Race/OfficialRace']).map(({ key, group, total }) => [key, group, total])).toEqual([
      ['id:12', 'Virginia International Raceway · #12', 3],
      ['id:13', 'Virginia International Raceway · #13', 2],
    ])
  })

  it('keeps distinct layouts separate when the simulator omitted track IDs', () => {
    const rows: BreakdownRow[] = [
      { group: 'Lime Rock Park', groupId: null, groupConfig: 'Grand Prix', stack: 'Race/OfficialRace', drivingHours: 3, sessions: 1, laps: 10, cleanLaps: 9, distanceKm: 40 },
      { group: 'Lime Rock Park', groupId: null, groupConfig: 'Chicanes', stack: 'Race/OfficialRace', drivingHours: 2, sessions: 1, laps: 7, cleanLaps: 6, distanceKm: 28 },
    ]
    expect(pivot(rows, ['Race/OfficialRace']).map(({ group, total }) => [group, total])).toEqual([
      ['Lime Rock Park · Grand Prix', 3],
      ['Lime Rock Park · Chicanes', 2],
    ])
  })
})

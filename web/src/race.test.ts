import { describe, expect, it } from 'vitest'
import type { Session } from './api'
import { positionChange, raceStats } from './race'

function race(patch: Partial<Session>): Session {
  return {
    id: 1, sessionKey: '1/2', subsessionId: 1, sessionNum: 2,
    sessionType: 'Race', eventContext: 'OfficialRace', leagueId: 0,
    seriesId: 0, official: 1, trackId: 1, trackName: 'Track', trackConfig: null,
    trackLengthKm: 4, carId: 1, carName: 'Car', carClassName: null,
    startedAt: '2026-08-12T20:00:00Z', endedAt: null,
    connectedSeconds: 900, inCarSeconds: 840, drivingSeconds: 810,
    lapsCompleted: 11, incidents: 0, bestLapTimeS: 70,
    startingPosition: null, finishPosition: null, finishClassPosition: null,
    qualifyPosition: null, qualifyClassPosition: null, qualifyBestTimeS: null,
    fieldSize: null, aiOpponentCount: 0, aiDetection: null, incidentSource: 'live',
    captureFile: null, ...patch,
  }
}

describe('race summaries', () => {
  it('uses only recorded finishes and paired grid positions', () => {
    const stats = raceStats([
      race({ drivingSeconds: 800, startingPosition: 5, finishPosition: 1 }),
      race({ drivingSeconds: 900, startingPosition: 2, finishPosition: 3 }),
      race({ drivingSeconds: 700 }),
    ])
    expect(stats).toEqual({
      races: 3, drivingSeconds: 2400, wins: 1, podiums: 2,
      classified: 2, avgFinish: 2, positionPairs: 2, avgPositionsGained: 1.5,
    })
  })

  it('does not turn absent results into zeroes', () => {
    const stats = raceStats([race({})])
    expect(stats.avgFinish).toBeNull()
    expect(stats.avgPositionsGained).toBeNull()
  })

  it('describes position movement explicitly', () => {
    expect(positionChange(race({ startingPosition: 8, finishPosition: 3 }))).toBe('Gained 5')
    expect(positionChange(race({ startingPosition: 2, finishPosition: 5 }))).toBe('Lost 3')
    expect(positionChange(race({ startingPosition: 4, finishPosition: 4 }))).toBe('No change')
    expect(positionChange(race({ startingPosition: null, finishPosition: 4 }))).toBe('—')
  })
})

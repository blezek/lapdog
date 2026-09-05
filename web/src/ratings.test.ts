import { describe, expect, it } from 'vitest'

import type { RatingPoint } from './api'
import { ratingSeries } from './ratings'

function point(
  discipline: RatingPoint['discipline'],
  iRating: number | null,
): RatingPoint {
  return {
    startedAt: '2026-09-01T18:00:00Z',
    sessionType: 'Race',
    eventContext: 'OfficialRace',
    iRating,
    safetyRating: iRating == null ? 3.5 : 3.6,
    licString: 'A 3.60',
    discipline,
  }
}

describe('ratingSeries', () => {
  it('emits only disciplines with observations of the selected rating', () => {
    const points = [
      point('Formula', 2100),
      point('Road', 2400),
      point('Formula', 2150),
      point('Dirt Oval', null),
      point(null, 9999),
    ]

    const got = ratingSeries(points, (p) => p.iRating)

    expect(got.map((series) => series.discipline)).toEqual(['Road', 'Formula'])
    expect(got[0]?.points.map((p) => p.iRating)).toEqual([2400])
    expect(got[1]?.points.map((p) => p.iRating)).toEqual([2100, 2150])
  })
})

import { describe, expect, it } from 'vitest'

import { consistencyBand, dimensionLabel, otherLabel, pageFilter } from './entity'

describe('dimension labels', () => {
  it('names the entity and its opposite', () => {
    expect(dimensionLabel('car')).toBe('Car')
    expect(dimensionLabel('track')).toBe('Track')
    // A car page breaks down by track, and vice versa. Getting this backwards
    // would label every row of the pace table wrongly.
    expect(otherLabel('car')).toBe('Track')
    expect(otherLabel('track')).toBe('Car')
  })
})

describe('consistencyBand', () => {
  it('bands on the convention the research found', () => {
    expect(consistencyBand(99.4)).toBe('good')
    expect(consistencyBand(98)).toBe('good')
    expect(consistencyBand(97.9)).toBe('fair')
    expect(consistencyBand(95)).toBe('fair')
    expect(consistencyBand(94.9)).toBe('poor')
  })

  it('has no band for a missing value', () => {
    // Suppressed consistency must not be coloured as though it were poor.
    expect(consistencyBand(null)).toBe('none')
    expect(consistencyBand(undefined)).toBe('none')
  })

  it('has no band for a non-finite value', () => {
    // NaN or Infinity must not fall through to the numeric comparisons and be
    // scored as poor.
    expect(consistencyBand(NaN)).toBe('none')
    expect(consistencyBand(Infinity)).toBe('none')
  })
})

describe('pageFilter', () => {
  it("drops the page's own dimension and keeps the other", () => {
    // /cars?car=105 is reachable by a bookmark or a hand-edited URL even though
    // the car dropdown is hidden, and left in place it collapsed the left-hand
    // list to the single car being chosen from.
    const cars = pageFilter({ carIds: [105], trackIds: [18], from: '2026-01-01' }, 'car')
    expect(cars.carIds).toBeUndefined()
    expect(cars.trackIds).toEqual([18])
    expect(cars.from).toBe('2026-01-01')

    // The mirror image: "this track, in this car" stays expressible.
    const tracks = pageFilter({ carIds: [105], trackIds: [18] }, 'track')
    expect(tracks.trackIds).toBeUndefined()
    expect(tracks.carIds).toEqual([105])
  })

  it('does not mutate the filter it was given', () => {
    // The filter is memoised by useFilter and shared with every other query on
    // the page, so mutating it in place would strip the dimension from all of them.
    const original = { carIds: [105] }
    pageFilter(original, 'car')
    expect(original.carIds).toEqual([105])
  })
})

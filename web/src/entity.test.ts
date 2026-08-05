import { describe, expect, it } from 'vitest'

import { consistencyBand, dimensionLabel, otherLabel } from './entity'

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

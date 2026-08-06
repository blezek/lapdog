import { describe, expect, it } from 'vitest'

import { licenceLabel } from './format'

describe('licenceLabel', () => {
  it('takes the class from the licence string and the number from the rating', () => {
    // The number must be the value being plotted, not the one embedded in the
    // string. The dashboard printed "A 3.55" above a line whose last point was
    // 3.94 — a card contradicting its own chart — because it showed the string
    // wholesale.
    expect(licenceLabel('A 3.55', 3.94)).toBe('A 3.94')
  })

  it('falls back to the bare number when there is no licence string', () => {
    // The Safety Rating can be derived from LicSubLevel with no string present, and
    // a class of "undefined" would be worse than no class.
    expect(licenceLabel(null, 3.94)).toBe('3.94')
  })

  it('shows the licence string when there is no number to show', () => {
    expect(licenceLabel('A 3.55', null)).toBe('A 3.55')
  })

  it('reports nothing when neither is known', () => {
    expect(licenceLabel(null, null)).toBe('—')
  })

  it('handles the rookie class, which is a word rather than a letter', () => {
    expect(licenceLabel('R 2.10', 2.1)).toBe('R 2.10')
    expect(licenceLabel('Rookie 2.10', 2.1)).toBe('ROOKIE 2.10')
  })

  it('always shows two decimals, so a whole rating does not read as an integer', () => {
    // iRacing writes Safety Ratings to two places. A bare "4" beside "3.94" would
    // look like a different kind of quantity.
    expect(licenceLabel('A 4.00', 4)).toBe('A 4.00')
  })
})

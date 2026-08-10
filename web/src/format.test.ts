import { describe, expect, it } from 'vitest'

import { hm, hms, licenceLabel } from './format'

describe('hms', () => {
  it('keeps the seconds a live total is read at', () => {
    // The observation the Live page exists for: 154 seconds in the car against
    // zero driving seconds. hm renders those as "0:02" and "0:00", a single digit
    // apart for a difference of two and a half minutes, which is why the live
    // totals do not use it.
    expect(hms(154)).toBe('2:34')
    expect(hms(0)).toBe('0:00')
    expect(hm(154)).toBe('0:02')
  })

  it('adds an hours field only once there is one', () => {
    // A permanent "0:" in front would read as a clock rather than a duration on
    // the sessions this page mostly shows.
    expect(hms(3599)).toBe('59:59')
    expect(hms(3600)).toBe('1:00:00')
    expect(hms(3661)).toBe('1:01:01')
  })

  it('never renders a negative duration', () => {
    expect(hms(-5)).toBe('0:00')
  })
})

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

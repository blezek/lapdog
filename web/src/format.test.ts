import { describe, expect, it } from 'vitest'

import {
  distance,
  hm,
  hms,
  licenceLabel,
  speed,
  startOfMonth,
  startOfWeek,
  startOfYear,
} from './format'

describe('speed', () => {
  it('converts simulator metres per second to the selected display unit', () => {
    expect(speed(10, 'metric')).toBe('36 kph')
    expect(speed(10, 'imperial')).toBe('22 mph')
  })

  it('rounds to the nearest whole display unit', () => {
    expect(speed(10.1, 'metric')).toBe('36 kph')
    expect(speed(10.2, 'metric')).toBe('37 kph')
    expect(speed(10.2, 'imperial')).toBe('23 mph')
  })

  it('keeps a stationary car as a real zero reading', () => {
    expect(speed(0, 'metric')).toBe('0 kph')
    expect(speed(0, 'imperial')).toBe('0 mph')
  })
})

describe('distance', () => {
  it('uses kilometres for metric settings and miles for imperial settings', () => {
    expect(distance(16.09344, 'metric')).toBe('16.1 km')
    expect(distance(16.09344, 'imperial')).toBe('10.0 mi')
  })
})

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

describe('calendar-aligned starts', () => {
  // A Tuesday, so a Sunday-first week began two days earlier. These snap to a
  // boundary in the viewer's own zone, which is why "this week" means the week they
  // are in rather than the one UTC happens to be in.
  const tue = new Date(2026, 7, 11)

  it('starts the week on the Sunday on or before', () => {
    expect(startOfWeek(tue)).toBe('2026-08-09')
    // A Sunday is its own week start, not pushed back seven days.
    expect(startOfWeek(new Date(2026, 7, 9))).toBe('2026-08-09')
  })

  it('starts the month on the first', () => {
    expect(startOfMonth(tue)).toBe('2026-08-01')
  })

  it('starts the year on January 1st', () => {
    expect(startOfYear(tue)).toBe('2026-01-01')
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

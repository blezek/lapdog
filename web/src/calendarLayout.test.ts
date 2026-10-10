import { describe, expect, it } from 'vitest'
import { calendarCellSize, calendarSpan } from './calendarLayout'

describe('calendar layout', () => {
  it('pads a cross-year range to complete Sunday-to-Saturday weeks', () => {
    expect(calendarSpan(['2026-01-01', '2025-12-31'])).toEqual({
      range: ['2025-12-28', '2026-01-03'],
      years: ['2025', '2026'],
      weeks: 1,
    })
  })

  it('shrinks cells when a long range exceeds the available width', () => {
    expect(calendarCellSize(400, 52, 17, 84)).toBe(6)
    expect(calendarCellSize(400, 4, 17, 84)).toBe(17)
  })
})

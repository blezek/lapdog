import { describe, expect, it } from 'vitest'
import { toQuery } from './api'
import { isEmptyArray, keepPrevious, viewState } from './query'

describe('toQuery time filters', () => {
  it('emits multiple cars and tracks as lists', () => {
    const q = new URLSearchParams(toQuery({ carIds: [173, 45], trackIds: [18, 341] }))
    expect(q.get('car_id')).toBe('173,45')
    expect(q.get('track_id')).toBe('18,341')
  })
  it('emits the hour and weekday parameters the server parses', () => {
    const q = new URLSearchParams(toQuery({ hourFrom: 18, hourTo: 23, weekdays: [0, 6] }))
    expect(q.get('hour_from')).toBe('18')
    expect(q.get('hour_to')).toBe('23')
    expect(q.get('weekday')).toBe('0,6')
  })

  it('keeps an hour of zero, which is a real bound and not absent', () => {
    // Midnight is 0. Dropping it as falsy would silently widen "from midnight" to
    // "any time", the same absent-versus-zero trap the rest of the app guards.
    const q = new URLSearchParams(toQuery({ hourFrom: 0 }))
    expect(q.get('hour_from')).toBe('0')
  })

  it('omits an empty weekday set rather than sending a blank parameter', () => {
    const q = new URLSearchParams(toQuery({ weekdays: [] }))
    expect(q.has('weekday')).toBe(false)
  })
})

/**
 * These pin the invariant that makes chart transitions possible: a panel whose query
 * is refetching but already holds data must keep rendering the chart.
 *
 * The bug this prevents is subtle because nothing errors. Gating on isLoading unmounts
 * the chart on every filter change, ECharts disposes the instance, and the replacement
 * has no previous state to tween from — so the chart snaps to its new values and the
 * animation settings look broken when they are fine.
 */
describe('viewState', () => {
  it('is ready while refetching if data is already present', () => {
    // The case that matters: a filter changed, the new request is in flight, and
    // placeholder data from the previous filter is on screen.
    expect(viewState({ isError: false, data: [{ a: 1 }] })).toBe('ready')
  })

  it('is loading only when there is nothing at all to show', () => {
    expect(viewState({ isError: false, data: undefined })).toBe('loading')
  })

  it('distinguishes an empty result from a missing one', () => {
    // An empty array is an answer — "nothing matched" — not an absence.
    expect(viewState({ isError: false, data: [] }, isEmptyArray)).toBe('empty')
    expect(viewState({ isError: false, data: undefined }, isEmptyArray)).toBe('loading')
  })

  it('reports an error only when no data survives it', () => {
    expect(viewState({ isError: true, data: undefined })).toBe('error')
    // A failed background refetch must not blank a chart that still holds good data.
    expect(viewState({ isError: true, data: [{ a: 1 }] })).toBe('ready')
  })

  it('treats a non-array payload as present', () => {
    // Totals is an object, so the empty check does not apply to it.
    expect(viewState({ isError: false, data: { sessions: 0 } }, isEmptyArray)).toBe('ready')
  })
})

describe('keepPrevious', () => {
  it('returns the previous data as the placeholder for a new key', () => {
    const prev = [{ key: 'Race/League', drivingHours: 12 }]
    expect(keepPrevious.placeholderData(prev)).toBe(prev)
  })

  it('passes undefined through on the very first load', () => {
    // There is no previous data to hold on to, so the first load must still show its
    // loading state rather than an empty chart.
    expect(keepPrevious.placeholderData(undefined)).toBeUndefined()
  })
})

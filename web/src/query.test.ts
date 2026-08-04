import { describe, expect, it } from 'vitest'
import { isEmptyArray, keepPrevious, viewState } from './query'

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

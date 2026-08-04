/*
 * Query helpers for views that must not flicker.
 *
 * The filter is part of every query key, so changing a filter creates a new query
 * rather than refetching an existing one. Left alone that means isLoading goes true,
 * the component swaps the chart for a placeholder, and the chart unmounts — ECharts
 * disposes the instance. When the data arrives a fresh instance is created with no
 * previous state, so there is nothing to animate from and the chart appears to jump
 * to its new shape. Merge settings cannot fix that, because the old instance is
 * already gone.
 *
 * Two things together fix it: keep the previous data visible while the new data
 * loads, and decide what to render from whether any data exists rather than from
 * whether a fetch is in flight.
 */

import type { UseQueryResult } from '@tanstack/react-query'

/**
 * keepPrevious keeps the last successful data as the placeholder for the next key.
 *
 * Spread into the aggregate and list queries, so the chart stays mounted across a
 * filter change and ECharts can tween between the two datasets.
 *
 * Deliberately not applied to single-entity queries such as one session's laps:
 * there, showing the previous entity's data under the new entity's heading would be
 * briefly wrong rather than merely stale.
 */
export const keepPrevious = {
  placeholderData: <T,>(prev: T | undefined): T | undefined => prev,
} as const

/** ViewState is what a data-backed panel should render. */
export type ViewState = 'error' | 'loading' | 'empty' | 'ready'

/**
 * viewState decides what a panel shows.
 *
 * The distinction that matters is "ready" versus "loading": a query that is fetching
 * but already holds data — including placeholder data from the previous filter — is
 * ready, because unmounting the chart to show a spinner is what breaks the
 * transition. Only the very first load, with nothing to show at all, is loading.
 */
export function viewState(
  query: Pick<UseQueryResult<unknown>, 'isError' | 'data'>,
  isEmpty?: (data: unknown) => boolean,
): ViewState {
  if (query.isError && query.data === undefined) return 'error'
  if (query.data === undefined) return 'loading'
  if (isEmpty?.(query.data)) return 'empty'
  return 'ready'
}

/** isEmptyArray reports whether the payload is an empty list. */
export function isEmptyArray(data: unknown): boolean {
  return Array.isArray(data) && data.length === 0
}

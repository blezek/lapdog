/*
 * Filter state, held in the URL.
 *
 * Keeping it in the query string rather than in React state means a filtered view
 * survives a reload, can be bookmarked, and moves with the browser's back button —
 * and the Export page can hand the very same parameters to the server, which is
 * what makes an export match what is on screen.
 */

import { useCallback, useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import type { Filter } from './api'
import {
  daysAgo,
  isoDay,
  startOfMonth,
  startOfWeek,
  startOfYear,
} from './format'
import { todayLocal } from './locale'

/**
 * rangePresets are the date ranges the interface offers.
 *
 * The rolling presets ("Last N days") count back from today; the calendar ones
 * ("This week/month/year") snap to a boundary. "Custom range" carries no bounds of
 * its own — it reads the from/to it finds in the URL, which is what the heatmap and
 * the date inputs write.
 */
export const rangePresets = [
  { id: 'today', label: 'Today' },
  { id: 'yesterday', label: 'Yesterday' },
  { id: '7', label: 'Last 7 days' },
  { id: '30', label: 'Last 30 days' },
  { id: '90', label: 'Last 90 days' },
  { id: '365', label: 'Last 365 days' },
  { id: 'week', label: 'This week' },
  { id: 'month', label: 'This month' },
  { id: 'year', label: 'This year' },
  { id: 'all', label: 'All time' },
  { id: 'custom', label: 'Custom range' },
] as const

export type RangeId = (typeof rangePresets)[number]['id']

export interface FilterState extends Filter {
  range: RangeId
}

const DEFAULT_RANGE: RangeId = '90'

/**
 * rangeBounds turns a preset into the from/to dates to send the server.
 *
 * Each is a bare YYYY-MM-DD in the viewer's own zone; the server expands "from" to
 * the start of its day and "to" to the end. A missing bound is deliberate: the
 * rolling presets leave "to" open because there is nothing later than now, and
 * "all" leaves both open so the query stays index-friendly rather than carrying an
 * ancient lower bound.
 */
function rangeBounds(range: RangeId, params: URLSearchParams): { from?: string; to?: string } {
  switch (range) {
    case 'today': {
      const t = isoDay(todayLocal())
      return { from: t, to: t }
    }
    case 'yesterday': {
      const y = daysAgo(1)
      return { from: y, to: y }
    }
    case 'week':
      return { from: startOfWeek() }
    case 'month':
      return { from: startOfMonth() }
    case 'year':
      return { from: startOfYear() }
    case 'all':
      return {}
    case 'custom':
      return {
        from: params.get('from') || undefined,
        to: params.get('to') || undefined,
      }
    default: {
      const n = Number(range)
      return Number.isFinite(n) && n > 0 ? { from: daysAgo(n) } : {}
    }
  }
}

/** useFilter reads and writes the shared filter from the URL. */
export function useFilter() {
  const [params, setParams] = useSearchParams()

  const state = useMemo<FilterState>(() => {
    const rawRange = (params.get('range') ?? DEFAULT_RANGE) as RangeId
    const range = rangePresets.some((p) => p.id === rawRange) ? rawRange : DEFAULT_RANGE
    const bounds = rangeBounds(range, params)

    const list = (k: string) => {
      const raw = params.get(k)
      return raw ? raw.split(',').filter(Boolean) : undefined
    }
    const int = (k: string) => {
      const raw = params.get(k)
      if (!raw) return undefined
      const n = Number(raw)
      return Number.isFinite(n) ? n : undefined
    }
    const intList = (k: string) => {
      const vals = list(k)?.map(Number).filter((n) => Number.isFinite(n))
      return vals?.length ? vals : undefined
    }

    return {
      range,
      from: bounds.from,
      to: bounds.to,
      sessionType: list('type'),
      eventContext: list('context'),
      trackId: int('track'),
      carId: int('car'),
      leagueId: int('league'),
      hourFrom: int('hf'),
      hourTo: int('ht'),
      weekdays: intList('dow'),
      excludeAi: params.get('ai') === 'exclude',
    }
  }, [params])

  /** update merges a patch into the URL, dropping keys set to undefined. */
  const update = useCallback(
    (patch: Partial<Record<string, string | undefined>>) => {
      const next = new URLSearchParams(params)
      for (const [k, v] of Object.entries(patch)) {
        if (v == null || v === '') next.delete(k)
        else next.set(k, v)
      }
      setParams(next, { replace: false })
    },
    [params, setParams],
  )

  /** toggleIn adds or removes a value from a comma-separated parameter. */
  const toggleIn = useCallback(
    (key: string, value: string) => {
      const current = (params.get(key) ?? '').split(',').filter(Boolean)
      const next = current.includes(value)
        ? current.filter((v) => v !== value)
        : [...current, value]
      update({ [key]: next.length ? next.join(',') : undefined })
    },
    [params, update],
  )

  const clear = useCallback(() => {
    setParams(new URLSearchParams(), { replace: false })
  }, [setParams])

  /** filter is the plain Filter to send to the API. */
  const filter: Filter = useMemo(() => {
    const { range: _range, ...rest } = state
    return rest
  }, [state])

  const active = useMemo(() => {
    return (
      (state.sessionType?.length ?? 0) > 0 ||
      (state.eventContext?.length ?? 0) > 0 ||
      state.trackId != null ||
      state.carId != null ||
      state.leagueId != null ||
      state.hourFrom != null ||
      state.hourTo != null ||
      (state.weekdays?.length ?? 0) > 0 ||
      state.excludeAi === true ||
      state.range !== DEFAULT_RANGE
    )
  }, [state])

  return { state, filter, update, toggleIn, clear, active, params }
}

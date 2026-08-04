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
import { daysAgo } from './format'

/** RangePreset are the date ranges the interface offers. */
export const rangePresets = [
  { id: '7', label: 'Last 7 days', days: 7 },
  { id: '30', label: 'Last 30 days', days: 30 },
  { id: '90', label: 'Last 90 days', days: 90 },
  { id: '365', label: 'Last year', days: 365 },
  { id: 'all', label: 'All time', days: 0 },
] as const

export type RangeId = (typeof rangePresets)[number]['id']

export interface FilterState extends Filter {
  range: RangeId
}

const DEFAULT_RANGE: RangeId = '90'

/** useFilter reads and writes the shared filter from the URL. */
export function useFilter() {
  const [params, setParams] = useSearchParams()

  const state = useMemo<FilterState>(() => {
    const range = (params.get('range') ?? DEFAULT_RANGE) as RangeId
    const preset = rangePresets.find((p) => p.id === range) ?? rangePresets[2]

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

    return {
      range,
      // "All time" omits the bound entirely rather than sending an ancient date,
      // so the server's predicate stays empty and the query stays index-friendly.
      from: preset.days > 0 ? daysAgo(preset.days) : undefined,
      sessionType: list('type'),
      eventContext: list('context'),
      trackId: int('track'),
      carId: int('car'),
      leagueId: int('league'),
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
      state.excludeAi === true ||
      state.range !== DEFAULT_RANGE
    )
  }, [state])

  return { state, filter, update, toggleIn, clear, active, params }
}

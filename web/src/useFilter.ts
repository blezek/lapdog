/*
 * Filter state, held in the URL.
 *
 * Keeping it in the query string rather than in React state means a filtered view
 * survives a reload, can be bookmarked, and moves with the browser's back button —
 * and the Export page can hand the very same parameters to the server, which is
 * what makes an export match what is on screen.
 */

import { useCallback, useMemo, useState } from 'react'
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
const SAVED_FILTERS_KEY = 'lapdog.savedFilters.v1'

export const filterParamKeys = new Set([
  'range', 'from', 'to', 'type', 'context', 'track', 'car', 'league',
  'hf', 'ht', 'dow', 'ai',
])

export interface SavedFilterSet {
  id: string
  name: string
  query: string
}

/** filterParams drops page-local state such as an entity selection or table page. */
export function filterParams(input: URLSearchParams): URLSearchParams {
  const out = new URLSearchParams()
  for (const [key, value] of input) {
    if (filterParamKeys.has(key)) out.append(key, value)
  }
  return out
}

export function readSavedFilterSets(storage: Pick<Storage, 'getItem'>): SavedFilterSet[] {
  try {
    const raw = storage.getItem(SAVED_FILTERS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter((x): x is SavedFilterSet => {
      if (typeof x !== 'object' || x == null) return false
      const item = x as Record<string, unknown>
      return typeof item.id === 'string' && typeof item.name === 'string' && typeof item.query === 'string'
    })
  } catch {
    return []
  }
}

export function writeSavedFilterSets(
  storage: Pick<Storage, 'setItem'>,
  sets: SavedFilterSet[],
) {
  storage.setItem(SAVED_FILTERS_KEY, JSON.stringify(sets))
}

export function upsertSavedFilterSet(
  sets: SavedFilterSet[],
  name: string,
  query: string,
  newID: string,
): { sets: SavedFilterSet[]; id: string } {
  const existing = sets.find((s) => s.name.toLocaleLowerCase() === name.toLocaleLowerCase())
  const id = existing?.id ?? newID
  return {
    id,
    sets: existing
      ? sets.map((s) => s.id === id ? { ...s, name, query } : s)
      : [...sets, { id, name, query }],
  }
}

export function removeSavedFilterSet(sets: SavedFilterSet[], id: string): SavedFilterSet[] {
  return sets.filter((s) => s.id !== id)
}

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
      trackIds: intList('track'),
      carIds: intList('car'),
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

  const [savedSets, setSavedSets] = useState<SavedFilterSet[]>(() =>
    typeof localStorage === 'undefined' ? [] : readSavedFilterSets(localStorage),
  )

  const saveSet = useCallback((name: string): string | null => {
    const trimmed = name.trim()
    if (!trimmed || typeof localStorage === 'undefined') return null
    const query = filterParams(params).toString()
    const result = upsertSavedFilterSet(
      savedSets,
      trimmed,
      query,
      globalThis.crypto?.randomUUID?.() ?? `${Date.now()}`,
    )
    writeSavedFilterSets(localStorage, result.sets)
    setSavedSets(result.sets)
    return result.id
  }, [params, savedSets])

  const loadSet = useCallback((id: string) => {
    const found = savedSets.find((s) => s.id === id)
    if (!found) return false
    setParams(new URLSearchParams(found.query), { replace: false })
    return true
  }, [savedSets, setParams])

  const deleteSet = useCallback((id: string) => {
    if (typeof localStorage === 'undefined') return
    const next = removeSavedFilterSet(savedSets, id)
    writeSavedFilterSets(localStorage, next)
    setSavedSets(next)
  }, [savedSets])

  /** filter is the plain Filter to send to the API. */
  const filter: Filter = useMemo(() => {
    const { range: _range, ...rest } = state
    return rest
  }, [state])

  const active = useMemo(() => {
    return (
      (state.sessionType?.length ?? 0) > 0 ||
      (state.eventContext?.length ?? 0) > 0 ||
      (state.trackIds?.length ?? 0) > 0 ||
      (state.carIds?.length ?? 0) > 0 ||
      state.leagueId != null ||
      state.hourFrom != null ||
      state.hourTo != null ||
      (state.weekdays?.length ?? 0) > 0 ||
      state.excludeAi === true ||
      state.range !== DEFAULT_RANGE
    )
  }, [state])

  return { state, filter, update, toggleIn, clear, active, params, savedSets, saveSet, loadSet, deleteSet }
}

/* The shared filter row, present above every data view. */

import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { day, label } from '../format'
import { weekdayNames } from '../locale'
import { rangePresets, useFilter } from '../useFilter'
import { DateFilter } from './DateFilter'

/**
 * Filters renders the controls in a single row above the content.
 *
 * All of them write to the URL, so the same state drives the charts, the tables
 * and the export.
 */
export function Filters({
  matched,
  hide,
}: {
  matched?: string
  /**
   * hide suppresses a dimension's dropdown.
   *
   * The Cars and Tracks pages set their own dimension from the selected entity, so
   * showing a second control for the same thing invites the two to disagree about
   * what the page is about.
   */
  hide?: ('car' | 'track')[]
}) {
  const { state, update, clear, active } = useFilter()
  const { data: facets } = useQuery({ queryKey: ['facets'], queryFn: api.facets })

  const typeContextOptions = (facets?.sessionTypes ?? []).flatMap((t) =>
    (facets?.eventContexts ?? []).map((c) => ({ type: t, context: c })),
  )

  return (
    <div className="filters">
      <DateFilter />

      <label className={`control${state.sessionType?.length ? ' control-active' : ''}`}>
        <select
          value={state.sessionType?.[0] ?? ''}
          onChange={(e) => update({ type: e.target.value || undefined })}
          aria-label="Session type"
        >
          <option value="">All session types</option>
          {(facets?.sessionTypes ?? []).map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </select>
      </label>

      <label className={`control${state.eventContext?.length ? ' control-active' : ''}`}>
        <select
          value={state.eventContext?.[0] ?? ''}
          onChange={(e) => update({ context: e.target.value || undefined })}
          aria-label="Event context"
        >
          <option value="">All contexts</option>
          {(facets?.eventContexts ?? []).map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
      </label>

      {!hide?.includes('track') && (
        <label className={`control${state.trackId != null ? ' control-active' : ''}`}>
          <select
            value={state.trackId ?? ''}
            onChange={(e) => update({ track: e.target.value || undefined })}
            aria-label="Track"
          >
            <option value="">All tracks</option>
            {(facets?.tracks ?? []).map((t) => (
              <option key={t.id} value={t.id}>
                {t.name} ({t.sessions})
              </option>
            ))}
          </select>
        </label>
      )}

      {!hide?.includes('car') && (
        <label className={`control${state.carId != null ? ' control-active' : ''}`}>
          <select
            value={state.carId ?? ''}
            onChange={(e) => update({ car: e.target.value || undefined })}
            aria-label="Car"
          >
            <option value="">All cars</option>
            {(facets?.cars ?? []).map((c) => (
              <option key={c.id} value={c.id}>
                {c.name} ({c.sessions})
              </option>
            ))}
          </select>
        </label>
      )}

      {/* AI results are not comparable to human ones, so excluding them is a
          first-class control rather than buried in the context filter. */}
      <label className={`control${state.excludeAi ? ' control-active' : ''}`}>
        <input
          type="checkbox"
          checked={state.excludeAi ?? false}
          onChange={(e) => update({ ai: e.target.checked ? 'exclude' : undefined })}
        />
        Exclude AI
      </label>

      {active && (
        <button type="button" className="control" onClick={clear}>
          Clear
        </button>
      )}

      {matched && <span className="matched">{matched}</span>}
      {typeContextOptions.length === 0 && null}
    </div>
  )
}

/** describeFilter renders the active filter as a sentence, for the export page. */
export function describeFilter(state: ReturnType<typeof useFilter>['state']): string {
  const parts: string[] = []
  if (state.range === 'custom') {
    if (state.from && state.to) parts.push(`${day(state.from)} to ${day(state.to)}`)
    else if (state.from) parts.push(`from ${day(state.from)}`)
    else if (state.to) parts.push(`until ${day(state.to)}`)
    else parts.push('custom range')
  } else {
    const preset = rangePresets.find((p) => p.id === state.range)
    if (preset) parts.push(preset.label.toLowerCase())
  }
  if (state.hourFrom != null || state.hourTo != null) {
    const a = state.hourFrom != null ? `${String(state.hourFrom).padStart(2, '0')}:00` : '00:00'
    const b = state.hourTo != null ? `${String(state.hourTo).padStart(2, '0')}:59` : '23:59'
    parts.push(`${a}–${b}`)
  }
  if (state.weekdays?.length) {
    const names = weekdayNames()
    parts.push([...state.weekdays].sort((x, y) => x - y).map((i) => names[i]).join(''))
  }
  if (state.sessionType?.length && state.eventContext?.length) {
    parts.push(label(state.sessionType[0]!, state.eventContext[0]!))
  } else {
    if (state.sessionType?.length) parts.push(state.sessionType.join(', '))
    if (state.eventContext?.length) parts.push(state.eventContext.join(', '))
  }
  if (state.excludeAi) parts.push('excluding AI')
  return parts.length ? parts.join(' · ') : 'everything'
}

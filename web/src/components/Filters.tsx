/* The shared filter row, present above every data view. */

import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { label } from '../format'
import { rangePresets, useFilter } from '../useFilter'

/**
 * Filters renders the controls in a single row above the content.
 *
 * All of them write to the URL, so the same state drives the charts, the tables
 * and the export.
 */
export function Filters({ matched }: { matched?: string }) {
  const { state, update, clear, active } = useFilter()
  const { data: facets } = useQuery({ queryKey: ['facets'], queryFn: api.facets })

  const typeContextOptions = (facets?.sessionTypes ?? []).flatMap((t) =>
    (facets?.eventContexts ?? []).map((c) => ({ type: t, context: c })),
  )

  return (
    <div className="filters">
      <label className="control control-active">
        <select
          value={state.range}
          onChange={(e) => update({ range: e.target.value })}
          aria-label="Date range"
        >
          {rangePresets.map((p) => (
            <option key={p.id} value={p.id}>
              {p.label}
            </option>
          ))}
        </select>
      </label>

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
  const preset = rangePresets.find((p) => p.id === state.range)
  if (preset) parts.push(preset.label.toLowerCase())
  if (state.sessionType?.length && state.eventContext?.length) {
    parts.push(label(state.sessionType[0]!, state.eventContext[0]!))
  } else {
    if (state.sessionType?.length) parts.push(state.sessionType.join(', '))
    if (state.eventContext?.length) parts.push(state.eventContext.join(', '))
  }
  if (state.excludeAi) parts.push('excluding AI')
  return parts.length ? parts.join(' · ') : 'everything'
}

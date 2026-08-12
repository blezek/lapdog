/* The shared filter row, present above every historical data view. */

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { day, label } from '../format'
import { weekdayNames } from '../locale'
import { rangePresets, useFilter } from '../useFilter'
import { DateFilter } from './DateFilter'

interface Option {
  value: string
  label: string
}

/** Filters writes one shared URL state, which navigation carries between views. */
export function Filters({
  matched,
  hide,
}: {
  matched?: string
  hide?: ('car' | 'track')[]
}) {
  const { state, update, toggleIn, clear, active } = useFilter()
  const { data: facets } = useQuery({ queryKey: ['facets'], queryFn: api.facets })

  return (
    <div className="filters-wrap">
      <div className="filters">
        <DateFilter />

        <MultiFilter
          label="Session types"
          allLabel="All session types"
          param="type"
          selected={(state.sessionType ?? []).map(String)}
          options={(facets?.sessionTypes ?? []).map((value) => ({ value, label: value }))}
          toggle={toggleIn}
        />

        <MultiFilter
          label="Contexts"
          allLabel="All contexts"
          param="context"
          selected={(state.eventContext ?? []).map(String)}
          options={(facets?.eventContexts ?? []).map((value) => ({ value, label: value }))}
          toggle={toggleIn}
        />

        {!hide?.includes('track') && (
          <MultiFilter
            label="Tracks"
            allLabel="All tracks"
            param="track"
            selected={(state.trackIds ?? []).map(String)}
            options={(facets?.tracks ?? []).map((x) => ({
              value: String(x.id),
              label: `${x.name} (${x.sessions})`,
            }))}
            toggle={toggleIn}
            searchable
          />
        )}

        {!hide?.includes('car') && (
          <MultiFilter
            label="Cars"
            allLabel="All cars"
            param="car"
            selected={(state.carIds ?? []).map(String)}
            options={(facets?.cars ?? []).map((x) => ({
              value: String(x.id),
              label: `${x.name} (${x.sessions})`,
            }))}
            toggle={toggleIn}
            searchable
          />
        )}

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
      </div>
      <FilterSetManager />
    </div>
  )
}

function MultiFilter({
  label,
  allLabel,
  param,
  selected,
  options,
  toggle,
  searchable = false,
}: {
  label: string
  allLabel: string
  param: string
  selected: string[]
  options: Option[]
  toggle: (key: string, value: string) => void
  searchable?: boolean
}) {
  const [search, setSearch] = useState('')
  const selectedSet = new Set(selected)
  const shown = search
    ? options.filter((o) => o.label.toLocaleLowerCase().includes(search.toLocaleLowerCase()))
    : options
  const summary = selected.length === 0
    ? allLabel
    : selected.length === 1
      ? (options.find((o) => o.value === selected[0])?.label.replace(/ \(\d+\)$/, '') ?? `1 ${label.toLocaleLowerCase()}`)
      : `${selected.length} ${label.toLocaleLowerCase()}`

  return (
    <details className={`filter-menu${selected.length ? ' control-active' : ''}`}>
      <summary className="control" aria-label={label}>{summary}</summary>
      <div className="filter-menu-panel">
        {searchable && (
          <input
            type="search"
            className="filter-search"
            value={search}
            placeholder={`Find ${label.toLocaleLowerCase()}`}
            onChange={(e) => setSearch(e.target.value)}
          />
        )}
        <div className="filter-options">
          {shown.map((option) => (
            <label key={option.value} className="filter-option">
              <input
                type="checkbox"
                checked={selectedSet.has(option.value)}
                onChange={() => toggle(param, option.value)}
              />
              <span>{option.label}</span>
            </label>
          ))}
          {shown.length === 0 && <span className="filter-none">No matches</span>}
        </div>
      </div>
    </details>
  )
}

function FilterSetManager() {
  const { savedSets, saveSet, loadSet, deleteSet } = useFilter()
  const [selected, setSelected] = useState('')
  const [name, setName] = useState('')

  const save = () => {
    const id = saveSet(name)
    if (id) {
      setSelected(id)
      setName('')
    }
  }

  return (
    <div className="filter-sets" aria-label="Saved filter sets">
      <span className="filter-sets-title">Saved filters</span>
      <select
        aria-label="Saved filter set"
        value={selected}
        onChange={(e) => {
          setSelected(e.target.value)
          if (e.target.value) loadSet(e.target.value)
        }}
      >
        <option value="">Choose a set…</option>
        {savedSets.map((set) => <option key={set.id} value={set.id}>{set.name}</option>)}
      </select>
      <input
        aria-label="Filter set name"
        value={name}
        placeholder="Name this filter"
        onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Enter') save() }}
      />
      <button type="button" className="control" disabled={!name.trim()} onClick={save}>Save</button>
      <button
        type="button"
        className="control danger"
        disabled={!selected}
        onClick={() => {
          deleteSet(selected)
          setSelected('')
        }}
      >
        Delete
      </button>
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
    if (state.sessionType.length === 1 && state.eventContext.length === 1) {
      parts.push(label(state.sessionType[0]!, state.eventContext[0]!))
    } else {
      parts.push(`${state.sessionType.join(', ')} · ${state.eventContext.join(', ')}`)
    }
  } else {
    if (state.sessionType?.length) parts.push(state.sessionType.join(', '))
    if (state.eventContext?.length) parts.push(state.eventContext.join(', '))
  }
  if (state.carIds?.length) parts.push(`${state.carIds.length} car${state.carIds.length === 1 ? '' : 's'}`)
  if (state.trackIds?.length) parts.push(`${state.trackIds.length} track${state.trackIds.length === 1 ? '' : 's'}`)
  if (state.excludeAi) parts.push('excluding AI')
  return parts.length ? parts.join(' · ') : 'everything'
}

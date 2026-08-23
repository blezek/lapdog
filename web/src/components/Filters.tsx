/* The shared filter row, present above every historical data view. */

import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api } from '../api'
import { day, label } from '../format'
import { weekdayNames } from '../locale'
import {
  canonicalFilterQuery,
  filterParams,
  rangePresets,
  savedFilterSummary,
  useFilter,
} from '../useFilter'
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
  hide?: ('car' | 'track' | 'type')[]
}) {
  const { state, update, toggleIn, clear, active } = useFilter()
  const { data: facets } = useQuery({ queryKey: ['facets'], queryFn: api.facets })
  const [openMenu, setOpenMenu] = useState<string | null>(null)
  const wrap = useRef<HTMLDivElement | null>(null)

  // Every filter belongs to one toolbar, so opening one dismisses the previous
  // popover. Outside clicks and Escape dismiss the toolbar as a whole rather than
  // making each menu install a competing document listener.
  useEffect(() => {
    if (openMenu == null) return
    const onDown = (event: MouseEvent) => {
      if (wrap.current && !wrap.current.contains(event.target as Node)) setOpenMenu(null)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOpenMenu(null)
      wrap.current
        ?.querySelector<HTMLElement>(`[data-filter-trigger="${openMenu}"]`)
        ?.focus()
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [openMenu])

  const toggleMenu = (menu: string) => setOpenMenu((current) => current === menu ? null : menu)

  return (
    <div className="filters-wrap" ref={wrap}>
      <div className="filters">
        <DateFilter
          open={openMenu === 'date'}
          onToggle={() => toggleMenu('date')}
          onClose={() => setOpenMenu(null)}
        />

        {!hide?.includes('type') && (
          <MultiFilter
            id="type"
            label="Session types"
            allLabel="All session types"
            param="type"
            selected={(state.sessionType ?? []).map(String)}
            options={(facets?.sessionTypes ?? []).map((value) => ({ value, label: value }))}
            toggle={toggleIn}
            clear={() => update({ type: undefined })}
            open={openMenu === 'type'}
            onToggle={() => toggleMenu('type')}
          />
        )}

        <MultiFilter
          id="context"
          label="Contexts"
          allLabel="All contexts"
          param="context"
          selected={(state.eventContext ?? []).map(String)}
          options={(facets?.eventContexts ?? []).map((value) => ({ value, label: value }))}
          toggle={toggleIn}
          clear={() => update({ context: undefined })}
          open={openMenu === 'context'}
          onToggle={() => toggleMenu('context')}
        />

        {!hide?.includes('track') && (
          <MultiFilter
            id="track"
            label="Tracks"
            allLabel="All tracks"
            param="track"
            selected={(state.trackIds ?? []).map(String)}
            options={(facets?.tracks ?? []).map((x) => ({
              value: String(x.id),
              label: `${x.name} (${x.sessions})`,
            }))}
            toggle={toggleIn}
            clear={() => update({ track: undefined })}
            open={openMenu === 'track'}
            onToggle={() => toggleMenu('track')}
            searchable
          />
        )}

        {!hide?.includes('car') && (
          <MultiFilter
            id="car"
            label="Cars"
            allLabel="All cars"
            param="car"
            selected={(state.carIds ?? []).map(String)}
            options={(facets?.cars ?? []).map((x) => ({
              value: String(x.id),
              label: `${x.name} (${x.sessions})`,
            }))}
            toggle={toggleIn}
            clear={() => update({ car: undefined })}
            open={openMenu === 'car'}
            onToggle={() => toggleMenu('car')}
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

        <FilterSetManager
          open={openMenu === 'saved'}
          onToggle={() => toggleMenu('saved')}
          onClose={() => setOpenMenu(null)}
        />

        {active && (
          <button type="button" className="control" onClick={clear}>
            Clear
          </button>
        )}

        {matched && <span className="matched">{matched}</span>}
      </div>
    </div>
  )
}

function MultiFilter({
  id,
  label,
  allLabel,
  param,
  selected,
  options,
  toggle,
  clear,
  open,
  onToggle,
  searchable = false,
}: {
  id: string
  label: string
  allLabel: string
  param: string
  selected: string[]
  options: Option[]
  toggle: (key: string, value: string) => void
  clear: () => void
  open: boolean
  onToggle: () => void
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
    <div className={`filter-menu${selected.length ? ' control-active' : ''}`}>
      <button
        type="button"
        id={`filter-trigger-${id}`}
        className="control"
        aria-label={label}
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls={`filter-panel-${id}`}
        data-filter-trigger={id}
        onClick={onToggle}
      >
        {summary}
      </button>
      {open && <div
        id={`filter-panel-${id}`}
        className="filter-menu-panel"
        role="group"
        aria-labelledby={`filter-trigger-${id}`}
      >
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
        {selected.length > 0 && (
          <button type="button" className="filter-menu-clear" onClick={clear}>
            Clear {label.toLocaleLowerCase()}
          </button>
        )}
      </div>}
    </div>
  )
}

function FilterSetManager({
  open,
  onToggle,
  onClose,
}: {
  open: boolean
  onToggle: () => void
  onClose: () => void
}) {
  const { params, savedSets, saveSet, loadSet, deleteSet, clear } = useFilter()
  const [loadedID, setLoadedID] = useState<string | null>(null)
  const [mode, setMode] = useState<'list' | 'save' | 'manage'>('list')
  const [name, setName] = useState('')
  const currentQuery = canonicalFilterQuery(filterParams(params))
  const activeSet = savedSets.find((set) => canonicalFilterQuery(set.query) === currentQuery)
  const loadedSet = savedSets.find((set) => set.id === loadedID)
  const currentLabel = activeSet?.name
    ?? (loadedSet ? `${loadedSet.name} • Modified` : currentQuery ? 'Custom' : 'Default')

  useEffect(() => {
    if (!open) setMode('list')
  }, [open])

  const save = () => {
    const id = saveSet(name)
    if (id) {
      setLoadedID(id)
      setName('')
      onClose()
    }
  }

  const load = (id: string) => {
    if (!loadSet(id)) return
    setLoadedID(id)
    onClose()
  }

  const loadDefault = () => {
    clear()
    setLoadedID(null)
    onClose()
  }
  const defaultActive = currentQuery === '' && activeSet == null

  return (
    <div className="filter-menu filter-sets">
      <button
        type="button"
        id="filter-trigger-saved"
        className="control"
        aria-haspopup="true"
        aria-expanded={open}
        aria-controls="filter-panel-saved"
        data-filter-trigger="saved"
        onClick={onToggle}
      >
        View: {currentLabel}
      </button>
      {open && <div
        id="filter-panel-saved"
        className="filter-menu-panel filter-sets-panel"
        role="group"
        aria-labelledby="filter-trigger-saved"
      >
        {mode === 'list' && <>
          <div className="filter-sets-title">Choose a view</div>
          <div className="saved-view-list">
            <button
              type="button"
              className={`saved-view${defaultActive ? ' saved-view-active' : ''}`}
              data-saved-view="default"
              onClick={loadDefault}
            >
              <span className="saved-view-check" aria-hidden="true">{defaultActive ? '✓' : ''}</span>
              <span>
                <strong>Default</strong>
                <small>Last 90 days · no other filters</small>
              </span>
            </button>
            {savedSets.map((set) => {
              const active = activeSet?.id === set.id
              return <button
                key={set.id}
                type="button"
                className={`saved-view${active ? ' saved-view-active' : ''}`}
                data-saved-view={set.id}
                onClick={() => load(set.id)}
              >
                <span className="saved-view-check" aria-hidden="true">{active ? '✓' : ''}</span>
                <span>
                  <strong>{set.name}</strong>
                  <small>{savedFilterSummary(set.query)}</small>
                </span>
              </button>
            })}
            {savedSets.length === 0 && (
              <div className="saved-view-empty">No saved views yet.</div>
            )}
          </div>
          <div className="saved-view-actions">
            <button type="button" onClick={() => setMode('save')}>+ Save current filters…</button>
            {savedSets.length > 0 && (
              <button type="button" onClick={() => setMode('manage')}>Manage saved views…</button>
            )}
          </div>
        </>}

        {mode === 'save' && <>
          <div className="filter-sets-title">Save current filters</div>
          <div className="filter-set-row">
            <input
              autoFocus
              aria-label="Saved view name"
              value={name}
              placeholder="Name this view"
              onChange={(event) => setName(event.target.value)}
              onKeyDown={(event) => { if (event.key === 'Enter') save() }}
            />
            <button type="button" className="control" disabled={!name.trim()} onClick={save}>Save</button>
          </div>
          <button type="button" className="saved-view-back" onClick={() => setMode('list')}>
            ← Back to saved views
          </button>
        </>}

        {mode === 'manage' && <>
          <div className="filter-sets-title">Manage saved views</div>
          <div className="saved-view-manage-list">
            {savedSets.map((set) => (
              <div className="saved-view-manage" key={set.id}>
                <span>{set.name}</span>
                <button
                  type="button"
                  className="danger"
                  aria-label={`Delete ${set.name}`}
                  onClick={() => {
                    deleteSet(set.id)
                    if (loadedID === set.id) setLoadedID(null)
                  }}
                >
                  Delete
                </button>
              </div>
            ))}
          </div>
          <button type="button" className="saved-view-back" onClick={() => setMode('list')}>
            ← Back to saved views
          </button>
        </>}
      </div>}
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

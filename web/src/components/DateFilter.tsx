/*
 * The dates-and-time control.
 *
 * One button in the filter row opens a popover holding everything time-related:
 * the range presets, an explicit from/to range, a calendar heatmap to pick dates
 * off, and the two dimensions that are about *when* rather than *how long* — the
 * hour of day and the day of week. They live together because they answer the same
 * question ("which sessions, by time") and because keeping them in the row would
 * make it four controls longer.
 *
 * Everything writes to the URL through useFilter, so a chosen window survives a
 * reload and travels to the export unchanged, exactly like the other filters.
 */

import { useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type DailyRow, type Filter } from '../api'
import { day, dayShort } from '../format'
import { monthNames, weekdayNames } from '../locale'
import { rangePresets, useFilter, type RangeId } from '../useFilter'
import { useTheme, type Theme } from '../theme'
import { Chart, tooltipStyle, useElementWidth } from './Chart'

/** hourOptions is Any plus every hour of the day, for the two hour selects. */
const hourOptions = Array.from({ length: 24 }, (_, h) => h)

export function DateFilter({
  open,
  onToggle,
  onClose,
}: {
  open: boolean
  onToggle: () => void
  onClose: () => void
}) {
  const { state } = useFilter()

  const timeActive =
    state.hourFrom != null || state.hourTo != null || (state.weekdays?.length ?? 0) > 0
  const summary = summarize(state)

  return (
    <div className="datefilter">
      <button
        type="button"
        id="filter-trigger-date"
        className={`control${state.range !== '90' || timeActive ? ' control-active' : ''}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls="filter-panel-date"
        data-filter-trigger="date"
        onClick={onToggle}
      >
        <span aria-hidden="true">🗓</span>
        {summary}
      </button>

      {open && <DatePanel onClose={onClose} />}
    </div>
  )
}

/** DatePanel is the popover body, split out so its many hooks stay off the button. */
function DatePanel({ onClose }: { onClose: () => void }) {
  const { state, filter, update, toggleIn } = useFilter()
  const theme = useTheme()
  const { data: config } = useQuery({ queryKey: ['settings'], queryFn: api.settings })

  // A range is chosen a day at a time: the first click sets both ends to that day,
  // the second stretches the window to span the two. Held in a ref so the chart's
  // click handler always reads the latest anchor without re-subscribing, and mirrored
  // into state only to drive the hint text.
  const anchor = useRef<string | null>(null)
  const [picking, setPicking] = useState(false)

  const pickDay = (d: string) => {
    if (anchor.current == null) {
      anchor.current = d
      setPicking(true)
      update({ range: 'custom', from: d, to: d })
      return
    }
    const [from, to] = [anchor.current, d].sort()
    anchor.current = null
    setPicking(false)
    update({ range: 'custom', from, to })
  }

  const setPreset = (id: RangeId) => {
    anchor.current = null
    setPicking(false)
    applyDatePreset(id, update, onClose)
  }

  const setCustomDate = (which: 'from' | 'to', value: string) => {
    anchor.current = null
    setPicking(false)
    update({ range: 'custom', [which]: value || undefined })
  }

  const resetDateTime = () => applyDateTimeReset(update, onClose)

  const weekdays = weekdayNames()
  const selectedDow = new Set(state.weekdays ?? [])

  return (
    <div
      id="filter-panel-date"
      className="datepanel"
      role="dialog"
      aria-label="Dates and time filters"
      aria-labelledby="filter-trigger-date"
    >
      <div className="datepanel-presets">
        {rangePresets.map((p) => (
          <button
            key={p.id}
            type="button"
            className={`chip${state.range === p.id ? ' chip-on' : ''}`}
            onClick={() => setPreset(p.id)}
          >
            {p.label}
          </button>
        ))}
      </div>

      {config?.debug && <DebugDateBounds filter={filter} />}

      <div className="datepanel-section">
        <div className="datepanel-heading">Custom range</div>
        <div className="datepanel-dates">
          <label>
            From
            <input
              type="date"
              value={state.range === 'custom' ? (state.from ?? '') : ''}
              max={state.to}
              onChange={(e) => setCustomDate('from', e.target.value)}
            />
          </label>
          <label>
            To
            <input
              type="date"
              value={state.range === 'custom' ? (state.to ?? '') : ''}
              min={state.from}
              onChange={(e) => setCustomDate('to', e.target.value)}
            />
          </label>
        </div>
        <HeatmapPicker filter={state} theme={theme} onPick={pickDay} />
        <div className="datepanel-hint">
          {picking
            ? 'Now click the other end of the range.'
            : 'Click a day you drove, then another, to select a range.'}
        </div>
      </div>

      <div className="datepanel-section">
        <div className="datepanel-heading">Time of day</div>
        <div className="datepanel-hours">
          <label>
            From
            <select
              value={state.hourFrom ?? ''}
              onChange={(e) => update({ hf: e.target.value || undefined })}
            >
              <option value="">Any</option>
              {hourOptions.map((h) => (
                <option key={h} value={h}>
                  {hourLabel(h)}
                </option>
              ))}
            </select>
          </label>
          <label>
            To
            <select
              value={state.hourTo ?? ''}
              onChange={(e) => update({ ht: e.target.value || undefined })}
            >
              <option value="">Any</option>
              {hourOptions.map((h) => (
                <option key={h} value={h}>
                  {hourLabel(h)}
                </option>
              ))}
            </select>
          </label>
        </div>
      </div>

      <div className="datepanel-section">
        <div className="datepanel-heading">Days of the week</div>
        <div className="datepanel-days">
          {weekdays.map((name, i) => (
            <button
              key={i}
              type="button"
              className={`chip${selectedDow.has(i) ? ' chip-on' : ''}`}
              aria-pressed={selectedDow.has(i)}
              aria-label={`weekday ${name}`}
              onClick={() => toggleIn('dow', String(i))}
            >
              {name}
            </button>
          ))}
        </div>
      </div>

      {(state.range !== '90' || state.hourFrom != null || state.hourTo != null ||
        (state.weekdays?.length ?? 0) > 0) && (
        <button type="button" className="datepanel-clear" onClick={resetDateTime}>
          Reset date and time
        </button>
      )}
    </div>
  )
}

/** applyDatePreset closes one-click choices but leaves Custom open for editing. */
export function applyDatePreset(
  id: RangeId,
  update: (patch: { range: RangeId; from: undefined; to: undefined }) => void,
  dismiss: () => void,
) {
  // A preset owns its own bounds, so any custom from/to must go or it would linger
  // in the URL and reappear the next time "Custom range" was selected.
  update({ range: id, from: undefined, to: undefined })
  if (id !== 'custom') dismiss()
}

/** applyDateTimeReset is another complete one-click choice, so it also dismisses. */
export function applyDateTimeReset(
  update: (patch: {
    range: RangeId
    from: undefined
    to: undefined
    hf: undefined
    ht: undefined
    dow: undefined
  }) => void,
  dismiss: () => void,
) {
  update({
    range: '90',
    from: undefined,
    to: undefined,
    hf: undefined,
    ht: undefined,
    dow: undefined,
  })
  dismiss()
}

/** DebugDateBounds reports the server's own interpretation of the filter. */
function DebugDateBounds({ filter }: { filter: Filter }) {
  const { data } = useQuery({
    queryKey: ['filter-bounds', filter],
    queryFn: () => api.filterBounds(filter),
  })

  return (
    <div className="datepanel-section datepanel-debug" aria-label="Resolved date filter bounds">
      <div className="datepanel-heading">Debug: resolved date filter</div>
      <div className="datepanel-debug-row">
        <span>Beginning</span>
        <code>{data ? (data.beginning || 'No lower bound') : 'Resolving…'}</code>
      </div>
      <div className="datepanel-debug-row">
        <span>End</span>
        <code>{data ? (data.end || 'No upper bound') : 'Resolving…'}</code>
      </div>
    </div>
  )
}

/**
 * HeatmapPicker is a calendar of driving hours the user clicks to choose dates.
 *
 * The date bounds and the time-of-day and weekday constraints are stripped from the
 * filter it queries: the picker's job is to show the whole span of days available so
 * any of them can be chosen, not to shrink to the window already selected. The other
 * dimensions — car, track, session type — are kept, so the density shown is of the
 * sessions the rest of the filter is about.
 *
 * Only days with recorded driving carry a data point, so only those are clickable.
 * That is the right constraint for a "when did I drive" picker; the date inputs above
 * cover any calendar day, driven or not.
 */
function HeatmapPicker({
  filter,
  theme,
  onPick,
}: {
  filter: Filter
  theme: Theme
  onPick: (day: string) => void
}) {
  const pickerFilter: Filter = useMemo(() => {
    const { range: _range, from, to, hourFrom, hourTo, weekdays, ...rest } = filter
    return rest
  }, [filter])

  const { data } = useQuery({
    queryKey: ['daily-picker', pickerFilter],
    queryFn: () => api.daily(pickerFilter),
  })

  const [ref, width] = useElementWidth<HTMLDivElement>()
  const rows = data ?? []

  const onEvent = useMemo(
    () => ({
      type: 'click',
      handler: (params: unknown) => {
        const p = params as { data?: unknown; value?: unknown }
        const item = Array.isArray(p.data) ? p.data : Array.isArray(p.value) ? p.value : null
        const d = item?.[0]
        if (typeof d === 'string') onPick(d)
      },
    }),
    [onPick],
  )

  const option = useMemo(() => calendarOption(rows, theme, width), [rows, theme, width])

  return (
    <div className="datepanel-heatmap" ref={ref}>
      {rows.length === 0 ? (
        <div className="datepanel-hint">No sessions match the other filters yet.</div>
      ) : (
        <Chart
          option={option}
          className="chart datepicker-cal"
          onEvent={onEvent}
          ariaLabel="Calendar of driving days — click one to filter to it"
        />
      )}
    </div>
  )
}

const CellMax = 15

/** calendarOption builds the ECharts calendar-heatmap option for the picker. */
function calendarOption(rows: DailyRow[], theme: Theme, width: number) {
  const data = rows.map((r) => [r.day, Number(r.drivingHours.toFixed(2))])
  const max = Math.max(1, ...rows.map((r) => r.drivingHours))

  const days = rows.map((r) => r.day).sort()
  const first = days[0] ?? '2020-01-01'
  const last = days[days.length - 1] ?? first
  const rangeStart = weekStart(first)
  const rangeEnd = weekEnd(last)
  const spanDays = (Date.parse(rangeEnd) - Date.parse(rangeStart)) / 86_400_000 + 1
  const weeks = Math.max(1, Math.round(spanDays / 7))
  const usable = Math.max(0, width - 24)
  const cell = width > 0 ? Math.max(3, Math.min(CellMax, Math.floor(usable / weeks))) : CellMax
  const years = [...new Set(days.map((d) => d.slice(0, 4)))]

  return {
    tooltip: {
      ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
      formatter: (p: { value: [string, number] }) =>
        `${day(p.value[0])}<br/><strong>${p.value[1].toFixed(1)} h</strong> driving`,
    },
    visualMap: {
      min: 0,
      max,
      type: 'continuous',
      show: false,
      inRange: { color: theme.seq },
    },
    calendar: {
      top: 22,
      left: 16,
      right: 8,
      range: [rangeStart, rangeEnd],
      cellSize: [cell, cell],
      splitLine: { show: false },
      itemStyle: { color: 'transparent', borderWidth: 1, borderColor: theme.surface },
      yearLabel: { show: years.length > 1, color: theme.textMuted, fontSize: 9 },
      monthLabel: { color: theme.textMuted, fontSize: 9, nameMap: monthNames() },
      dayLabel: { color: theme.textMuted, fontSize: 8, firstDay: 0, nameMap: weekdayNames() },
    },
    series: [{ type: 'heatmap', coordinateSystem: 'calendar', data }],
  }
}

/** weekStart returns the Sunday on or before an ISO date, matching the grid. */
function weekStart(iso: string): string {
  const d = new Date(`${iso}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() - d.getUTCDay())
  return d.toISOString().slice(0, 10)
}

/** weekEnd returns the Saturday on or after an ISO date. */
function weekEnd(iso: string): string {
  const d = new Date(`${iso}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + (6 - d.getUTCDay()))
  return d.toISOString().slice(0, 10)
}

/** hourLabel renders an hour as a clock time, e.g. 18 as "18:00". */
function hourLabel(h: number): string {
  return `${String(h).padStart(2, '0')}:00`
}

/** summarize renders the current selection as the button's short label. */
function summarize(state: ReturnType<typeof useFilter>['state']): string {
  const preset = rangePresets.find((p) => p.id === state.range)
  let range: string
  if (state.range === 'custom') {
    if (state.from && state.to) range = `${dayShort(state.from)} – ${dayShort(state.to)}`
    else if (state.from) range = `From ${dayShort(state.from)}`
    else if (state.to) range = `Until ${dayShort(state.to)}`
    else range = 'Custom range'
  } else {
    range = preset?.label ?? 'Dates'
  }

  const extra: string[] = []
  if (state.hourFrom != null || state.hourTo != null) {
    const a = state.hourFrom != null ? hourLabel(state.hourFrom) : '00:00'
    const b = state.hourTo != null ? hourLabel(state.hourTo) : '23:00'
    extra.push(`${a}–${b}`)
  }
  if (state.weekdays?.length) {
    const names = weekdayNames()
    extra.push(
      [...state.weekdays]
        .sort((x, y) => x - y)
        .map((i) => names[i])
        .join(''),
    )
  }
  return extra.length ? `${range} · ${extra.join(' · ')}` : range
}

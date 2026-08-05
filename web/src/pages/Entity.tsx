/*
 * The Cars and Tracks pages.
 *
 * One component, rendered twice with a different dimension, because the two pages
 * are the same view of two dimensions. A car page lists cars and breaks the
 * selected one down by track; a track page does the mirror image. Writing it once
 * is what keeps the metric definitions from drifting apart.
 */

import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'

import { api, type Filter, type PaceRow } from '../api'
import { delta, hours, lapTime, monthLabel, num, pct } from '../format'
import { useFilter } from '../useFilter'
import { useTheme, type Theme } from '../theme'
import { Card, Empty, ErrorNote, Loading, Stat } from '../components/ui'
import { Filters } from '../components/Filters'
import { Chart, baseGrid, axisStyle, valueAxisStyle, tooltipStyle } from '../components/Chart'
import { StackedByCategory } from '../components/StackedByCategory'
import { isEmptyArray, keepPrevious, viewState } from '../query'
import {
  consistencyBand,
  dimensionLabel,
  otherLabel,
  pageFilter,
  type Dimension,
} from '../entity'

export function EntityPage({ dimension }: { dimension: Dimension }) {
  const { filter: urlFilter } = useFilter()
  const [params, setParams] = useSearchParams()

  // The page's own dimension is set by the left-hand list, so it must not also
  // arrive as a hard filter — see pageFilter.
  const filter = pageFilter(urlFilter, dimension)

  const list = useQuery({
    queryKey: ['entities', dimension, filter],
    queryFn: () => api.entities(filter, dimension),
    ...keepPrevious,
  })

  const listState = viewState(list, isEmptyArray)
  const items = list.data ?? []

  // The selection lives under a dimension-agnostic key rather than `car` or
  // `track`: useFilter already reads those two keys as hard filters
  // (carId/trackId), so reusing either name here would make selecting a row
  // silently filter the list down to that one row. `sel` is outside
  // useFilter's vocabulary and each route has exactly one dimension, so one
  // key suffices for both pages.
  const selectedParam = params.get('sel')
  const parsedSelection =
    selectedParam != null && /^\d+$/.test(selectedParam) ? Number(selectedParam) : null
  // Accept the URL's selection only when it is an integer that actually
  // names a row in the current list. Anything else — an unparseable value,
  // or an id carried over from the other dimension's page — falls back to
  // the first item rather than reaching the API as `id=NaN` or a 400.
  const validSelection =
    parsedSelection != null && items.some((e) => e.id === parsedSelection)
  const selected = validSelection ? parsedSelection : (items[0]?.id ?? null)

  function select(id: number) {
    const next = new URLSearchParams(params)
    next.set('sel', String(id))
    setParams(next, { replace: true })
  }

  return (
    <>
      <div className="page-head">
        <h1>{dimensionLabel(dimension)}s</h1>
      </div>
      <p className="page-sub">
        How you drive each {dimension}, and whether you are getting better at it.
      </p>

      <Filters hide={[dimension]} />

      {list.isError && <ErrorNote error={list.error} />}

      <div className="explorer two">
        <div className="session-list">
          {listState === 'loading' ? (
            <Loading />
          ) : items.length === 0 ? (
            <Empty>Nothing matches this filter.</Empty>
          ) : (
            items.map((e) => (
              <button
                key={e.id}
                type="button"
                className={`session-row${selected === e.id ? ' active' : ''}`}
                onClick={() => select(e.id)}
              >
                <div className="when">{e.name}</div>
                <div className="what">
                  {hours(e.drivingHours)} · {num(e.laps)} laps · {num(e.sessions)} sessions
                </div>
              </button>
            ))
          )}
        </div>

        <div>
          {listState === 'loading' ? (
            // The list has nothing to show yet, so telling the user to pick
            // an entity is premature — mirror the left pane's loading state
            // instead of asking for a choice before one exists.
            <Loading />
          ) : selected == null ? (
            <Empty>Select a {dimension}.</Empty>
          ) : (
            <Review dimension={dimension} id={selected} />
          )}
        </div>
      </div>
    </>
  )
}

function Review({ dimension, id }: { dimension: Dimension; id: number }) {
  const { filter: urlFilter } = useFilter()
  const filter = pageFilter(urlFilter, dimension)
  const theme = useTheme()

  const stats = useQuery({
    queryKey: ['entity', dimension, id, filter],
    queryFn: () => api.entity(filter, dimension, id),
    ...keepPrevious,
  })
  const pace = useQuery({
    queryKey: ['pace', dimension, id, filter],
    queryFn: () => api.pace(filter, dimension, id),
    ...keepPrevious,
  })

  // The progression chart needs one value of the opposite dimension, because a
  // line mixing tracks would rise and fall with the circuit rather than the pace.
  const [other, setOther] = useState<number | null>(null)
  const paceRows = pace.data ?? []
  // The selection is validated against the rows actually present rather than
  // trusted, the same way the entity selection is in EntityPage. Review holds
  // `other` in state and is rendered without a key, so React keeps it across a
  // switch to another entity: picking Okayama for one car and then clicking
  // another left the <select> showing that car's first track while the query ran
  // for the retained id, and the card read "No timed laps here in this range".
  const otherId =
    other != null && paceRows.some((r) => r.otherId === other)
      ? other
      : (paceRows[0]?.otherId ?? null)

  if (stats.isError) return <ErrorNote error={stats.error} />
  if (!stats.data) return <Loading />
  const s = stats.data

  // The entity's own dimension is fixed by the selection, so the filter passed to
  // the shared breakdown must carry it.
  const scoped: Filter = { ...filter, [dimension === 'car' ? 'carId' : 'trackId']: id }

  return (
    <>
      <div className="card" style={{ marginBottom: 14 }}>
        <strong style={{ fontSize: 15 }}>{s.name}</strong>
        <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 3 }}>
          {hours(s.drivingHours)} driving · {num(s.sessions)} sessions ·{' '}
          {num(s.laps)} laps · {num(Math.round(s.distanceKm))} km
        </div>
      </div>

      <div className="grid kpis" style={{ gridTemplateColumns: 'repeat(4, 1fr)' }}>
        <Stat
          label="Clean laps"
          value={s.cleanLapPct == null ? '—' : pct(s.cleanLapPct / 100)}
          note="no incident on the lap"
        />
        <Stat
          label="Incident points / 100 km"
          value={s.incidentPointsPer100Km == null ? '—' : s.incidentPointsPer100Km.toFixed(2)}
          note={`${num(s.incidentPoints)} points total`}
        />
        <Stat
          label="Positions gained"
          value={s.avgPositionsGained == null ? '—' : s.avgPositionsGained.toFixed(2)}
          note={`${num(s.races)} races`}
        />
        <Stat
          label="Wins / podiums"
          value={`${num(s.wins)} / ${num(s.podiums)}`}
          // The store counts every race with a recorded finish, AI included, so the
          // note says that rather than "human races" — a label must not claim more
          // than the query supports, the same reason the incident counter below is
          // "incident points" rather than "incidents".
          note="all races, incl. AI"
        />
      </div>

      <div className="grid" style={{ marginTop: 14, marginBottom: 14 }}>
        <Card
          title={`Pace by ${otherLabel(dimension).toLowerCase()}`}
          table={<PaceTable rows={paceRows} dimension={dimension} full />}
        >
          {viewState(pace, isEmptyArray) === 'loading' ? (
            <Loading />
          ) : paceRows.length === 0 ? (
            <Empty>No timed laps in this range.</Empty>
          ) : (
            <PaceTable rows={paceRows} dimension={dimension} />
          )}
        </Card>
      </div>

      {otherId != null && (
        <div className="grid" style={{ marginBottom: 14 }}>
          <Progression
            dimension={dimension}
            id={id}
            otherId={otherId}
            rows={paceRows}
            onPick={setOther}
            theme={theme}
          />
        </div>
      )}

      <StackedByCategory
        title="Driving time by category"
        by={dimension === 'car' ? 'track' : 'car'}
        filter={scoped}
      />

      <div className="grid" style={{ marginTop: 14, marginBottom: 14 }}>
        <RacecraftPanel dimension={dimension} id={id} filter={filter} />
      </div>

      <div className="grid">
        <RivalsPanel filter={scoped} />
      </div>
    </>
  )
}

/**
 * PaceTable is a table in both of the card's slots rather than a chart and a table.
 *
 * The values are a mix of times, percentages and counts across many rows, which a
 * bar chart cannot carry without either dropping columns or needing two axes. The
 * two slots differ in width instead: `full` adds the absolute consistency delta,
 * which is the same measurement the percentage already reports expressed in
 * seconds. Every other column earns its place in the default view — spec §5.2's
 * table names the track, the personal best, the consistency, the laps and the
 * sessions, and §5.2 adds the in-range best and its delta to the PB — so the
 * seconds restatement is the one that yields when eight columns will not read
 * comfortably. That also gives the card's Table button something real to do.
 */
function PaceTable({
  rows,
  dimension,
  full = false,
}: {
  rows: PaceRow[]
  dimension: Dimension
  full?: boolean
}) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">{otherLabel(dimension)}</th>
            <th className="no-sort num">Personal best</th>
            <th className="no-sort num">Best in range</th>
            <th className="no-sort num">Δ to PB</th>
            <th className="no-sort num">Consistency</th>
            {full && <th className="no-sort num">Δ consistency</th>}
            <th className="no-sort num">Laps</th>
            <th className="no-sort num">Sessions</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => {
            const band = consistencyBand(r.consistencyPct)
            // The in-range best can only ever be at or behind the personal best,
            // since the PB is computed over the same predicate without the date
            // bounds. delta() still carries the sign from the value itself rather
            // than assuming the direction.
            const toPb =
              r.personalBestS == null || r.bestInRangeS == null
                ? null
                : r.bestInRangeS - r.personalBestS
            return (
              <tr key={r.otherId}>
                <td>{r.otherName}</td>
                <td className="num">{lapTime(r.personalBestS)}</td>
                <td className="num">{lapTime(r.bestInRangeS)}</td>
                <td className="num">{toPb == null ? '—' : delta(toPb)}</td>
                <td className={`num cons-${band}`}>
                  {r.consistencyPct == null ? '—' : `${r.consistencyPct.toFixed(2)}%`}
                </td>
                {full && (
                  <td className="num">
                    {r.consistencyDeltaS == null ? '—' : `${r.consistencyDeltaS.toFixed(3)}s`}
                  </td>
                )}
                <td className="num">{num(r.laps)}</td>
                <td className="num">{num(r.sessions)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function Progression({
  dimension,
  id,
  otherId,
  rows,
  onPick,
  theme,
}: {
  dimension: Dimension
  id: number
  otherId: number
  rows: PaceRow[]
  onPick: (id: number) => void
  theme: Theme
}) {
  const { filter: urlFilter } = useFilter()
  const filter = pageFilter(urlFilter, dimension)
  const q = useQuery({
    queryKey: ['progression', dimension, id, otherId, filter],
    queryFn: () => api.progression(filter, dimension, id, otherId),
    ...keepPrevious,
  })
  const data = q.data ?? []

  const option = useMemo(
    () => ({
      grid: baseGrid,
      tooltip: {
        trigger: 'axis',
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (ps: { name: string; value: number }[]) => {
          const p = ps[0]
          return p ? `${p.name}<br/><strong>${lapTime(p.value)}</strong> best lap` : ''
        },
      },
      xAxis: {
        type: 'category',
        // monthLabel, not the raw "2026-08" key the store groups by: the same
        // month renders as "Aug 2026" on the dashboard, and both go through the
        // one shared helper so they cannot drift apart again.
        data: data.map((r) => monthLabel(r.month)),
        ...axisStyle(theme.textMuted, theme.baseline),
      },
      // Lower is better for a lap time, so the axis is inverted: a line rising on
      // screen means improvement, which is the direction a reader expects.
      //
      // scale: true keeps zero out of the range. A value axis includes zero by
      // default, and every best lap here sits within a couple of seconds of the
      // others — with zero forced in, the whole line presses flat against the
      // bottom of the plot and shows no trend at all. No lap takes zero seconds, so
      // that space is dead anyway.
      //
      // The axis labels go through the same lapTime formatter as the tooltip, so a
      // reader sees "1:01.500" on the axis rather than a bare "61" that disagrees
      // with what the tooltip says on hover.
      yAxis: {
        type: 'value',
        inverse: true,
        scale: true,
        ...valueAxisStyle(theme.textMuted, theme.line),
        axisLabel: {
          ...valueAxisStyle(theme.textMuted, theme.line).axisLabel,
          formatter: (v: number) => lapTime(v),
        },
      },
      series: [
        {
          type: 'line',
          data: data.map((r) => Number(r.bestLapS.toFixed(3))),
          smooth: false,
          symbolSize: 8,
          lineStyle: { width: 2, color: theme.accent },
          itemStyle: { color: theme.accent },
        },
      ],
    }),
    [data, theme],
  )

  return (
    <Card
      title="Best lap by month"
      table={
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th className="no-sort">Month</th>
                <th className="no-sort num">Best lap</th>
                <th className="no-sort num">Laps</th>
              </tr>
            </thead>
            <tbody>
              {data.map((r) => (
                <tr key={r.month}>
                  <td>{monthLabel(r.month)}</td>
                  <td className="num">{lapTime(r.bestLapS)}</td>
                  <td className="num">{num(r.laps)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      }
    >
      <div style={{ marginBottom: 8 }}>
        <select
          value={otherId}
          onChange={(e) => onPick(Number(e.target.value))}
          aria-label={`Choose a ${otherLabel(dimension).toLowerCase()}`}
        >
          {rows.map((r) => (
            <option key={r.otherId} value={r.otherId}>
              {r.otherName}
            </option>
          ))}
        </select>
      </div>
      {viewState(q, isEmptyArray) === 'loading' ? (
        <Loading />
      ) : data.length === 0 ? (
        <Empty>No timed laps here in this range.</Empty>
      ) : (
        <Chart option={option} ariaLabel="Best lap time per month" />
      )}
    </Card>
  )
}

/**
 * RacecraftPanel carries the three figures spec §5.6 defines: the on-track pass
 * record, the average grid-to-finish, and race pace against qualifying pace.
 *
 * Human races only, so excludeAi is passed explicitly rather than assumed. The API
 * parses exclude_ai to false by default and no store query hard-codes the
 * exclusion — it is a user checkbox that starts off — so a panel that needs it must
 * opt in, exactly as the Dashboard's grid-to-finish panel does.
 *
 * Both queries are gated on having data before anything is stated. Reading them
 * while they are still in flight would let the panel say "no paired weekends" or
 * show an em dash for a record that does exist, which asserts a zero state before
 * it is known.
 */
function RacecraftPanel({
  dimension,
  id,
  filter,
}: {
  dimension: Dimension
  id: number
  filter: Filter
}) {
  const humanFilter: Filter = { ...filter, excludeAi: true }
  const rc = useQuery({
    queryKey: ['racecraft', dimension, id, humanFilter],
    queryFn: () => api.racecraft(humanFilter, dimension, id),
    ...keepPrevious,
  })
  // Race pace against qualifying pace. A positive delta is normal: qualifying runs
  // on low fuel with a clear track.
  const quali = useQuery({
    queryKey: ['quali-pace', dimension, id, humanFilter],
    queryFn: () => api.qualiPace(humanFilter, dimension, id),
    ...keepPrevious,
  })

  const r = rc.data
  const q = quali.data
  const loading = viewState(rc) === 'loading' || viewState(quali) === 'loading'

  return (
    <Card title="Racecraft">
      {loading || r == null || q == null ? (
        <Loading />
      ) : (
        <div className="grid kpis" style={{ gridTemplateColumns: 'repeat(3, 1fr)' }}>
          <Stat
            label="Passes made / passed"
            value={`${num(r.passesMade)} / ${num(r.timesPassed)}`}
            // The store counts only cause = 'OnTrack' in race sessions, with AI
            // excluded by the filter above, and the note says exactly that: a
            // place inherited because someone else pitted is not an overtake.
            note="on track, in human races"
          />
          <Stat
            label="Average grid → finish"
            value={
              r.avgStartPosition == null || r.avgFinishPosition == null
                ? '—'
                : `${r.avgStartPosition.toFixed(1)} → ${r.avgFinishPosition.toFixed(1)}`
            }
            note={
              r.races === 0
                ? 'no races with both positions recorded'
                : `${num(r.races)} races with both recorded`
            }
          />
          <Stat
            label="Race vs qualifying"
            // delta() takes the sign from the number's own value rather than a
            // literal "+" prepended unconditionally — the earlier version
            // rendered "+-0.78s" on a negative delta. A positive value is the
            // expected direction, so the note spells out which way is which
            // rather than leaving the reader to infer it from the sign.
            value={q.avgDeltaS == null ? '—' : `${delta(q.avgDeltaS)}s`}
            note={
              q.pairs === 0 ? 'no paired weekends' : `${num(q.pairs)} weekends · + slower`
            }
          />
        </div>
      )}
    </Card>
  )
}

function RivalsPanel({ filter }: { filter: Filter }) {
  // Human races only: the store counts AI opponents in a pass/passed tally that
  // is not a meaningful rivalry, so this panel follows the Dashboard's
  // grid-to-finish precedent and opts into excludeAi explicitly rather than
  // relying on a default the API does not apply.
  const rivalFilter: Filter = { ...filter, excludeAi: true }
  const q = useQuery({
    queryKey: ['rivals', rivalFilter],
    queryFn: () => api.rivals(rivalFilter),
    ...keepPrevious,
  })
  const rows = (q.data ?? []).slice(0, 12)

  const table = (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Driver</th>
            <th className="no-sort num">Passed them</th>
            <th className="no-sort num">Lost to</th>
            <th className="no-sort num">Net</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.name}>
              <td>{r.name}</td>
              <td className="num">{num(r.passedThem)}</td>
              <td className="num">{num(r.lostTo)}</td>
              <td className="num">{r.net > 0 ? `+${r.net}` : num(r.net)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )

  // No table prop: the panel is already a table, so passing the same markup to
  // both Card slots would render a Table button whose before and after DOM are
  // byte-identical — a visible control that does nothing. Card only draws the
  // toggle when a distinct table view exists.
  return (
    <Card title="Rivals">
      {viewState(q, isEmptyArray) === 'loading' ? (
        <Loading />
      ) : rows.length === 0 ? (
        <Empty>
          No on-track passes against a named opponent in this range. Practice and AI
          sessions do not contribute.
        </Empty>
      ) : (
        table
      )}
    </Card>
  )
}

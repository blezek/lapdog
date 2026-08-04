import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api, type Lap, type PositionEvent, type Session } from '../api'
import {
  causeLabel,
  dateTime,
  day,
  delta,
  hm,
  label,
  lapTime,
  num,
  position,
} from '../format'
import { useFilter } from '../useFilter'
import { useTheme, type Theme } from '../theme'
import { Chart, baseGrid, axisStyle, valueAxisStyle, tooltipStyle } from '../components/Chart'
import { Card, Empty, ErrorNote, Icon, Legend, Loading, Stat } from '../components/ui'
import { Filters } from '../components/Filters'

/**
 * Sessions is the explorer: facets narrow the set, the middle column lists what
 * matched, and the right pane shows the selected session in full.
 */
export function Sessions() {
  const { filter, state, toggleIn, update } = useFilter()
  const [selected, setSelected] = useState<number | null>(null)

  const list = useQuery({
    queryKey: ['sessions', filter],
    queryFn: () => api.sessions({ ...filter, limit: 500 }),
  })
  const facets = useQuery({ queryKey: ['facets'], queryFn: api.facets })

  const items = list.data?.items ?? []
  const current = selected ?? items[0]?.id ?? null

  return (
    <>
      <div className="page-head">
        <h1>Sessions</h1>
      </div>
      <p className="page-sub">
        Narrow the set, then open a session to see its laps and position changes.
      </p>

      <Filters
        matched={list.data ? `${num(list.data.total)} sessions matched` : undefined}
      />

      {list.isError && <ErrorNote error={list.error} />}

      <div className="explorer">
        <aside>
          <div className="card">
            <div className="facet-group">
              <div className="facet-title">Session type</div>
              {(facets.data?.sessionTypes ?? []).map((t) => (
                <label key={t} className="facet-row">
                  <input
                    type="checkbox"
                    checked={state.sessionType?.includes(t) ?? false}
                    onChange={() => toggleIn('type', t)}
                  />
                  {t}
                </label>
              ))}
            </div>

            <div className="facet-group">
              <div className="facet-title">Context</div>
              {(facets.data?.eventContexts ?? []).map((c) => (
                <label key={c} className="facet-row">
                  <input
                    type="checkbox"
                    checked={state.eventContext?.includes(c) ?? false}
                    onChange={() => toggleIn('context', c)}
                  />
                  {c}
                </label>
              ))}
            </div>

            <div className="facet-group">
              <div className="facet-title">Track</div>
              {(facets.data?.tracks ?? []).slice(0, 20).map((t) => (
                <button
                  key={t.id}
                  type="button"
                  className="facet-row"
                  style={{
                    background: 'none',
                    border: 0,
                    width: '100%',
                    font: 'inherit',
                    color: state.trackId === t.id ? 'var(--accent)' : undefined,
                  }}
                  onClick={() =>
                    update({ track: state.trackId === t.id ? undefined : String(t.id) })
                  }
                >
                  {t.name}
                  <span className="facet-count">{t.sessions}</span>
                </button>
              ))}
            </div>
          </div>
        </aside>

        <div className="session-list">
          {list.isLoading ? (
            <Loading />
          ) : items.length === 0 ? (
            <Empty>Nothing matches this filter.</Empty>
          ) : (
            items.map((s) => (
              <button
                key={s.id}
                type="button"
                className={`session-row${current === s.id ? ' active' : ''}`}
                onClick={() => setSelected(s.id)}
              >
                <div className="when">
                  {/*
                    The year is included here, unlike the dense tables elsewhere. This
                    list spans whatever the filter covers — two years under "All time" —
                    so a bare "Aug 4" is ambiguous between several sessions that are a
                    year apart and look otherwise identical.
                  */}
                  {day(s.startedAt)} · {label(s.sessionType, s.eventContext)}
                </div>
                <div className="what">
                  {s.trackName ?? 'Unknown track'} · {hm(s.drivingSeconds)} driving
                  {s.lapsCompleted > 0 && ` · ${s.lapsCompleted} laps`}
                </div>
              </button>
            ))
          )}
        </div>

        <div>{current == null ? <Empty>Select a session.</Empty> : <Detail id={current} />}</div>
      </div>
    </>
  )
}

/* ------------------------------------------------------------------- detail */

function Detail({ id }: { id: number }) {
  const theme = useTheme()
  const session = useQuery({ queryKey: ['session', id], queryFn: () => api.session(id) })
  const laps = useQuery({ queryKey: ['session-laps', id], queryFn: () => api.sessionLaps(id) })
  const events = useQuery({
    queryKey: ['session-positions', id],
    queryFn: () => api.sessionPositions(id),
  })

  if (session.isLoading) return <Loading />
  if (session.isError) return <ErrorNote error={session.error} />
  if (!session.data) return null
  const s = session.data

  return (
    <>
      <div className="card" style={{ marginBottom: 14 }}>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, flexWrap: 'wrap' }}>
          <strong style={{ fontSize: 15 }}>{label(s.sessionType, s.eventContext)}</strong>
          <span style={{ color: 'var(--text-secondary)', fontSize: 13 }}>
            {s.trackName ?? 'Unknown track'}
            {s.trackConfig ? ` · ${s.trackConfig}` : ''} · {s.carName ?? 'Unknown car'}
          </span>
        </div>
        <div style={{ color: 'var(--text-muted)', fontSize: 12, marginTop: 3 }}>
          {dateTime(s.startedAt)}
          {s.captureFile && (
            <>
              {' · capture '}
              <span className="mono">{s.captureFile}</span>
            </>
          )}
        </div>
      </div>

      {/* The three counters, side by side, because the difference between them is
          the point: connected includes the garage, in-car includes the pit box. */}
      <div className="grid kpis" style={{ gridTemplateColumns: 'repeat(3, 1fr)' }}>
        <Stat label="Connected" value={hm(s.connectedSeconds)} />
        <Stat label="In car" value={hm(s.inCarSeconds)} />
        <Stat label="Driving" value={hm(s.drivingSeconds)} />
      </div>

      <div className="grid" style={{ marginTop: 14 }}>
        <div className="card">
          <div className="card-head">
            <span className="card-title">Result</span>
          </div>
          <ResultRows session={s} />
        </div>

        <Card
          title="Lap times"
          table={<LapTable laps={laps.data ?? []} bestLap={s.bestLapTimeS} />}
        >
          {laps.isLoading ? (
            <Loading />
          ) : (laps.data?.length ?? 0) === 0 ? (
            <Empty>No completed laps in this session.</Empty>
          ) : (
            <LapChart laps={laps.data!} theme={theme} />
          )}
        </Card>

        {s.sessionType === 'Race' && (
          <Card title="Position changes" table={<EventTable events={events.data ?? []} />}>
            {events.isLoading ? (
              <Loading />
            ) : (events.data?.length ?? 0) === 0 ? (
              <Empty>No position changes recorded.</Empty>
            ) : (
              <EventList events={events.data!} />
            )}
          </Card>
        )}
      </div>
    </>
  )
}

function ResultRows({ session: s }: { session: Session }) {
  const rows: [string, string][] = [
    ['Laps completed', num(s.lapsCompleted)],
    ['Best lap', lapTime(s.bestLapTimeS)],
    ['Incidents', `${num(s.incidents)} (${s.incidentSource === 'live' ? 'live' : 'from results'})`],
  ]
  if (s.qualifyPosition != null) {
    rows.push(['Qualified', `${position(s.qualifyPosition)}${
      s.qualifyBestTimeS ? ` · ${lapTime(s.qualifyBestTimeS)}` : ''
    }`])
  }
  if (s.startingPosition != null) rows.push(['Started', position(s.startingPosition)])
  if (s.finishPosition != null) {
    rows.push(['Finished', `${position(s.finishPosition)}${
      s.fieldSize ? ` of ${s.fieldSize}` : ''
    }`])
  }
  if (s.aiOpponentCount > 0) {
    rows.push([
      'AI opponents',
      `${s.aiOpponentCount}${s.aiDetection === 'heuristic' ? ' (inferred)' : ''}`,
    ])
  }

  return (
    <div className="table-wrap">
      <table>
        <tbody>
          {rows.map(([k, v]) => (
            <tr key={k}>
              <td style={{ color: 'var(--text-muted)' }}>{k}</td>
              <td className="num" style={{ color: 'var(--text-primary)' }}>
                {v}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/**
 * LapChart plots lap times with the session best emphasised.
 *
 * One series is the point and the rest is context, so this is emphasis: the line
 * in the accent hue, the personal best marked, and pit laps de-emphasised rather
 * than given a competing colour.
 */
function LapChart({ laps, theme }: { laps: Lap[]; theme: Theme }) {
  const option = useMemo(() => {
    const timed = laps.filter((l) => l.lapTimeS != null && l.lapTimeS > 0)
    const best = Math.min(...timed.map((l) => l.lapTimeS!))

    return {
      grid: baseGrid,
      tooltip: {
        trigger: 'axis',
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (ps: { dataIndex: number }[]) => {
          const l = timed[ps[0]?.dataIndex ?? 0]
          if (!l) return ''
          return [
            `Lap ${l.lapNumber}`,
            `<strong>${lapTime(l.lapTimeS)}</strong>`,
            l.deltaToBestS != null ? `Δ best ${delta(l.deltaToBestS)}` : '',
            l.isPitLap ? 'Pit lap' : '',
            l.incidentsOnLap > 0 ? `${l.incidentsOnLap} incident(s)` : '',
          ]
            .filter(Boolean)
            .join('<br/>')
        },
      },
      xAxis: {
        type: 'category',
        data: timed.map((l) => l.lapNumber),
        name: 'lap',
        nameTextStyle: { color: theme.textMuted, fontSize: 10 },
        ...axisStyle(theme.textMuted, theme.baseline),
      },
      yAxis: {
        type: 'value',
        scale: true,
        ...valueAxisStyle(theme.textMuted, theme.line),
        axisLabel: { color: theme.textMuted, fontSize: 11, formatter: (v: number) => lapTime(v) },
      },
      series: [
        {
          type: 'line',
          data: timed.map((l) => ({
            value: l.lapTimeS,
            // Pit laps are recessive rather than a second colour: they are the
            // same measurement, just not comparable.
            itemStyle: { color: l.isPitLap ? theme.deEmphasis : theme.accent },
            symbolSize: l.isPitLap ? 5 : 7,
          })),
          lineStyle: { color: theme.accent, width: 2 },
          smooth: false,
          markLine: {
            silent: true,
            symbol: 'none',
            data: [{ yAxis: best, label: { formatter: `best ${lapTime(best)}`, fontSize: 10 } }],
            lineStyle: { color: theme.deEmphasis, type: 'dashed', width: 1 },
            label: { color: theme.textMuted },
          },
        },
      ],
    }
  }, [laps, theme])

  return (
    <>
      <Chart option={option} ariaLabel="Lap times for this session" />
      <Legend
        items={[
          { label: 'Timed lap', colour: theme.accent },
          { label: 'Pit lap', colour: theme.deEmphasis },
        ]}
      />
    </>
  )
}

function LapTable({ laps, bestLap }: { laps: Lap[]; bestLap: number | null }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort num">Lap</th>
            <th className="no-sort num">Time</th>
            <th className="no-sort num">Δ best</th>
            <th className="no-sort num">Fuel</th>
            <th className="no-sort num">Inc</th>
            <th className="no-sort num">Pos</th>
            <th className="no-sort">Pit</th>
          </tr>
        </thead>
        <tbody>
          {laps.map((l) => (
            <tr key={l.id}>
              <td className="num">{l.lapNumber}</td>
              <td
                className="num"
                style={{
                  color:
                    bestLap != null && l.lapTimeS === bestLap
                      ? 'var(--text-primary)'
                      : undefined,
                  fontWeight: bestLap != null && l.lapTimeS === bestLap ? 600 : undefined,
                }}
              >
                {lapTime(l.lapTimeS)}
              </td>
              <td className="num">{delta(l.deltaToBestS)}</td>
              <td className="num">{l.fuelUsedL != null ? l.fuelUsedL.toFixed(2) : '—'}</td>
              <td className="num">{l.incidentsOnLap || '—'}</td>
              <td className="num">{position(l.position)}</td>
              <td>{l.isPitLap ? 'yes' : ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/**
 * EventList shows position changes with the cause spelled out.
 *
 * The cause is what separates a real overtake from someone else pitting, so it is
 * always shown in words rather than implied by a colour.
 */
function EventList({ events }: { events: PositionEvent[] }) {
  const onTrack = events.filter((e) => e.cause === 'OnTrack')
  const made = onTrack.filter((e) => e.toPosition < e.fromPosition).length
  const lost = onTrack.filter((e) => e.toPosition > e.fromPosition).length
  const attrition = events.length - onTrack.length

  return (
    <>
      <div className="grid kpis" style={{ gridTemplateColumns: 'repeat(3, 1fr)', marginBottom: 10 }}>
        <Stat label="Passes made" value={String(made)} />
        <Stat label="Times passed" value={String(lost)} />
        <Stat label="By attrition" value={String(attrition)} note="not counted as passes" />
      </div>
      <EventTable events={events} />
    </>
  )
}

function EventTable({ events }: { events: PositionEvent[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort num">Lap</th>
            <th className="no-sort">Change</th>
            <th className="no-sort">Cause</th>
            <th className="no-sort">Opponent</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => {
            const gained = e.toPosition < e.fromPosition
            return (
              <tr key={e.id}>
                <td className="num">{e.lapNumber}</td>
                <td>
                  <Icon
                    name={gained ? 'chart-line' : 'chart-line'}
                    className="legend-swatch"
                  />
                  P{e.fromPosition} → P{e.toPosition}
                </td>
                <td>{causeLabel(e.cause)}</td>
                <td>{e.opponentName ?? '—'}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

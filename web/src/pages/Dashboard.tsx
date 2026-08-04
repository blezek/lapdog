import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api, type DailyRow, type SummaryRow } from '../api'
import { hours, labelForKey, num, pct, position, dayShort, lapTime } from '../format'
import { useFilter } from '../useFilter'
import { useTheme, type Theme } from '../theme'
import { categoryColour, categoryOrderAll, totalsFromSummary } from '../categories'
import { Chart, baseGrid, axisStyle, valueAxisStyle, tooltipStyle } from '../components/Chart'
import { Card, Empty, ErrorNote, Legend, Loading, Stat } from '../components/ui'
import { Filters } from '../components/Filters'
import { StackedByCategory } from '../components/StackedByCategory'

export function Dashboard() {
  const { filter } = useFilter()
  const theme = useTheme()

  const totals = useQuery({ queryKey: ['totals', filter], queryFn: () => api.totals(filter) })
  const byCategory = useQuery({
    queryKey: ['summary', filter, 'month-category'],
    queryFn: () => api.summary(filter, 'typecontext'),
  })
  const byMonth = useQuery({
    queryKey: ['summary', filter, 'month'],
    queryFn: () => api.summary(filter, 'month'),
  })
  const daily = useQuery({ queryKey: ['daily', filter], queryFn: () => api.daily(filter) })

  return (
    <>
      <div className="page-head">
        <h1>Dashboard</h1>
      </div>
      <p className="page-sub">
        Time in the simulator, and how it was spent. Driving time excludes the garage,
        the pit box and replay playback.
      </p>

      <Filters />

      {totals.isError && <ErrorNote error={totals.error} />}

      <div className="grid kpis">
        {totals.isLoading || !totals.data ? (
          <Loading />
        ) : (
          <>
            <Stat label="Driving" value={hours(totals.data.drivingHours)} />
            <Stat
              label="Utilisation"
              value={pct(totals.data.utilisation)}
              note={`of ${hours(totals.data.connectedHours, 0)} connected`}
              meter={totals.data.utilisation}
            />
            <Stat label="Laps" value={num(totals.data.laps)} />
            <Stat
              label="Incidents / hour"
              value={totals.data.incidentsPerHour.toFixed(2)}
              note={`${num(totals.data.incidents)} total`}
            />
            <Stat
              label="Passes / passed"
              value={`${num(totals.data.passesMade)} / ${num(totals.data.timesPassed)}`}
              note="on-track only"
            />
          </>
        )}
      </div>

      <div className="grid" style={{ marginBottom: 14 }}>
        <Card
          title="Driving hours per day"
          table={<DailyTable rows={daily.data ?? []} />}
        >
          {daily.isLoading ? (
            <Loading />
          ) : (daily.data?.length ?? 0) === 0 ? (
            <Empty>No sessions in this range.</Empty>
          ) : (
            <CalendarHeatmap rows={daily.data!} theme={theme} />
          )}
        </Card>
      </div>

      <CarAndTrackBreakdown />

      <div className="grid two-col">
        <Card
          title="Driving hours by category"
          table={<CategoryTable rows={byCategory.data ?? []} />}
        >
          {byCategory.isLoading ? (
            <Loading />
          ) : (byCategory.data?.length ?? 0) === 0 ? (
            <Empty>No sessions in this range.</Empty>
          ) : (
            <CategoryBar rows={byCategory.data!} theme={theme} />
          )}
        </Card>

        <Card title="Driving hours per month" table={<MonthTable rows={byMonth.data ?? []} />}>
          {byMonth.isLoading ? (
            <Loading />
          ) : (byMonth.data?.length ?? 0) === 0 ? (
            <Empty>No sessions in this range.</Empty>
          ) : (
            <MonthBar rows={byMonth.data!} theme={theme} />
          )}
        </Card>
      </div>

      <div className="grid" style={{ marginTop: 14 }}>
        <GridToFinish />
      </div>
    </>
  )
}

/* ------------------------------------------------- car and track breakdowns */

/**
 * CarAndTrackBreakdown shows where the time went, by car and by track.
 *
 * Both charts drill down rather than staying fixed. With every car selected the bars
 * are cars; narrow to one car and the same panel becomes that car's tracks, which is
 * the more detailed question a driver asks next — "I have spent forty hours in the
 * GT3, where?" The track panel mirrors it, becoming a per-car split once a single
 * track is chosen.
 *
 * The drill-down reuses the ordinary filter rather than holding its own state, so the
 * charts, the tables and any export all stay describing the same set.
 */
function CarAndTrackBreakdown() {
  const { filter, state } = useFilter()

  const oneCar = state.carId != null
  const oneTrack = state.trackId != null

  const carTitle = oneCar
    ? 'Where this car was driven, by track'
    : 'Driving hours by car, split by category'
  const trackTitle = oneTrack
    ? 'What was driven at this track, by car'
    : 'Driving hours by track, split by category'

  return (
    <div className="grid two-col" style={{ marginBottom: 14 }}>
      <StackedByCategory
        title={carTitle}
        by={oneCar ? 'track' : 'car'}
        filter={filter}
        maxGroups={oneCar ? 16 : 12}
      />
      <StackedByCategory
        title={trackTitle}
        by={oneTrack ? 'car' : 'track'}
        filter={filter}
        maxGroups={oneTrack ? 12 : 16}
      />
    </div>
  )
}

/* --------------------------------------------------------- calendar heatmap */

/**
 * CalendarHeatmap shows driving hours per day.
 *
 * Magnitude over a grid, so the colour job is sequential: one hue, light to dark.
 * The lightest step means near zero and is allowed to recede toward the surface;
 * the table view is what makes the values readable regardless.
 */
function CalendarHeatmap({ rows, theme }: { rows: DailyRow[]; theme: Theme }) {
  const option = useMemo(() => {
    const data = rows.map((r) => [r.day, Number(r.drivingHours.toFixed(2))])
    const max = Math.max(1, ...rows.map((r) => r.drivingHours))

    // The range follows the data, not the calendar year.
    //
    // Passing a year drew all twelve months regardless of what was in them, so a
    // ninety-day filter rendered three months of squares against nine months of
    // emptiness — which reads as nine months of not driving rather than as a
    // ninety-day window. Padding the ends to whole weeks keeps the grid from
    // starting or finishing mid-column.
    const days = rows.map((r) => r.day).sort()
    const first = days[0] ?? '2020-01-01'
    const last = days[days.length - 1] ?? first
    const range = [weekStart(first), weekEnd(last)]
    const years = [...new Set(days.map((d) => d.slice(0, 4)))]

    return {
      tooltip: {
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (p: { value: [string, number] }) =>
          `${p.value[0]}<br/><strong>${p.value[1].toFixed(1)} h</strong> driving`,
      },
      visualMap: {
        min: 0,
        max,
        type: 'continuous',
        orient: 'horizontal',
        left: 40,
        bottom: 2,
        // For a continuous visual map ECharts treats itemHeight as the bar's length
        // and itemWidth as its thickness, and swaps them for horizontal orientation.
        // Setting them the intuitive way round produces a thin vertical sliver.
        itemWidth: 11,
        itemHeight: 90,
        text: [`${max.toFixed(1)} h`, '0'],
        textStyle: { color: theme.textMuted, fontSize: 10 },
        inRange: { color: theme.seq },
      },
      calendar: {
        top: 26,
        left: 40,
        range,
        // Square cells.
        //
        // Only the left edge is pinned. Giving both left and right fixes the box
        // width, and ECharts then ignores the cell width entirely — which is why
        // 'auto' and an explicit size both produced cells stretched wider than a
        // month label. With one edge pinned the grid grows from cell size instead.
        cellSize: [14, 14],
        splitLine: { show: false },
        itemStyle: {
          color: 'transparent',
          borderWidth: 2,
          borderColor: theme.surface,
        },
        yearLabel: { show: years.length > 1, color: theme.textMuted, fontSize: 10 },
        monthLabel: { color: theme.textMuted, fontSize: 10, nameMap: 'en' },
        // The week starts on Sunday, so Sunday is the top row. Since the driver
        // never races on one, that row reads as consistently empty — which is
        // itself worth seeing rather than hiding mid-grid.
        dayLabel: { color: theme.textMuted, fontSize: 9, firstDay: 0, nameMap: 'en' },
      },
      series: [{ type: 'heatmap', coordinateSystem: 'calendar', data }],
    }
  }, [rows, theme])

  return (
    <Chart
      option={option}
      className="chart calendar"
      ariaLabel="Calendar heatmap of driving hours per day"
    />
  )
}

function DailyTable({ rows }: { rows: DailyRow[] }) {
  const recent = [...rows].reverse().slice(0, 40)
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Day</th>
            <th className="no-sort num">Driving</th>
          </tr>
        </thead>
        <tbody>
          {recent.map((r) => (
            <tr key={r.day}>
              <td>{r.day}</td>
              <td className="num">{hours(r.drivingHours)}</td>
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length > recent.length && (
        <div className="pager">Showing the {recent.length} most recent of {rows.length} days.</div>
      )}
    </div>
  )
}

/* ------------------------------------------------------------- category bar */

/**
 * CategoryBar compares driving hours across session categories.
 *
 * The categories are the subject, so the colour job is categorical, assigned in
 * fixed slot order. Horizontal because the labels are long. Past eight categories
 * the tail folds into "Other" rather than generating a ninth hue.
 */
function CategoryBar({ rows, theme }: { rows: SummaryRow[]; theme: Theme }) {
  const folded = useMemo(() => foldTail(rows, 8), [rows])
  // Colour is keyed to the category itself via the canonical order, so a filter that
  // reorders the bars never repaints the survivors.
  const canonical = useMemo(() => categoryOrderAll(totalsFromSummary(rows)), [rows])

  const option = useMemo(() => {
    const sorted = [...folded].sort((a, b) => a.drivingHours - b.drivingHours)
    return {
      grid: { ...baseGrid, left: 8, right: 46 },
      tooltip: {
        trigger: 'item',
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (p: { name: string; value: number; dataIndex: number }) =>
          `${p.name}<br/><strong>${p.value.toFixed(1)} h</strong> · ${
            sorted[p.dataIndex]?.sessions ?? 0
          } sessions`,
      },
      xAxis: { type: 'value', ...valueAxisStyle(theme.textMuted, theme.line) },
      yAxis: {
        type: 'category',
        data: sorted.map((r) => labelForKey(r.key)),
        ...axisStyle(theme.textSecondary, theme.baseline),
      },
      series: [
        {
          type: 'bar',
          // groupId is the category itself, so a filter change animates each bar to
          // its own new length instead of sliding between reordered slots.
          //
          // The colour is keyed to the category's rank in the full unsorted list
          // rather than to its position on screen, so a category keeps its hue when
          // the ordering shifts. Colour follows the entity, never its rank.
          data: sorted.map((r) => ({
            value: Number(r.drivingHours.toFixed(2)),
            groupId: r.key,
            itemStyle: {
              color: categoryColour(theme, canonical, r.key),
              // 4px rounded data-ends, anchored to the baseline.
              borderRadius: [0, 4, 4, 0],
            },
          })),
          universalTransition: { enabled: true, divideShape: 'clone' },
          barMaxWidth: 15,
          // Direct labels, which is what makes the low-contrast light-mode hues
          // legible without relying on the swatch.
          label: {
            show: true,
            position: 'right',
            formatter: (p: { value: number }) => `${p.value.toFixed(1)}`,
            color: theme.textSecondary,
            fontSize: 11,
          },
        },
      ],
    }
  }, [folded, theme])

  return (
    <>
      <Chart option={option} ariaLabel="Driving hours by session category" />
      {/* The legend uses the same colourIndex as the bars. It previously indexed
          the unsorted list while the bars indexed the sorted one, so the swatches
          named the wrong colours. */}
      <Legend
        items={folded.map((r) => ({
          label: labelForKey(r.key),
          colour: categoryColour(theme, canonical, r.key),
        }))}
      />
    </>
  )
}


function CategoryTable({ rows }: { rows: SummaryRow[] }) {
  const sorted = [...rows].sort((a, b) => b.drivingHours - a.drivingHours)
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Category</th>
            <th className="no-sort num">Driving</th>
            <th className="no-sort num">Sessions</th>
            <th className="no-sort num">Laps</th>
            <th className="no-sort num">Incidents</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => (
            <tr key={r.key}>
              <td>{labelForKey(r.key)}</td>
              <td className="num">{hours(r.drivingHours)}</td>
              <td className="num">{num(r.sessions)}</td>
              <td className="num">{num(r.laps)}</td>
              <td className="num">{num(r.incidents)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/* ---------------------------------------------------------------- month bar */

/**
 * MonthBar is a trend over time, so it takes a single hue rather than a
 * categorical palette. One series needs no legend — the title names it.
 */
function MonthBar({ rows, theme }: { rows: SummaryRow[]; theme: Theme }) {
  const option = useMemo(() => {
    const sorted = [...rows].sort((a, b) => a.key.localeCompare(b.key))
    return {
      grid: baseGrid,
      tooltip: {
        trigger: 'axis',
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (ps: { name: string; value: number }[]) => {
          const p = ps[0]
          if (!p) return ''
          return `${p.name}<br/><strong>${p.value.toFixed(1)} h</strong> driving`
        },
      },
      xAxis: {
        type: 'category',
        data: sorted.map((r) => r.key),
        ...axisStyle(theme.textMuted, theme.baseline),
      },
      yAxis: { type: 'value', ...valueAxisStyle(theme.textMuted, theme.line) },
      series: [
        {
          type: 'bar',
          data: sorted.map((r) => Number(r.drivingHours.toFixed(2))),
          itemStyle: { color: theme.accent, borderRadius: [4, 4, 0, 0] },
          barMaxWidth: 22,
        },
      ],
    }
  }, [rows, theme])

  return <Chart option={option} ariaLabel="Driving hours per month" />
}

function MonthTable({ rows }: { rows: SummaryRow[] }) {
  const sorted = [...rows].sort((a, b) => b.key.localeCompare(a.key))
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Month</th>
            <th className="no-sort num">Driving</th>
            <th className="no-sort num">Sessions</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((r) => (
            <tr key={r.key}>
              <td>{r.key}</td>
              <td className="num">{hours(r.drivingHours)}</td>
              <td className="num">{num(r.sessions)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/* ------------------------------------------------------------ grid to finish */

/**
 * GridToFinish shows where each race started and where it ended.
 *
 * Before-and-after per item, so the form is a dumbbell in one hue at two shades —
 * not two series in different colours, which would imply they are unrelated
 * quantities. AI races are excluded because their results are not comparable.
 */
function GridToFinish() {
  const { filter } = useFilter()
  const theme = useTheme()

  const races = useQuery({
    queryKey: ['races-grid', filter],
    queryFn: () =>
      api.sessions({
        ...filter,
        sessionType: ['Race'],
        excludeAi: true,
        limit: 40,
      }),
  })

  const rows = useMemo(
    () =>
      (races.data?.items ?? [])
        .filter((s) => s.qualifyPosition != null && s.finishPosition != null)
        .reverse(),
    [races.data],
  )

  const option = useMemo(() => {
    const start = rows.map((s) => s.qualifyPosition!)
    const end = rows.map((s) => s.finishPosition!)
    return {
      grid: { ...baseGrid, top: 24 },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (ps: { dataIndex: number }[]) => {
          const s = rows[ps[0]?.dataIndex ?? 0]
          if (!s) return ''
          return [
            `<strong>${s.trackName ?? 'Unknown'}</strong>`,
            `${dayShort(s.startedAt)} · ${s.carName ?? ''}`,
            `Grid ${position(s.qualifyPosition)} → Finish ${position(s.finishPosition)}`,
            s.fieldSize ? `Field of ${s.fieldSize}` : '',
          ]
            .filter(Boolean)
            .join('<br/>')
        },
      },
      xAxis: {
        type: 'category',
        data: rows.map((s) => dayShort(s.startedAt)),
        ...axisStyle(theme.textMuted, theme.baseline),
      },
      // Position 1 is best, so the axis is inverted: up means better.
      yAxis: {
        type: 'value',
        inverse: true,
        min: 1,
        name: 'position',
        nameTextStyle: { color: theme.textMuted, fontSize: 10 },
        ...valueAxisStyle(theme.textMuted, theme.line),
      },
      series: [
        {
          // The connecting stem, drawn as a custom mark so it reads as one item
          // per race rather than as two independent series.
          type: 'custom',
          renderItem: (params: { dataIndex: number }, apiRef: any) => {
            const i = params.dataIndex
            const from = apiRef.coord([i, start[i]])
            const to = apiRef.coord([i, end[i]])
            return {
              type: 'line',
              shape: { x1: from[0], y1: from[1], x2: to[0], y2: to[1] },
              style: { stroke: theme.line, lineWidth: 2 },
            }
          },
          data: rows.map((_, i) => i),
          silent: true,
        },
        {
          type: 'line',
          name: 'Grid',
          data: start,
          symbolSize: 9,
          lineStyle: { width: 0 },
          itemStyle: {
            color: theme.surface,
            borderColor: theme.accent,
            borderWidth: 2,
          },
        },
        {
          type: 'line',
          name: 'Finish',
          data: end,
          symbolSize: 9,
          lineStyle: { width: 0 },
          itemStyle: { color: theme.accent },
        },
      ],
    }
  }, [rows, theme])

  return (
    <Card title="Grid to finish, most recent races" table={<GridTable rows={rows} />}>
      {races.isLoading ? (
        <Loading />
      ) : rows.length === 0 ? (
        <Empty>No races with a qualifying result in this range.</Empty>
      ) : (
        <>
          <Chart
            option={option}
            className="chart"
            ariaLabel="Grid position and finishing position for recent races"
          />
          <Legend
            items={[
              { label: 'Grid', colour: theme.line },
              { label: 'Finish', colour: theme.accent },
            ]}
          />
        </>
      )}
    </Card>
  )
}

function GridTable({ rows }: { rows: import('../api').Session[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Date</th>
            <th className="no-sort">Track</th>
            <th className="no-sort num">Grid</th>
            <th className="no-sort num">Finish</th>
            <th className="no-sort num">Change</th>
            <th className="no-sort num">Field</th>
            <th className="no-sort num">Best lap</th>
          </tr>
        </thead>
        <tbody>
          {[...rows].reverse().map((s) => {
            const change = (s.qualifyPosition ?? 0) - (s.finishPosition ?? 0)
            return (
              <tr key={s.id}>
                <td>{dayShort(s.startedAt)}</td>
                <td>{s.trackName ?? '—'}</td>
                <td className="num">{position(s.qualifyPosition)}</td>
                <td className="num">{position(s.finishPosition)}</td>
                <td className="num">{change > 0 ? `+${change}` : change === 0 ? '0' : change}</td>
                <td className="num">{s.fieldSize ?? '—'}</td>
                <td className="num">{lapTime(s.bestLapTimeS)}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

/* ------------------------------------------------------------------ helpers */

/**
 * weekStart returns the Sunday on or before an ISO date.
 *
 * Sunday because the heatmap starts its week there, so the grid begins on a full
 * column rather than part way down one.
 */
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

/**
 * foldTail keeps the largest categories and folds the remainder into "Other".
 *
 * This is what prevents a ninth categorical hue from ever being needed: a
 * generated colour would be indistinguishable from an existing one under
 * colour-vision deficiency.
 */
function foldTail(rows: SummaryRow[], keep: number): SummaryRow[] {
  if (rows.length <= keep) return rows
  const sorted = [...rows].sort((a, b) => b.drivingHours - a.drivingHours)
  const head = sorted.slice(0, keep - 1)
  const tail = sorted.slice(keep - 1)
  const other: SummaryRow = {
    key: 'Other/Other',
    connectedHours: 0,
    inCarHours: 0,
    drivingHours: 0,
    sessions: 0,
    laps: 0,
    incidents: 0,
  }
  for (const r of tail) {
    other.connectedHours += r.connectedHours
    other.inCarHours += r.inCarHours
    other.drivingHours += r.drivingHours
    other.sessions += r.sessions
    other.laps += r.laps
    other.incidents += r.incidents
  }
  return [...head, other]
}

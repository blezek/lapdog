import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api, type Config, type Filter } from '../api'
import {
  categoryColour,
  categoryOrder,
  categoryOrderAll,
  totalsFromBreakdown,
} from '../categories'
import { distance, labelForKey, num } from '../format'
import {
  rankLeaderboard,
  type LeaderboardGroup,
  type LeaderboardMetric,
} from '../leaderboard'
import { useTheme, type Theme } from '../theme'
import { isEmptyArray, keepPrevious, viewState } from '../query'
import { Chart, axisStyle, baseGrid, tooltipStyle, valueAxisStyle } from './Chart'
import { Card, Empty, ErrorNote, Legend, Loading } from './ui'

/** Six top-ten rankings requested together: laps, clean laps and distance by entity. */
export function LapAndDistanceLeaderboards({
  filter,
  units,
}: {
  filter: Filter
  units: Config['units']
}) {
  const distanceName = units === 'metric' ? 'kilometres' : 'miles'
  return (
    <>
      <div className="grid two-col" style={{ marginBottom: 14 }}>
        <LeaderboardCard
          title="Most laps driven by car"
          by="car"
          metric="laps"
          filter={filter}
          units={units}
        />
        <LeaderboardCard
          title="Most laps driven by track"
          by="track"
          metric="laps"
          filter={filter}
          units={units}
        />
      </div>
      <div className="grid two-col" style={{ marginBottom: 14 }}>
        <LeaderboardCard
          title="Most clean laps by car"
          by="car"
          metric="cleanLaps"
          filter={filter}
          units={units}
        />
        <LeaderboardCard
          title="Most clean laps by track"
          by="track"
          metric="cleanLaps"
          filter={filter}
          units={units}
        />
      </div>
      <div className="grid two-col" style={{ marginBottom: 14 }}>
        <LeaderboardCard
          title={`Most ${distanceName} driven by car`}
          by="car"
          metric="distance"
          filter={filter}
          units={units}
        />
        <LeaderboardCard
          title={`Most ${distanceName} driven by track`}
          by="track"
          metric="distance"
          filter={filter}
          units={units}
        />
      </div>
    </>
  )
}

function LeaderboardCard({
  title,
  by,
  metric,
  filter,
  units,
}: {
  title: string
  by: 'car' | 'track'
  metric: LeaderboardMetric
  filter: Filter
  units: Config['units']
}) {
  const theme = useTheme()
  const query = useQuery({
    queryKey: ['breakdown', by, filter],
    queryFn: () => api.breakdown(filter, by),
    ...keepPrevious,
  })
  const rows = query.data ?? []
  const totals = useMemo(() => totalsFromBreakdown(rows), [rows])
  // The canonical order is based on driving time, just like the existing category
  // charts. Race does not change colour merely because this card ranks lap count.
  const order = useMemo(() => categoryOrder(totals), [totals])
  const fullOrder = useMemo(() => categoryOrderAll(totals), [totals])
  const groups = useMemo(() => rankLeaderboard(rows, metric, order), [rows, metric, order])
  const fullGroups = useMemo(
    () => rankLeaderboard(rows, metric, fullOrder),
    [rows, metric, fullOrder],
  )

  return (
    <Card
      title={title}
      table={
        <LeaderboardTable
          groups={fullGroups}
          order={fullOrder}
          metric={metric}
          units={units}
        />
      }
    >
      {viewState(query, isEmptyArray) === 'error' ? (
        <ErrorNote error={query.error} />
      ) : viewState(query, isEmptyArray) === 'loading' ? (
        <Loading />
      ) : groups.length === 0 ? (
        <Empty>{emptyMessage(metric)}</Empty>
      ) : (
        <>
          <LeaderboardChart
            groups={groups}
            order={order}
            metric={metric}
            theme={theme}
            units={units}
          />
          <Legend
            items={order.map((key) => ({
              label: labelForKey(key),
              colour: categoryColour(theme, order, key),
            }))}
          />
        </>
      )}
    </Card>
  )
}

function LeaderboardChart({
  groups,
  order,
  metric,
  theme,
  units,
}: {
  groups: LeaderboardGroup[]
  order: string[]
  metric: LeaderboardMetric
  theme: Theme
  units: Config['units']
}) {
  const option = useMemo(() => {
    const ordered = [...groups].reverse()
    return {
      grid: { ...baseGrid, left: 8, right: 62 },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (points: { dataIndex: number }[]) => {
          const group = ordered[points[0]?.dataIndex ?? 0]
          if (!group) return ''
          const lines = [`<strong>${group.group}</strong>`, formatMetric(group.total, metric, units)]
          for (const key of order) {
            const value = group.byCategory.get(key) ?? 0
            if (value <= 0) continue
            lines.push(
              `<span style="display:inline-block;width:8px;height:8px;border-radius:2px;` +
                `background:${categoryColour(theme, order, key)};margin-right:5px"></span>` +
                `${labelForKey(key)} ${formatMetric(value, metric, units)}`,
            )
          }
          return lines.join('<br/>')
        },
      },
      xAxis: {
        type: 'value',
        minInterval: metric === 'distance' ? undefined : 1,
        ...valueAxisStyle(theme.textMuted, theme.line),
      },
      yAxis: {
        type: 'category',
        data: ordered.map((group) => group.group),
        ...axisStyle(theme.textSecondary, theme.baseline),
      },
      series: order.map((key, index) => ({
        type: 'bar',
        stack: 'total',
        name: labelForKey(key),
        itemStyle: {
          color: categoryColour(theme, order, key),
          borderColor: theme.surface,
          borderWidth: 2,
        },
        barMaxWidth: 17,
        label:
          index === order.length - 1
            ? {
                show: true,
                position: 'right',
                formatter: (point: { dataIndex: number }) =>
                  formatMetric(ordered[point.dataIndex]?.total ?? 0, metric, units, false),
                color: theme.textSecondary,
                fontSize: 11,
              }
            : { show: false },
        data: ordered.map((group) => Number((group.byCategory.get(key) ?? 0).toFixed(3))),
      })),
    }
  }, [groups, metric, order, theme, units])

  return (
    <Chart
      option={option}
      className="chart tall"
      ariaLabel={`Top ten ${metric === 'cleanLaps' ? 'clean laps' : metric} driven per group, split by session category`}
    />
  )
}

function LeaderboardTable({
  groups,
  order,
  metric,
  units,
}: {
  groups: LeaderboardGroup[]
  order: string[]
  metric: LeaderboardMetric
  units: Config['units']
}) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Rank</th>
            <th className="no-sort">Car or track</th>
            <th className="no-sort num">Total</th>
            {order.map((key) => (
              <th key={key} className="no-sort num">
                {labelForKey(key)}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {groups.map((group, index) => (
            <tr key={group.group}>
              <td>{index + 1}</td>
              <td>{group.group}</td>
              <td className="num">{formatMetric(group.total, metric, units)}</td>
              {order.map((key) => {
                const value = group.byCategory.get(key) ?? 0
                return (
                  <td key={key} className="num">
                    {value > 0 ? formatMetric(value, metric, units, false) : '—'}
                  </td>
                )
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function formatMetric(
  value: number,
  metric: LeaderboardMetric,
  units: Config['units'],
  unit = true,
): string {
  if (metric !== 'distance') return `${num(Math.round(value))}${unit ? ' laps' : ''}`
  return distance(value, units, unit)
}

function emptyMessage(metric: LeaderboardMetric): string {
  if (metric === 'laps') return 'No completed laps in this range.'
  if (metric === 'cleanLaps') return 'No clean timed laps in this range.'
  return 'No completed laps with known track distance in this range.'
}

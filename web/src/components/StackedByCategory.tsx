/*
 * Driving time per group, split by category.
 *
 * Part-to-whole across several groups, so the form is a stacked bar with categorical
 * colour. Horizontal, because car and track names are long and a vertical axis would
 * either clip them or rotate them.
 */

import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api, type Filter } from '../api'
import { hours, labelForKey, num } from '../format'
import { seriesColour, useTheme, type Theme } from '../theme'
import {
  categoryOrder,
  pivot,
  slotOf,
  totalsFromBreakdown,
  type GroupTotals,
} from '../categories'
import { Chart, baseGrid, axisStyle, valueAxisStyle, tooltipStyle } from './Chart'
import { Card, Empty, ErrorNote, Legend, Loading } from './ui'

export interface StackedByCategoryProps {
  title: string
  /** by is the outer dimension: "car", "track", "carclass" or "league". */
  by: string
  filter: Filter
  /** maxGroups caps how many bars are drawn; the rest fold into an "Other" bar. */
  maxGroups?: number
}

export function StackedByCategory({
  title,
  by,
  filter,
  maxGroups = 14,
}: StackedByCategoryProps) {
  const theme = useTheme()
  const query = useQuery({
    queryKey: ['breakdown', by, filter],
    queryFn: () => api.breakdown(filter, by),
  })

  const rows = query.data ?? []
  const order = useMemo(() => categoryOrder(totalsFromBreakdown(rows)), [rows])
  const groups = useMemo(() => foldGroups(pivot(rows, order), maxGroups), [rows, order, maxGroups])

  return (
    <Card title={title} table={<StackTable groups={groups} order={order} />}>
      {query.isError ? (
        <ErrorNote error={query.error} />
      ) : query.isLoading ? (
        <Loading />
      ) : groups.length === 0 ? (
        <Empty>No sessions in this range.</Empty>
      ) : (
        <>
          <StackChart groups={groups} order={order} theme={theme} />
          <Legend
            items={order.map((k) => ({
              label: labelForKey(k),
              colour: seriesColour(theme, slotOf(order, k)),
            }))}
          />
        </>
      )}
    </Card>
  )
}

function StackChart({
  groups,
  order,
  theme,
}: {
  groups: GroupTotals[]
  order: string[]
  theme: Theme
}) {
  const option = useMemo(() => {
    // Least at the top so the longest bar sits at the bottom, which reads as a
    // ranking rather than an arbitrary list on an inverted category axis.
    const ordered = [...groups].reverse()

    return {
      // Long names need room; the total label needs a little on the right.
      grid: { ...baseGrid, left: 8, right: 54 },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        ...tooltipStyle(theme.surface, theme.textPrimary, theme.line),
        formatter: (ps: { dataIndex: number }[]) => {
          const g = ordered[ps[0]?.dataIndex ?? 0]
          if (!g) return ''
          const lines = [`<strong>${g.group}</strong>`, `${hours(g.total)} driving`]
          for (const k of order) {
            const v = g.byCategory.get(k) ?? 0
            if (v <= 0) continue
            lines.push(
              `<span style="display:inline-block;width:8px;height:8px;border-radius:2px;` +
                `background:${seriesColour(theme, slotOf(order, k))};margin-right:5px"></span>` +
                `${labelForKey(k)} ${v.toFixed(1)} h`,
            )
          }
          lines.push(`${num(g.sessions)} sessions · ${num(g.laps)} laps`)
          return lines.join('<br/>')
        },
      },
      xAxis: {
        type: 'value',
        name: 'driving hours',
        nameTextStyle: { color: theme.textMuted, fontSize: 10 },
        ...valueAxisStyle(theme.textMuted, theme.line),
      },
      yAxis: {
        type: 'category',
        data: ordered.map((g) => g.group),
        ...axisStyle(theme.textSecondary, theme.baseline),
      },
      series: order.map((key, i) => {
        const last = i === order.length - 1
        return {
          type: 'bar',
          stack: 'total',
          name: labelForKey(key),
          // A 2px gap in the surface colour between segments, so adjacent hues never
          // touch — which is what keeps a boundary readable when two categories sit
          // close together in the palette.
          itemStyle: {
            color: seriesColour(theme, slotOf(order, key)),
            borderColor: theme.surface,
            borderWidth: 2,
          },
          barMaxWidth: 17,
          // Only the outermost segment carries a label, and it shows the row total
          // rather than its own value: a number on every segment would be unreadable
          // at this size.
          label: last
            ? {
                show: true,
                position: 'right',
                formatter: (p: { dataIndex: number }) =>
                  (ordered[p.dataIndex]?.total ?? 0).toFixed(1),
                color: theme.textSecondary,
                fontSize: 11,
              }
            : { show: false },
          data: ordered.map((g) => Number((g.byCategory.get(key) ?? 0).toFixed(3))),
        }
      }),
    }
  }, [groups, order, theme])

  return <Chart option={option} className="chart tall" ariaLabel="Driving hours per group, split by session category" />
}

function StackTable({ groups, order }: { groups: GroupTotals[]; order: string[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th className="no-sort">Group</th>
            <th className="no-sort num">Total</th>
            {order.map((k) => (
              <th key={k} className="no-sort num">
                {labelForKey(k)}
              </th>
            ))}
            <th className="no-sort num">Sessions</th>
          </tr>
        </thead>
        <tbody>
          {groups.map((g) => (
            <tr key={g.group}>
              <td>{g.group}</td>
              <td className="num">{hours(g.total)}</td>
              {order.map((k) => {
                const v = g.byCategory.get(k) ?? 0
                return (
                  <td key={k} className="num">
                    {v > 0 ? v.toFixed(1) : '—'}
                  </td>
                )
              })}
              <td className="num">{num(g.sessions)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

/**
 * foldGroups caps the number of bars, folding the remainder into one "Other" bar.
 *
 * A chart with forty tracks is a wall, not a comparison. The table view keeps every
 * group, so nothing is actually lost — and the fold is labelled with how many it
 * covers rather than silently dropping them.
 */
function foldGroups(groups: GroupTotals[], max: number): GroupTotals[] {
  if (groups.length <= max) return groups
  const head = groups.slice(0, max - 1)
  const tail = groups.slice(max - 1)

  const other: GroupTotals = {
    group: `Other (${tail.length})`,
    total: 0,
    byCategory: new Map(),
    sessions: 0,
    laps: 0,
  }
  for (const g of tail) {
    other.total += g.total
    other.sessions += g.sessions
    other.laps += g.laps
    for (const [k, v] of g.byCategory) {
      other.byCategory.set(k, (other.byCategory.get(k) ?? 0) + v)
    }
  }
  return [...head, other]
}

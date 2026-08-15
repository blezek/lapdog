import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type SortingState,
} from '@tanstack/react-table'

import { api, type Session } from '../api'
import { dayShort, hm, num, position } from '../format'
import { positionChange, raceStats } from '../race'
import { useFilter } from '../useFilter'
import { Filters } from '../components/Filters'
import { Card, Empty, ErrorNote, Loading, Stat } from '../components/ui'

const LIMIT = 500
const col = createColumnHelper<Session>()

/** Races is the result-oriented view of race sessions only. */
export function Races() {
  const { filter } = useFilter()
  const raceFilter = useMemo(() => ({ ...filter, sessionType: ['Race'] }), [filter])
  const [sorting, setSorting] = useState<SortingState>([{ id: 'startedAt', desc: true }])
  const query = useQuery({
    queryKey: ['races', raceFilter],
    queryFn: () => api.sessions({ ...raceFilter, limit: LIMIT }),
  })
  const rows = query.data?.items ?? []
  const total = query.data?.total ?? 0
  const stats = useMemo(() => raceStats(rows), [rows])
  const scope = total > rows.length ? `latest ${num(rows.length)} races` : `${num(rows.length)} races`

  const columns = useMemo(() => [
    col.accessor('startedAt', { header: 'Date', cell: (c) => dayShort(c.getValue()) }),
    col.accessor('trackName', { header: 'Track', cell: (c) => c.getValue() ?? 'Unknown track' }),
    col.accessor('carName', { header: 'Car', cell: (c) => c.getValue() ?? 'Unknown car' }),
    col.accessor('drivingSeconds', {
      header: 'Driving', cell: (c) => hm(c.getValue()), meta: { num: true },
    }),
    col.accessor('lapsCompleted', { header: 'Laps', meta: { num: true } }),
    col.accessor('incidents', { header: 'Inc', meta: { num: true } }),
    col.accessor('startingPosition', {
      header: 'Grid', cell: (c) => position(c.getValue()), meta: { num: true },
    }),
    col.accessor('finishPosition', {
      header: 'Finish', cell: (c) => position(c.getValue()), meta: { num: true },
    }),
    col.accessor((race) => positionChange(race), {
      id: 'positionChange', header: 'Grid to finish',
    }),
  ], [])

  const table = useReactTable({
    data: rows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  return (
    <>
      <div className="page-head"><h1>Races</h1></div>
      <p className="page-sub">
        Race results, time, incidents, and movement from the grid to the finish.
      </p>
      <Filters matched={query.data ? `${num(total)} races matched` : undefined} hide={['type']} />

      {query.isError && <ErrorNote error={query.error} />}
      {query.isLoading ? <Loading /> : (
        <>
          <div className="grid kpis race-kpis">
            <Stat label="Races" value={num(total)} note={total > rows.length ? `${scope} summarized below` : undefined} />
            <Stat label="Race time" value={hm(stats.drivingSeconds)} note={scope} />
            <Stat label="Wins" value={num(stats.wins)} note={`${num(stats.classified)} recorded finishes`} />
            <Stat label="Podiums" value={num(stats.podiums)} note={`${num(stats.classified)} recorded finishes`} />
            <Stat
              label="Average finish"
              value={stats.avgFinish == null ? '—' : `P${stats.avgFinish.toFixed(1)}`}
              note={`${num(stats.classified)} recorded finishes`}
            />
            <Stat
              label="Average positions gained"
              value={stats.avgPositionsGained == null ? '—' : stats.avgPositionsGained.toFixed(1)}
              note={`${num(stats.positionPairs)} races with grid and finish`}
            />
          </div>

          <Card title={`Race results — ${scope}`}>
            {rows.length === 0 ? <Empty>No races match this filter.</Empty> : (
              <div className="table-wrap">
                <table>
                  <thead>
                    {table.getHeaderGroups().map((group) => (
                      <tr key={group.id}>
                        {group.headers.map((header) => {
                          const isNum = (header.column.columnDef.meta as { num?: boolean })?.num
                          const direction = header.column.getIsSorted()
                          return (
                            <th
                              key={header.id}
                              className={isNum ? 'num' : undefined}
                              onClick={header.column.getToggleSortingHandler()}
                              title="Sort"
                            >
                              {flexRender(header.column.columnDef.header, header.getContext())}
                              {direction === 'asc' ? ' ↑' : direction === 'desc' ? ' ↓' : ''}
                            </th>
                          )
                        })}
                      </tr>
                    ))}
                  </thead>
                  <tbody>
                    {table.getRowModel().rows.map((row) => (
                      <tr key={row.original.id}>
                        {row.getVisibleCells().map((cell) => {
                          const isNum = (cell.column.columnDef.meta as { num?: boolean })?.num
                          return (
                            <td key={cell.id} className={isNum ? 'num' : undefined}>
                              {flexRender(cell.column.columnDef.cell, cell.getContext())}
                            </td>
                          )
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}
    </>
  )
}

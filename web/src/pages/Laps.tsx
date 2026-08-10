import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  createColumnHelper,
  flexRender,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
  type SortingState,
} from '@tanstack/react-table'

import { api, type LapRow } from '../api'
import { dayShort, delta, label, lapTime, num, position } from '../format'
import { useFilter } from '../useFilter'
import { Card, Empty, ErrorNote, Loading } from '../components/ui'
import { Filters } from '../components/Filters'

const PAGE = 250

const col = createColumnHelper<LapRow>()

/**
 * Laps is one flat table of every lap, for comparing across sessions.
 *
 * Sorting and paging are client-side over a bounded page, which is why the fetch
 * carries an explicit limit: the point is comparison within a filtered set, not
 * scrolling thirty-seven thousand rows.
 */
export function Laps() {
  const { filter } = useFilter()
  const [page, setPage] = useState(0)
  const [sorting, setSorting] = useState<SortingState>([{ id: 'lapTimeS', desc: false }])
  const [validOnly, setValidOnly] = useState(true)

  useEffect(() => setPage(0), [filter])

  const query = useQuery({
    queryKey: ['laps', filter, page],
    queryFn: () => api.laps({ ...filter, limit: PAGE, offset: page * PAGE }),
  })

  const rows = useMemo(() => {
    const items = query.data?.items ?? []
    if (!validOnly) return items
    // A pit lap or a lap with an incident is not a representative lap time, so the
    // default view hides them rather than letting them distort a comparison.
    return items.filter((l) => !l.isPitLap && l.incidentsOnLap === 0 && (l.lapTimeS ?? 0) > 0)
  }, [query.data, validOnly])

  const columns = useMemo(
    () => [
      col.accessor('startedAt', {
        header: 'Date',
        cell: (c) => dayShort(c.getValue()),
      }),
      col.accessor((r) => label(r.sessionType, r.eventContext), {
        id: 'category',
        header: 'Category',
      }),
      col.accessor('trackName', { header: 'Track' }),
      col.accessor('carName', { header: 'Car' }),
      col.accessor('lapNumber', { header: 'Lap', meta: { num: true } }),
      col.accessor('lapTimeS', {
        header: 'Lap time',
        cell: (c) => lapTime(c.getValue()),
        meta: { num: true },
      }),
      col.accessor('deltaToBestS', {
        header: 'Δ best',
        cell: (c) => delta(c.getValue()),
        meta: { num: true },
      }),
      col.accessor('fuelUsedL', {
        header: 'Fuel',
        cell: (c) => (c.getValue() != null ? c.getValue()!.toFixed(2) : '—'),
        meta: { num: true },
      }),
      col.accessor('incidentsOnLap', {
        header: 'Inc',
        cell: (c) => c.getValue() || '—',
        meta: { num: true },
      }),
      col.accessor('position', {
        header: 'Pos',
        cell: (c) => position(c.getValue()),
        meta: { num: true },
      }),
    ],
    [],
  )

  const table = useReactTable({
    data: rows,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  const total = query.data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / PAGE))

  return (
    <>
      <div className="page-head">
        <h1>Laps</h1>
      </div>
      <p className="page-sub">
        Every lap, across every session. Click a column to sort.
      </p>

      <Filters matched={total ? `${num(total)} laps matched` : undefined} />

      {query.isError && <ErrorNote error={query.error} />}

      <Card
        title={`Laps ${total ? `— showing ${rows.length} of ${num(total)}` : ''}`}
        actions={
          <button
            type="button"
            className="ghost-btn"
            aria-pressed={validOnly}
            onClick={() => setValidOnly((v) => !v)}
          >
            {validOnly ? 'Clean laps only' : 'All laps'}
          </button>
        }
      >
        {query.isLoading ? (
          <Loading />
        ) : rows.length === 0 ? (
          <Empty>
            No laps match this filter
            {validOnly ? '. Try “All laps” to include pit and incident laps.' : '.'}
          </Empty>
        ) : (
          <>
            <div className="table-wrap">
              <table>
                <thead>
                  {table.getHeaderGroups().map((hg) => (
                    <tr key={hg.id}>
                      {hg.headers.map((h) => {
                        const isNum = (h.column.columnDef.meta as { num?: boolean })?.num
                        const dir = h.column.getIsSorted()
                        return (
                          <th
                            key={h.id}
                            className={isNum ? 'num' : undefined}
                            onClick={h.column.getToggleSortingHandler()}
                            title="Sort"
                          >
                            {flexRender(h.column.columnDef.header, h.getContext())}
                            {dir === 'asc' ? ' ↑' : dir === 'desc' ? ' ↓' : ''}
                          </th>
                        )
                      })}
                    </tr>
                  ))}
                </thead>
                <tbody>
                  {table.getRowModel().rows.map((r) => (
                    <tr key={r.id}>
                      {r.getVisibleCells().map((c) => {
                        const isNum = (c.column.columnDef.meta as { num?: boolean })?.num
                        return (
                          <td key={c.id} className={isNum ? 'num' : undefined}>
                            {flexRender(c.column.columnDef.cell, c.getContext())}
                          </td>
                        )
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {pages > 1 && (
              <div className="pager">
                <button
                  type="button"
                  className="ghost-btn"
                  disabled={page === 0}
                  onClick={() => setPage((p) => Math.max(0, p - 1))}
                >
                  Previous
                </button>
                <span>
                  Page {page + 1} of {pages}
                </span>
                <button
                  type="button"
                  className="ghost-btn"
                  disabled={page + 1 >= pages}
                  onClick={() => setPage((p) => p + 1)}
                >
                  Next
                </button>
              </div>
            )}
          </>
        )}
      </Card>
    </>
  )
}

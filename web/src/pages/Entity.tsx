/*
 * The Cars and Tracks pages.
 *
 * One component, rendered twice with a different dimension, because the two pages
 * are the same view of two dimensions. A car page lists cars and breaks the
 * selected one down by track; a track page does the mirror image. Writing it once
 * is what keeps the metric definitions from drifting apart.
 */

import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'

import { api } from '../api'
import { hours, num } from '../format'
import { useFilter } from '../useFilter'
import { Empty, ErrorNote, Loading } from '../components/ui'
import { Filters } from '../components/Filters'
import { isEmptyArray, keepPrevious, viewState } from '../query'
import { dimensionLabel, type Dimension } from '../entity'

export function EntityPage({ dimension }: { dimension: Dimension }) {
  const { filter } = useFilter()
  const [params, setParams] = useSearchParams()

  const list = useQuery({
    queryKey: ['entities', dimension, filter],
    queryFn: () => api.entities(filter, dimension),
    ...keepPrevious,
  })

  const items = list.data ?? []
  const selectedParam = params.get(dimension)
  const selected = selectedParam ? Number(selectedParam) : (items[0]?.id ?? null)

  function select(id: number) {
    const next = new URLSearchParams(params)
    next.set(dimension, String(id))
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
          {viewState(list, isEmptyArray) === 'loading' ? (
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
          {selected == null ? (
            <Empty>Select a {dimension}.</Empty>
          ) : (
            <Review dimension={dimension} id={selected} />
          )}
        </div>
      </div>
    </>
  )
}

/** Review is the right-hand pane. Task 8 fills it in. */
function Review({ dimension, id }: { dimension: Dimension; id: number }) {
  const { filter } = useFilter()
  const stats = useQuery({
    queryKey: ['entity', dimension, id, filter],
    queryFn: () => api.entity(filter, dimension, id),
    ...keepPrevious,
  })

  if (stats.isError) return <ErrorNote error={stats.error} />
  if (!stats.data) return <Loading />
  return <div className="card">{stats.data.name}</div>
}

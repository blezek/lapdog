import { useQuery } from '@tanstack/react-query'

import { api } from '../api'
import { Filters } from '../components/Filters'
import { LapAndDistanceLeaderboards } from '../components/Leaderboards'
import { ErrorNote, Loading } from '../components/ui'
import { useFilter } from '../useFilter'

/** Top10 collects the car and track rankings in one filterable destination. */
export function Top10() {
  const { filter } = useFilter()
  const settings = useQuery({ queryKey: ['settings'], queryFn: api.settings })

  return (
    <>
      <div className="page-head">
        <h1>Top 10</h1>
      </div>
      <p className="page-sub">
        The leading cars and tracks by completed laps, clean laps and distance driven.
      </p>

      <Filters />

      {settings.isError ? (
        <ErrorNote error={settings.error} />
      ) : !settings.data ? (
        <Loading />
      ) : (
        <LapAndDistanceLeaderboards filter={filter} units={settings.data.units} />
      )}
    </>
  )
}

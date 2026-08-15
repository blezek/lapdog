import type { Session } from './api'

export interface RaceStats {
  races: number
  drivingSeconds: number
  wins: number
  podiums: number
  classified: number
  avgFinish: number | null
  positionPairs: number
  avgPositionsGained: number | null
}

/** raceStats derives result summaries only from races with the required facts. */
export function raceStats(rows: Session[]): RaceStats {
  const finishes = rows
    .map((race) => race.finishPosition)
    .filter((finish): finish is number => finish != null && finish > 0)
  const changes = rows
    .filter((race) =>
      race.startingPosition != null && race.startingPosition > 0 &&
      race.finishPosition != null && race.finishPosition > 0,
    )
    .map((race) => race.startingPosition! - race.finishPosition!)

  return {
    races: rows.length,
    drivingSeconds: rows.reduce((sum, race) => sum + race.drivingSeconds, 0),
    wins: finishes.filter((finish) => finish === 1).length,
    podiums: finishes.filter((finish) => finish <= 3).length,
    classified: finishes.length,
    avgFinish: finishes.length
      ? finishes.reduce((sum, finish) => sum + finish, 0) / finishes.length
      : null,
    positionPairs: changes.length,
    avgPositionsGained: changes.length
      ? changes.reduce((sum, change) => sum + change, 0) / changes.length
      : null,
  }
}

/** positionChange states direction in words rather than relying on sign or colour. */
export function positionChange(race: Session): string {
  if (
    race.startingPosition == null || race.startingPosition <= 0 ||
    race.finishPosition == null || race.finishPosition <= 0
  ) return '—'
  const change = race.startingPosition - race.finishPosition
  if (change > 0) return `Gained ${change}`
  if (change < 0) return `Lost ${Math.abs(change)}`
  return 'No change'
}

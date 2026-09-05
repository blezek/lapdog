import type { RatingPoint } from './api'

export const ratingDisciplines = [
  'Road',
  'Formula',
  'Oval',
  'Dirt Road',
  'Dirt Oval',
] as const

export type RatingDiscipline = (typeof ratingDisciplines)[number]

export interface RatingSeries {
  discipline: RatingDiscipline
  points: RatingPoint[]
}

/**
 * ratingSeries groups observations by their independent iRacing licence.
 * Unknown categories and disciplines with no value are deliberately absent:
 * inventing a category would be worse than leaving an unclassified point off the
 * chart, and an empty series would claim data exists where it does not.
 */
export function ratingSeries(
  points: RatingPoint[],
  pick: (point: RatingPoint) => number | null,
): RatingSeries[] {
  return ratingDisciplines.flatMap((discipline) => {
    const matching = points.filter(
      (point) => point.discipline === discipline && pick(point) != null,
    )
    return matching.length === 0 ? [] : [{ discipline, points: matching }]
  })
}

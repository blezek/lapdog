/*
 * Pure helpers for the Cars and Tracks pages.
 *
 * Separate from the page component so they are testable without a DOM, and so the
 * two pages cannot disagree about what a band means.
 */

/** Dimension is which entity a page is about. */
export type Dimension = 'car' | 'track'

/** dimensionLabel names the entity itself. */
export function dimensionLabel(d: Dimension): string {
  return d === 'car' ? 'Car' : 'Track'
}

/**
 * otherLabel names the opposite dimension, which is what a per-entity breakdown
 * groups by: a car's pace is reported per track, and a track's per car.
 */
export function otherLabel(d: Dimension): string {
  return d === 'car' ? 'Track' : 'Car'
}

/** ConsistencyBand is how a consistency percentage should be presented. */
export type ConsistencyBand = 'good' | 'fair' | 'poor' | 'none'

/**
 * consistencyBand maps a percentage onto a band.
 *
 * The thresholds follow the convention the research found in iRacing Insights,
 * which colours at 98 and 95 percent. They are only meaningful against the
 * per-session definition the store implements; applied to a figure pooled across
 * sessions they would call a consistent driver merely fair.
 *
 * A missing value bands as "none" rather than "poor": consistency is suppressed
 * below five laps, and colouring that as bad would invent a judgement.
 */
export function consistencyBand(pct: number | null | undefined): ConsistencyBand {
  if (pct == null || !Number.isFinite(pct)) return 'none'
  if (pct >= 98) return 'good'
  if (pct >= 95) return 'fair'
  return 'poor'
}

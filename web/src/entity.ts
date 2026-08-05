/*
 * Pure helpers for the Cars and Tracks pages.
 *
 * Separate from the page component so they are testable without a DOM, and so the
 * two pages cannot disagree about what a band means.
 */

import type { Filter } from './api'

/** Dimension is which entity a page is about. */
export type Dimension = 'car' | 'track'

/**
 * pageFilter drops the page's own dimension from the shared filter.
 *
 * The page's dimension is chosen by the left-hand list, and the filter bar hides
 * its dropdown for exactly that reason. Hiding the control does not clear the
 * value, though: a bookmarked or hand-edited `/cars?car=105` still parses to
 * carId, which collapsed the list to the single car being chosen from with no
 * visible control to undo it — the only escape was the generic Clear button,
 * which also discarded the range.
 *
 * Stripping it here means the list always offers every entity, and the selected
 * entity's own panels scope themselves by id instead. The *other* dimension is
 * left alone, so "this car, at this track" stays expressible.
 */
export function pageFilter(f: Filter, d: Dimension): Filter {
  const out = { ...f }
  if (d === 'car') delete out.carId
  else delete out.trackId
  return out
}

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

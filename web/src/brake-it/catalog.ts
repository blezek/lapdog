import type { Scenario } from './types'

export type ScenarioFilters = {
  car: string
  track: string
}

export function filterScenarios(scenarios: Scenario[], filters: ScenarioFilters): Scenario[] {
  return scenarios.filter((scenario) =>
    (!filters.car || scenario.carName === filters.car)
    && (!filters.track || scenario.trackName === filters.track),
  )
}

export function catalogCars(scenarios: Scenario[]): string[] {
  return distinct(scenarios.map((scenario) => scenario.carName))
}

export function catalogTracks(scenarios: Scenario[], car: string): string[] {
  return distinct(
    scenarios
      .filter((scenario) => !car || scenario.carName === car)
      .map((scenario) => scenario.trackName),
  )
}

export function scenariosForCombination(
  scenarios: Scenario[],
  car: string | null,
  track: string | null,
): Scenario[] {
  return scenarios.filter((scenario) => scenario.carName === car && scenario.trackName === track)
}

export function catalogCarChoices(scenarios: Scenario[]): Array<string | null> {
  return distinctNullable(scenarios.map((scenario) => scenario.carName))
}

export function catalogTrackChoices(scenarios: Scenario[]): Array<string | null> {
  return distinctNullable(scenarios.map((scenario) => scenario.trackName))
}

export function carChoicesForTrack(
  scenarios: Scenario[],
  track: string | null,
  selectedCar: string | null,
): Array<string | null> {
  return catalogCarChoices(scenarios).filter(
    (car) => car === selectedCar || carTrackCombinationExists(scenarios, car, track),
  )
}

export function trackChoicesForCar(
  scenarios: Scenario[],
  car: string | null,
  selectedTrack: string | null,
): Array<string | null> {
  return catalogTrackChoices(scenarios).filter(
    (track) => track === selectedTrack || carTrackCombinationExists(scenarios, car, track),
  )
}

export function carTrackCombinationExists(
  scenarios: Scenario[],
  car: string | null,
  track: string | null,
): boolean {
  return scenariosForCombination(scenarios, car, track).length > 0
}

function distinct(values: Array<string | null>): string[] {
  return [...new Set(values.filter((value): value is string => Boolean(value)))].sort((a, b) => a.localeCompare(b))
}

function distinctNullable(values: Array<string | null>): Array<string | null> {
  const strings = distinct(values)
  return values.some((value) => value === null) ? [...strings, null] : strings
}

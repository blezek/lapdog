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

function distinct(values: Array<string | null>): string[] {
  return [...new Set(values.filter((value): value is string => Boolean(value)))].sort((a, b) => a.localeCompare(b))
}

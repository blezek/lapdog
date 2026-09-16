import { describe, expect, it } from 'vitest'

import { catalogCars, catalogTracks, filterScenarios } from './catalog'
import type { Scenario } from './types'

function scenario(id: string, carName: string | null, trackName: string | null): Scenario {
  return {
    id,
    name: id,
    description: '',
    approachMs: 1000,
    acceleratorFallTargetMs: 200,
    brakeRiseTargetMs: 300,
    targetBrakePercent: 70,
    brakeTolerancePercent: 5,
    brakeHoldMs: 500,
    trailBrakeReleaseMs: 900,
    transitionEnabled: false,
    transitionMode: null,
    transitionDurationMs: null,
    transitionBrakePercent: null,
    acceleratorRampMs: 1000,
    origin: 'builtin',
    catalogVersion: 1,
    carName,
    trackName,
    sourceProvider: carName ? 'garage61' : null,
    retired: false,
    createdAt: '',
    updatedAt: '',
  }
}

const scenarios = [
  scenario('mx5-spa', 'Mazda MX-5 Cup', 'Spa Grand Prix'),
  scenario('mx5-road-atlanta', 'Mazda MX-5 Cup', 'Road Atlanta Full Course'),
  scenario('m2-spa', 'BMW M2 Racing (G87)', 'Spa Grand Prix'),
  scenario('general', null, null),
]

describe('Brake-It catalog filters', () => {
  it('filters by car and track without treating missing metadata as a match', () => {
    expect(filterScenarios(scenarios, { car: 'Mazda MX-5 Cup', track: 'Spa Grand Prix' }).map((item) => item.id)).toEqual(['mx5-spa'])
    expect(filterScenarios(scenarios, { car: '', track: 'Spa Grand Prix' }).map((item) => item.id)).toEqual(['mx5-spa', 'm2-spa'])
  })

  it('builds sorted car and car-dependent track choices', () => {
    expect(catalogCars(scenarios)).toEqual(['BMW M2 Racing (G87)', 'Mazda MX-5 Cup'])
    expect(catalogTracks(scenarios, 'Mazda MX-5 Cup')).toEqual(['Road Atlanta Full Course', 'Spa Grand Prix'])
  })
})

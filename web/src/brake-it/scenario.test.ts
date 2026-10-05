import { describe, expect, it } from 'vitest'

import { evaluateRun } from './analysis'
import { buildTargetSamples, getPracticeFinishMS, getScenarioTiming, targetAt } from './scenario'
import type { Scenario } from './types'

const scenario: Scenario = {
  id: 'test',
  name: 'Test scenario',
  description: '',
  approachMs: 1000,
  acceleratorFallTargetMs: 200,
  brakeRiseTargetMs: 400,
  targetBrakePercent: 70,
  brakeTolerancePercent: 5,
  brakeHoldMs: 500,
  trailBrakeReleaseMs: 900,
  transitionEnabled: true,
  transitionMode: 'low-brake',
  transitionDurationMs: 300,
  transitionBrakePercent: 4,
  acceleratorRampMs: 800,
  origin: 'builtin',
  catalogVersion: 1,
  carName: null,
  trackName: null,
  sourceProvider: null,
  retired: false,
  createdAt: '',
  updatedAt: '',
}

describe('Brake-It scenario model', () => {
  it('derives every phase boundary from the scenario', () => {
    expect(getScenarioTiming(scenario)).toEqual({
      brakeStartMs: 1000,
      brakeRiseEndMs: 1400,
      acceleratorFallEndMs: 1200,
      thresholdEndMs: 1900,
      trailEndMs: 2800,
      accelerationStartMs: 3100,
      acceleratorRampEndMs: 3900,
      finishMs: 5900,
    })
  })

  it('keeps zero-pressure targets distinct from missing values', () => {
    expect(targetAt(scenario, 0)).toMatchObject({ accelerator: 100, brake: 0 })
    expect(targetAt(scenario, 2950)).toMatchObject({ accelerator: 0, brake: 4 })
    expect(targetAt(scenario, 3900)).toMatchObject({ accelerator: 100, brake: 0 })
  })

  it('scores a sampled target trace without inventing absent crossings', () => {
    const perfect = buildTargetSamples(scenario, 300)
    expect(evaluateRun(scenario, perfect).score).toBeGreaterThan(95)

    const stationary = perfect.map((sample) => ({ ...sample, accelerator: 100, brake: 0 }))
    const metrics = evaluateRun(scenario, stationary)
    expect(metrics.acceleratorFallMs).toBeNull()
    expect(metrics.brakeRiseMs).toBeNull()
    expect(metrics.score).toBeLessThan(40)
  })

  it('ends braking-only practice after the trail and leaves acceleration absent', () => {
    const finishMS = getPracticeFinishMS(scenario, false)
    expect(finishMS).toBe(2800)
    expect(getPracticeFinishMS(scenario, true)).toBe(5900)

    const brakingOnly = buildTargetSamples(scenario, 180, finishMS)
    const metrics = evaluateRun(scenario, brakingOnly, false)
    expect(metrics.score).toBeGreaterThan(95)
    expect(metrics.transitionErrorPercent).toBeNull()
    expect(metrics.acceleratorRampErrorPercent).toBeNull()
  })
})

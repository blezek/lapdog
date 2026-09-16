import type { PedalSample, Scenario, ScenarioTiming } from './types'

export const FULL_ACCELERATION_ZONE_MS = 2000
export const DEFAULT_TRANSITION_DURATION_MS = 600
export const DEFAULT_TRANSITION_BRAKE_PERCENT = 5

export const clamp = (value: number, min: number, max: number) =>
  Math.min(max, Math.max(min, value))

export function transitionDurationFor(scenario: Scenario): number {
  if (!scenario.transitionEnabled) return 0
  return clamp(scenario.transitionDurationMs ?? DEFAULT_TRANSITION_DURATION_MS, 150, 2500)
}

export function transitionBrakeFor(scenario: Scenario): number {
  if (!scenario.transitionEnabled || scenario.transitionMode !== 'low-brake') return 0
  return clamp(scenario.transitionBrakePercent ?? DEFAULT_TRANSITION_BRAKE_PERCENT, 1, 25)
}

export function getScenarioTiming(scenario: Scenario): ScenarioTiming {
  const brakeStartMs = scenario.approachMs
  const brakeRiseEndMs = brakeStartMs + scenario.brakeRiseTargetMs
  const acceleratorFallEndMs = brakeStartMs + scenario.acceleratorFallTargetMs
  const thresholdEndMs =
    brakeStartMs +
    Math.max(scenario.brakeRiseTargetMs, scenario.acceleratorFallTargetMs) +
    scenario.brakeHoldMs
  const trailEndMs = thresholdEndMs + scenario.trailBrakeReleaseMs
  const accelerationStartMs = trailEndMs + transitionDurationFor(scenario)
  const acceleratorRampEndMs = accelerationStartMs + scenario.acceleratorRampMs
  return {
    brakeStartMs,
    brakeRiseEndMs,
    acceleratorFallEndMs,
    thresholdEndMs,
    trailEndMs,
    accelerationStartMs,
    acceleratorRampEndMs,
    finishMs: acceleratorRampEndMs + FULL_ACCELERATION_ZONE_MS,
  }
}

export function targetAt(scenario: Scenario, timeMs: number): PedalSample {
  const timing = getScenarioTiming(scenario)
  const transitionBrake = transitionBrakeFor(scenario)
  let brake = 0
  let accelerator = 100

  if (timeMs >= timing.brakeStartMs && timeMs < timing.brakeRiseEndMs) {
    brake = ((timeMs - timing.brakeStartMs) / scenario.brakeRiseTargetMs) * scenario.targetBrakePercent
  } else if (timeMs >= timing.brakeRiseEndMs && timeMs < timing.thresholdEndMs) {
    brake = scenario.targetBrakePercent
  } else if (timeMs >= timing.thresholdEndMs && timeMs < timing.trailEndMs) {
    brake =
      scenario.targetBrakePercent -
      (scenario.targetBrakePercent - transitionBrake) *
        ((timeMs - timing.thresholdEndMs) / scenario.trailBrakeReleaseMs)
  } else if (
    scenario.transitionEnabled &&
    timeMs >= timing.trailEndMs &&
    timeMs < timing.accelerationStartMs
  ) {
    brake = transitionBrake
  }

  if (timeMs >= timing.brakeStartMs && timeMs < timing.acceleratorFallEndMs) {
    accelerator =
      100 * (1 - (timeMs - timing.brakeStartMs) / scenario.acceleratorFallTargetMs)
  } else if (timeMs >= timing.acceleratorFallEndMs && timeMs < timing.accelerationStartMs) {
    accelerator = 0
  } else if (timeMs >= timing.accelerationStartMs && timeMs < timing.acceleratorRampEndMs) {
    accelerator =
      (100 * (timeMs - timing.accelerationStartMs)) / scenario.acceleratorRampMs
  } else if (timeMs >= timing.acceleratorRampEndMs) {
    accelerator = 100
  }

  return {
    timeMs,
    accelerator: clamp(accelerator, 0, 100),
    brake: clamp(brake, 0, 100),
    source: 'target',
  }
}

export function phaseForTime(scenario: Scenario, timeMs: number) {
  const t = getScenarioTiming(scenario)
  if (timeMs < t.brakeStartMs) return { id: 'approach', label: 'Approach', color: 'green' }
  if (timeMs < t.brakeRiseEndMs) return { id: 'brake-start', label: 'Brake', color: 'red' }
  if (timeMs < t.thresholdEndMs) return { id: 'threshold', label: 'Threshold', color: 'amber' }
  if (timeMs < t.trailEndMs) return { id: 'trail', label: 'Trail', color: 'blue' }
  if (timeMs < t.accelerationStartMs) {
    return scenario.transitionMode === 'low-brake'
      ? { id: 'low-brake', label: 'Low brake', color: 'amber' }
      : { id: 'coast', label: 'Coast', color: 'neutral' }
  }
  if (timeMs < t.acceleratorRampEndMs) return { id: 'accelerate', label: 'Accelerate', color: 'green' }
  if (timeMs < t.finishMs) return { id: 'full-throttle', label: 'Full throttle', color: 'green' }
  return { id: 'complete', label: 'Complete', color: 'neutral' }
}

export function buildTargetSamples(scenario: Scenario, count = 180): PedalSample[] {
  const { finishMs } = getScenarioTiming(scenario)
  return Array.from({ length: count }, (_, index) =>
    targetAt(scenario, (finishMs * index) / (count - 1)),
  )
}

export function formatMS(ms: number | null): string {
  if (ms == null || Number.isNaN(ms)) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${Math.round(ms)}ms`
}

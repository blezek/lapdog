import { clamp, getScenarioTiming, targetAt } from './scenario'
import type { PedalSample, RunMetrics, Scenario } from './types'

function firstCrossing(
  samples: PedalSample[],
  afterMS: number,
  predicate: (sample: PedalSample) => boolean,
): number | null {
  const sample = samples.find((candidate) => candidate.timeMs >= afterMS && predicate(candidate))
  return sample ? sample.timeMs - afterMS : null
}

function mean(values: number[]): number {
  return values.length === 0 ? 0 : values.reduce((total, value) => total + value, 0) / values.length
}

function integrateTimeInBand(
  samples: PedalSample[],
  startMS: number,
  endMS: number,
  lower: number,
  upper: number,
): number {
  let total = 0
  for (let index = 1; index < samples.length; index += 1) {
    const previous = samples[index - 1]
    const current = samples[index]
    if (!previous || !current || current.timeMs < startMS || previous.timeMs > endMS) continue
    const midpointBrake = (previous.brake + current.brake) / 2
    if (midpointBrake >= lower && midpointBrake <= upper) {
      total += Math.min(current.timeMs, endMS) - Math.max(previous.timeMs, startMS)
    }
  }
  return Math.max(0, total)
}

export function evaluateRun(
  scenario: Scenario,
  samples: PedalSample[],
  accelerationIncluded = true,
): RunMetrics {
  const timing = getScenarioTiming(scenario)
  const lowerTarget = scenario.targetBrakePercent - scenario.brakeTolerancePercent
  const upperTarget = scenario.targetBrakePercent + scenario.brakeTolerancePercent
  const holdSamples = samples.filter(
    (sample) => sample.timeMs >= timing.brakeRiseEndMs && sample.timeMs <= timing.thresholdEndMs,
  )
  const trailSamples = samples.filter(
    (sample) => sample.timeMs >= timing.thresholdEndMs && sample.timeMs <= timing.trailEndMs,
  )
  const transitionSamples = samples.filter(
    (sample) =>
      scenario.transitionEnabled &&
      sample.timeMs >= timing.trailEndMs &&
      sample.timeMs <= timing.accelerationStartMs,
  )
  const acceleratorSamples = samples.filter(
    (sample) =>
      sample.timeMs >= timing.accelerationStartMs &&
      sample.timeMs <= timing.acceleratorRampEndMs,
  )
  const acceleratorFallMs = firstCrossing(samples, timing.brakeStartMs, (sample) => sample.accelerator <= 5)
  const brakeRiseMs = firstCrossing(samples, timing.brakeStartMs, (sample) => sample.brake >= lowerTarget)
  const averageBrakeDeviationPercent = mean(
    holdSamples.map((sample) => Math.abs(sample.brake - scenario.targetBrakePercent)),
  )
  const holdTimeInBandMs = integrateTimeInBand(
    samples,
    timing.brakeRiseEndMs,
    timing.thresholdEndMs,
    lowerTarget,
    upperTarget,
  )
  const trailErrorPercent = mean(
    trailSamples.map((sample) => Math.abs(sample.brake - targetAt(scenario, sample.timeMs).brake)),
  )
  const transitionErrorPercent = accelerationIncluded && scenario.transitionEnabled
    ? mean(
        transitionSamples.map((sample) => {
          const target = targetAt(scenario, sample.timeMs)
          return Math.abs(sample.brake - target.brake) + Math.abs(sample.accelerator - target.accelerator) * 0.5
        }),
      )
    : null
  const acceleratorRampErrorPercent = accelerationIncluded
    ? mean(
        acceleratorSamples.map((sample) =>
          Math.abs(sample.accelerator - targetAt(scenario, sample.timeMs).accelerator),
        ),
      )
    : null
  const fallPenalty = acceleratorFallMs === null ? 24 : Math.max(0, acceleratorFallMs - scenario.acceleratorFallTargetMs) / 18
  const risePenalty = brakeRiseMs === null ? 24 : Math.max(0, brakeRiseMs - scenario.brakeRiseTargetMs) / 18
  const holdTargetMS = timing.thresholdEndMs - timing.brakeRiseEndMs
  const holdMissRatio = holdTargetMS <= 0 ? 0 : clamp((holdTargetMS - holdTimeInBandMs) / holdTargetMS, 0, 1)
  const score = clamp(
    100 -
      fallPenalty -
      risePenalty -
      averageBrakeDeviationPercent * 1.25 -
      holdMissRatio * 26 -
      trailErrorPercent * 0.75 -
      (transitionErrorPercent ?? 0) * 0.75 -
      (acceleratorRampErrorPercent ?? 0) * 0.75,
    0,
    100,
  )
  return {
    score,
    acceleratorFallMs,
    brakeRiseMs,
    averageBrakeDeviationPercent,
    holdTimeInBandMs,
    trailErrorPercent,
    transitionErrorPercent,
    acceleratorRampErrorPercent,
  }
}

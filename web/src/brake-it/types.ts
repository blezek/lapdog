export type PedalInput = {
  accelerator: number
  brake: number
  source: string
}

export type PedalSample = PedalInput & { timeMs: number }

export type TransitionMode = 'coast' | 'low-brake'

export type Scenario = {
  id: string
  name: string
  description: string
  approachMs: number
  acceleratorFallTargetMs: number
  brakeRiseTargetMs: number
  targetBrakePercent: number
  brakeTolerancePercent: number
  brakeHoldMs: number
  trailBrakeReleaseMs: number
  transitionEnabled: boolean
  transitionMode: TransitionMode | null
  transitionDurationMs: number | null
  transitionBrakePercent: number | null
  acceleratorRampMs: number
  origin: 'builtin' | 'custom'
  catalogVersion: number | null
  carName: string | null
  trackName: string | null
  sourceProvider: 'garage61' | null
  retired: boolean
  createdAt: string
  updatedAt: string
}

export type ScenarioTiming = {
  brakeStartMs: number
  brakeRiseEndMs: number
  acceleratorFallEndMs: number
  thresholdEndMs: number
  trailEndMs: number
  accelerationStartMs: number
  acceleratorRampEndMs: number
  finishMs: number
}

export type RunMetrics = {
  score: number
  acceleratorFallMs: number | null
  brakeRiseMs: number | null
  averageBrakeDeviationPercent: number
  holdTimeInBandMs: number
  trailErrorPercent: number
  transitionErrorPercent: number | null
  acceleratorRampErrorPercent: number
}

export type RunResult = {
  id: string
  scenarioId: string
  scenarioName: string
  deviceLabel: string
  createdAt: string
  scoringVersion: number
  metrics: RunMetrics
  samples: PedalSample[]
}

export type BrakeSettings = {
  selectedScenarioId: string | null
  baudRate: number
  usbVendorId: string | null
  usbProductId: string | null
}

export type ControllerDevice = {
  id: string
  label: string
  kind: 'keyboard' | 'serial'
  status: 'available' | 'connected' | 'unsupported' | 'error'
  detail: string
  port?: SerialPort
}

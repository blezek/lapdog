export type PedalInput = {
  accelerator: number
  brake: number
  source: string
}

export type PedalSample = PedalInput & { timeMs: number }

export type TransitionMode = 'coast' | 'low-brake'

export type Garage61Entity = {
  name: string
  variant?: string
}

export type Garage61LapContribution = {
  zone?: number
  telemetryWindowStartLapPercent?: number
  telemetryWindowEndLapPercent?: number
  peakBrakePercent?: number
}

export type Garage61LapCitation = {
  lapId: string
  garage61Url: string
  garage61TelemetryUrl?: string
  garage61AnalysisUrl?: string
  garage61AnalyzeUrl?: string
  lapTimeSec: number
  contribution: Garage61LapContribution
}

export type ScenarioSourceModel = Record<string, string | number | boolean | null | undefined> & {
  targetBrakeMedianPercent?: number
  targetBrakeP10Percent?: number
  targetBrakeP90Percent?: number
  targetBrakeVariationPercent?: number
}

export type ScenarioSource = {
  provider: 'garage61'
  generatedAt?: string
  generatorVersion?: string
  method?: string
  track: Garage61Entity
  car: Garage61Entity
  model: ScenarioSourceModel
  sourceLaps: Garage61LapCitation[]
}

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
  source?: ScenarioSource
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
  acceleratorRampErrorPercent: number | null
}

export type RunResult = {
  id: string
  scenarioId: string
  scenarioName: string
  deviceLabel: string
  createdAt: string
  scoringVersion: number
  accelerationIncluded: boolean
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

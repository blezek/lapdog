import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { PracticeCue, ScenarioMetadataFields, ScenarioPicker, ScenarioTraceEditor, Simulator, SourcePanel, TraceChart } from './BrakeItApp'
import type { PedalSample, Scenario } from './types'

const scenario: Scenario = {
  id: 'garage61-test-zone-1',
  name: 'Test braking zone',
  description: 'Generated target',
  approachMs: 2200,
  acceleratorFallTargetMs: 280,
  brakeRiseTargetMs: 420,
  targetBrakePercent: 76,
  brakeTolerancePercent: 6,
  brakeHoldMs: 1050,
  trailBrakeReleaseMs: 1850,
  transitionEnabled: false,
  transitionMode: null,
  transitionDurationMs: null,
  transitionBrakePercent: null,
  acceleratorRampMs: 1700,
  origin: 'builtin',
  catalogVersion: 1,
  carName: 'Global Mazda MX-5 Cup',
  trackName: 'Road Atlanta Full Course',
  sourceProvider: 'garage61',
  source: {
    provider: 'garage61',
    generatedAt: '2026-09-18T12:00:00Z',
    track: { name: 'Road Atlanta', variant: 'Full Course' },
    car: { name: 'Global Mazda MX-5 Cup' },
    model: {
      targetBrakeMedianPercent: 76,
      targetBrakeP10Percent: 70,
      targetBrakeP90Percent: 82,
      targetBrakeVariationPercent: 6,
    },
    sourceLaps: [{
      lapId: 'lap-one',
      garage61Url: 'https://garage61.net/app/analyze;t=lap-one',
      garage61AnalyzeUrl: 'https://garage61.net/app/analyze;t=lap-one',
      lapTimeSec: 87.123,
      contribution: {
        zone: 1,
        telemetryWindowStartLapPercent: 12.5,
        telemetryWindowEndLapPercent: 16.75,
      },
    }],
  },
  retired: false,
  createdAt: '2026-09-18T12:00:00Z',
  updatedAt: '2026-09-18T12:00:00Z',
}

describe('Brake-It parity', () => {
  it('links car, track, and scenario choices in the simulator', () => {
    const second = {
      ...scenario,
      id: 'garage61-test-zone-2',
      name: 'Second braking zone',
    }
    const bmwSpa = {
      ...scenario,
      id: 'garage61-bmw-spa-zone-1',
      name: 'BMW Spa braking zone',
      carName: 'BMW M2 Racing (G87)',
      trackName: 'Spa Grand Prix',
    }
    const html = renderToStaticMarkup(
      <ScenarioPicker scenarios={[scenario, second, bmwSpa]} selectedID={second.id} disabled onSelect={() => undefined} />,
    )

    expect(html).toContain('aria-label="Practice car"')
    expect(html).toContain('aria-label="Practice track"')
    expect(html).toContain('aria-label="Practice scenario"')
    expect(html).toContain('BMW M2 Racing (G87) · unavailable at Road Atlanta Full Course')
    expect(html).toContain('Spa Grand Prix · unavailable for Global Mazda MX-5 Cup')
    expect(html).toContain('Test braking zone')
    expect(html).toContain('Second braking zone')
    expect(html).not.toContain('BMW Spa braking zone')
    expect(html).toContain('value="garage61-test-zone-2" selected=""')
    expect(html).toContain('Stop the current run to change scenarios.')

    const simulator = renderToStaticMarkup(
      <Simulator
        scenarios={[scenario, second, bmwSpa]}
        scenario={second}
        input={{ accelerator: 0, brake: 0, source: 'keyboard' }}
        deviceLabel="Keyboard simulator"
        onSelectScenario={() => undefined}
        onResult={() => undefined}
      />,
    )
    expect(simulator).toContain('aria-label="Practice car"')
    expect(simulator).toContain('aria-label="Practice track"')
    expect(simulator).toContain('aria-label="Practice scenario"')
    expect(simulator).toContain('Second braking zone')
  })

  it('renders the trace axes, phase markers, zones, and tolerance band', () => {
    const html = renderToStaticMarkup(<TraceChart scenario={scenario} samples={[]} nowMS={0} />)

    expect(html).toContain('0%')
    expect(html).toContain('100%')
    expect(html).toContain('Brake')
    expect(html).toContain('Trail')
    expect(html).toContain('Accel')
    expect(html).toContain('zone-threshold')
    expect(html).toContain('class="tolerance"')
  })

  it('hides target traces without hiding the driver traces', () => {
    const samples: PedalSample[] = [
      { timeMs: 0, accelerator: 100, brake: 0, source: 'keyboard' },
      { timeMs: 1000, accelerator: 0, brake: 70, source: 'keyboard' },
    ]
    const html = renderToStaticMarkup(
      <TraceChart scenario={scenario} samples={samples} nowMS={1000} showTarget={false} />,
    )

    expect(html).not.toContain('target-accelerator')
    expect(html).not.toContain('target-brake')
    expect(html).not.toContain('class="tolerance"')
    expect(html).toContain('live-accelerator')
    expect(html).toContain('live-brake')
    expect(html).toContain('aria-label="Driver accelerator and brake traces"')
  })

  it('renders distinct accessible cues for braking, trail, coast, and acceleration', () => {
    const brake = renderToStaticMarkup(<PracticeCue phase="brake-start" />)
    const trail = renderToStaticMarkup(<PracticeCue phase="trail" />)
    const coast = renderToStaticMarkup(<PracticeCue phase="coast" />)
    const accelerate = renderToStaticMarkup(<PracticeCue phase="accelerate" />)

    expect(brake).toContain('Brake now: Begin braking')
    expect(brake).toContain('STOP')
    expect(trail).toContain('Trail brake: Ease off the brake smoothly')
    expect(coast).toContain('Coast: Keep both pedals released')
    expect(accelerate).toContain('Accelerate: Build accelerator smoothly')
    expect(accelerate).toContain('cue-accelerate')
  })

  it('renders a Garage61 link for every retained source lap', () => {
    const html = renderToStaticMarkup(<SourcePanel scenario={scenario} />)

    expect(html).toContain('Garage61 citations')
    expect(html).toContain('87.123s')
    expect(html).toContain('12.50–16.75% lap')
    expect(html).toContain('href="https://garage61.net/app/analyze;t=lap-one"')
    expect(html).toContain('Export citations')
  })

  it('keeps the original zoom and trace handles in the scenario editor', () => {
    const html = renderToStaticMarkup(
      <ScenarioTraceEditor scenario={scenario} editable={false} onChange={() => undefined} />,
    )

    expect(html).toContain('aria-label="Trace zoom"')
    expect(html).toContain('aria-label="Brake rise and target"')
    expect(html).toContain('aria-label="Trail end"')
    expect(html).toContain('duplicate to edit')
  })

  it('edits custom car and track metadata with catalog suggestions', () => {
    const bmw = {
      ...scenario,
      id: 'garage61-bmw-spa-zone-1',
      carName: 'BMW M2 Racing (G87)',
      trackName: 'Spa Grand Prix',
    }
    const custom = { ...scenario, id: 'custom-zone', origin: 'custom' as const }
    const html = renderToStaticMarkup(
      <ScenarioMetadataFields scenarios={[scenario, bmw, custom]} scenario={custom} editable onChange={() => undefined} />,
    )

    expect(html).toContain('aria-label="Scenario car"')
    expect(html).toContain('aria-label="Scenario track"')
    expect(html).toContain('list="brake-scenario-car-options"')
    expect(html).toContain('value="BMW M2 Racing (G87)"')
    expect(html).toContain('value="Global Mazda MX-5 Cup"')
    expect(html).toContain('value="Road Atlanta Full Course"')
    expect(html).not.toContain('value="Spa Grand Prix"')
    expect(html).not.toContain('disabled=""')

    const readonly = renderToStaticMarkup(
      <ScenarioMetadataFields scenarios={[scenario]} scenario={scenario} editable={false} onChange={() => undefined} />,
    )
    expect(readonly.match(/disabled=""/g)).toHaveLength(2)
  })
})

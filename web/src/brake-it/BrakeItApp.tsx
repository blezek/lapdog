import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, Navigate, NavLink, Route, Routes } from 'react-router-dom'

import { api } from '../api'
import brakeItIcon from '../assets/brake-it-icon.png'
import brakeItKart from '../assets/brake-it-kart.png'
import { Icon } from '../components/ui'
import { applyTheme } from '../theme'
import { evaluateRun } from './analysis'
import { brakeApi } from './api'
import { CuePlayer } from './audio'
import { catalogCars, catalogTracks, filterScenarios } from './catalog'
import {
  buildTargetSamples,
  clamp,
  formatMS,
  getScenarioTiming,
  phaseForTime,
  targetAt,
} from './scenario'
import {
  getAuthorizedSerialDevices,
  hasWebSerial,
  openSerialController,
  requestSerialDevice,
  type SerialConnection,
} from './serial'
import type {
  BrakeSettings,
  ControllerDevice,
  PedalInput,
  PedalSample,
  RunMetrics,
  RunResult,
  Scenario,
} from './types'
import { useKeyboardController } from './useKeyboardController'
import './styles.css'

const keyboardDevice: ControllerDevice = {
  id: 'keyboard',
  label: 'Keyboard simulator',
  kind: 'keyboard',
  status: 'connected',
  detail: 'Arrow keys',
}

const defaultSettings: BrakeSettings = {
  selectedScenarioId: null,
  baudRate: 115200,
  usbVendorId: null,
  usbProductId: null,
}

const tabs = [
  ['/brake-it/simulator', 'Simulator', 'steering'],
  ['/brake-it/scenarios', 'Scenarios', 'speedometer'],
  ['/brake-it/devices', 'Devices', 'cog'],
  ['/brake-it/results', 'Results', 'chart-line'],
] as const

export function BrakeItApp() {
  const [scenarios, setScenarios] = useState<Scenario[]>([])
  const [results, setResults] = useState<RunResult[]>([])
  const [settings, setSettings] = useState<BrakeSettings>(defaultSettings)
  const [selectedScenarioID, setSelectedScenarioID] = useState('')
  const [devices, setDevices] = useState<ControllerDevice[]>([keyboardDevice])
  const [selectedDeviceID, setSelectedDeviceID] = useState('keyboard')
  const [serialInput, setSerialInput] = useState<PedalInput>({ accelerator: 0, brake: 0, source: 'serial' })
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const serialConnection = useRef<SerialConnection | null>(null)
  const keyboardInput = useKeyboardController(selectedDeviceID === 'keyboard')
  const selectedScenario = scenarios.find((scenario) => scenario.id === selectedScenarioID) ?? scenarios[0]
  const selectedDevice = devices.find((device) => device.id === selectedDeviceID) ?? keyboardDevice
  const currentInput = selectedDevice.kind === 'keyboard' ? keyboardInput : serialInput

  useEffect(() => {
    const before = document.title
    document.title = 'Brake-It · LapDog'
    return () => { document.title = before }
  }, [])

  const refreshDevices = useCallback(async () => {
    if (!hasWebSerial()) {
      setDevices([keyboardDevice])
      return
    }
    try {
      const serial = await getAuthorizedSerialDevices()
      setDevices((current) => {
        const connected = current.filter((device) => device.kind === 'serial' && device.status === 'connected')
        const connectedIDs = new Set(connected.map((device) => device.id))
        return [keyboardDevice, ...connected, ...serial.filter((device) => !connectedIDs.has(device.id))]
      })
    } catch (caught) {
      setError(message(caught))
    }
  }, [])

  useEffect(() => {
    let active = true
    void Promise.all([brakeApi.scenarios(), brakeApi.results(), brakeApi.settings(), api.settings()])
      .then(([scenarioRows, resultRows, savedSettings, lapdogSettings]) => {
        if (!active) return
        applyTheme(lapdogSettings.theme)
        setScenarios(scenarioRows)
        setResults(resultRows)
        setSettings(savedSettings)
        const preferred = savedSettings.selectedScenarioId
        setSelectedScenarioID(
          preferred && scenarioRows.some((scenario) => scenario.id === preferred)
            ? preferred
            : (scenarioRows[0]?.id ?? ''),
        )
      })
      .catch((caught: unknown) => active && setError(message(caught)))
      .finally(() => active && setLoading(false))
    void refreshDevices()
    return () => {
      active = false
      void serialConnection.current?.close()
      serialConnection.current = null
    }
  }, [refreshDevices])

  useEffect(() => {
    if (!navigator.serial) return
    const changed = () => void refreshDevices()
    navigator.serial.addEventListener('connect', changed)
    navigator.serial.addEventListener('disconnect', changed)
    return () => {
      navigator.serial?.removeEventListener('connect', changed)
      navigator.serial?.removeEventListener('disconnect', changed)
    }
  }, [refreshDevices])

  const selectScenario = async (id: string) => {
    setSelectedScenarioID(id)
    const next = { ...settings, selectedScenarioId: id }
    setSettings(next)
    try {
      await brakeApi.saveSettings(next)
    } catch (caught) {
      setError(message(caught))
    }
  }

  const updateScenarioLocal = (scenario: Scenario) => {
    setScenarios((current) => current.map((item) => (item.id === scenario.id ? scenario : item)))
  }

  const saveScenario = async (scenario: Scenario) => {
    try {
      const saved = await brakeApi.updateScenario(scenario)
      updateScenarioLocal(saved)
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }

  const duplicateScenario = async (scenario: Scenario) => {
    const copy: Scenario = {
      ...scenario,
      id: crypto.randomUUID(),
      name: `${scenario.name} Copy`,
      origin: 'custom',
      catalogVersion: null,
      retired: false,
      createdAt: '',
      updatedAt: '',
    }
    try {
      const saved = await brakeApi.createScenario(copy)
      setScenarios((current) => [...current, saved])
      await selectScenario(saved.id)
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }

  const deleteScenario = async (scenario: Scenario) => {
    try {
      await brakeApi.deleteScenario(scenario.id)
      const remaining = scenarios.filter((item) => item.id !== scenario.id)
      setScenarios(remaining)
      await selectScenario(remaining[0]?.id ?? '')
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }

  const storeResult = useCallback(async (result: RunResult) => {
    try {
      const saved = await brakeApi.saveResult(result)
      setResults((current) => [saved, ...current])
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }, [])

  const requestSerial = async () => {
    try {
      const vendor = parseHex(settings.usbVendorId)
      const product = parseHex(settings.usbProductId)
      const filters = vendor === undefined ? [] : [{ usbVendorId: vendor, ...(product === undefined ? {} : { usbProductId: product }) }]
      const device = await requestSerialDevice(filters)
      setDevices((current) => [...current.filter((item) => item.id !== device.id), device])
      setSelectedDeviceID(device.id)
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }

  const connectSerial = async (device: ControllerDevice) => {
    if (!device.port) return
    try {
      await serialConnection.current?.close()
      serialConnection.current = await openSerialController(
        device.port,
        settings.baudRate,
        setSerialInput,
        (detail, supported) => {
          setDevices((current) =>
            current.map((item) =>
              item.id === device.id
                ? { ...item, status: supported === false ? 'unsupported' : 'connected', detail }
                : item,
            ),
          )
        },
      )
      setSelectedDeviceID(device.id)
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }

  const disconnectSerial = async () => {
    await serialConnection.current?.close()
    serialConnection.current = null
    setSelectedDeviceID('keyboard')
    await refreshDevices()
  }

  const saveDeviceSettings = async (next: BrakeSettings) => {
    try {
      const saved = await brakeApi.saveSettings(next)
      setSettings(saved)
      setError(null)
    } catch (caught) {
      setError(message(caught))
    }
  }

  if (loading) return <div className="brake-it brake-loading">Loading Brake-it…</div>

  return (
    <div className="brake-it">
      <header className="brake-topbar">
        <div className="brake-brand">
          <span className="brake-mark"><img src={brakeItIcon} alt="" /></span>
          <div><strong>Brake-It</strong><span>Pedal timing lab</span></div>
        </div>
        <nav className="brake-tabs" aria-label="Brake-it">
          {tabs.map(([to, label, icon]) => (
            <NavLink key={to} to={to} aria-label={label} title={label} className={({ isActive }) => (isActive ? 'active' : '')}>
              <Icon name={icon} /><span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <Link className="brake-back" to="/dashboard">← Back to LapDog</Link>
      </header>

      {error && <div className="brake-error" role="alert">{error}<button type="button" onClick={() => setError(null)}>Dismiss</button></div>}

      {!selectedScenario ? (
        <div className="brake-empty">No Brake-it scenario is available.</div>
      ) : (
        <Routes>
          <Route path="/brake-it" element={<Navigate to="/brake-it/simulator" replace />} />
          <Route path="/brake-it/simulator" element={<Simulator scenario={selectedScenario} input={currentInput} deviceLabel={selectedDevice.label} onResult={storeResult} />} />
          <Route path="/brake-it/scenarios" element={<ScenarioEditor scenarios={scenarios} scenario={selectedScenario} onSelect={selectScenario} onChange={updateScenarioLocal} onSave={saveScenario} onDuplicate={duplicateScenario} onDelete={deleteScenario} />} />
          <Route path="/brake-it/devices" element={<Devices devices={devices} selectedDeviceID={selectedDeviceID} settings={settings} onSelect={setSelectedDeviceID} onRequest={requestSerial} onRefresh={refreshDevices} onConnect={connectSerial} onDisconnect={disconnectSerial} onSettings={saveDeviceSettings} />} />
          <Route path="/brake-it/results" element={<Results results={results} scenarios={scenarios} />} />
          <Route path="*" element={<Navigate to="/brake-it/simulator" replace />} />
        </Routes>
      )}
    </div>
  )
}

function Simulator({ scenario, input, deviceLabel, onResult }: { scenario: Scenario; input: PedalInput; deviceLabel: string; onResult: (result: RunResult) => void }) {
  const timing = getScenarioTiming(scenario)
  const [running, setRunning] = useState(false)
  const [nowMS, setNowMS] = useState(0)
  const [samples, setSamples] = useState<PedalSample[]>([])
  const [audioEnabled, setAudioEnabled] = useState(true)
  const inputRef = useRef(input)
  const frame = useRef(0)
  const startAt = useRef(0)
  const lastSample = useRef(-100)
  const saved = useRef(false)
  const lastPhase = useRef('approach')
  const cues = useRef(new CuePlayer())
  const phase = phaseForTime(scenario, nowMS)
  const target = targetAt(scenario, nowMS)
  const metrics = useMemo(() => (samples.length > 3 ? evaluateRun(scenario, samples) : null), [samples, scenario])

  useEffect(() => { inputRef.current = input }, [input])
  useEffect(() => { cues.current.setEnabled(audioEnabled) }, [audioEnabled])
  useEffect(() => () => { cancelAnimationFrame(frame.current); void cues.current.close() }, [])

  useEffect(() => {
    if (!running) return
    const tick = (timestamp: number) => {
      const elapsed = Math.min(timing.finishMs, timestamp - startAt.current)
      const nextPhase = phaseForTime(scenario, elapsed)
      setNowMS(elapsed)
      if (nextPhase.id !== lastPhase.current) {
        lastPhase.current = nextPhase.id
        const cue = cueFor(nextPhase.id)
        if (cue) void cues.current.play(cue)
      }
      if (elapsed - lastSample.current >= 32 || elapsed >= timing.finishMs) {
        lastSample.current = elapsed
        const snapshot = inputRef.current
        setSamples((current) => [...current, { timeMs: elapsed, accelerator: snapshot.accelerator, brake: snapshot.brake, source: snapshot.source }])
      }
      if (elapsed >= timing.finishMs) {
        setRunning(false)
        return
      }
      frame.current = requestAnimationFrame(tick)
    }
    frame.current = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame.current)
  }, [running, scenario, timing.finishMs])

  useEffect(() => {
    const final = samples.at(-1)
    if (running || saved.current || !final || final.timeMs < timing.finishMs - 10) return
    saved.current = true
    onResult({
      id: crypto.randomUUID(),
      scenarioId: scenario.id,
      scenarioName: scenario.name,
      deviceLabel,
      createdAt: '',
      scoringVersion: 1,
      metrics: evaluateRun(scenario, samples),
      samples,
    })
    void cues.current.play('done')
  }, [deviceLabel, onResult, running, samples, scenario, timing.finishMs])

  const start = async () => {
    saved.current = false
    lastPhase.current = 'approach'
    lastSample.current = -100
    setSamples([])
    setNowMS(0)
    startAt.current = performance.now()
    await cues.current.wake()
    setRunning(true)
  }

  const stop = () => {
    setRunning(false)
    cancelAnimationFrame(frame.current)
  }

  return (
    <main className="brake-simulator">
      <div className={`brake-cue brake-cue-${phase.color}`}>
        <div><span>Phase</span><strong>{phase.label}</strong></div>
        <div><span>Time</span><strong>{formatMS(nowMS)} / {formatMS(timing.finishMs)}</strong></div>
        <div><span>Device</span><strong>{deviceLabel}</strong></div>
      </div>
      <div className="brake-sim-grid">
        <section className="brake-panel">
          <div className="brake-panel-head">
            <div className="brake-scenario-heading">
              <img className="brake-scenario-art" src={brakeItKart} alt="" />
              <div><strong>{scenario.name}</strong><span>{scenario.description}</span></div>
            </div>
            <div className="brake-actions">
              <button type="button" onClick={() => setAudioEnabled((value) => !value)}>{audioEnabled ? 'Audio on' : 'Audio off'}</button>
              <button type="button" onClick={() => { stop(); setSamples([]); setNowMS(0); saved.current = true }}>Reset</button>
              <button className={running ? 'danger' : 'primary'} type="button" onClick={running ? stop : start}>{running ? 'Stop' : 'Start'}</button>
            </div>
          </div>
          <TraceChart scenario={scenario} samples={samples} nowMS={nowMS} />
          <p className="brake-key-hint">Keyboard: ↑ accelerator · ↓ brake · ←/→ trim brake pressure</p>
        </section>
        <aside className="brake-panel brake-live">
          <PedalMeter label="Accelerator" value={input.accelerator} kind="accelerator" />
          <PedalMeter label="Brake" value={input.brake} kind="brake" target={target.brake > 0 ? target.brake : null} tolerance={scenario.brakeTolerancePercent} />
          <div className="brake-targets"><div><span>Target accelerator</span><strong>{Math.round(target.accelerator)}%</strong></div><div><span>Target brake</span><strong>{Math.round(target.brake)}%</strong></div></div>
          {metrics && <MetricTiles metrics={metrics} />}
        </aside>
      </div>
    </main>
  )
}

function TraceChart({ scenario, samples, nowMS, compact = false }: { scenario: Scenario; samples: PedalSample[]; nowMS: number; compact?: boolean }) {
  const timing = getScenarioTiming(scenario)
  const targets = buildTargetSamples(scenario)
  const width = 1000
  const height = 320
  const x = (time: number) => (time / timing.finishMs) * width
  const y = (value: number) => height - 20 - value * 2.8
  const path = (values: PedalSample[], key: 'accelerator' | 'brake') =>
    values.map((sample, index) => `${index === 0 ? 'M' : 'L'}${x(sample.timeMs).toFixed(1)},${y(sample[key]).toFixed(1)}`).join(' ')
  const lower = clamp(scenario.targetBrakePercent - scenario.brakeTolerancePercent, 0, 100)
  const upper = clamp(scenario.targetBrakePercent + scenario.brakeTolerancePercent, 0, 100)
  return (
    <svg className={`brake-chart${compact ? ' compact' : ''}`} viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Target and actual accelerator and brake traces">
      {[0, 25, 50, 75, 100].map((value) => <line key={value} x1="0" x2={width} y1={y(value)} y2={y(value)} className="grid" />)}
      <rect x={x(timing.brakeRiseEndMs)} y={y(upper)} width={Math.max(0, x(timing.thresholdEndMs) - x(timing.brakeRiseEndMs))} height={Math.max(0, y(lower) - y(upper))} className="tolerance" />
      <path d={path(targets, 'accelerator')} className="target-accelerator" />
      <path d={path(targets, 'brake')} className="target-brake" />
      {samples.length > 1 && <path d={path(samples, 'accelerator')} className="live-accelerator" />}
      {samples.length > 1 && <path d={path(samples, 'brake')} className="live-brake" />}
      <line x1={x(nowMS)} x2={x(nowMS)} y1="0" y2={height} className="progress" />
    </svg>
  )
}

function PedalMeter({ label, value, kind, target, tolerance = 0 }: { label: string; value: number; kind: string; target?: number | null; tolerance?: number }) {
  return <div className="brake-pedal"><div><span>{label}</span><strong>{Math.round(value)}%</strong></div><div className="brake-meter">{target != null && <i className="band" style={{ left: `${clamp(target - tolerance, 0, 100)}%`, width: `${clamp(target + tolerance, 0, 100) - clamp(target - tolerance, 0, 100)}%` }} />}<i className={kind} style={{ width: `${clamp(value, 0, 100)}%` }} /></div></div>
}

function MetricTiles({ metrics }: { metrics: RunMetrics }) {
  return <div className="brake-metrics"><div><span>Score</span><strong>{Math.round(metrics.score)}</strong></div><div><span>Throttle fall</span><strong>{formatMS(metrics.acceleratorFallMs)}</strong></div><div><span>Brake rise</span><strong>{formatMS(metrics.brakeRiseMs)}</strong></div><div><span>Trail error</span><strong>{metrics.trailErrorPercent.toFixed(1)}%</strong></div></div>
}

function ScenarioEditor({ scenarios, scenario, onSelect, onChange, onSave, onDuplicate, onDelete }: { scenarios: Scenario[]; scenario: Scenario; onSelect: (id: string) => void; onChange: (scenario: Scenario) => void; onSave: (scenario: Scenario) => void; onDuplicate: (scenario: Scenario) => void; onDelete: (scenario: Scenario) => void }) {
  const editable = scenario.origin === 'custom'
  const [carFilter, setCarFilter] = useState('')
  const [trackFilter, setTrackFilter] = useState('')
  const cars = useMemo(() => catalogCars(scenarios), [scenarios])
  const tracks = useMemo(() => catalogTracks(scenarios, carFilter), [scenarios, carFilter])
  const filtered = useMemo(
    () => filterScenarios(scenarios, { car: carFilter, track: trackFilter }),
    [scenarios, carFilter, trackFilter],
  )
  const selectCar = (car: string) => {
    setCarFilter(car)
    if (trackFilter && !catalogTracks(scenarios, car).includes(trackFilter)) setTrackFilter('')
  }
  const number = (key: keyof Scenario, value: number) => onChange({ ...scenario, [key]: value })
  const toggleTransition = (enabled: boolean) => onChange({
    ...scenario,
    transitionEnabled: enabled,
    transitionMode: enabled ? (scenario.transitionMode ?? 'coast') : null,
    transitionDurationMs: enabled ? (scenario.transitionDurationMs ?? 600) : null,
    transitionBrakePercent: enabled && scenario.transitionMode === 'low-brake'
      ? (scenario.transitionBrakePercent ?? 5)
      : null,
  })
  return <main className="brake-screen-grid"><aside className="brake-panel"><h2>Scenarios</h2><div className="brake-catalog-filters"><label>Car<select aria-label="Search scenarios by car" value={carFilter} onChange={(event) => selectCar(event.target.value)}><option value="">All cars</option>{cars.map((car) => <option key={car} value={car}>{car}</option>)}</select></label><label>Track<select aria-label="Search scenarios by track" value={trackFilter} onChange={(event) => setTrackFilter(event.target.value)}><option value="">All tracks</option>{tracks.map((track) => <option key={track} value={track}>{track}</option>)}</select></label></div><div className="brake-list">{filtered.map((item) => <button key={item.id} type="button" className={item.id === scenario.id ? 'active' : ''} onClick={() => onSelect(item.id)}><strong>{item.name}</strong><span>{item.carName && item.trackName ? `${item.carName} · ${item.trackName}` : item.origin === 'builtin' ? 'Built in' : 'Custom'}</span></button>)}{filtered.length === 0 && <p className="brake-list-empty">No scenarios match this car and track.</p>}</div><div className="brake-actions"><button type="button" onClick={() => onDuplicate(scenario)}>Duplicate</button>{editable && <button className="danger-text" type="button" onClick={() => onDelete(scenario)}>Delete</button>}</div></aside><section className="brake-panel"><div className="brake-panel-head"><div><h1>{scenario.name}</h1><span>{editable ? 'Custom scenario' : 'Built-in scenario · duplicate to edit'}{scenario.carName && scenario.trackName ? ` · ${scenario.carName} · ${scenario.trackName}` : ''}</span></div>{editable && <button className="primary" type="button" onClick={() => onSave(scenario)}>Save</button>}</div><div className="brake-form"><label>Name<input disabled={!editable} value={scenario.name} onChange={(event) => onChange({ ...scenario, name: event.target.value })} /></label><label className="wide">Description<input disabled={!editable} value={scenario.description} onChange={(event) => onChange({ ...scenario, description: event.target.value })} /></label><Range label="Approach" value={scenario.approachMs} min={500} max={5000} step={50} unit="ms" disabled={!editable} onChange={(value) => number('approachMs', value)} /><Range label="Throttle fall" value={scenario.acceleratorFallTargetMs} min={100} max={2000} step={10} unit="ms" disabled={!editable} onChange={(value) => number('acceleratorFallTargetMs', value)} /><Range label="Brake rise" value={scenario.brakeRiseTargetMs} min={100} max={2200} step={10} unit="ms" disabled={!editable} onChange={(value) => number('brakeRiseTargetMs', value)} /><Range label="Target brake" value={scenario.targetBrakePercent} min={20} max={100} step={1} unit="%" disabled={!editable} onChange={(value) => number('targetBrakePercent', value)} /><Range label="Tolerance" value={scenario.brakeTolerancePercent} min={2} max={35} step={1} unit="%" disabled={!editable} onChange={(value) => number('brakeTolerancePercent', value)} /><Range label="Hold" value={scenario.brakeHoldMs} min={0} max={3500} step={25} unit="ms" disabled={!editable} onChange={(value) => number('brakeHoldMs', value)} /><Range label="Trail release" value={scenario.trailBrakeReleaseMs} min={350} max={4500} step={25} unit="ms" disabled={!editable} onChange={(value) => number('trailBrakeReleaseMs', value)} /><label className="brake-check wide"><input type="checkbox" checked={scenario.transitionEnabled} disabled={!editable} onChange={(event) => toggleTransition(event.target.checked)} /><span>Pause between trail braking and throttle</span></label>{scenario.transitionEnabled && <><label>Transition mode<select disabled={!editable} value={scenario.transitionMode ?? 'coast'} onChange={(event) => onChange({ ...scenario, transitionMode: event.target.value as 'coast' | 'low-brake', transitionBrakePercent: event.target.value === 'low-brake' ? (scenario.transitionBrakePercent ?? 5) : null })}><option value="coast">Coast</option><option value="low-brake">Hold low brake</option></select></label><Range label="Transition" value={scenario.transitionDurationMs ?? 600} min={150} max={2500} step={25} unit="ms" disabled={!editable} onChange={(value) => number('transitionDurationMs', value)} />{scenario.transitionMode === 'low-brake' && <Range label="Low brake target" value={scenario.transitionBrakePercent ?? 5} min={1} max={25} step={1} unit="%" disabled={!editable} onChange={(value) => number('transitionBrakePercent', value)} />}</>}<Range label="Throttle ramp" value={scenario.acceleratorRampMs} min={300} max={4500} step={25} unit="ms" disabled={!editable} onChange={(value) => number('acceleratorRampMs', value)} /></div><TraceChart scenario={scenario} samples={[]} nowMS={0} compact /></section></main>
}

function Range({ label, value, min, max, step, unit, disabled, onChange }: { label: string; value: number; min: number; max: number; step: number; unit: string; disabled: boolean; onChange: (value: number) => void }) {
  return <label className="brake-range"><span>{label}<strong>{value}{unit}</strong></span><input type="range" min={min} max={max} step={step} value={value} disabled={disabled} onChange={(event) => onChange(Number(event.target.value))} /></label>
}

function Devices({ devices, selectedDeviceID, settings, onSelect, onRequest, onRefresh, onConnect, onDisconnect, onSettings }: { devices: ControllerDevice[]; selectedDeviceID: string; settings: BrakeSettings; onSelect: (id: string) => void; onRequest: () => void; onRefresh: () => void; onConnect: (device: ControllerDevice) => void; onDisconnect: () => void; onSettings: (settings: BrakeSettings) => void }) {
  const supported = hasWebSerial()
  const [draft, setDraft] = useState(settings)
  useEffect(() => setDraft(settings), [settings])
  return <main className="brake-screen-grid"><aside className="brake-panel"><h2>Controller support</h2><p><strong>{supported ? 'Web Serial available' : 'Keyboard only'}</strong></p><p>{supported ? 'Chrome or Edge can connect to a permitted serial pedal controller.' : 'This browser does not expose Web Serial. The keyboard simulator remains available.'}</p><div className="brake-actions"><button type="button" onClick={onRefresh}>Refresh</button><button type="button" disabled={!supported} onClick={onRequest}>Add controller</button></div></aside><section className="brake-panel"><div className="brake-panel-head"><h1>Devices</h1><button className="primary" type="button" onClick={() => onSettings(draft)}>Save device settings</button></div><div className="brake-device-settings"><label>Baud rate<input type="number" min="1200" max="3000000" value={draft.baudRate} onChange={(event) => setDraft({ ...draft, baudRate: Number(event.target.value) })} /></label><label>USB vendor ID<input placeholder="0x2341" value={draft.usbVendorId ?? ''} onChange={(event) => setDraft({ ...draft, usbVendorId: event.target.value || null })} /></label><label>USB product ID<input placeholder="0x0043" value={draft.usbProductId ?? ''} onChange={(event) => setDraft({ ...draft, usbProductId: event.target.value || null })} /></label></div><div className="brake-list">{devices.map((device) => <div className={`brake-device${device.id === selectedDeviceID ? ' active' : ''}`} key={device.id}><button type="button" onClick={() => onSelect(device.id)}><strong>{device.label}</strong><span>{device.detail}</span></button>{device.kind === 'serial' && (device.status === 'connected' ? <button type="button" onClick={onDisconnect}>Disconnect</button> : <button type="button" onClick={() => onConnect(device)}>Connect</button>)}</div>)}</div></section></main>
}

function Results({ results, scenarios }: { results: RunResult[]; scenarios: Scenario[] }) {
  const byID = new Map(scenarios.map((scenario) => [scenario.id, scenario]))
  return <main className="brake-results"><div className="brake-panel-head"><div><h1>Results</h1><span>{results.length} completed run{results.length === 1 ? '' : 's'} stored in LapDog</span></div></div>{results.length === 0 ? <div className="brake-empty">No completed runs yet.</div> : results.map((result) => <article className="brake-panel brake-result" key={result.id}><div className="brake-panel-head"><div><strong>{result.scenarioName}</strong><span>{new Date(result.createdAt).toLocaleString()} · {result.deviceLabel}</span></div><b>{Math.round(result.metrics.score)}</b></div><MetricTiles metrics={result.metrics} />{byID.get(result.scenarioId) && <TraceChart scenario={byID.get(result.scenarioId)!} samples={result.samples} nowMS={result.samples.at(-1)?.timeMs ?? 0} compact />}</article>)}</main>
}

function cueFor(id: string): 'brake' | 'threshold' | 'trail' | 'transition' | 'accelerate' | null {
  if (id === 'brake-start') return 'brake'
  if (id === 'threshold') return 'threshold'
  if (id === 'trail') return 'trail'
  if (id === 'coast' || id === 'low-brake') return 'transition'
  if (id === 'accelerate') return 'accelerate'
  return null
}

function parseHex(value: string | null): number | undefined {
  if (!value?.trim()) return undefined
  const parsed = Number(value.toLowerCase().startsWith('0x') ? value : `0x${value}`)
  return Number.isFinite(parsed) ? parsed : undefined
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

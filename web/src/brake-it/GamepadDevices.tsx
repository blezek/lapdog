import { useEffect, useRef, useState, type FormEvent } from 'react'

import {
  currentGamepad,
  bindingForInput,
  detectPedalInput,
  gamepadState,
  hasGamepadAPI,
  pedalVerification,
  rawBindingValue,
  resolveSelectedGamepad,
  stableAxes,
  useGamepadSnapshot,
  type GamepadScan,
  type GamepadSnapshot,
  type RawGamepadState,
} from './gamepad'
import type { BrakeDevice, ControllerDevice, PedalBinding } from './types'

type CalibrationStage = 'released' | 'accelerator' | 'release-accelerator' | 'brake' | 'verify-accelerator' | 'verify-release' | 'verify-brake'

export function GamepadDevices({
  devices,
  scanIssue = null,
  configurations,
  selectedDeviceID,
  onSelect,
  onRefresh,
  onSave,
  onRename,
  onRemove,
}: {
  devices: ControllerDevice[]
  scanIssue?: GamepadScan['issue']
  configurations: BrakeDevice[]
  selectedDeviceID: string
  onSelect: (id: string) => void
  onRefresh: () => void
  onSave: (device: BrakeDevice, exists: boolean) => Promise<boolean>
  onRename: (device: BrakeDevice, label: string) => Promise<boolean>
  onRemove: (device: BrakeDevice) => void
}) {
  const [target, setTarget] = useState<ControllerDevice | null>(null)
  const [inspected, setInspected] = useState<ControllerDevice | null>(null)
  const [renamingID, setRenamingID] = useState<string | null>(null)
  const [deviceNotice, setDeviceNotice] = useState<string | null>(null)
  useEffect(() => {
    const disconnected = (event: GamepadEvent) => {
      const identity = event.gamepad.id.trim() || `Game controller ${event.gamepad.index + 1}`
      if (target?.gamepadIndex === event.gamepad.index && target.gamepadId === identity) {
        setTarget(null)
        setDeviceNotice('Pedal detection stopped because the controller disconnected. Inspect it again after reconnecting.')
      }
      if (inspected?.gamepadIndex === event.gamepad.index && inspected.gamepadId === identity) setInspected(null)
    }
    window.addEventListener('gamepaddisconnected', disconnected)
    return () => window.removeEventListener('gamepaddisconnected', disconnected)
  }, [inspected, target])
  const calibrationDevice = target ? resolveSelectedGamepad(devices, target.id, target.gamepadId ?? null) : null
  const existing = calibrationDevice ? configurations.find((item) => item.gamepadId === calibrationDevice.gamepadId) ?? null : null
  const diagnosticDevice = inspected
    ? resolveSelectedGamepad(devices, inspected.id, inspected.gamepadId ?? null)
    : devices.find((device) => device.id === selectedDeviceID) ?? devices[0]
  const diagnosticConfiguration = configurations.find((item) => item.gamepadId === diagnosticDevice?.gamepadId) ?? null
  return (
    <main className="brake-screen-grid">
      <aside className="brake-panel">
        <h2>Controller discovery</h2>
        <p><strong>{scanIssue === 'blocked' ? 'Device access blocked' : hasGamepadAPI() ? 'Gamepad API available' : 'Keyboard only'}</strong></p>
        <p>
          Keep this tab visible and press a pedal to let the browser expose its controller.
          Refresh rereads devices already visible to the browser.
        </p>
        <div className="brake-actions"><button type="button" onClick={onRefresh}>Refresh devices</button></div>
      </aside>
      <section className="brake-panel">
        <div className="brake-panel-head"><div><h1>Pedal devices</h1><span>Mappings stay in this LapDog database.</span></div></div>
        {deviceNotice && <p className="brake-capture-issue" role="status">{deviceNotice}</p>}
        {(scanIssue === 'unsupported' || !hasGamepadAPI()) && <p className="brake-list-empty" role="status">This browser does not expose the Gamepad API.</p>}
        {scanIssue === 'blocked' && <p className="brake-list-empty" role="status">This page cannot read gamepads. Check the browser's gamepad permission or the page's embedding policy.</p>}
        {scanIssue === 'failed' && <p className="brake-list-empty" role="status">Controller scan failed. Try refreshing this page.</p>}
        {hasGamepadAPI() && scanIssue === null && devices.length === 0 && <p className="brake-list-empty" role="status">No controller exposed yet. Press a pedal with this tab visible. If it remains missing, check the device in Windows Game Controllers (joy.cpl).</p>}
        <div className="brake-list">
          {devices.map((device) => {
            const configured = configurations.find((item) => item.gamepadId === device.gamepadId)
            const duplicated = devices.filter((item) => item.gamepadId === device.gamepadId).length > 1
            return (
              <div className={`brake-device${device.id === selectedDeviceID ? ' active' : ''}${device.id === diagnosticDevice?.id ? ' inspected' : ''}`} key={device.id}>
                <button type="button" aria-label={`Inspect axes for ${configured?.label ?? device.label}, browser index ${device.gamepadIndex}`} onClick={() => { setInspected(device); setDeviceNotice(null) }}>
                  <strong>{configured?.label ?? device.label}</strong>
                  <span>{configured && configured.label !== device.label ? `${device.label} · ` : ''}{device.detail} · {configured ? 'Pedals configured' : 'Needs pedal detection'}</span>
                  {device.id === diagnosticDevice?.id && <span>Inspecting axes</span>}
                  {duplicated && <span>Several connected devices share this name and one saved calibration. Choose the intended browser index for this session.</span>}
                </button>
                {configured && <button type="button" onClick={() => onSelect(device.id)}>Use in simulator</button>}
                <button type="button" onClick={() => { setTarget(device); setDeviceNotice(null) }}>{configured ? 'Reconfigure' : 'Detect pedals'}</button>
                {configured && <button type="button" onClick={() => setRenamingID(configured.id)}>Rename</button>}
                {configured && <button className="danger-text" type="button" onClick={() => onRemove(configured)}>Remove</button>}
                {configured && renamingID === configured.id && <RenameDevice device={configured} onRename={onRename} onClose={() => setRenamingID(null)} />}
              </div>
            )
          })}
          {configurations.filter((configured) => !devices.some((device) => device.gamepadId === configured.gamepadId)).map((configured) => (
            <div className="brake-device" key={configured.id}>
              <div className="brake-device-summary"><strong>{configured.label}</strong><span>Not connected · {configured.gamepadId}</span></div>
              <button type="button" onClick={() => setRenamingID(configured.id)}>Rename</button>
              <button className="danger-text" type="button" onClick={() => onRemove(configured)}>Remove</button>
              {renamingID === configured.id && <RenameDevice device={configured} onRename={onRename} onClose={() => setRenamingID(null)} />}
            </div>
          ))}
        </div>
        {!target && diagnosticDevice && (
          <GamepadMonitor key={diagnosticDevice.id} device={diagnosticDevice} configuration={diagnosticConfiguration} />
        )}
        {!target && inspected && !diagnosticDevice && <p className="brake-list-empty" role="status">The inspected controller disconnected. Choose another device to inspect.</p>}
        {target && !calibrationDevice && <div className="brake-calibration" role="status"><p>The controller being configured is no longer visible. Reconnect it and press a pedal, or cancel detection.</p><button type="button" onClick={() => setTarget(null)}>Cancel detection</button></div>}
        {calibrationDevice && calibrationDevice.gamepadIndex !== undefined && (
          <PedalCalibration
            key={calibrationDevice.id}
            device={calibrationDevice}
            existing={existing}
            onCancel={() => setTarget(null)}
            onSave={async (configured) => {
              const saved = await onSave(configured, existing !== null)
              if (saved) setTarget(null)
              return saved
            }}
          />
        )}
      </section>
    </main>
  )
}

function RenameDevice({ device, onRename, onClose }: { device: BrakeDevice; onRename: (device: BrakeDevice, label: string) => Promise<boolean>; onClose: () => void }) {
  const [name, setName] = useState(device.label)
  const [saving, setSaving] = useState(false)
  const save = async (event: FormEvent) => {
    event.preventDefault()
    const label = name.trim()
    if (!label || label.length > 200 || saving) return
    setSaving(true)
    try {
      if (await onRename(device, label)) onClose()
    } finally {
      setSaving(false)
    }
  }
  return <form className="brake-device-rename" onSubmit={(event) => { void save(event) }}>
    <label htmlFor={`device-name-${device.id}`}>Device name
      <input id={`device-name-${device.id}`} value={name} maxLength={200} required autoFocus disabled={saving} onChange={(event) => setName(event.target.value)} />
    </label>
    <button className="primary" type="submit" disabled={saving || !name.trim()}>{saving ? 'Saving…' : 'Save name'}</button>
    <button type="button" disabled={saving} onClick={onClose}>Cancel</button>
  </form>
}

function GamepadMonitor({ device, configuration }: { device: ControllerDevice; configuration: BrakeDevice | null }) {
  const snapshot = useGamepadSnapshot(device.gamepadIndex, device.gamepadId)
  const readings = configuration ? pedalVerification(snapshot, configuration.accelerator, configuration.brake) : null
  return (
    <div className="brake-calibration" role="region" aria-label="Live controller inputs">
      <div className="brake-panel-head"><div><h2>Live inputs</h2><span>{configuration?.label ?? device.label}</span></div></div>
      <p>These are the browser's raw, zero-based axis numbers. Press each pedal to see which value changes.</p>
      {snapshot && <p>Mapping: {snapshot.mapping || 'unmapped'} · Browser timestamp: {snapshot.timestamp.toFixed(1)}</p>}
      {snapshot?.axes.length === 0 && <p role="status">This controller reports no axes, so it cannot be calibrated under the axis-only pedal setting.</p>}
      {configuration && snapshot && readings && <PedalVerification accelerator={readings.accelerator} brake={readings.brake} acceleratorBinding={configuration.accelerator} brakeBinding={configuration.brake} />}
      <GamepadRawInputs snapshot={snapshot} />
    </div>
  )
}

function GamepadRawInputs({ snapshot, baseline, selected, onChoose }: {
  snapshot: GamepadSnapshot | null
  baseline?: RawGamepadState | null
  selected?: number | null
  onChoose?: (inputIndex: number) => void
}) {
  if (!snapshot) return <p className="brake-list-empty">Controller unavailable. Reconnect it and press a pedal.</p>
  return <div className="brake-raw-groups">
    <div>
      <h3>Axes</h3>
      {snapshot.axes.length === 0 && <p className="brake-list-empty">No axes reported; this controller cannot be calibrated here.</p>}
      <div className="brake-raw-list">{snapshot.axes.map((value, index) => {
        const rest = baseline?.axes[index]
        const changed = rest !== undefined && Math.abs(value - rest) > 0.15
        const picked = selected === index
        return <div className={`brake-raw-input${picked ? ' picked' : ''}`} key={index}>
          <span>Axis {index}</span>
          <progress max="100" value={(value + 1) * 50} />
          <code>{value.toFixed(3)}</code>
          {onChoose && <button type="button" disabled={!changed} onClick={() => onChoose(index)}>{picked ? 'Selected' : 'Select'}</button>}
        </div>
      })}</div>
    </div>
  </div>
}

function PedalCalibration({ device, existing, onCancel, onSave }: {
  device: ControllerDevice
  existing: BrakeDevice | null
  onCancel: () => void
  onSave: (device: BrakeDevice) => Promise<boolean>
}) {
  const [stage, setStage] = useState<CalibrationStage>('released')
  const [baseline, setBaseline] = useState<RawGamepadState | null>(null)
  const [accelerator, setAccelerator] = useState<PedalBinding | null>(null)
  const [brake, setBrake] = useState<PedalBinding | null>(null)
  const [manualInput, setManualInput] = useState<number | null>(null)
  const [sampling, setSampling] = useState(false)
  const [captureIssue, setCaptureIssue] = useState<string | null>(null)
  const mounted = useRef(true)
  useEffect(() => {
    mounted.current = true
    return () => { mounted.current = false }
  }, [])
  const snapshot = useGamepadSnapshot(device.gamepadIndex, device.gamepadId)
  const candidate = baseline && snapshot && (stage === 'accelerator' || stage === 'brake')
    ? manualInput !== null
      ? bindingForInput(baseline, snapshot, manualInput)
      : detectPedalInput(baseline, snapshot)
    : null
  const acceleratorRaw = accelerator && snapshot ? rawBindingValue(snapshot, accelerator) : null
  const acceleratorReleased = acceleratorRaw !== null && !!accelerator && Math.abs(acceleratorRaw - accelerator.restValue) < 0.08
  const verification = pedalVerification(snapshot, accelerator, brake)
  const acceleratorValue = verification.accelerator
  const brakeValue = verification.brake

  const captureStableAxes = async (): Promise<number[] | null> => {
    const samples: number[][] = []
    for (let attempt = 0; attempt < 6; attempt += 1) {
      const gamepad = device.gamepadIndex === undefined ? null : currentGamepad(device.gamepadIndex, device.gamepadId)
      if (!gamepad) return null
      samples.push(gamepadState(gamepad).axes)
      if (attempt < 5) await new Promise<void>((resolve) => window.setTimeout(resolve, 50))
    }
    return stableAxes(samples)
  }
  const captureReleased = async () => {
    setSampling(true)
    setCaptureIssue(null)
    const axes = await captureStableAxes()
    if (!mounted.current) return
    setSampling(false)
    if (!axes) {
      setCaptureIssue('Keep the pedals and other controller axes still, then capture their released positions again.')
      return
    }
    setBaseline({ axes, buttons: [] })
    setManualInput(null)
    setStage('accelerator')
  }
  const capturePressed = async (): Promise<PedalBinding | null> => {
    if (!baseline || !candidate) return null
    const inputIndex = candidate.inputIndex
    setSampling(true)
    setCaptureIssue(null)
    const axes = await captureStableAxes()
    if (!mounted.current) return null
    setSampling(false)
    const binding = axes ? bindingForInput(baseline, { axes, buttons: [] }, inputIndex) : null
    if (!binding) setCaptureIssue('Hold the pedal steady near its full press while capturing, then try again.')
    return binding
  }
  const acceptAccelerator = async () => {
    const binding = await capturePressed()
    if (!binding || !mounted.current) return
    setAccelerator(binding)
    setManualInput(null)
    setStage('release-accelerator')
  }
  const detectBrake = () => {
    if (!acceleratorReleased) return
    setManualInput(null)
    setStage('brake')
  }
  const acceptBrake = async () => {
    if (!snapshot || !accelerator) return
    const binding = await capturePressed()
    if (!binding || !mounted.current) return
    setBrake(binding)
    setManualInput(null)
    setStage('verify-accelerator')
  }
  const saveVerified = async () => {
    if (!verification.brakeReady || !accelerator || !brake) return
    setSampling(true)
    setCaptureIssue(null)
    const saved = await onSave({
      id: existing?.id ?? crypto.randomUUID(),
      gamepadId: device.gamepadId ?? device.label,
      label: existing?.label ?? device.label,
      accelerator,
      brake,
      createdAt: existing?.createdAt ?? '',
      updatedAt: '',
    })
    if (!mounted.current) return
    setSampling(false)
    if (!saved) setCaptureIssue('The mapping was not saved. Check the error above and try again.')
  }
  const restart = () => {
    setStage('released')
    setBaseline(null)
    setAccelerator(null)
    setBrake(null)
    setManualInput(null)
    setCaptureIssue(null)
  }

  return (
    <div className="brake-calibration" role="region" aria-label="Pedal detection">
      <div className="brake-panel-head"><div><h2>Configure {existing?.label ?? device.label}</h2><span>{device.detail}</span></div><button type="button" disabled={sampling} onClick={onCancel}>Cancel</button></div>
      {stage === 'released' && <><p>Release every pedal and keep the controller still while Brake-It samples its resting axes.</p><button className="primary" type="button" disabled={!snapshot?.axes.length || sampling} onClick={() => void captureReleased()}>{sampling ? 'Capturing…' : 'Capture released pedals'}</button></>}
      {stage === 'accelerator' && <><p>Press and hold the accelerator fully. Brake-It will choose whichever axis moves the most, then sample the held position.</p><DetectedInput binding={candidate} /><button className="primary" type="button" disabled={!candidate || sampling} onClick={() => void acceptAccelerator()}>{sampling ? 'Capturing…' : 'Use this accelerator'}</button></>}
      {stage === 'release-accelerator' && <><p>Release the accelerator completely before detecting the brake.</p><p><strong>{acceleratorReleased ? 'Accelerator released' : 'Waiting for accelerator release…'}</strong></p><button className="primary" type="button" disabled={!acceleratorReleased} onClick={detectBrake}>Detect brake</button></>}
      {stage === 'brake' && <><p>Press and hold the brake fully and keep the accelerator released while Brake-It samples the held position.</p><DetectedInput binding={candidate} /><button className="primary" type="button" disabled={!candidate || sampling} onClick={() => void acceptBrake()}>{sampling ? 'Capturing…' : 'Use this brake'}</button></>}
      {stage === 'verify-accelerator' && <><p>Verification: release the brake, then press and hold the accelerator fully. Check that the accelerator rises and the brake stays near zero.</p><PedalVerification accelerator={acceleratorValue} brake={brakeValue} acceleratorBinding={accelerator} brakeBinding={brake} /><button className="primary" type="button" disabled={!verification.acceleratorReady} onClick={() => setStage('verify-release')}>Accelerator verified</button></>}
      {stage === 'verify-release' && <><p>Release both pedals before verifying the brake.</p><PedalVerification accelerator={acceleratorValue} brake={brakeValue} acceleratorBinding={accelerator} brakeBinding={brake} /><button className="primary" type="button" disabled={!snapshot || acceleratorValue > 10 || brakeValue > 10} onClick={() => setStage('verify-brake')}>Verify brake</button></>}
      {stage === 'verify-brake' && <><p>Verification: press and hold the brake fully. Check that the brake rises and the accelerator stays near zero.</p><PedalVerification accelerator={acceleratorValue} brake={brakeValue} acceleratorBinding={accelerator} brakeBinding={brake} /><button className="primary" type="button" disabled={!verification.brakeReady || sampling} onClick={() => void saveVerified()}>{sampling ? 'Saving…' : 'Save verified mapping'}</button></>}
      {captureIssue && <p className="brake-capture-issue" role="alert">{captureIssue}</p>}
      {(stage === 'accelerator' || stage === 'brake') && <p>If automatic detection chooses the wrong input, select the changing axis below while holding the pedal. Press it fully again before accepting.</p>}
      {stage.startsWith('verify') && <button type="button" disabled={sampling} onClick={restart}>Start detection over</button>}
      <GamepadRawInputs snapshot={snapshot} baseline={baseline} selected={manualInput} onChoose={stage === 'accelerator' || stage === 'brake' ? setManualInput : undefined} />
    </div>
  )
}

function PedalVerification({ accelerator, brake, acceleratorBinding, brakeBinding }: {
  accelerator: number
  brake: number
  acceleratorBinding: PedalBinding | null
  brakeBinding: PedalBinding | null
}) {
  return <div className="brake-pedal-readings">
    <label>Accelerator{acceleratorBinding?.inputKind === 'axis' ? ` (axis ${acceleratorBinding.inputIndex})` : ''} <strong>{accelerator.toFixed(0)}%</strong><progress max="100" value={accelerator} /></label>
    <label>Brake{brakeBinding?.inputKind === 'axis' ? ` (axis ${brakeBinding.inputIndex})` : ''} <strong>{brake.toFixed(0)}%</strong><progress max="100" value={brake} /></label>
  </div>
}

function DetectedInput({ binding }: { binding: PedalBinding | null }) {
  if (!binding) return <p className="brake-detection-waiting">Waiting for pedal movement…</p>
  return <p className="brake-detection-found"><strong>Detected {binding.inputKind} {binding.inputIndex}</strong><span>{binding.restValue.toFixed(3)} released → {binding.pressedValue.toFixed(3)} pressed</span></p>
}

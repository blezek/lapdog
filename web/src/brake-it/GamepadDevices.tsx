import { useEffect, useRef, useState } from 'react'

import {
  currentGamepad,
  detectPedalInput,
  gamepadState,
  hasGamepadAPI,
  rawBindingValue,
  type RawGamepadState,
} from './gamepad'
import type { BrakeDevice, ControllerDevice, PedalBinding } from './types'

type CalibrationStage = 'released' | 'accelerator' | 'release-accelerator' | 'brake'

export function GamepadDevices({
  devices,
  configurations,
  selectedDeviceID,
  onSelect,
  onRefresh,
  onSave,
  onRemove,
}: {
  devices: ControllerDevice[]
  configurations: BrakeDevice[]
  selectedDeviceID: string
  onSelect: (id: string) => void
  onRefresh: () => void
  onSave: (device: BrakeDevice, exists: boolean) => void
  onRemove: (device: BrakeDevice) => void
}) {
  const [target, setTarget] = useState<ControllerDevice | null>(null)
  const existing = target ? configurations.find((item) => item.gamepadId === target.gamepadId) ?? null : null
  return (
    <main className="brake-screen-grid">
      <aside className="brake-panel">
        <h2>HID pedal support</h2>
        <p><strong>{hasGamepadAPI() ? 'Gamepad API available' : 'Keyboard only'}</strong></p>
        <p>
          Press a pedal once so the browser exposes the controller, then refresh.
          Brake-It reads the same axes and analog buttons shown by browser gamepad testers.
        </p>
        <div className="brake-actions"><button type="button" onClick={onRefresh}>Refresh devices</button></div>
      </aside>
      <section className="brake-panel">
        <div className="brake-panel-head"><div><h1>Pedal devices</h1><span>Mappings stay in this LapDog database.</span></div></div>
        {!hasGamepadAPI() && <p className="brake-list-empty">This browser does not expose the Gamepad API.</p>}
        {hasGamepadAPI() && devices.length === 0 && <p className="brake-list-empty">No controller is visible yet. Press a pedal or button, then refresh.</p>}
        <div className="brake-list">
          {devices.map((device) => {
            const configured = configurations.find((item) => item.gamepadId === device.gamepadId)
            return (
              <div className={`brake-device${device.id === selectedDeviceID ? ' active' : ''}`} key={device.id}>
                <button type="button" disabled={!configured} onClick={() => onSelect(device.id)}>
                  <strong>{device.label}</strong>
                  <span>{device.detail} · {configured ? 'Pedals configured' : 'Needs pedal detection'}</span>
                </button>
                <button type="button" onClick={() => setTarget(device)}>{configured ? 'Reconfigure' : 'Detect pedals'}</button>
                {configured && <button className="danger-text" type="button" onClick={() => onRemove(configured)}>Remove</button>}
              </div>
            )
          })}
        </div>
        {target && target.gamepadIndex !== undefined && (
          <PedalCalibration
            device={target}
            existing={existing}
            onCancel={() => setTarget(null)}
            onSave={(configured) => {
              onSave(configured, existing !== null)
              setTarget(null)
            }}
          />
        )}
      </section>
    </main>
  )
}

function PedalCalibration({ device, existing, onCancel, onSave }: {
  device: ControllerDevice
  existing: BrakeDevice | null
  onCancel: () => void
  onSave: (device: BrakeDevice) => void
}) {
  const [stage, setStage] = useState<CalibrationStage>('released')
  const [baseline, setBaseline] = useState<RawGamepadState | null>(null)
  const [candidate, setCandidate] = useState<PedalBinding | null>(null)
  const [accelerator, setAccelerator] = useState<PedalBinding | null>(null)
  const [acceleratorReleased, setAcceleratorReleased] = useState(false)
  const candidateRef = useRef<PedalBinding | null>(null)

  useEffect(() => {
    if (!baseline || device.gamepadIndex === undefined || (stage !== 'accelerator' && stage !== 'brake' && stage !== 'release-accelerator')) return
    let active = true
    let frame = 0
    const tick = () => {
      const gamepad = currentGamepad(device.gamepadIndex!, device.gamepadId)
      if (gamepad) {
        const current = gamepadState(gamepad)
        if (stage === 'release-accelerator' && accelerator) {
          const raw = rawBindingValue(current, accelerator)
          setAcceleratorReleased(raw !== null && Math.abs(raw - accelerator.restValue) < 0.08)
        } else {
          const detected = detectPedalInput(baseline, current)
          if (detected) {
            const previous = candidateRef.current
            const delta = Math.abs(detected.pressedValue - detected.restValue)
            const previousDelta = previous ? Math.abs(previous.pressedValue - previous.restValue) : 0
            if (!previous || delta > previousDelta) {
              candidateRef.current = detected
              setCandidate(detected)
            }
          }
        }
      }
      if (active) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => { active = false; cancelAnimationFrame(frame) }
  }, [accelerator, baseline, device.gamepadId, device.gamepadIndex, stage])

  const captureReleased = () => {
    const gamepad = device.gamepadIndex === undefined ? null : currentGamepad(device.gamepadIndex, device.gamepadId)
    if (!gamepad) return
    setBaseline(gamepadState(gamepad))
    candidateRef.current = null
    setCandidate(null)
    setStage('accelerator')
  }
  const acceptAccelerator = () => {
    if (!candidate) return
    setAccelerator(candidate)
    candidateRef.current = null
    setCandidate(null)
    setStage('release-accelerator')
  }
  const detectBrake = () => {
    if (!acceleratorReleased) return
    candidateRef.current = null
    setCandidate(null)
    setStage('brake')
  }
  const acceptBrake = () => {
    if (!accelerator || !candidate) return
    onSave({
      id: existing?.id ?? crypto.randomUUID(),
      gamepadId: device.gamepadId ?? device.label,
      label: device.label,
      accelerator,
      brake: candidate,
      createdAt: existing?.createdAt ?? '',
      updatedAt: '',
    })
  }

  return (
    <div className="brake-calibration" role="region" aria-label="Pedal detection">
      <div className="brake-panel-head"><div><h2>Configure {device.label}</h2><span>{device.detail}</span></div><button type="button" onClick={onCancel}>Cancel</button></div>
      {stage === 'released' && <><p>Release every pedal, then capture their resting positions.</p><button className="primary" type="button" onClick={captureReleased}>Capture released pedals</button></>}
      {stage === 'accelerator' && <><p>Press and hold the accelerator fully. Brake-It will choose whichever axis or analog button moves the most.</p><DetectedInput binding={candidate} /><button className="primary" type="button" disabled={!candidate} onClick={acceptAccelerator}>Use this accelerator</button></>}
      {stage === 'release-accelerator' && <><p>Release the accelerator completely before detecting the brake.</p><p><strong>{acceleratorReleased ? 'Accelerator released' : 'Waiting for accelerator release…'}</strong></p><button className="primary" type="button" disabled={!acceleratorReleased} onClick={detectBrake}>Detect brake</button></>}
      {stage === 'brake' && <><p>Press and hold the brake fully. The accelerator may remain released.</p><DetectedInput binding={candidate} /><button className="primary" type="button" disabled={!candidate} onClick={acceptBrake}>Save pedal mapping</button></>}
    </div>
  )
}

function DetectedInput({ binding }: { binding: PedalBinding | null }) {
  if (!binding) return <p className="brake-detection-waiting">Waiting for pedal movement…</p>
  return <p className="brake-detection-found"><strong>Detected {binding.inputKind} {binding.inputIndex + 1}</strong><span>{binding.restValue.toFixed(3)} released → {binding.pressedValue.toFixed(3)} pressed</span></p>
}

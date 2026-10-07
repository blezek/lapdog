import { useCallback, useEffect, useRef, useState } from 'react'

import { clamp } from './scenario'
import type { BrakeDevice, ControllerDevice, PedalBinding, PedalInput } from './types'

export type RawGamepadState = { axes: number[]; buttons: number[] }
export type GamepadSnapshot = RawGamepadState & { mapping: string; timestamp: number }
export type GamepadScan = { devices: ControllerDevice[]; issue: 'unsupported' | 'blocked' | 'failed' | null }

export function hasGamepadAPI(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.getGamepads === 'function'
}

export function gamepadState(gamepad: Gamepad): RawGamepadState {
  return {
    axes: [...gamepad.axes],
    // Saved mappings may still refer to buttons, although new calibration uses axes only.
    buttons: gamepad.buttons.map((button) => button.value),
  }
}

export function rawBindingValue(state: RawGamepadState, binding: PedalBinding): number | null {
  const values = binding.inputKind === 'axis' ? state.axes : state.buttons
  const value = values[binding.inputIndex]
  return value === undefined || !Number.isFinite(value) ? null : value
}

export function pedalPercent(raw: number | null, binding: PedalBinding): number {
  if (raw === null) return 0
  const span = binding.pressedValue - binding.restValue
  if (Math.abs(span) < 0.1) return 0
  return clamp(((raw - binding.restValue) / span) * 100, 0, 100)
}

export function pedalVerification(
  state: RawGamepadState | null,
  acceleratorBinding: PedalBinding | null,
  brakeBinding: PedalBinding | null,
): { accelerator: number; brake: number; acceleratorReady: boolean; brakeReady: boolean } {
  const accelerator = state && acceleratorBinding
    ? pedalPercent(rawBindingValue(state, acceleratorBinding), acceleratorBinding) : 0
  const brake = state && brakeBinding
    ? pedalPercent(rawBindingValue(state, brakeBinding), brakeBinding) : 0
  return {
    accelerator,
    brake,
    acceleratorReady: !!state && !!acceleratorBinding && !!brakeBinding && accelerator >= 80 && brake <= 20,
    brakeReady: !!state && !!acceleratorBinding && !!brakeBinding && brake >= 80 && accelerator <= 20,
  }
}

export function detectPedalInput(
  baseline: RawGamepadState,
  current: RawGamepadState,
  minimumChange = 0.15,
): PedalBinding | null {
  let found: PedalBinding | null = null
  let largest = minimumChange
  for (let inputIndex = 0; inputIndex < Math.min(baseline.axes.length, current.axes.length); inputIndex += 1) {
    const binding = bindingForInput(baseline, current, inputIndex, minimumChange)
    if (!binding) continue
    const delta = Math.abs(binding.pressedValue - binding.restValue)
    if (delta <= largest) continue
    largest = delta
    found = binding
  }
  return found
}

export function bindingForInput(
  baseline: RawGamepadState,
  current: RawGamepadState,
  inputIndex: number,
  minimumChange = 0.15,
): PedalBinding | null {
  const before = baseline.axes[inputIndex]
  const now = current.axes[inputIndex]
  if (before === undefined || now === undefined || !Number.isFinite(before) || !Number.isFinite(now)) return null
  if (Math.abs(now - before) <= minimumChange) return null
  return { inputKind: 'axis', inputIndex, restValue: before, pressedValue: now }
}

// A capture is usable only when every reported axis stays near one position.
// Averaging several readings avoids saving a single noisy or moving sample.
export function stableAxes(samples: number[][], maxSpread = 0.08): number[] | null {
  if (samples.length < 5 || samples[0]?.length === 0) return null
  const count = samples[0]!.length
  if (samples.some((sample) => sample.length !== count || sample.some((value) => !Number.isFinite(value)))) return null
  const averaged: number[] = []
  for (let index = 0; index < count; index += 1) {
    const values = samples.map((sample) => sample[index]!)
    if (Math.max(...values) - Math.min(...values) > maxSpread) return null
    averaged.push(values.reduce((sum, value) => sum + value, 0) / values.length)
  }
  return averaged
}

export function currentGamepad(index: number, expectedID?: string): Gamepad | null {
  if (!hasGamepadAPI()) return null
  try {
    const gamepad = navigator.getGamepads()[index]
    if (!gamepad || gamepad.connected === false) return null
    const identity = gamepad.id.trim() || `Game controller ${gamepad.index + 1}`
    if (expectedID && identity !== expectedID) return null
    return gamepad
  } catch {
    return null
  }
}

export function useGamepadSnapshot(index?: number, expectedID?: string): GamepadSnapshot | null {
  const [snapshot, setSnapshot] = useState<GamepadSnapshot | null>(null)
  const signature = useRef('')
  useEffect(() => {
    let active = true
    let frame = 0
    const tick = () => {
      const gamepad = index === undefined ? null : currentGamepad(index, expectedID)
      const next = gamepad ? { ...gamepadState(gamepad), mapping: gamepad.mapping, timestamp: gamepad.timestamp } : null
      const nextSignature = JSON.stringify(next)
      if (signature.current !== nextSignature) {
        signature.current = nextSignature
        setSnapshot(next)
      }
      if (active) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => { active = false; cancelAnimationFrame(frame) }
  }, [index, expectedID])
  return snapshot
}

export function scanGamepads(): GamepadScan {
  if (!hasGamepadAPI()) return { devices: [], issue: 'unsupported' }
  try {
    const devices = [...navigator.getGamepads()].flatMap((gamepad) => {
      if (!gamepad || gamepad.connected === false) return []
      const identity = gamepad.id.trim() || `Game controller ${gamepad.index + 1}`
      return [{
        id: `gamepad-${gamepad.index}`,
        label: identity.slice(0, 200),
        kind: 'gamepad' as const,
        status: 'available' as const,
        detail: `${gamepad.axes.length} axes · browser index ${gamepad.index}`,
        gamepadIndex: gamepad.index,
        gamepadId: identity,
      }]
    })
    return { devices, issue: null }
  } catch (error) {
    return { devices: [], issue: error instanceof DOMException && error.name === 'SecurityError' ? 'blocked' : 'failed' }
  }
}

export function resolveSelectedGamepad(devices: ControllerDevice[], selectedID: string, identity: string | null): ControllerDevice | null {
  if (!identity) return null
  const selected = devices.find((device) => device.id === selectedID && device.gamepadId === identity)
  if (selected) return selected
  const matches = devices.filter((device) => device.gamepadId === identity)
  return matches.length === 1 ? matches[0] ?? null : null
}

export function useGamepadDevices(): { devices: ControllerDevice[]; issue: GamepadScan['issue']; refresh: () => void } {
  const [scan, setScan] = useState<GamepadScan>({ devices: [], issue: null })
  const signature = useRef('')
  const refresh = useCallback(() => {
    const next = scanGamepads()
    const nextSignature = JSON.stringify(next)
    if (nextSignature === signature.current) return
    signature.current = nextSignature
    setScan(next)
  }, [])

  useEffect(() => {
    refresh()
    const changed = () => refresh()
    const visible = () => { if (document.visibilityState === 'visible') refresh() }
    window.addEventListener('gamepadconnected', changed)
    window.addEventListener('gamepaddisconnected', changed)
    window.addEventListener('focus', changed)
    document.addEventListener('visibilitychange', visible)
    const timer = window.setInterval(visible, 1000)
    return () => {
      window.removeEventListener('gamepadconnected', changed)
      window.removeEventListener('gamepaddisconnected', changed)
      window.removeEventListener('focus', changed)
      document.removeEventListener('visibilitychange', visible)
      window.clearInterval(timer)
    }
  }, [refresh])
  return { ...scan, refresh }
}

export function useGamepadInput(
  device: ControllerDevice | null,
  calibration: BrakeDevice | null,
): PedalInput {
  const [input, setInput] = useState<PedalInput>({ accelerator: 0, brake: 0, source: 'gamepad' })
  const frame = useRef(0)
  useEffect(() => {
    if (device?.kind !== 'gamepad' || device.gamepadIndex === undefined || !calibration) {
      setInput({ accelerator: 0, brake: 0, source: 'gamepad' })
      return
    }
    let active = true
    const tick = () => {
      const gamepad = currentGamepad(device.gamepadIndex!, device.gamepadId)
      if (gamepad) {
        const state = gamepadState(gamepad)
        setInput({
          accelerator: pedalPercent(rawBindingValue(state, calibration.accelerator), calibration.accelerator),
          brake: pedalPercent(rawBindingValue(state, calibration.brake), calibration.brake),
          source: 'gamepad',
        })
      } else {
        // Pedal positions describe the present moment. Never retain the last
        // pressure after a controller disconnects or moves to another index.
        setInput({ accelerator: 0, brake: 0, source: 'gamepad' })
      }
      if (active) frame.current = requestAnimationFrame(tick)
    }
    frame.current = requestAnimationFrame(tick)
    return () => {
      active = false
      cancelAnimationFrame(frame.current)
    }
  }, [calibration, device])
  return input
}

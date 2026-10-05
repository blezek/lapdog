import { useCallback, useEffect, useRef, useState } from 'react'

import { clamp } from './scenario'
import type { BrakeDevice, ControllerDevice, PedalBinding, PedalInput } from './types'

export type RawGamepadState = { axes: number[]; buttons: number[] }

export function hasGamepadAPI(): boolean {
  return typeof navigator !== 'undefined' && typeof navigator.getGamepads === 'function'
}

export function gamepadState(gamepad: Gamepad): RawGamepadState {
  return {
    axes: [...gamepad.axes],
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

export function detectPedalInput(
  baseline: RawGamepadState,
  current: RawGamepadState,
  minimumChange = 0.15,
): PedalBinding | null {
  let found: PedalBinding | null = null
  let largest = minimumChange
  const inspect = (inputKind: 'axis' | 'button', before: number[], now: number[]) => {
    for (let inputIndex = 0; inputIndex < Math.min(before.length, now.length); inputIndex += 1) {
      const delta = Math.abs(now[inputIndex]! - before[inputIndex]!)
      if (delta <= largest) continue
      largest = delta
      found = {
        inputKind,
        inputIndex,
        restValue: before[inputIndex]!,
        pressedValue: now[inputIndex]!,
      }
    }
  }
  inspect('axis', baseline.axes, current.axes)
  inspect('button', baseline.buttons, current.buttons)
  return found
}

export function currentGamepad(index: number, expectedID?: string): Gamepad | null {
  if (!hasGamepadAPI()) return null
  try {
    const gamepad = navigator.getGamepads()[index]
    if (!gamepad || (expectedID && gamepad.id && gamepad.id !== expectedID)) return null
    return gamepad
  } catch {
    return null
  }
}

function enumerateGamepads(): ControllerDevice[] {
  if (!hasGamepadAPI()) return []
  try {
    return [...navigator.getGamepads()].flatMap((gamepad) => {
      if (!gamepad) return []
      const identity = gamepad.id.trim() || `Game controller ${gamepad.index + 1}`
      return [{
        id: `gamepad-${gamepad.index}`,
        label: identity.slice(0, 200),
        kind: 'gamepad' as const,
        status: 'available' as const,
        detail: `${gamepad.axes.length} axes · ${gamepad.buttons.length} buttons · browser index ${gamepad.index}`,
        gamepadIndex: gamepad.index,
        gamepadId: identity,
      }]
    })
  } catch {
    return []
  }
}

export function useGamepadDevices(): { devices: ControllerDevice[]; refresh: () => void } {
  const [devices, setDevices] = useState<ControllerDevice[]>([])
  const signature = useRef('')
  const refresh = useCallback(() => {
    const next = enumerateGamepads()
    const nextSignature = JSON.stringify(next)
    if (nextSignature === signature.current) return
    signature.current = nextSignature
    setDevices(next)
  }, [])

  useEffect(() => {
    refresh()
    const changed = () => refresh()
    window.addEventListener('gamepadconnected', changed)
    window.addEventListener('gamepaddisconnected', changed)
    const timer = window.setInterval(refresh, 1000)
    return () => {
      window.removeEventListener('gamepadconnected', changed)
      window.removeEventListener('gamepaddisconnected', changed)
      window.clearInterval(timer)
    }
  }, [refresh])
  return { devices, refresh }
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

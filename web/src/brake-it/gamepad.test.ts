import { afterEach, describe, expect, it, vi } from 'vitest'

import { bindingForInput, currentGamepad, detectPedalInput, pedalPercent, pedalVerification, rawBindingValue, resolveSelectedGamepad, scanGamepads, stableAxes } from './gamepad'
import type { ControllerDevice } from './types'

afterEach(() => vi.unstubAllGlobals())

describe('Brake-It gamepad pedal mapping', () => {
  it('detects the input with the largest movement without assuming an axis number', () => {
    expect(detectPedalInput(
      { axes: [0, 1, 0], buttons: [0, 0] },
      { axes: [0.03, -0.8, 0.2], buttons: [0, 0.4] },
    )).toEqual({ inputKind: 'axis', inputIndex: 1, restValue: 1, pressedValue: -0.8 })
  })

  it('ignores button movement when detecting a pedal', () => {
    expect(detectPedalInput(
      { axes: [0, 1], buttons: [0, 0] },
      { axes: [0.04, 0.6], buttons: [0, 1] },
    )).toEqual({ inputKind: 'axis', inputIndex: 1, restValue: 1, pressedValue: 0.6 })
    expect(detectPedalInput(
      { axes: [0], buttons: [0, 0] },
      { axes: [0.04], buttons: [0, 1] },
    )).toBeNull()
  })

  it('lets the driver choose a moving input even when another input moves more', () => {
    const released = { axes: [0, 1], buttons: [0] }
    const pressed = { axes: [0.9, 0.4], buttons: [0.3] }
    expect([
      bindingForInput(released, pressed, 1),
      bindingForInput(released, pressed, 3),
    ]).toEqual([
      { inputKind: 'axis', inputIndex: 1, restValue: 1, pressedValue: 0.4 },
      null,
    ])
  })

  it('accepts a held pedal but rejects motion during the resting or pressed capture', () => {
    const released = stableAxes([[1, 1], [1, 0.99], [1, 1], [1, 1], [1, 1], [1, 1]])
    const pressed = stableAxes([[-1, 1], [-0.99, 1], [-1, 1], [-1, 1], [-1, 1], [-1, 1]])
    expect(released).not.toBeNull()
    expect(pressed).not.toBeNull()
    expect(bindingForInput({ axes: released!, buttons: [] }, { axes: pressed!, buttons: [] }, 0)?.inputKind).toBe('axis')
    expect(stableAxes([[1], [0.9], [0.8], [0.7], [0.6], [0.5]])).toBeNull()
    expect(stableAxes([[1], [1], [1], [1]])).toBeNull()
  })

  it('still reads a saved button mapping', () => {
    expect(rawBindingValue(
      { axes: [1], buttons: [0.8] },
      { inputKind: 'button', inputIndex: 0, restValue: 0, pressedValue: 1 },
    )).toBe(0.8)
  })

  it('requires the requested pedal to rise while the other stays released', () => {
    const accelerator = { inputKind: 'axis' as const, inputIndex: 0, restValue: 1, pressedValue: -1 }
    const brake = { inputKind: 'axis' as const, inputIndex: 1, restValue: 1, pressedValue: -1 }
    expect([
      pedalVerification({ axes: [-1, 1], buttons: [] }, accelerator, brake),
      pedalVerification({ axes: [1, -1], buttons: [] }, accelerator, brake),
      pedalVerification({ axes: [-1, -1], buttons: [] }, accelerator, brake),
      pedalVerification(null, accelerator, brake),
    ]).toEqual([
      { accelerator: 100, brake: 0, acceleratorReady: true, brakeReady: false },
      { accelerator: 0, brake: 100, acceleratorReady: false, brakeReady: true },
      { accelerator: 100, brake: 100, acceleratorReady: false, brakeReady: false },
      { accelerator: 0, brake: 0, acceleratorReady: false, brakeReady: false },
    ])
  })

  it('normalizes forward and inverted pedal ranges', () => {
    expect(pedalPercent(0, { inputKind: 'axis', inputIndex: 0, restValue: -1, pressedValue: 1 })).toBe(50)
    expect(pedalPercent(0, { inputKind: 'axis', inputIndex: 0, restValue: 1, pressedValue: -1 })).toBe(50)
    expect(pedalPercent(-1, { inputKind: 'axis', inputIndex: 0, restValue: 1, pressedValue: -1 })).toBe(100)
  })

  it('treats a blocked or unavailable gamepad read as disconnected', () => {
    vi.stubGlobal('navigator', { getGamepads: () => { throw new DOMException('blocked', 'SecurityError') } })
    expect(currentGamepad(0, 'Sim Pedals')).toBeNull()
  })

  it('does not attach a saved mapping to an unidentified controller at a reused index', () => {
    const gamepad = { id: '', index: 0, connected: true }
    vi.stubGlobal('navigator', { getGamepads: () => [gamepad] })
    expect([
      currentGamepad(0, 'Sim Pedals'),
      currentGamepad(0, 'Game controller 1')?.index,
    ]).toEqual([null, 0])
  })

  it('distinguishes a hidden controller from blocked and failed scans', () => {
    vi.stubGlobal('navigator', { getGamepads: () => [] })
    expect(scanGamepads()).toEqual({ devices: [], issue: null })
    vi.stubGlobal('navigator', { getGamepads: () => { throw new DOMException('Denied', 'SecurityError') } })
    expect(scanGamepads()).toEqual({ devices: [], issue: 'blocked' })
    vi.stubGlobal('navigator', { getGamepads: () => { throw new Error('Read failed') } })
    expect(scanGamepads()).toEqual({ devices: [], issue: 'failed' })
  })

  it('reports axes without listing controller buttons', () => {
    vi.stubGlobal('navigator', { getGamepads: () => [{
      id: 'Pedal set', index: 0, connected: true,
      axes: [0, 0, 0], buttons: [{ value: 1 }, { value: 0 }],
    }] })
    expect(scanGamepads().devices[0]?.detail).toBe('3 axes · browser index 0')
  })

  it('follows a uniquely identified controller to a new slot without selecting a reused slot', () => {
    const device = (index: number, gamepadId: string): ControllerDevice => ({
      id: `gamepad-${index}`, label: gamepadId, gamepadId, gamepadIndex: index,
      kind: 'gamepad', status: 'available', detail: '',
    })
    const moved = [device(0, 'Other controller'), device(1, 'Pedal set')]
    expect(resolveSelectedGamepad(moved, 'gamepad-0', 'Pedal set')).toEqual(moved[1])
    expect(resolveSelectedGamepad([device(1, 'Pedal set'), device(2, 'Pedal set')], 'gamepad-0', 'Pedal set')).toBeNull()
    expect(resolveSelectedGamepad(moved, 'gamepad-0', 'Missing pedal set')).toBeNull()
  })
})

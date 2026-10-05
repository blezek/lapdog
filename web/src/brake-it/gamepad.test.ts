import { afterEach, describe, expect, it, vi } from 'vitest'

import { currentGamepad, detectPedalInput, pedalPercent } from './gamepad'

afterEach(() => vi.unstubAllGlobals())

describe('Brake-It gamepad pedal mapping', () => {
  it('detects the input with the largest movement without assuming an axis number', () => {
    expect(detectPedalInput(
      { axes: [0, 1, 0], buttons: [0, 0] },
      { axes: [0.03, -0.8, 0.2], buttons: [0, 0.4] },
    )).toEqual({ inputKind: 'axis', inputIndex: 1, restValue: 1, pressedValue: -0.8 })
  })

  it('detects analog button pedals', () => {
    expect(detectPedalInput(
      { axes: [0], buttons: [0, 0] },
      { axes: [0.04], buttons: [0, 0.75] },
    )).toEqual({ inputKind: 'button', inputIndex: 1, restValue: 0, pressedValue: 0.75 })
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
})

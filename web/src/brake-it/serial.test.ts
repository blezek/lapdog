import { describe, expect, it } from 'vitest'

import { parseSerialMessage } from './serial'

describe('Brake-It serial protocol', () => {
  it('accepts JSON controller messages', () => {
    expect(parseSerialMessage('{"accelerator":0.25,"brake":72}')).toEqual({
      accelerator: 0.25,
      brake: 72,
    })
  })

  it('accepts simple key-value controller messages', () => {
    expect(parseSerialMessage('throttle=18, brake=64')).toEqual({ throttle: 18, brake: 64 })
  })

  it('rejects input with no numeric axes', () => {
    expect(parseSerialMessage('controller ready')).toBeNull()
    expect(parseSerialMessage('42')).toBeNull()
    expect(parseSerialMessage('null')).toBeNull()
  })
})

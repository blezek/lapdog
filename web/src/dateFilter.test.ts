import { describe, expect, it, vi } from 'vitest'

import { applyDatePreset, applyDateTimeReset } from './components/DateFilter'

describe('date filter presets', () => {
  it('applies and dismisses a one-click preset', () => {
    const update = vi.fn()
    const dismiss = vi.fn()

    applyDatePreset('yesterday', update, dismiss)

    expect(update).toHaveBeenCalledWith({
      range: 'yesterday',
      from: undefined,
      to: undefined,
    })
    expect(dismiss).toHaveBeenCalledOnce()
  })

  it('keeps the panel open when custom dates still need editing', () => {
    const update = vi.fn()
    const dismiss = vi.fn()

    applyDatePreset('custom', update, dismiss)

    expect(update).toHaveBeenCalledWith({
      range: 'custom',
      from: undefined,
      to: undefined,
    })
    expect(dismiss).not.toHaveBeenCalled()
  })

  it('dismisses after resetting all date and time controls', () => {
    const update = vi.fn()
    const dismiss = vi.fn()

    applyDateTimeReset(update, dismiss)

    expect(update).toHaveBeenCalledWith({
      range: 'all',
      from: undefined,
      to: undefined,
      hf: undefined,
      ht: undefined,
      dow: undefined,
    })
    expect(dismiss).toHaveBeenCalledOnce()
  })
})

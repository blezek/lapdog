import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'

import { DurationProvider, useDuration } from './duration'

function Total() {
  const duration = useDuration()
  return <span>{duration.hours(25.5)}</span>
}

describe('elapsed-total preference', () => {
  it('keeps the existing decimal display until clock format is chosen', () => {
    expect(renderToStaticMarkup(<Total />)).toContain('25.5 h')
    expect(renderToStaticMarkup(
      <DurationProvider config={{ durationFormat: 'clock', showDurationSeconds: false }}>
        <Total />
      </DurationProvider>,
    )).toContain('1:01:30')
  })
})

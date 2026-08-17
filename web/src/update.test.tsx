import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it } from 'vitest'

import { ReleaseNotes } from './components/UpdatePopdown'
import type { UpdateStatus } from './api'
import { buildIdentity, installActionLabel, shouldOpenUpdate, updateStatusMessage } from './update'

const status = (patch: Partial<UpdateStatus> = {}): UpdateStatus => ({
  state: 'available', currentVersion: 'v1.0.0', currentRevision: null,
  availableRelease: null, lastCheck: null, deferredUntil: null, skippedVersion: null,
  acceptedVersion: null, promptEligible: false, recording: false, reindexing: false,
  restartSafe: true, pendingRestart: false, error: null, ...patch,
})

describe('ReleaseNotes', () => {
  it('renders supported Markdown without raw HTML', () => {
    const html = renderToStaticMarkup(<ReleaseNotes source={'## Fixes\n- Safe update\n<script>alert(1)</script>\n[Release](https://example.test/release)'} />)
    expect(html).toContain('<h3>Fixes</h3>')
    expect(html).toContain('<li>Safe update</li>')
    expect(html).toContain('&lt;script&gt;alert(1)&lt;/script&gt;')
    expect(html).toContain('href="https://example.test/release"')
    expect(html).not.toContain('<script>')
  })

  it('does not turn non-HTTPS Markdown links into anchors', () => {
    const html = renderToStaticMarkup(<ReleaseNotes source="[unsafe](javascript:alert(1))" />)
    expect(html).not.toContain('<a')
  })
})

describe('update presentation state', () => {
  it('shows stamped release identity in Settings', () => {
    expect(buildIdentity('0.2.0', 'd81bb49abcdef')).toEqual({
      version: 'LapDog 0.2.0',
      revision: 'Revision d81bb49a',
    })
  })

  it('labels an unstamped build without inventing a revision', () => {
    expect(buildIdentity('dev', null)).toEqual({
      version: 'Development build',
      revision: 'Revision unknown',
    })
  })

  it('opens for one-time eligibility or a tray deep-link', () => {
    expect(shouldOpenUpdate(status({ promptEligible: true }), '')).toBe(true)
    expect(shouldOpenUpdate(status(), '?update=1')).toBe(true)
    expect(shouldOpenUpdate(status(), '')).toBe(false)
  })

  it('uses recording-dependent install labels', () => {
    expect(installActionLabel(true)).toBe('Update after session')
    expect(installActionLabel(false)).toBe('Update and restart')
  })

  it('keeps staged waiting and restart failure distinct', () => {
    expect(updateStatusMessage(status({ state: 'waiting' }))).toContain('active session')
    expect(updateStatusMessage(status({ state: 'restart-required' }))).toContain('Restart LapDog')
    expect(updateStatusMessage(status())).toBeNull()
  })
})

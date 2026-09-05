import { renderToStaticMarkup } from 'react-dom/server'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import { ReleaseNotes, UpdateActions, UpdatePopdown, UpdateProgress } from './components/UpdatePopdown'
import type { UpdateStatus } from './api'
import { buildIdentity, installActionLabel, shouldOpenUpdate, updateStatusMessage } from './update'

const status = (patch: Partial<UpdateStatus> = {}): UpdateStatus => ({
  state: 'available', currentVersion: 'v1.0.0', currentRevision: null,
  availableRelease: null, lastCheck: null, deferredUntil: null, skippedVersion: null,
  acceptedVersion: null, promptEligible: false, recording: false, reindexing: false,
  restartSafe: true, pendingRestart: false, download: null, error: null, ...patch,
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
    expect(installActionLabel(true)).toBe('Upgrade after session')
    expect(installActionLabel(false)).toBe('Upgrade now')
  })

  it('offers upgrade, defer, and exact-version skip choices', () => {
    const html = renderToStaticMarkup(<UpdateActions recording={false} disabled={false} onAction={() => {}} />)
    expect(html).toContain('Upgrade now')
    expect(html).toContain('Ask me later')
    expect(html).toContain('Skip this version')
  })

  it('keeps staged waiting and restart failure distinct', () => {
    expect(updateStatusMessage(status({ state: 'waiting' }))).toContain('active session')
    expect(updateStatusMessage(status({ state: 'restart-required' }))).toContain('Restart LapDog')
    expect(updateStatusMessage(status())).toBeNull()
  })

  it('renders determinate and indeterminate download progress', () => {
    const measured = renderToStaticMarkup(<UpdateProgress progress={{ phase: 'archive', downloadedBytes: 5 * 1024 * 1024, totalBytes: 10 * 1024 * 1024 }} />)
    expect(measured).toContain('Downloading update… 50% · 5.0 MB of 10.0 MB')
    expect(measured).toContain('max="10485760"')
    expect(measured).toContain('value="5242880"')

    const unknown = renderToStaticMarkup(<UpdateProgress progress={{ phase: 'archive', downloadedBytes: 2048, totalBytes: null }} />)
    expect(unknown).toContain('Downloading update… 2.0 KB')
    expect(unknown).not.toContain('max=')

    const verifying = renderToStaticMarkup(<UpdateProgress progress={{ phase: 'verifying', downloadedBytes: 10, totalBytes: 10 }} />)
    expect(verifying).toContain('Verifying downloaded update…')
  })

  it('places progress in an accepted update popdown', () => {
    const current = status({
      state: 'downloading',
      acceptedVersion: 'v1.1.0',
      availableRelease: { version: 'v1.1.0', url: 'https://example.test/release', notes: '', publishedAt: null },
      download: { phase: 'archive', downloadedBytes: 50, totalBytes: 100 },
    })
    const html = renderToStaticMarkup(
      <QueryClientProvider client={new QueryClient()}>
        <UpdatePopdown status={current} open={false} onClose={() => {}} />
      </QueryClientProvider>,
    )
    expect(html).toContain('Update download progress')
    expect(html).toContain('Downloading update… 50%')
  })
})

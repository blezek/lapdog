import { describe, expect, it } from 'vitest'

import { idleReasonFor, viewFor, staleAfterMs } from './live'
import type { LiveResponse } from './api'

const base: LiveResponse = {
  frame: null,
  status: {
    connected: false, paused: false, intervalSeconds: 1,
    sessionKey: '', sessionLabel: '', trackName: '', carName: '',
    connectedSeconds: 0, inCarSeconds: 0, drivingSeconds: 0,
    laps: 0, missingVars: [], incidentSource: '',
    sessionsRecorded: 0,
  },
  intervalSeconds: 1,
  supported: true,
  platform: 'windows',
}

describe('staleAfterMs', () => {
  it('is exactly three poll intervals', () => {
    // A thirty-second poll rate must not read as permanently broken, which a
    // fixed threshold would cause. Exact, not "more than 30 seconds": the
    // multiplier is the rule, and an assertion that only bounds it below would
    // accept any inflation of it.
    expect(staleAfterMs(30)).toBe(90_000)
    expect(staleAfterMs(1)).toBe(3000)
  })

  it('never drops below exactly two seconds', () => {
    // A quarter-second poll rate would otherwise flap between live and stale on
    // ordinary scheduling jitter. The floor is 2000 exactly, not merely at least.
    expect(staleAfterMs(0.25)).toBe(2000)
    expect(staleAfterMs(0.5)).toBe(2000)
  })
})

describe('viewFor', () => {
  const now = new Date('2026-08-10T12:00:00Z').getTime()

  it('reports unsupported before anything else', () => {
    // Not being able to read telemetry at all is a different fact from no
    // simulator being present, and it outranks it.
    const res = { ...base, supported: false, platform: 'darwin' }
    expect(viewFor(res, now)).toBe('unsupported')
  })

  it('reports idle when no frame has arrived', () => {
    expect(viewFor(base, now)).toBe('idle')
  })

  it('reports live for a recent frame', () => {
    const res = { ...base, frame: { ...frameAt(now - 500) } }
    expect(viewFor(res, now)).toBe('live')
  })

  it('reports stale once the threshold passes', () => {
    const res = { ...base, frame: { ...frameAt(now - 10_000) } }
    expect(viewFor(res, now)).toBe('stale')
  })

  it('turns stale on the clock alone, with the payload unchanged', () => {
    // The transition the page exists to show. A stalled collector answers every
    // poll with the same frame, so the only thing that changes is the time — and
    // the verdict has to change with it. This is the rule behind the page's own
    // timer: without one, the same response was only ever judged once, at the
    // instant it arrived, and the verdict stayed green for as long as the stall
    // lasted.
    const res = { ...base, frame: { ...frameAt(now) } }
    expect(viewFor(res, now)).toBe('live')
    expect(viewFor(res, now + 2999)).toBe('live')
    expect(viewFor(res, now + 3001)).toBe('stale')
  })

  function frameAt(ms: number) {
    return {
      at: new Date(ms).toISOString(),
      inCar: true, driving: false, replay: false, reason: 'in the pit box',
      lap: 3, lapDistPct: 0.42, lapCurrentTimeS: 98.4,
      lapLastTimeS: 101.9, lapBestTimeS: 98.1,
      speed: 35.6, gear: 4, fuelLevel: 38.2, incidents: 0,
    }
  }
})

describe('idleReasonFor', () => {
  it('names a refusal rather than calling it an absent simulator', () => {
    // The case the page was built for, and the one it used to report backwards: a
    // refused session has no frame while the simulator is connected and
    // publishing, so "waiting for iRacing" was the opposite of the truth.
    const status = {
      ...base.status,
      connected: true,
      missingVars: ['CarIdxTrackSurface'],
    }
    expect(idleReasonFor(status)).toBe('refused')
  })

  it('prefers the refusal to the pause, because one is a fault and one is not', () => {
    const status = {
      ...base.status,
      connected: true,
      paused: true,
      missingVars: ['CarIdxTrackSurface'],
    }
    expect(idleReasonFor(status)).toBe('refused')
  })

  it('says a paused collector is paused', () => {
    // While paused no frame is handled at all, so the silence says nothing about
    // whether a simulator is running.
    const status = { ...base.status, connected: true, paused: true }
    expect(idleReasonFor(status)).toBe('paused')
  })

  it('distinguishes connected-with-no-session from no simulator', () => {
    // A session that has just ended clears the retained frame while the
    // connection remains, and denying a connection the collector reports would
    // be the same class of error as claiming one it has not made.
    expect(idleReasonFor({ ...base.status, connected: true })).toBe('noSession')
    expect(idleReasonFor({ ...base.status, connected: false })).toBe('waiting')
  })

  it('treats a null missingVars as no refusal', () => {
    // The field is null on the wire whenever nothing is missing, so a null must
    // not be read as a refusal with an unnamed variable.
    const status = { ...base.status, connected: true, missingVars: null }
    expect(idleReasonFor(status)).toBe('noSession')
  })
})

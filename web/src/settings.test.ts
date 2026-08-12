import { describe, expect, it } from 'vitest'
import type { CaptureReindexStatus } from './api'
import { captureReindexMessage } from './pages/Settings'

function status(patch: Partial<CaptureReindexStatus>): CaptureReindexStatus {
  return {
    state: 'idle', total: 0, processed: 0, replayed: 0, failed: 0, segments: 0, ...patch,
  }
}

describe('capture re-index status', () => {
  it('reports concrete progress while running', () => {
    expect(captureReindexMessage(status({ state: 'running', total: 12, processed: 5 })))
      .toBe('Processed 5 of 12 capture(s).')
  })

  it('distinguishes a clean completion from per-capture failures', () => {
    expect(captureReindexMessage(status({ state: 'complete', total: 12, replayed: 12, segments: 14 })))
      .toBe('Last run replayed 12 of 12 capture(s) and processed 14 session segment(s).')
    expect(captureReindexMessage(status({
      state: 'complete', total: 12, replayed: 10, segments: 11, failed: 2,
    }))).toBe('Last run replayed 10 of 12 capture(s) and processed 11 session segment(s); 2 failed.')
  })

  it('surfaces a fatal job error without inventing one when absent', () => {
    expect(captureReindexMessage(status({ state: 'failed', error: 'database closed' })))
      .toBe('Re-index failed: database closed')
    expect(captureReindexMessage(status({ state: 'failed' })))
      .toBe('Re-index failed: unknown error')
  })
})

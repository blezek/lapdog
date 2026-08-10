import type { LiveResponse } from './api'

/** What the page should render. */
export type LiveView = 'unsupported' | 'idle' | 'stale' | 'live'

/**
 * How long a frame stays current, in milliseconds.
 *
 * Three poll intervals, floored at two seconds. Derived from the interval rather
 * than fixed because a thirty-second poll rate would otherwise show a permanent
 * and false "not reading"; floored because a quarter-second rate would flap on
 * ordinary scheduling jitter.
 */
export function staleAfterMs(intervalSeconds: number): number {
  return Math.max(2000, intervalSeconds * 3000)
}

/**
 * viewFor decides what the page shows.
 *
 * Order matters. Being unable to read telemetry at all outranks having no
 * simulator, because they are different facts and the remedy differs: one is
 * "this build cannot", the other is "start iRacing".
 */
export function viewFor(res: LiveResponse, now: number): LiveView {
  if (!res.supported) return 'unsupported'
  if (!res.frame) return 'idle'
  const age = now - new Date(res.frame.at).getTime()
  return age > staleAfterMs(res.intervalSeconds) ? 'stale' : 'live'
}

/**
 * Why there is no frame to show.
 *
 * Four different facts with four different remedies, and the page reported all of
 * them as "waiting for iRacing". In the refused case that is the opposite of the
 * truth: the simulator is connected and publishing, and what it is not publishing
 * is the reason nothing records — the exact diagnosis this page exists to give.
 */
export type IdleReason = 'refused' | 'paused' | 'noSession' | 'waiting'

/**
 * idleReasonFor explains an absent frame from the status beside it.
 *
 * A refusal outranks a pause because it is a fault and a pause is an instruction:
 * a paused collector is doing what it was told and will record once resumed,
 * while absent variables mean it will refuse the session either way. A pause
 * outranks the rest because while paused no frame is handled at all, so nothing
 * further can be inferred from the silence.
 */
export function idleReasonFor(status: LiveResponse['status']): IdleReason {
  if (status.missingVars != null && status.missingVars.length > 0) return 'refused'
  if (status.paused) return 'paused'
  // Connected with no frame: the simulator is being read, but nothing is being
  // recorded — a session has ended, or none has started. That is a different fact
  // from the simulator not being there, and saying "waiting for iRacing" would
  // deny a connection the collector reports it has made.
  if (status.connected) return 'noSession'
  return 'waiting'
}

/**
 * pollMs is how often to ask for a new frame.
 *
 * The collector's own interval, clamped. Polling faster returns the frame
 * already held; polling slower than three seconds makes the age counter stop
 * feeling live.
 */
export function pollMs(intervalSeconds: number): number {
  return Math.min(3000, Math.max(500, intervalSeconds * 1000))
}

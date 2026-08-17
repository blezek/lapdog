import type { UpdateStatus } from './api'

/** shouldOpenUpdate combines a tray deep-link with one-time prompt eligibility. */
export function shouldOpenUpdate(status: UpdateStatus, search: string): boolean {
  return new URLSearchParams(search).get('update') === '1' || status.promptEligible
}

export interface BuildIdentity {
  version: string
  revision: string
}

/** buildIdentity turns stamped build facts into the copy shown in Settings. */
export function buildIdentity(version: string, revision: string | null): BuildIdentity {
  return {
    version: version === 'dev' ? 'Development build' : `LapDog ${version}`,
    revision: revision ? `Revision ${revision.slice(0, 8)}` : 'Revision unknown',
  }
}

export function installActionLabel(recording: boolean): string {
  return recording ? 'Update after session' : 'Update and restart'
}

export function updateStatusMessage(status: UpdateStatus): string | null {
  if (status.state === 'waiting') {
    return 'The verified update is ready. LapDog will restart after the active session and capture re-index finish.'
  }
  if (status.state === 'restart-required') {
    return 'The update is verified, but LapDog could not start the replacement automatically. Restart LapDog to retry.'
  }
  return null
}

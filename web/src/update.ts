import type { UpdateStatus } from './api'

/** shouldOpenUpdate combines a tray deep-link with one-time prompt eligibility. */
export function shouldOpenUpdate(status: UpdateStatus, search: string): boolean {
  return new URLSearchParams(search).get('update') === '1' || status.promptEligible
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

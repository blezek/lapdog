import type { UpdateDownloadProgress, UpdateStatus } from './api'

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
  return recording ? 'Upgrade after session' : 'Upgrade now'
}

export function downloadProgressText(progress: UpdateDownloadProgress): string {
  if (progress.phase === 'verifying') return 'Verifying downloaded update…'
  if (progress.totalBytes === null || progress.totalBytes <= 0) {
    return `Downloading update… ${formatBytes(progress.downloadedBytes)}`
  }
  const percent = Math.min(100, Math.round(progress.downloadedBytes / progress.totalBytes * 100))
  return `Downloading update… ${percent}% · ${formatBytes(progress.downloadedBytes)} of ${formatBytes(progress.totalBytes)}`
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
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

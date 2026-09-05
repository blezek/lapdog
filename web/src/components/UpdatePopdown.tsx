import { Fragment, type ReactNode, useEffect, useRef } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'

import { api, type UpdateDownloadProgress, type UpdateStatus } from '../api'
import { downloadProgressText, installActionLabel, updateStatusMessage } from '../update'

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = []
  const re = /\[([^\]]+)\]\((https:\/\/[^\s)]+)\)/g
  let at = 0
  for (const match of text.matchAll(re)) {
    const i = match.index ?? 0
    if (i > at) out.push(text.slice(at, i))
    out.push(<a key={`${i}-${match[2]}`} href={match[2]} target="_blank" rel="noreferrer">{match[1]}</a>)
    at = i + match[0].length
  }
  if (at < text.length) out.push(text.slice(at))
  return out
}

/** ReleaseNotes supports the release Markdown LapDog emits and never inserts HTML. */
export function ReleaseNotes({ source }: { source: string }) {
  const blocks: ReactNode[] = []
  let bullets: string[] = []
  const flush = () => {
    if (bullets.length) blocks.push(<ul key={`ul-${blocks.length}`}>{bullets.map((x, i) => <li key={i}>{inline(x)}</li>)}</ul>)
    bullets = []
  }
  source.split(/\r?\n/).forEach((raw) => {
    const line = raw.trim()
    if (!line) { flush(); return }
    if (line.startsWith('## ')) { flush(); blocks.push(<h3 key={`h-${blocks.length}`}>{line.slice(3)}</h3>); return }
    if (line.startsWith('# ')) { flush(); blocks.push(<h3 key={`h-${blocks.length}`}>{line.slice(2)}</h3>); return }
    if (line.startsWith('- ')) { bullets.push(line.slice(2)); return }
    flush(); blocks.push(<p key={`p-${blocks.length}`}>{inline(line)}</p>)
  })
  flush()
  return <Fragment>{blocks.length ? blocks : <p>No release notes were published.</p>}</Fragment>
}

export function UpdateProgress({ progress }: { progress: UpdateDownloadProgress }) {
  const measured = progress.totalBytes !== null && progress.totalBytes > 0
  return (
    <div className="update-progress" aria-live="polite">
      <span>{downloadProgressText(progress)}</span>
      <progress
        aria-label="Update download progress"
        max={measured ? progress.totalBytes ?? undefined : undefined}
        value={measured ? progress.downloadedBytes : undefined}
      />
    </div>
  )
}

export function UpdateActions({ recording, disabled, onAction }: {
  recording: boolean
  disabled: boolean
  onAction: (action: 'install' | 'later' | 'skip') => void
}) {
  return (
    <div className="update-actions">
      <button type="button" className="control primary" disabled={disabled} onClick={() => onAction('install')}>{installActionLabel(recording)}</button>
      <button type="button" className="control" disabled={disabled} onClick={() => onAction('later')}>Ask me later</button>
      <button type="button" className="control" disabled={disabled} onClick={() => onAction('skip')}>Skip this version</button>
    </div>
  )
}

export function UpdatePopdown({ status, open, onClose }: { status: UpdateStatus; open: boolean; onClose: () => void }) {
  const qc = useQueryClient()
  const dialog = useRef<HTMLDialogElement>(null)
  const action = useMutation({
    mutationFn: api.updateAction,
    onSuccess: (next) => qc.setQueryData(['update'], next),
  })

  useEffect(() => {
    const node = dialog.current
    if (!node) return
    if (open && !node.open) node.showModal()
    if (!open && node.open) node.close()
  }, [open])

  const rel = status.availableRelease
  const busy = ['checking', 'downloading', 'applying'].includes(status.state)
  const message = updateStatusMessage(status)
  return (
    <dialog ref={dialog} className="update-popdown" aria-labelledby="update-title" onClose={onClose} onCancel={onClose}>
      <div className="update-head">
        <h2 id="update-title">LapDog update</h2>
        <button type="button" className="ghost-btn" aria-label="Close update details" onClick={onClose}>Close</button>
      </div>
      <p className="build-line">Running {status.currentVersion} · {status.currentRevision?.slice(0, 8) ?? 'unknown commit'}</p>
      {rel ? (
        <>
          <p className="update-target">{rel.version} is available. <a href={rel.url} target="_blank" rel="noreferrer">View on GitHub</a></p>
          <div className="release-notes"><ReleaseNotes source={rel.notes} /></div>
          {status.state === 'downloading' && status.download && <UpdateProgress progress={status.download} />}
          {message && <p className={status.state === 'restart-required' ? 'update-error' : 'update-message'}>{message}</p>}
          {status.error && <p className="update-error">{status.error}</p>}
          {!status.acceptedVersion && <UpdateActions recording={status.recording} disabled={busy || action.isPending} onAction={(next) => action.mutate(next)} />}
        </>
      ) : (
        <p>{status.state === 'checking' ? 'Checking GitHub…' : status.state === 'disabled' ? 'Automatic updates are available in Windows release builds.' : 'LapDog is current.'}</p>
      )}
    </dialog>
  )
}

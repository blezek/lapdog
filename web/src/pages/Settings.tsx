import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, type Config } from '../api'
import { bytes, num } from '../format'
import { applyTheme } from '../theme'
import { Banner, Card, ErrorNote, Loading } from '../components/ui'

/**
 * Settings edits the server's configuration.
 *
 * Each control sends only the field it changed. A partial body leaves everything
 * else alone, so two people editing different settings cannot clobber each other
 * and a field the interface does not know about is never zeroed.
 */
export function Settings() {
  const qc = useQueryClient()
  const config = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  const status = useQuery({ queryKey: ['status'], queryFn: api.status })

  const [restart, setRestart] = useState<string[]>([])
  const [failed, setFailed] = useState<string | null>(null)

  const save = useMutation({
    mutationFn: (patch: Partial<Config>) => api.saveSettings(patch),
    onSuccess: (res) => {
      setFailed(null)
      setRestart(res.restartRequired ?? [])
      qc.setQueryData(['settings'], res.config)
      if (res.config.theme) applyTheme(res.config.theme)
    },
    // The server validates, so its message is the useful one to show — it names
    // the field and the legal range.
    onError: (e: unknown) => setFailed(e instanceof Error ? e.message : String(e)),
  })

  if (config.isLoading) return <Loading />
  if (config.isError) return <ErrorNote error={config.error} />
  if (!config.data) return null

  const c = config.data
  const set = (patch: Partial<Config>) => save.mutate(patch)

  return (
    <>
      <div className="page-head">
        <h1>Settings</h1>
      </div>
      <p className="page-sub">Changes save immediately.</p>

      {failed && <Banner kind="bad">{failed}</Banner>}
      {restart.length > 0 && (
        <Banner kind="warn">
          Saved. Changing <strong>{restart.join(', ')}</strong> takes effect the next
          time LapDog starts.
        </Banner>
      )}
      {status.data?.missingVars && status.data.missingVars.length > 0 && (
        <Banner kind="bad">
          The simulator is not publishing{' '}
          <span className="mono">{status.data.missingVars.join(', ')}</span>, so sessions
          are not being recorded. Recording nothing is deliberate: recording wrong data
          would be worse.
        </Banner>
      )}

      <div className="card">
        <div className="section-title">Telemetry</div>

        <div className="setting">
          <div className="setting-label">
            Poll interval
            <span className="setting-hint">
              How often the simulator is read. Lower is finer time accounting and more
              CPU. Takes effect immediately.
            </span>
          </div>
          <div className="setting-control">
            <NumberField
              value={c.pollIntervalSeconds}
              min={0.25}
              max={30}
              step={0.25}
              suffix="s"
              onCommit={(v) => set({ pollIntervalSeconds: v })}
            />
          </div>
        </div>

        <div className="setting">
          <div className="setting-label">
            Minimum session length
            <span className="setting-hint">
              Sessions shorter than this are discarded, which drops accidental joins.
            </span>
          </div>
          <div className="setting-control">
            <NumberField
              value={c.minSessionSeconds}
              min={0}
              max={3600}
              step={5}
              suffix="s"
              onCommit={(v) => set({ minSessionSeconds: v })}
            />
          </div>
        </div>

        <div className="section-title">Recording</div>

        <div className="setting">
          <div className="setting-label">
            Record capture files
            <span className="setting-hint">
              Saves the telemetry frames the collector polled, so a session can be
              replayed for debugging later.
            </span>
          </div>
          <div className="setting-control">
            <Toggle
              checked={c.captureEnabled}
              onChange={(v) => set({ captureEnabled: v })}
              label="Record capture files"
            />
          </div>
        </div>

        <div className="setting">
          <div className="setting-label">
            Keep at most
            <span className="setting-hint">
              Oldest captures are deleted once the folder passes this size. Zero means
              keep everything. Currently {bytes(c.captureMaxBytes)}.
            </span>
          </div>
          <div className="setting-control">
            <select
              value={String(c.captureMaxBytes)}
              onChange={(e) => set({ captureMaxBytes: Number(e.target.value) })}
            >
              {[
                ['0', 'Unlimited'],
                [String(512 * 1024 * 1024), '512 MB'],
                [String(1024 * 1024 * 1024), '1 GB'],
                [String(2 * 1024 * 1024 * 1024), '2 GB'],
                [String(5 * 1024 * 1024 * 1024), '5 GB'],
              ].map(([v, l]) => (
                <option key={v} value={v}>
                  {l}
                </option>
              ))}
            </select>
          </div>
        </div>

        <div className="section-title">Application</div>

        <div className="setting">
          <div className="setting-label">
            Web interface port
            <span className="setting-hint">
              Restart required. The server always binds 127.0.0.1 only, so it is never
              reachable from another machine.
            </span>
          </div>
          <div className="setting-control">
            <NumberField
              value={c.port}
              min={1}
              max={65535}
              step={1}
              onCommit={(v) => set({ port: v })}
            />
          </div>
        </div>

        <div className="setting">
          <div className="setting-label">Start with Windows</div>
          <div className="setting-control">
            <Toggle
              checked={c.startWithWindows}
              onChange={(v) => set({ startWithWindows: v })}
              label="Start with Windows"
            />
          </div>
        </div>

        <div className="setting">
          <div className="setting-label">Units</div>
          <div className="setting-control">
            <select
              value={c.units}
              onChange={(e) => set({ units: e.target.value as Config['units'] })}
            >
              <option value="metric">Metric</option>
              <option value="imperial">Imperial</option>
            </select>
          </div>
        </div>

        <div className="setting">
          <div className="setting-label">
            Theme
            <span className="setting-hint">
              Dark mode uses its own colour steps chosen for the dark surface, not an
              inversion of the light ones.
            </span>
          </div>
          <div className="setting-control">
            <select
              value={c.theme}
              onChange={(e) => set({ theme: e.target.value as Config['theme'] })}
            >
              <option value="system">System</option>
              <option value="light">Light</option>
              <option value="dark">Dark</option>
            </select>
          </div>
        </div>

        <div className="section-title">Status</div>

        <div className="setting">
          <div className="setting-label">
            iRacing
            <span className="setting-hint">
              {status.data
                ? `Reading every ${status.data.intervalSeconds}s · incidents from ${
                    status.data.incidentSource || 'results'
                  } · ${num(status.data.sessionsRecorded)} session(s) recorded this run`
                : 'Unknown'}
            </span>
          </div>
          <div className="setting-control">
            {status.data?.connected ? 'Connected' : 'Not connected'}
          </div>
        </div>

        <div className="setting">
          <div className="setting-label">
            Database
            <span className="setting-hint mono">{status.data?.databasePath ?? '—'}</span>
          </div>
          <div className="setting-control">{status.data?.version ?? ''}</div>
        </div>
      </div>

      <Card title="Iconography">
        <p style={{ margin: 0, fontSize: 12.5, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
          Icons are Material Design Icons from the{' '}
          <a href="https://pictogrammers.com/" target="_blank" rel="noreferrer">
            Pictogrammers
          </a>{' '}
          project, released under the Apache 2.0 licence and vendored into the
          executable. The licence text ships with the application.
        </p>
      </Card>
    </>
  )
}

/**
 * NumberField commits on blur or Enter rather than on every keystroke.
 *
 * Committing per keystroke would send "0.2" on the way to "0.25" and the server
 * would reject it as below the minimum, which reads as the field fighting the user.
 */
function NumberField({
  value,
  min,
  max,
  step,
  suffix,
  onCommit,
}: {
  value: number
  min: number
  max: number
  step: number
  suffix?: string
  onCommit: (v: number) => void
}) {
  const [draft, setDraft] = useState(String(value))
  useEffect(() => setDraft(String(value)), [value])

  const commit = () => {
    const n = Number(draft)
    if (!Number.isFinite(n) || n === value) {
      setDraft(String(value))
      return
    }
    onCommit(n)
  }

  return (
    <>
      <input
        type="number"
        value={draft}
        min={min}
        max={max}
        step={step}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === 'Enter') commit()
          if (e.key === 'Escape') setDraft(String(value))
        }}
      />
      {suffix && <span style={{ color: 'var(--text-muted)', fontSize: 12 }}>{suffix}</span>}
    </>
  )
}

function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: string
}) {
  return (
    <input
      type="checkbox"
      checked={checked}
      aria-label={label}
      onChange={(e) => onChange(e.target.checked)}
      style={{ width: 17, height: 17, accentColor: 'var(--accent)' }}
    />
  )
}

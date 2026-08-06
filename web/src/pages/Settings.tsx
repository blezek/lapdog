import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { api, type Config } from '../api'
import { bytes, num } from '../format'
import { applyTheme } from '../theme'
import { Banner, Card, ErrorNote, Loading } from '../components/ui'

/*
 * Slider stops.
 *
 * The sliders step through a fixed list of values rather than sweeping the raw
 * range. Poll interval spans 0.25 to 30 seconds, and on a linear track everything
 * anyone actually uses — a quarter of a second to a few seconds — would be squeezed
 * into the first sixth of the travel, with two thirds of the track given over to
 * intervals so coarse that lap attribution stops working. Discrete stops give every
 * position a value worth landing on.
 *
 * The number field beside each slider stays, so any legal value can still be typed
 * exactly, including ones that are not stops.
 */

/** POLL_STOPS covers the useful poll rates within the server's legal bounds. */
const POLL_STOPS = [0.25, 0.5, 1, 2, 3, 5, 10, 15, 30]

/** MIN_SESSION_STOPS runs from recording everything to a ten-minute floor. */
const MIN_SESSION_STOPS = [0, 10, 30, 60, 120, 300, 600]

/** formatSeconds renders a duration as seconds or whole minutes. */
function formatSeconds(v: number): string {
  if (v === 0) return 'off'
  if (v < 60) return `${v} s`
  const m = v / 60
  return `${Number.isInteger(m) ? m : m.toFixed(1)} min`
}

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
              CPU. Takes effect immediately, with no restart.
            </span>
          </div>
          <div className="setting-control">
            <StopsSlider
              value={c.pollIntervalSeconds}
              stops={POLL_STOPS}
              format={(v) => `${v} s`}
              ariaLabel="Poll interval"
              onCommit={(v) => set({ pollIntervalSeconds: v })}
            />
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
              Zero records everything.
            </span>
          </div>
          <div className="setting-control">
            <StopsSlider
              value={c.minSessionSeconds}
              stops={MIN_SESSION_STOPS}
              format={formatSeconds}
              ariaLabel="Minimum session length"
              onCommit={(v) => set({ minSessionSeconds: v })}
            />
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

        {/*
            Read-only, and deliberately so: the source is fixed by the simulator and
            the paths follow from the data directory the platform dictates. Nothing
            here is a preference. It is shown because when nothing records, the first
            question is what the reader was pointed at — and because the source is the
            single most misunderstood thing about this application.
        */}
        <div className="setting">
          <div className="setting-label">
            Telemetry source
            <span className="setting-hint mono">{status.data?.telemetry.source ?? '—'}</span>
            <span className="setting-hint">
              {status.data?.telemetry.sourceKind ?? ''}
              {status.data?.telemetry.available
                ? ` · this build can read it on ${status.data.telemetry.platform}; whether it is currently reached is the iRacing row above`
                : status.data
                  ? ` · this build cannot read it on ${status.data.telemetry.platform}, so no session will record on this machine`
                  : ''}
            </span>
          </div>
          {/*
              "Supported", not "Available": this reports only whether the build can
              read live telemetry on this operating system. It says nothing about
              whether the simulator was reached, and calling it Available invited
              exactly that reading — a label must not claim more than it knows.
          */}
          <div className="setting-control">
            {status.data?.telemetry.available ? 'Supported' : 'Not supported'}
          </div>
        </div>

        {/* Absent variables are why a connected session records nothing, so they are
            named rather than counted. */}
        {status.data?.missingVars && status.data.missingVars.length > 0 && (
          <div className="setting">
            <div className="setting-label">
              Refused: missing telemetry variables
              <span className="setting-hint mono">
                {status.data.missingVars.join(', ')}
              </span>
              <span className="setting-hint">
                The session was not recorded rather than recorded wrongly. These names
                come from the simulator's variable list.
              </span>
            </div>
            <div className="setting-control">{status.data.missingVars.length}</div>
          </div>
        )}

        <div className="setting">
          <div className="setting-label">
            Database
            <span className="setting-hint mono">{status.data?.databasePath ?? '—'}</span>
          </div>
          <div className="setting-control">{status.data?.version ?? ''}</div>
        </div>

        <div className="setting">
          <div className="setting-label">
            Captures
            <span className="setting-hint mono">{status.data?.telemetry.capturesDir ?? '—'}</span>
            <span className="setting-hint">
              Copy this folder to replay real sessions elsewhere with lapdogctl ingest.
            </span>
          </div>
          <div className="setting-control">{config.data?.captureEnabled ? 'On' : 'Off'}</div>
        </div>

        <div className="setting">
          <div className="setting-label">
            Log
            <span className="setting-hint mono">{status.data?.telemetry.logPath ?? '—'}</span>
            <span className="setting-hint">
              Records why a session was refused, if one was.
            </span>
          </div>
          <div className="setting-control" />
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

/**
 * StopsSlider steps through a fixed list of values.
 *
 * The slider's own position is an index into stops, not the value itself, which is
 * what makes every position a sensible setting.
 *
 * Dragging updates the displayed value continuously but only commits when the
 * gesture ends. A range input fires onChange for every step it passes through, so
 * committing per change would send a PUT for each intermediate value on the way to
 * the one the user wanted.
 *
 * If the stored value is not one of the stops — because it was typed into the number
 * field, or set before the stop list changed — the nearest stop is highlighted and
 * the exact value is still shown, so the slider never misrepresents the setting.
 */
function StopsSlider({
  value,
  stops,
  format,
  ariaLabel,
  onCommit,
}: {
  value: number
  stops: number[]
  format: (v: number) => string
  ariaLabel: string
  onCommit: (v: number) => void
}) {
  const nearest = nearestIndex(stops, value)
  const [index, setIndex] = useState(nearest)
  const [dragging, setDragging] = useState(false)

  // Follow the server's value unless the user is mid-drag, which would otherwise
  // yank the handle back while they are still moving it.
  useEffect(() => {
    if (!dragging) setIndex(nearest)
  }, [nearest, dragging])

  const shown = stops[index] ?? value
  const exact = !dragging && shown !== value

  const commit = () => {
    setDragging(false)
    const next = stops[index]
    if (next != null && next !== value) onCommit(next)
  }

  return (
    <span className="slider">
      <input
        type="range"
        min={0}
        max={stops.length - 1}
        step={1}
        value={index}
        aria-label={ariaLabel}
        aria-valuetext={format(shown)}
        onChange={(e) => {
          setDragging(true)
          setIndex(Number(e.target.value))
        }}
        onPointerUp={commit}
        onKeyUp={commit}
        onBlur={commit}
      />
      <span className="slider-value" title={exact ? `Exact value: ${value}` : undefined}>
        {format(shown)}
        {exact && <span className="slider-exact"> (set to {value})</span>}
      </span>
    </span>
  )
}

/** nearestIndex returns the index of the stop closest to value. */
function nearestIndex(stops: number[], value: number): number {
  let best = 0
  let bestGap = Infinity
  stops.forEach((s, i) => {
    const gap = Math.abs(s - value)
    if (gap < bestGap) {
      bestGap = gap
      best = i
    }
  })
  return best
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

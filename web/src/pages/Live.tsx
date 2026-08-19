import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api, type Config, type LiveFrame, type LiveResponse } from '../api'
import { hms, lapTime, num, pct, speed } from '../format'
import { keepPrevious } from '../query'
import { idleReasonFor, pollMs, viewFor } from '../live'
import { Card, Loading, Stat } from '../components/ui'

/**
 * Live answers "is LapDog reading telemetry right now, and what is it seeing".
 *
 * Four bands hold the frame's own fields and clear to "—" once the frame goes
 * stale; a fifth, the totals band, comes from the session's accumulated status
 * and is never cleared, because it records what already happened rather than
 * what is happening this instant. Getting that asymmetry backwards would defeat
 * the point of the page.
 */
export function Live() {
  const query = useQuery({
    queryKey: ['live'],
    queryFn: api.live,
    // The poll rate follows the collector's own interval, read off the query's
    // last-known data — there is nothing else to read it from before the first
    // response has arrived, so pollMs(1) is the sensible default until then.
    refetchInterval: (q) => pollMs(q.state.data?.intervalSeconds ?? 1),
    ...keepPrevious,
  })
  const config = useQuery({ queryKey: ['settings'], queryFn: api.settings })

  const now = useNow(pollMs(query.data?.intervalSeconds ?? 1))

  return (
    <>
      <div className="page-head">
        <h1>Live</h1>
      </div>
      <p className="page-sub">
        Whether LapDog is reading telemetry right now, and what it is seeing this
        instant.
      </p>

      {!query.data || !config.data ? (
        <Loading />
      ) : (
        <LiveBody res={query.data} now={now} units={config.data.units} />
      )}
    </>
  )
}

/**
 * useNow returns wall-clock time, re-read on a timer.
 *
 * The verdict has to be able to change while the payload does not. A stalled or
 * paused collector answers every poll with a byte-identical frame; React Query's
 * structural sharing then keeps `data` reference-equal, so nothing re-renders and
 * a page that read the clock only during render would hold a green "Live" verdict
 * with frozen readings for as long as the stall lasted. That is the single failure
 * this page exists to prevent, so the clock is driven independently of the query
 * rather than incidentally by it.
 *
 * The tick matches the poll rate: the age of the last frame is not worth
 * recomputing more often than a new frame could have arrived.
 */
function useNow(periodMs: number): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), periodMs)
    return () => clearInterval(id)
  }, [periodMs])
  return now
}

function LiveBody({ res, now, units }: { res: LiveResponse; now: number; units: Config['units'] }) {
  const view = viewFor(res, now)

  if (view === 'unsupported') return <Unsupported platform={res.platform} />
  if (view === 'idle') return <Idle res={res} />
  return <LiveBands res={res} now={now} live={view === 'live'} units={units} />
}

/**
 * Unsupported states the one fact this build knows for certain on this
 * platform. The wording matches Settings.tsx's telemetry row exactly, so the
 * same fact is never worded two different ways in two different places.
 */
function Unsupported({ platform }: { platform: string }) {
  return (
    <div className="card">
      <div className="setting">
        <div className="setting-label">
          Telemetry
          <span className="setting-hint">
            This build cannot read it on {platform}, so no session will record on
            this machine.
          </span>
        </div>
        <div className="setting-control">Not supported</div>
      </div>
    </div>
  )
}

/**
 * Idle explains an absent frame, which has four different causes.
 *
 * The refused case is the one that matters most: the simulator is connected and
 * publishing, and the variables it is not publishing are why nothing records.
 * Reporting that as "waiting for iRacing" said the opposite of the truth about
 * precisely the scenario this page was built to diagnose. Its wording is
 * Settings.tsx's, verbatim, so the same refusal is never described two ways.
 */
function Idle({ res }: { res: LiveResponse }) {
  const reason = idleReasonFor(res.status)
  const missing = res.status.missingVars ?? []

  const headline =
    reason === 'refused'
      ? 'Refused: missing telemetry variables'
      : reason === 'paused'
        ? 'Recording paused'
        : reason === 'noSession'
          ? 'Connected, no session recording'
          : 'Waiting for iRacing'

  const hint =
    reason === 'refused'
      ? "The session was not recorded rather than recorded wrongly. These names come from the simulator's variable list."
      : reason === 'paused'
        ? `Recording is paused, so no frame is being handled and no time is being credited. Polling every ${res.intervalSeconds}s once resumed.`
        : reason === 'noSession'
          ? `Reading the simulator every ${res.intervalSeconds}s, but no session is being recorded, so there is nothing instantaneous to show.`
          : `Polling every ${res.intervalSeconds}s. No frame has been handled yet.`

  return (
    <div className="card">
      <div className="setting">
        <div className="setting-label">
          {headline}
          {/* Named rather than counted, for the same reason Settings names them:
              the name is what says which spelling the simulator did not publish. */}
          {missing.length > 0 && (
            <span className="setting-hint mono">{missing.join(', ')}</span>
          )}
          <span className="setting-hint">{hint}</span>
        </div>
        {/* The connection is reported in every idle case, because "no frame" and
            "no simulator" are different facts and this is the one that tells them
            apart. */}
        <div className="setting-control">
          {res.status.connected ? 'Connected' : 'Not connected'}
        </div>
      </div>
      <div className="setting">
        <div className="setting-label">Sessions recorded this run</div>
        <div className="setting-control">{num(res.status.sessionsRecorded)}</div>
      </div>
    </div>
  )
}

/**
 * Bands renders the five sections. `live` gates only the four instantaneous
 * ones: the totals band ignores it entirely, which is the asymmetry the whole
 * design rests on.
 */
export function LiveBands({
  res,
  now,
  live,
  units,
}: {
  res: LiveResponse
  now: number
  live: boolean
  units: Config['units']
}) {
  const frame = res.frame as LiveFrame
  const ageSeconds = Math.max(0, Math.round((now - new Date(frame.at).getTime()) / 1000))

  // Cleared to null once stale, so every instantaneous field below renders its
  // own "—" through the same null-handling its live value would use anyway.
  const f = <T,>(v: T): T | null => (live ? v : null)

  return (
    <div className="bands">
      <Card
        title="State"
        actions={
          <span className={`verdict ${live ? 'live' : 'stale'}`}>
            {live ? 'Live' : `Not reading — last seen ${ageSeconds}s ago`}
          </span>
        }
      >
        {/*
            Which session these readings belong to. Not gated by staleness: it
            identifies the session whose totals are shown below, which is a
            record of what was joined rather than a reading that expires.
        */}
        <SessionLine res={res} />
        <div className="lights">
          {/*
              Connected is gated with the rest. A response that has stopped being
              current cannot vouch for the connection either — the query keeps its
              last payload when a fetch fails, so an ungated light would show green
              for a server that is no longer answering.
          */}
          <Light label="Connected" on={f(res.status.connected)} />
          <Light label="In car" on={f(frame.inCar)} />
          <Light label="Driving" on={f(frame.driving)} />
          <Light label="Replay" on={f(frame.replay)} />
        </div>
        {live && frame.reason && (
          <p className="band-note">Not counting driving time — {frame.reason}.</p>
        )}
      </Card>

      <div className="grid kpis">
        <Stat label="Lap" value={intOrDash(f(frame.lap))} />
        <Stat
          label="Lap distance"
          value={f(frame.lapDistPct) == null ? '—' : pct(frame.lapDistPct as number)}
          // Zero, not absent: the meter must empty rather than vanish, or the card
          // changes height as soon as the frame goes stale and the page jumps at
          // the moment it is being read most carefully.
          meter={f(frame.lapDistPct) ?? 0}
        />
        <Stat label="Current lap" value={lapTime(f(frame.lapCurrentTimeS))} />
        <Stat label="Last lap" value={lapTime(f(frame.lapLastTimeS))} />
        <Stat label="Best lap" value={lapTime(f(frame.lapBestTimeS))} />
        {/* Laps completed is a count of what happened, so it is not gated. */}
        <Stat label="Laps completed" value={num(res.status.laps)} />
      </div>

      <div className="grid kpis">
        <Stat label="Speed" value={f(frame.speed) == null ? '—' : speed(frame.speed as number, units)} />
        <Stat label="Gear" value={gearLabel(f(frame.gear))} />
        <Stat label="Fuel" value={f(frame.fuelLevel) == null ? '—' : `${num(frame.fuelLevel as number)} L`} />
        <Stat label="Incidents" value={intOrDash(f(frame.incidents))} />
      </div>

      {/*
          All three totals, because driving seconds alone cannot be read. The
          observation this page was built for is 154 seconds in the car against
          zero driving seconds — correct, the car was in the pit box — and it is
          only an observation rather than a mystery when the other two are beside
          it.
      */}
      <Card title="This session">
        <div className="grid kpis">
          <Stat label="Connected" value={hms(res.status.connectedSeconds)} />
          <Stat label="In car" value={hms(res.status.inCarSeconds)} />
          <Stat label="Driving" value={hms(res.status.drivingSeconds)} />
        </div>
        <p className="band-note">
          Accumulated for the session above; {num(res.status.sessionsRecorded)}{' '}
          session(s) recorded this run. These keep their real values even while the
          bands above go stale, because they record what already happened rather
          than what is happening now.
        </p>
      </Card>
    </div>
  )
}

/**
 * SessionLine names the session the readings come from.
 *
 * Only the parts the status actually carries. An empty component would otherwise
 * render as a stray separator, which reads as a value that failed to load.
 */
function SessionLine({ res }: { res: LiveResponse }) {
  const parts = [res.status.sessionLabel, res.status.trackName, res.status.carName].filter(
    (p) => p !== '',
  )
  if (parts.length === 0) return null
  return <p className="session-line">{parts.join(' · ')}</p>
}

/** Light shows one boolean fact in words as well as colour, never colour alone. */
function Light({ label, on }: { label: string; on: boolean | null }) {
  return (
    <span className={`light ${on == null ? 'light-unknown' : on ? 'light-on' : 'light-off'}`}>
      <span className="light-dot" />
      {label}: {on == null ? '—' : on ? 'Yes' : 'No'}
    </span>
  )
}

/** intOrDash renders a small identifier count, or an em dash when absent. */
function intOrDash(v: number | null): string {
  return v == null ? '—' : num(v)
}

/** gearLabel renders reverse and neutral in the notation a driver reads, not as -1 and 0. */
function gearLabel(gear: number | null): string {
  if (gear == null) return '—'
  if (gear < 0) return 'R'
  if (gear === 0) return 'N'
  return String(gear)
}

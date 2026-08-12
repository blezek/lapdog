/*
 * API client.
 *
 * The types mirror the Go structs in internal/store and internal/collector. They
 * are hand-written rather than generated because the surface is small and stable,
 * and a wrong field name shows up immediately as a TypeScript error at the call
 * site.
 */

export interface Session {
  id: number
  sessionKey: string
  subsessionId: number
  sessionNum: number
  sessionType: string
  eventContext: string
  leagueId: number
  seriesId: number
  official: number
  trackId: number | null
  trackName: string | null
  trackConfig: string | null
  trackLengthKm: number | null
  carId: number | null
  carName: string | null
  carClassName: string | null
  startedAt: string
  endedAt: string | null
  connectedSeconds: number
  inCarSeconds: number
  drivingSeconds: number
  lapsCompleted: number
  incidents: number
  bestLapTimeS: number | null
  startingPosition: number | null
  finishPosition: number | null
  finishClassPosition: number | null
  qualifyPosition: number | null
  qualifyClassPosition: number | null
  qualifyBestTimeS: number | null
  fieldSize: number | null
  aiOpponentCount: number
  aiDetection: string | null
  incidentSource: string
  captureFile: string | null
}

export interface Lap {
  id: number
  sessionId: number
  lapNumber: number
  lapTimeS: number | null
  deltaToBestS: number | null
  fuelUsedL: number | null
  fuelLevelEndL: number | null
  incidentsOnLap: number
  isPitLap: boolean
  position: number | null
  classPosition: number | null
  recordedAt: string
}

export interface LapRow extends Lap {
  startedAt: string
  trackName: string
  carName: string
  sessionType: string
  eventContext: string
}

export type Cause = 'OnTrack' | 'OpponentPit' | 'OpponentOffWorld' | 'Unknown'

export interface PositionEvent {
  id: number
  sessionId: number
  lapNumber: number
  sessionTimeS: number
  fromPosition: number
  toPosition: number
  isClass: boolean
  opponentCarIdx: number | null
  opponentName: string | null
  cause: Cause
  recordedAt: string
}

export interface Totals {
  connectedHours: number
  inCarHours: number
  drivingHours: number
  utilisation: number
  incidentsPerHour: number
  sessions: number
  laps: number
  incidents: number
  passesMade: number
  timesPassed: number
}

export interface SummaryRow {
  key: string
  connectedHours: number
  inCarHours: number
  drivingHours: number
  sessions: number
  laps: number
  incidents: number
}

export interface BreakdownRow {
  group: string
  stack: string
  drivingHours: number
  sessions: number
  laps: number
}

export interface DailyRow {
  day: string
  drivingHours: number
}

export interface EntityRow {
  id: number
  name: string
  drivingHours: number
  sessions: number
  laps: number
}

export interface EntityStats {
  id: number
  name: string
  drivingHours: number
  inCarHours: number
  connectedHours: number
  sessions: number
  laps: number
  distanceKm: number
  incidentPoints: number
  incidentPointsPer100Km: number | null
  cleanLapPct: number | null
  cleanLaps: number
  timedLaps: number
  races: number
  wins: number
  podiums: number
  avgPositionsGained: number | null
}

export interface PaceRow {
  otherId: number
  otherName: string
  personalBestS: number | null
  bestInRangeS: number | null
  laps: number
  sessions: number
  consistencyPct: number | null
  consistencyDeltaS: number | null
}

export interface ProgressionRow {
  month: string
  bestLapS: number
  laps: number
}

export interface RivalRow {
  name: string
  passedThem: number
  lostTo: number
  net: number
}

export interface Racecraft {
  passesMade: number
  timesPassed: number
  races: number
  avgStartPosition: number | null
  avgFinishPosition: number | null
}

export interface QualiPace {
  pairs: number
  avgDeltaS: number | null
}

export interface ComboCell {
  combo: string
  category: string
  hours: number
  comboHours: number
}

/**
 * One observation of the driver's ratings, from the session that saw it.
 *
 * Both ratings are nullable: sessions recorded before LapDog tracked identity have
 * neither, and an offline session may carry an iRating with no licence string.
 */
export interface RatingPoint {
  startedAt: string
  sessionType: string
  eventContext: string
  iRating: number | null
  safetyRating: number | null
  licString: string | null
}

/**
 * The driver's identity and how their ratings moved across the filtered range.
 *
 * The deltas are computed by the server over the same range the points cover, so
 * the headline figures cannot disagree with the chart beside them. A null delta
 * means fewer than two observations — different from a delta of zero.
 */
export interface Ratings {
  userId: number | null
  licString: string | null
  iRating: number | null
  safetyRating: number | null
  iRatingDelta: number | null
  safetyRatingDelta: number | null
  peakIRating: number | null
  points: RatingPoint[]
}

export interface Facet {
  id: number
  name: string
  sessions: number
}

export interface Facets {
  tracks: Facet[]
  cars: Facet[]
  leagues: Facet[]
  sessionTypes: string[]
  eventContexts: string[]
  groupBy: string[]
  breakdownBy: string[]
}

export interface Status {
  connected: boolean
  paused: boolean
  intervalSeconds: number
  sessionKey: string
  sessionLabel: string
  trackName: string
  carName: string
  // All three accounted totals for the active session. Driving alone cannot be
  // interpreted: zero driving seconds is either a fault or a car sitting in the
  // pit box, and only the two figures beside it say which.
  connectedSeconds: number
  inCarSeconds: number
  drivingSeconds: number
  laps: number
  missingVars: string[] | null
  incidentSource: string
  sessionsRecorded: number
  version: string
  databasePath: string
  telemetry: Telemetry
}

/**
 * Telemetry describes where the readings come from.
 *
 * Read-only on purpose: the source is fixed by the simulator and the paths follow
 * from the data directory the platform dictates, so there is nothing here a user
 * could usefully change. It exists to be read out into a bug report.
 */
export interface Telemetry {
  source: string
  sourceKind: string
  available: boolean
  platform: string
  dataDir: string
  capturesDir: string
  logPath: string
}

/**
 * One frame as the simulator reported it.
 *
 * Every telemetry value is nullable because absent and zero differ: a speed of
 * zero is a stationary car, and a null speed is a variable that was not
 * published or not readable.
 */
export interface LiveFrame {
  at: string
  inCar: boolean
  driving: boolean
  replay: boolean
  /** Why driving time is not accruing; empty when it is. */
  reason: string
  lap: number | null
  lapDistPct: number | null
  lapCurrentTimeS: number | null
  lapLastTimeS: number | null
  lapBestTimeS: number | null
  speed: number | null
  gear: number | null
  fuelLevel: number | null
  incidents: number | null
}

export interface LiveResponse {
  frame: LiveFrame | null
  // The live endpoint serves collector.Status directly, not the statusResponse
  // wrapper /api/status adds — so version, databasePath and telemetry are never
  // present here. Omit rather than a hand-copied subset, so the two cannot drift
  // apart on the fields they do share.
  status: Omit<Status, 'version' | 'databasePath' | 'telemetry'>
  intervalSeconds: number
  /** Whether this build can read live telemetry at all. */
  supported: boolean
  platform: string
}

export interface Config {
  pollIntervalSeconds: number
  minSessionSeconds: number
  captureEnabled: boolean
  captureMaxBytes: number
  port: number
  startWithWindows: boolean
  units: 'metric' | 'imperial'
  theme: 'system' | 'light' | 'dark'
  debug: boolean
}

export interface SettingsResponse {
  config: Config
  restartRequired: string[] | null
}

export interface CaptureReindexFailure {
  file: string
  error: string
}

export interface CaptureReindexStatus {
  state: 'idle' | 'running' | 'complete' | 'failed'
  total: number
  processed: number
  replayed: number
  failed: number
  segments: number
  failures?: CaptureReindexFailure[]
  startedAt?: string
  finishedAt?: string
  error?: string
}

export interface ListResponse<T> {
  items: T[]
  total: number
}

/** Filter mirrors store.Filter. Undefined fields are omitted from the query. */
export interface Filter {
  from?: string
  to?: string
  sessionType?: string[]
  eventContext?: string[]
  trackIds?: number[]
  carIds?: number[]
  leagueId?: number
  /** Inclusive local hour-of-day bounds, 0..23. Either may stand alone. */
  hourFrom?: number
  hourTo?: number
  /** Local weekdays to keep, 0 (Sunday) through 6 (Saturday). Empty means all. */
  weekdays?: number[]
  excludeAi?: boolean
  limit?: number
  offset?: number
}

export interface LapFilter extends Filter {
  cleanLaps?: boolean
}

/** toQuery renders a Filter as URL search parameters. */
export function toQuery(f: Filter, extra: Record<string, string> = {}): string {
  const q = new URLSearchParams()
  if (f.from) q.set('from', f.from)
  if (f.to) q.set('to', f.to)
  if (f.sessionType?.length) q.set('session_type', f.sessionType.join(','))
  if (f.eventContext?.length) q.set('event_context', f.eventContext.join(','))
  if (f.trackIds?.length) q.set('track_id', f.trackIds.join(','))
  if (f.carIds?.length) q.set('car_id', f.carIds.join(','))
  if (f.leagueId != null) q.set('league_id', String(f.leagueId))
  if (f.hourFrom != null) q.set('hour_from', String(f.hourFrom))
  if (f.hourTo != null) q.set('hour_to', String(f.hourTo))
  if (f.weekdays?.length) q.set('weekday', f.weekdays.join(','))
  if (f.excludeAi) q.set('exclude_ai', 'true')
  if (f.limit != null) q.set('limit', String(f.limit))
  if (f.offset != null) q.set('offset', String(f.offset))
  for (const [k, v] of Object.entries(extra)) q.set(k, v)
  return q.toString()
}

/** ApiError carries the server's message so the interface can show it verbatim. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

async function get<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: { Accept: 'application/json' } })
  if (!res.ok) {
    // The server sends {"error": "..."} for every failure, so surface that rather
    // than a bare status code the user cannot act on.
    let msg = `${res.status} ${res.statusText}`
    try {
      const body = (await res.json()) as { error?: string }
      if (body.error) msg = body.error
    } catch {
      /* a non-JSON body leaves the status text as the message */
    }
    throw new ApiError(res.status, msg)
  }
  return (await res.json()) as T
}

export const api = {
  status: () => get<Status>('/api/status'),
  live: () => get<LiveResponse>('/api/live'),
  facets: () => get<Facets>('/api/facets'),

  totals: (f: Filter) => get<Totals>(`/api/totals?${toQuery(f)}`),
  summary: (f: Filter, groupBy: string) =>
    get<SummaryRow[]>(`/api/summary?${toQuery(f, { group_by: groupBy })}`),
  daily: (f: Filter) => get<DailyRow[]>(`/api/daily?${toQuery(f)}`),
  breakdown: (f: Filter, by: string) =>
    get<BreakdownRow[]>(`/api/breakdown?${toQuery(f, { by })}`),

  entities: (f: Filter, by: string) =>
    get<EntityRow[]>(`/api/entities?${toQuery(f, { by })}`),
  entity: (f: Filter, by: string, id: number) =>
    get<EntityStats>(`/api/entity?${toQuery(f, { by, id: String(id) })}`),
  pace: (f: Filter, by: string, id: number) =>
    get<PaceRow[]>(`/api/pace?${toQuery(f, { by, id: String(id) })}`),
  progression: (f: Filter, by: string, id: number, other: number) =>
    get<ProgressionRow[]>(
      `/api/progression?${toQuery(f, { by, id: String(id), other: String(other) })}`,
    ),
  rivals: (f: Filter) => get<RivalRow[]>(`/api/rivals?${toQuery(f)}`),
  racecraft: (f: Filter, by: string, id: number) =>
    get<Racecraft>(`/api/racecraft?${toQuery(f, { by, id: String(id) })}`),
  qualiPace: (f: Filter, by: string, id: number) =>
    get<QualiPace>(`/api/quali-pace?${toQuery(f, { by, id: String(id) })}`),
  // The heatmap's row cap is sent as `top`, not `limit`: Filter already has its
  // own `limit` (pagination, e.g. api.sessions), and toQuery's extra params
  // overwrite the filter's own via URLSearchParams.set — a Filter carrying a
  // `limit` would silently clobber this one under the same key.
  combos: (f: Filter, top = 10) =>
    get<ComboCell[]>(`/api/combos?${toQuery(f, { top: String(top) })}`),

  ratings: (f: Filter) => get<Ratings>(`/api/ratings?${toQuery(f)}`),

  sessions: (f: Filter) => get<ListResponse<Session>>(`/api/sessions?${toQuery(f)}`),
  session: (id: number) => get<Session>(`/api/sessions/${id}`),
  sessionLaps: (id: number) => get<Lap[]>(`/api/sessions/${id}/laps`),
  sessionPositions: (id: number) => get<PositionEvent[]>(`/api/sessions/${id}/positions`),

  laps: (f: LapFilter) => {
    const { cleanLaps, ...base } = f
    return get<ListResponse<LapRow>>(
      `/api/laps?${toQuery(base, cleanLaps ? { clean_laps: 'true' } : {})}`,
    )
  },

  settings: () => get<Config>('/api/settings'),
  captureReindexStatus: () => get<CaptureReindexStatus>('/api/captures/reindex'),
  startCaptureReindex: async (): Promise<CaptureReindexStatus> => {
    const res = await fetch('/api/captures/reindex', {
      method: 'POST',
      headers: { Accept: 'application/json' },
    })
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`
      try {
        const body = (await res.json()) as { error?: string }
        if (body.error) msg = body.error
      } catch {
        /* keep the status text */
      }
      throw new ApiError(res.status, msg)
    }
    return (await res.json()) as CaptureReindexStatus
  },
  saveSettings: async (patch: Partial<Config>): Promise<SettingsResponse> => {
    const res = await fetch('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    if (!res.ok) {
      let msg = `${res.status} ${res.statusText}`
      try {
        const body = (await res.json()) as { error?: string }
        if (body.error) msg = body.error
      } catch {
        /* keep the status text */
      }
      throw new ApiError(res.status, msg)
    }
    return (await res.json()) as SettingsResponse
  },

  /** exportUrl builds a download link, which the browser fetches directly. */
  exportUrl: (f: Filter, scope: string, format: string) =>
    `/api/export?${toQuery(f, { scope, format })}`,
}

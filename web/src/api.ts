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
  drivingSeconds: number
  laps: number
  missingVars: string[] | null
  incidentSource: string
  sessionsRecorded: number
  version: string
  databasePath: string
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
}

export interface SettingsResponse {
  config: Config
  restartRequired: string[] | null
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
  trackId?: number
  carId?: number
  leagueId?: number
  excludeAi?: boolean
  limit?: number
  offset?: number
}

/** toQuery renders a Filter as URL search parameters. */
export function toQuery(f: Filter, extra: Record<string, string> = {}): string {
  const q = new URLSearchParams()
  if (f.from) q.set('from', f.from)
  if (f.to) q.set('to', f.to)
  if (f.sessionType?.length) q.set('session_type', f.sessionType.join(','))
  if (f.eventContext?.length) q.set('event_context', f.eventContext.join(','))
  if (f.trackId != null) q.set('track_id', String(f.trackId))
  if (f.carId != null) q.set('car_id', String(f.carId))
  if (f.leagueId != null) q.set('league_id', String(f.leagueId))
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

  sessions: (f: Filter) => get<ListResponse<Session>>(`/api/sessions?${toQuery(f)}`),
  session: (id: number) => get<Session>(`/api/sessions/${id}`),
  sessionLaps: (id: number) => get<Lap[]>(`/api/sessions/${id}/laps`),
  sessionPositions: (id: number) => get<PositionEvent[]>(`/api/sessions/${id}/positions`),

  laps: (f: Filter) => get<ListResponse<LapRow>>(`/api/laps?${toQuery(f)}`),

  settings: () => get<Config>('/api/settings'),
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

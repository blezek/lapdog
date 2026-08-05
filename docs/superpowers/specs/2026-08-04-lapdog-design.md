# LapDog — Design Specification

**Date:** 2026-08-04
**Status:** Approved design, not yet implemented
**Target platform:** Windows (amd64, arm64)
**Development platform:** macOS (cross-compiled)

## 1. Purpose

LapDog is a Windows tray application that monitors the iRacing telemetry API and records how the user spends time in the simulator.

It answers two kinds of question. First, accounting: how many hours went to public practice versus race practice versus qualifying versus racing, this week, this season, at this track, in this car. Second, performance: how lap times trend, how consistent the user is, and in races, how often they pass versus get passed.

The application is entirely local. It reads shared memory written by the sim, writes to a local SQLite database, and serves a web UI on `localhost:47047`. There is no cloud component in this version, though the schema is prepared for one.

## 2. Scope

### In scope

- Windows tray application, single process, single binary.
- Web UI served on port 47047, bound to loopback only.
- Reading live iRacing telemetry from the shared memory-mapped file.
- Classifying sessions into a session type and an event context, including distinguishing AI races from races against humans.
- Recording three distinct time measures per session segment.
- Recording one row per completed lap.
- Recording qualifying result position and race finish position per session.
- Recording attributed position-change events during races.
- Recording capture files of polled telemetry, for offline development and testing.
- Data exploration UI: dashboard, session explorer, flat lap table, export screen, settings.
- Export to CSV and JSON.
- A development-only CLI for inspecting and authoring capture files.

### Out of scope for this version

- Uploading to a central hub. The schema carries the affordances (see §11.5) but no client, server, or auth is built.
- Reading iRacing's own `.ibt` telemetry files. Desirable later as a history-backfill feature; see §18.2.
- Storing high-rate telemetry sample streams. Only session-level and lap-level rows are stored.
- Team/multi-driver sessions. Only the local driver is recorded.
- Any non-Windows build of the tray application. The core packages must compile and test on macOS and Linux, but only Windows produces a working app.

## 3. Decision log

Every decision below was made deliberately during design. The rationale matters more than the choice, because it tells a future maintainer when the choice should be revisited.

| Decision | Choice | Rationale |
|---|---|---|
| Storage granularity | Session rows + lap rows + race position events | Supports every graph required without a sample stream. Database stays in the low megabytes per year. |
| Session classification | Two orthogonal fields: `session_type` and `event_context` | Derives "race practice vs public practice" and "league vs public race" without baking labels in at write time. New pivots need no migration. |
| Time measures | Three counters: connected, in-car, driving | Sitting in a practice server for two hours and driving for twenty minutes are different facts. Three counters cost almost nothing over one. |
| Frontend | React 19 + Vite + TypeScript | The hard part of this app is data manipulation, not rendering. React has the best off-the-shelf table and chart ecosystem. Bundle size is irrelevant for a localhost app. |
| Charting | Apache ECharts | Canvas rendering handles years of lap data. `dataZoom`/`brush` give time-range exploration free. Native calendar heatmap suits the core "time spent" visual. Toolbox provides CSV and PNG export. |
| Data grid | TanStack Table v8 (headless) | Sorting, multi-filter, grouping and aggregation without owning the markup. AG Grid was rejected because real pivoting is behind its paid tier. |
| Database | SQLite via `modernc.org/sqlite` | Real SQL for the aggregation queries the whole UI is built on. Pure Go, so `GOOS=windows go build` cross-compiles from macOS with no mingw toolchain. |
| Concurrency model | One writer connection, one reader pool, WAL | SQLite permits one writer and many concurrent readers. Serialising writes through a single collector-owned connection means `SQLITE_BUSY` never occurs by construction. |
| Provenance | Store the classification source YAML subset as JSON on the session row | Classification depends on YAML fields whose real-world behaviour cannot be fully verified until many varied sessions have been driven. Stored source makes a wrong rule fixable retroactively without re-driving. |
| Process topology | Single process: tray, HTTP server, collector as goroutines | A Windows Service split would survive UI crashes and logoff, but iRacing only runs while logged in. Not worth the service install, permissions and IPC complexity. |
| Capture | In-app, default on, one file per session segment | Capturing the frames the collector actually polled means a replay reproduces exactly what the collector saw, not an approximation. |
| Capture format | Raw variable-buffer bytes, interleaved with timestamped YAML frames | Replay feeds bytes through the same decoder as live, so tests cover binary layout parsing. |
| Capture retention | Size cap, default 2 GB, prune oldest first | Bounded by the resource that actually matters. `0` means unlimited. |
| Export formats | CSV and JSON | Both stream from a SQL query with stdlib only. Covers spreadsheets and programmatic use. Parquet and XLSX were rejected as dependency cost for little gain. |
| Position events | Attributed, with a cause tag; races only | A position change is not an overtake. Attributing the swap to a specific car and checking that car's state distinguishes real passes from other drivers' pit stops and retirements. |
| AI races | Separate `event_context = AI`, detected by runtime probe | Beating AI opponents is not comparable to beating humans, so mixing them would corrupt every pace and pass statistic. The detection field is unverified against the bundled docs (§6.5), which is survivable because stored provenance makes it re-classifiable. |
| Qualifying and finish positions | Stored on the session row | Both come from YAML already being parsed (`QualifyResultsInfo.Results[]` and `Sessions[].ResultsPositions[]`), so the cost is four columns and no new parsing. |
| Offline testing sessions | Always counted, no setting | User decision. |
| Replay playback time | Never counted, no setting | User decision. `IsReplayPlaying` is a hard exclusion in the collector. |
| Anonymisation | Never, anywhere | User decision. See §17 for the consequence. |
| UI layout | Hybrid: dashboard landing page plus a faceted session explorer | Chosen from three mocked alternatives. |

## 4. Architecture

### 4.1 Process and package structure

Single process. The tray owns process lifetime; the HTTP server and the collector run as goroutines.

```
cmd/lapdog                     tray app, built -H windowsgui
cmd/lapdogctl                  dev-only console tool (inspect, build, reclassify)

internal/irsdk                 Windows shared-memory reader. The ONLY OS-specific package.
internal/source                Source interface + live and replay implementations
internal/capture               capture file writer, reader, and NDJSON codec
internal/collector             poll loop, session state machine, time accounting, lap and position detection
internal/classify              pure function: session YAML -> (session_type, event_context)
internal/store                 SQLite access, migrations, aggregation queries
internal/api                   HTTP handlers, JSON endpoints, CSV/JSON export
internal/web                   embed.FS of the built frontend
internal/tray                  systray integration, icon state, menu
internal/config                config file load/save, live-reloadable fields

web/                           React + Vite + TypeScript source
web/dist/                      build output, embedded at compile time
testdata/                      .lpd capture fixtures and hand-authored NDJSON
```

The organising principle: **only `internal/irsdk` is Windows-specific.** Every package holding logic worth getting wrong — classification, time accounting, lap detection, position attribution, aggregation, export — sits behind the `Source` interface and is unit-tested on macOS against captured fixtures.

### 4.2 Data flow

```
iRacing sim
  └─ writes 60 Hz to Local\IRSDKMemMapFileName (triple-buffered)
       └─ internal/irsdk       maps the file, detects torn reads, decodes rows
            └─ Source          Next() (Frame, error)          [live | replay]
                 ├─ internal/capture     writes polled frames to .lpd
                 └─ internal/collector   state machine, three counters
                      ├─ internal/classify   YAML -> type + context
                      └─ internal/store      upsert session, insert laps/events
                           └─ lapdog.db
                                └─ internal/api    JSON + CSV/JSON export
                                     └─ internal/web -> browser
```

### 4.3 File locations

All under `%LOCALAPPDATA%\lapdog\`:

| Path | Contents |
|---|---|
| `lapdog.db` | SQLite database |
| `lapdog.db-wal`, `lapdog.db-shm` | WAL sidecar files |
| `config.json` | User settings |
| `lapdog.log` | Rotating log |
| `captures\<started>-<subsession>-<sessionnum>.lpd` | Capture files |

`%LOCALAPPDATA%` is deliberate: it is not synced by OneDrive. SQLite WAL requires a real local filesystem — the `-shm` shared-memory file misbehaves on SMB shares and under file-sync tools. The application must refuse to start with a clear error if the resolved database directory is a network path.

## 5. iRacing SDK integration

### 5.1 Shared memory access

The sim publishes a memory-mapped file named `Local\IRSDKMemMapFileName` and signals a Windows event `Local\IRSDKDataValidEvent` when new data is written.

Access via `golang.org/x/sys/windows`: `OpenFileMapping` then `MapViewOfFile`. No cgo.

Layout, per `documentation/irsdk_1_20/irsdk_defines.h`:

```
irsdk_header
  ver, status, tickRate
  sessionInfoUpdate, sessionInfoLen, sessionInfoOffset
  numVars, varHeaderOffset
  numBuf, bufLen, curBufTickCount, curBuf
  varBuf[4] { tickCount, bufOffset, tickCountBegin, pad }
```

`status & irsdk_stConnected` indicates the sim is live. `ver` should equal `IRSDK_VER` (2); a higher value is logged as a warning but not treated as fatal.

### 5.2 Torn read detection

Buffers are triple-buffered and the sim cannot know when a client finishes reading. Each `irsdk_varBuf` carries `tickCountBegin` (written before the sim starts writing) and `tickCount` (written after it completes).

Read procedure: select the buffer with the highest `tickCount`, copy `bufLen` bytes out, then re-read `tickCount` and `tickCountBegin`. If they no longer match the values observed before the copy, discard the frame and retry. A frame is never partially applied.

### 5.3 Runtime variable discovery

The available variable set depends on the car and session, and **has grown beyond the 2015 documentation**. The SDK samples reference `SessionLapsRemainEx`, `CarIdxLapCompleted`, `CarIdxBestLapNum` and `CarIdxBestLapTime`, none of which appear in `telemetry_11_23_15.pdf`.

Therefore the reader enumerates `irsdk_varHeader[numVars]` at connect time and builds a name-to-offset map. Variables are split into two sets.

**Required core.** All confirmed present in the 2015 documentation. If any is missing, the session is not recorded and the omission is logged and surfaced in Settings → Status. Recording wrong data is worse than recording none.

`SessionNum`, `SessionState`, `SessionTime`, `SessionTimeRemain`, `SessionLapsRemain`, `IsOnTrack`, `IsOnTrackCar`, `IsInGarage`, `IsReplayPlaying`, `OnPitRoad`, `Lap`, `LapCurrentLapTime`, `LapLastLapTime`, `LapBestLapTime`, `LapBestLap`, `LapDist`, `LapDistPct`, `FuelLevel`, `PlayerCarPosition`, `PlayerCarClassPosition`, `CarIdxTrackSurface`

`CarIdxTrackSurface` is in the core set rather than the race-only set because `driving_seconds` depends on it in every session type (§7.2). Only the player's own index is read outside races.

**Required for races only.** Read only when `session_type = Race`.

`CarIdxPosition`, `CarIdxClassPosition`, `CarIdxOnPitRoad`, `CarIdxLap`

**Optional, probed at runtime.** Used when present, silently skipped otherwise.

`PlayerCarMyIncidentCount` — preferred over the YAML incident count because it updates live rather than at session end.

### 5.4 Session YAML

`sessionInfoOffset` points to a YAML string of `sessionInfoLen` bytes. It is re-read only when `sessionInfoUpdate` increments, which is infrequent — it changes as drivers register and as results are posted.

Only a documented subset is parsed. The parser must tolerate unknown keys and missing keys rather than failing.

From `WeekendInfo`: `TrackID`, `TrackDisplayName`, `TrackDisplayShortName`, `TrackConfigName`, `TrackLength`, `SeriesID`, `SeasonID`, `SessionID`, `SubSessionID`, `LeagueID`, `Official`, `EventType`, `Category`, `SimMode`, `TeamRacing`.

From `SessionInfo.Sessions[]`: `SessionNum`, `SessionType`, `SessionLaps`, `SessionTime`, `ResultsOfficial`, `ResultsLapsComplete`, and `ResultsPositions[]` entries (`CarIdx`, `Position`, `ClassPosition`, `LapsComplete`, `Incidents`, `FastestTime`, `ReasonOutId`).

From `QualifyResultsInfo.Results[]`: `CarIdx`, `Position`, `ClassPosition`, `FastestLap`, `FastestTime`. This is the authoritative qualifying result and is documented in Appendix B.

From `DriverInfo`: `DriverCarIdx`, `DriverCarEstLapTime`, and for `Drivers[]`: `CarIdx`, `UserName`, `UserID`, `CarID`, `CarPath`, `CarScreenName`, `CarScreenNameShort`, `CarClassID`, `CarClassShortName`, `IRating`, `LicString`, and `CarIsAI` (unverified, see §6.5).

### 5.5 Real car and track names

Display names come directly from the YAML. No lookup table and no iRacing web API are needed.

| Field | Source | Example |
|---|---|---|
| `car_name` | `DriverInfo.Drivers[DriverCarIdx].CarScreenName` | `Porsche 911 GT3 R` |
| `car_class_name` | `CarClassShortName` | `GT3` |
| `car_id` | `CarID` | stable numeric id |
| `track_name` | `WeekendInfo.TrackDisplayName` | `Watkins Glen International` |
| `track_config` | `WeekendInfo.TrackConfigName` | `Boot` |
| `track_id` | `WeekendInfo.TrackID` | stable numeric id |

`car_id` and `track_id` are stored alongside the display names so that a rename upstream does not fragment history.

## 6. Session classification

`internal/classify` is a pure function with no I/O and no state: session YAML subset in, `(session_type, event_context)` out. This makes it exhaustively table-testable, which matters because it is the highest-risk logic in the application.

### 6.1 session_type

Read from `SessionInfo.Sessions[SessionNum].SessionType`, normalised.

| YAML value | `session_type` |
|---|---|
| `Practice`, `Open Practice`, `Lone Practice` | `Practice` |
| `Open Qualify`, `Lone Qualify`, `Qualify` | `Qualify` |
| `Race`, `Heat`, `Consolation` | `Race` |
| `Warmup` | `Warmup` |
| `Offline Testing`, `Testing` | `OfflineTest` |
| `Time Trial` | `TimeTrial` |
| anything else | `Unknown`, with the raw string logged |

### 6.2 event_context

Evaluated in order; first match wins.

1. `WeekendInfo.LeagueID != 0` → `League`
2. `ai_opponent_count > 0` → `AI`
3. `WeekendInfo.SimMode` is not `full`, or `session_type = OfflineTest` → `Offline`
4. `session_type = TimeTrial` → `TimeTrial`
5. `WeekendInfo.Official == 1` and the weekend contains a `Race` session → `OfficialRace`
6. `WeekendInfo.Official == 1` and it does not → `OfficialPractice`
7. otherwise → `Hosted`

`AI` is checked before `Offline` because an AI event is always offline, and "raced against AI" is the more specific and more useful fact. It is checked after `League` because a league session never contains AI opponents, so `League` winning is harmless and keeps league accounting intact.

`AI` is an `event_context` rather than a `session_type`, which means it composes correctly with the orthogonal model: an AI event's practice, qualifying and race sessions become AI Practice, AI Qualifying and AI Race without any new session types.

"The weekend contains a Race session" means any entry in `SessionInfo.Sessions[]` normalises to `session_type = Race`. This is what separates race practice from public practice: a practice session inside a weekend that also has a race is race practice; a practice-only weekend is public practice.

### 6.3 UI labels

Labels are computed in the UI from the pair, never stored.

| session_type | event_context | Label |
|---|---|---|
| Practice | OfficialPractice | Public Practice |
| Practice | OfficialRace | Race Practice |
| Practice | League | League Practice |
| Practice | Hosted | Hosted Practice |
| Qualify | OfficialRace | Qualifying |
| Qualify | League | League Qualifying |
| Race | OfficialRace | Race |
| Race | League | League Race |
| Race | Hosted | Hosted Race |
| Practice | AI | AI Practice |
| Qualify | AI | AI Qualifying |
| Race | AI | AI Race |
| any | Offline | Offline Testing |
| TimeTrial | TimeTrial | Time Trial |

Because AI is a distinct context, every pace, pass and finish-position statistic can exclude it with a single predicate. The UI defaults to excluding `event_context = 'AI'` from personal-best and pass/passed metrics, with a toggle to include it.

### 6.4 Re-classification

`classify_source_json` on each session row stores the exact `WeekendInfo`, `Sessions[]`, `QualifyResultsInfo` and `DriverInfo.Drivers[]` subset the classifier saw. `lapdogctl reclassify --db <path>` replays that JSON through the current `classify` and rewrites `session_type`, `event_context` and `ai_opponent_count` in place. This makes a wrong rule fixable without re-driving anything.

The stored subset must include the full `Drivers[]` array, not just the player's entry, precisely so that AI detection can be re-derived once the correct field is confirmed.

### 6.5 AI opponent detection — unverified

**This is the one deliberately unverified part of the design, and it is called out so it is not mistaken for established fact.**

AI racing shipped in iRacing in 2020. `documentation/telemetry_11_23_15.pdf` predates it by five years, and a search of `irsdk_1_20/` and `irsdk_csharp_2024_03_09/` finds no AI-related field, variable or enum. Real iRacing session YAML is understood to carry `DriverInfo.Drivers[].CarIsAI` as an integer flag, but that cannot be confirmed from the bundled documentation.

The implementation therefore probes rather than assumes:

1. Parse `DriverInfo.Drivers[]`. If any entry has a truthy `CarIsAI`, set `ai_opponent_count` to the number of such entries excluding the player.
2. If the field is absent from every entry, fall back to a heuristic: `session_type = Race`, `SubSessionID == 0`, `Official == 0`, `LeagueID == 0`, and more than one driver present. Record `ai_detection = 'heuristic'`.
3. If the field is present, record `ai_detection = 'field'`.

Storing which method was used matters: it makes it possible to find every heuristically-classified session later and re-classify only those.

**First implementation task for this feature is empirical.** Drive one AI race, capture it, and read what the YAML in the resulting `.lpd` actually contains. Then fix the field name, and run `reclassify`. The heuristic exists so the feature is useful before that happens, not as a permanent substitute.

> **Correction, 2026-08-04.** This step named `lapdogctl inspect`, and that subcommand was not built — the delivered CLI has `ingest`, `summary`, `reclassify`, `serve` and `version`. `inspect` has to be added before this procedure can be followed at all.

Note that the heuristic cannot distinguish an AI race from an offline hosted race with no AI, and will misclassify AI practice sessions as `Offline` because it requires `session_type = Race`. Both errors are corrected by `reclassify` once the field is confirmed.

## 7. Time accounting

### 7.1 Session segment identity

A session segment corresponds to one entry in `SessionInfo.Sessions[]` for one subsession. A new segment starts when `SubSessionID` changes, or when `SessionNum` changes within the same subsession.

Identity is stored as `session_key`:

- `"<subsession_id>/<session_num>"` when `subsession_id != 0`
- `"offline/<session_num>/<started_at>"` when `subsession_id == 0`

The fallback exists because offline sessions report `SubSessionID = 0`, which is not unique.

### 7.2 The three counters

At each poll, the elapsed wall time since the previous poll is added to whichever counters qualify. Elapsed time comes from the injected `Clock`, not `time.Now()`, so replay can run faster than real time.

| Counter | Accrues when |
|---|---|
| `connected_seconds` | Sim connected and this session segment is the active one |
| `in_car_seconds` | Above, and `IsOnTrackCar` is true |
| `driving_seconds` | Above, and `CarIdxTrackSurface[DriverCarIdx]` is neither `irsdk_NotInWorld` (-1) nor `irsdk_InPitStall` (1) |

`driving_seconds` therefore includes `OffTrack`, `AproachingPits` and `OnTrack` — the user is driving in all three. Sitting stationary in the pit stall is not driving.

`IsReplayPlaying == true` suppresses all three counters. There is no setting for this.

If the gap between polls exceeds four times the configured poll interval — machine sleep, sim hang, debugger pause — only one interval's worth of time is credited, and the anomaly is logged. This prevents an overnight suspend from being recorded as ten hours of practice.

### 7.3 Flush policy

The collector holds session state in memory and upserts the session row every ten seconds and on every state transition (session change, session end, sim disconnect, application shutdown). A crash loses at most ten seconds of accumulated time.

Time accounting is deliberately **not** re-derivable. If the counter logic proves wrong, already-recorded sessions stay wrong. This was an accepted trade for a much simpler schema; the mitigation is that the counters are simple and heavily unit-tested, whereas classification is complex and therefore given stored provenance.

## 8. Lap detection

A lap is recorded when `Lap` increments and `LapLastLapTime > 0`.

| Column | Source |
|---|---|
| `lap_number` | the `Lap` value before the increment |
| `lap_time_s` | `LapLastLapTime` |
| `delta_to_best_s` | `lap_time_s` minus the session's best at that moment; null for the first lap |
| `fuel_used_l` | `FuelLevel` at previous lap crossing minus `FuelLevel` now; null if a refuel occurred (fuel increased) |
| `fuel_level_end_l` | `FuelLevel` at the crossing |
| `incidents_on_lap` | delta in the incident count over the lap |
| `is_pit_lap` | `OnPitRoad` was true at any poll during the lap |
| `position`, `class_position` | `PlayerCarPosition`, `PlayerCarClassPosition` at the crossing |

Incident count comes from `PlayerCarMyIncidentCount` when the runtime probe finds it, since it updates live. Otherwise it falls back to the player's entry in `SessionInfo.Sessions[].ResultsPositions[].Incidents`, which only updates when the YAML does — meaning per-lap attribution is approximate in that case. The distinction is recorded in the log at session start.

Lap rows are inserted immediately, not buffered, so a crash mid-session loses no completed laps.

The 1 Hz default poll rate means a lap crossing is detected up to one second late. This does not affect `lap_time_s`, which the sim computes. It can misattribute an incident that occurs within one second of a crossing to the wrong lap. Acceptable, and it improves at faster poll rates.

## 9. Position events

Recorded only when `session_type = Race`. Position in practice is an artefact of who happens to be on track.

At each poll the collector compares `PlayerCarPosition` to its previous value. On a change it emits one row, and determines which car it swapped with by finding the `CarIdx` that now holds the player's former position.

The cause tag is derived from that opponent's state at the moment of the swap:

| Condition on the opponent | `cause` |
|---|---|
| `CarIdxOnPitRoad[i]` is true | `OpponentPit` |
| `CarIdxTrackSurface[i] == irsdk_NotInWorld` | `OpponentOffWorld` |
| neither | `OnTrack` |
| opponent could not be identified | `Unknown` |

Pass and passed counts filter on `cause = OnTrack`:

```sql
-- passes made and conceded, per lap, for one session
SELECT lap_number,
       SUM(CASE WHEN to_position < from_position THEN 1 ELSE 0 END) AS passes_made,
       SUM(CASE WHEN to_position > from_position THEN 1 ELSE 0 END) AS times_passed
FROM position_events
WHERE session_id = ? AND cause = 'OnTrack'
GROUP BY lap_number;
```

Positions gained or lost through `OpponentPit` and `OpponentOffWorld` are retained in the table but excluded from the ratio, so attrition is visible without polluting the metric.

Known limitation: at 1 Hz, two position changes within the same second collapse into one event, and the intermediate position is lost. A simultaneous multi-car shuffle may attribute the swap to the wrong opponent. This is inherent to polling and is documented rather than solved.

## 10. Capture files

### 10.1 Purpose

Capture files let the collector be developed and tested on macOS against real iRacing data. Capture is a feature of the application itself, not a separate tool, and it records **the frames the collector polled** — so replaying a capture reproduces exactly what the collector saw.

Default on. One file per session segment, in `%LOCALAPPDATA%\lapdog\captures\`, named `<started_at>-<subsession_id>-<session_num>.lpd`. This makes one fixture equal one session row.

### 10.2 Format

```
file  = magic(8) || gzip(record*)
magic = "LPDCAP" 0x01 0x00          -- outside the gzip stream, so the file is identifiable

record = kind(uint8) || t(float64 LE) || len(uint32 LE) || payload
  t = seconds since capture start

kind 1  header    payload = JSON { tickRate, numVars, bufLen, varHeaders: [...] }
kind 2  session   payload = sessionInfoUpdate(uint32 LE) || raw YAML bytes
kind 3  vars      payload = tickCount(uint32 LE) || raw varBuf row (bufLen bytes)
```

A `kind 1` record is always first. `kind 2` is written when `sessionInfoUpdate` changes. `kind 3` is written once per poll.

Storing the raw variable-buffer row rather than decoded values means replay exercises the binary layout decoder, so tests cover it.

Expected size at 1 Hz, gzipped: roughly 0.5–2 MB per hour of driving.

### 10.3 Retention

Total size of the captures directory is capped, default 2 GB, configurable, `0` meaning unlimited. Pruning deletes oldest-first, runs at application start and after each session closes, and never deletes the file currently being written.

### 10.4 Development CLI

`cmd/lapdogctl` is a console binary, not shipped in releases.

| Command | Purpose |
|---|---|
| `lapdogctl inspect <file.lpd>` | Dump the capture as readable NDJSON, one object per frame, variables decoded. **Not built as of 2026-08-04.** |
| `lapdogctl build <file.ndjson> -o <file.lpd>` | Compile hand-written NDJSON into a capture, for authoring synthetic edge cases |
| `lapdogctl reclassify --db <path>` | Replay `classify_source_json` through the current classifier |

`build` is what makes edge cases reachable: a league race, a session ending mid-lap, a driver disconnecting mid-pass can all be hand-authored rather than driven.

## 11. Data model

SQLite, WAL mode. Timestamps are RFC3339 UTC strings. Durations are seconds as `REAL`.

### 11.1 sessions

```sql
CREATE TABLE sessions (
  id                    INTEGER PRIMARY KEY,
  uuid                  TEXT    NOT NULL UNIQUE,
  session_key           TEXT    NOT NULL UNIQUE,
  subsession_id         INTEGER NOT NULL DEFAULT 0,
  session_num           INTEGER NOT NULL,
  session_type          TEXT    NOT NULL,
  event_context         TEXT    NOT NULL,
  league_id             INTEGER NOT NULL DEFAULT 0,
  series_id             INTEGER NOT NULL DEFAULT 0,
  season_id             INTEGER NOT NULL DEFAULT 0,
  official              INTEGER NOT NULL DEFAULT 0,
  track_id              INTEGER,
  track_name            TEXT,
  track_config          TEXT,
  track_length_km       REAL,
  car_id                INTEGER,
  car_name              TEXT,
  car_class_id          INTEGER,
  car_class_name        TEXT,
  started_at            TEXT    NOT NULL,
  ended_at              TEXT,
  connected_seconds     REAL    NOT NULL DEFAULT 0,
  in_car_seconds        REAL    NOT NULL DEFAULT 0,
  driving_seconds       REAL    NOT NULL DEFAULT 0,
  laps_completed        INTEGER NOT NULL DEFAULT 0,
  incidents             INTEGER NOT NULL DEFAULT 0,
  best_lap_time_s       REAL,
  starting_position     INTEGER,
  finish_position       INTEGER,
  finish_class_position INTEGER,
  qualify_position      INTEGER,
  qualify_class_position INTEGER,
  qualify_best_time_s   REAL,
  field_size            INTEGER,
  ai_opponent_count     INTEGER NOT NULL DEFAULT 0,
  ai_detection          TEXT,                            -- 'field' | 'heuristic' | NULL
  incident_source       TEXT    NOT NULL DEFAULT 'yaml',  -- 'live' | 'yaml'
  classify_source_json  TEXT    NOT NULL,
  capture_file          TEXT,
  created_at            TEXT    NOT NULL,
  updated_at            TEXT    NOT NULL,
  uploaded_at           TEXT
);

CREATE INDEX idx_sessions_started  ON sessions(started_at);
CREATE INDEX idx_sessions_type_ctx ON sessions(session_type, event_context);
CREATE INDEX idx_sessions_track    ON sessions(track_id);
CREATE INDEX idx_sessions_car      ON sessions(car_id);
CREATE INDEX idx_sessions_upload   ON sessions(uploaded_at);
CREATE INDEX idx_sessions_ai       ON sessions(ai_detection);
```

### 11.1.1 Result positions

Four position values are stored, and they mean different things. Conflating them is the likely bug.

| Column | Source | Meaning |
|---|---|---|
| `qualify_position` | `QualifyResultsInfo.Results[]` matched on `DriverCarIdx` | Where the user qualified for the weekend. Present on the qualifying session row *and* copied onto the race session row of the same subsession, so a race can be analysed without a join. |
| `qualify_class_position` | same, `ClassPosition` | Class-relative qualifying result, for multi-class events |
| `qualify_best_time_s` | same, `FastestTime` | The lap that set the grid slot |
| `starting_position` | `PlayerCarPosition` at the first poll where `SessionState >= irsdk_StateRacing` | Where the user actually started, which differs from `qualify_position` after a pit-lane start, a penalty, or a grid drop |
| `finish_position` | `Sessions[session_num].ResultsPositions[]` matched on `DriverCarIdx` | Final classified position |
| `finish_class_position` | same, `ClassPosition` | Final class position |
| `field_size` | count of `ResultsPositions[]`, else count of non-spectator `Drivers[]` | Context for the positions — P5 of 6 is not P5 of 40 |

`QualifyResultsInfo` is only populated once qualifying has run, and `ResultsPositions` only fills in as the session concludes. Both are therefore read on every session-YAML update and written on each flush, not once at session start. All are nullable, because a practice session has none of them.

Because these are captured from YAML that `classify_source_json` already stores, they are re-derivable by `reclassify` in the same way the classification is.

### 11.2 laps

```sql
CREATE TABLE laps (
  id               INTEGER PRIMARY KEY,
  uuid             TEXT    NOT NULL UNIQUE,
  session_id       INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  lap_number       INTEGER NOT NULL,
  lap_time_s       REAL,
  delta_to_best_s  REAL,
  fuel_used_l      REAL,
  fuel_level_end_l REAL,
  incidents_on_lap INTEGER NOT NULL DEFAULT 0,
  is_pit_lap       INTEGER NOT NULL DEFAULT 0,
  position         INTEGER,
  class_position   INTEGER,
  recorded_at      TEXT    NOT NULL,
  uploaded_at      TEXT,
  UNIQUE(session_id, lap_number)
);

CREATE INDEX idx_laps_session ON laps(session_id, lap_number);
CREATE INDEX idx_laps_time    ON laps(lap_time_s);
```

### 11.3 position_events

```sql
CREATE TABLE position_events (
  id               INTEGER PRIMARY KEY,
  uuid             TEXT    NOT NULL UNIQUE,
  session_id       INTEGER NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  lap_number       INTEGER NOT NULL,
  session_time_s   REAL    NOT NULL,
  from_position    INTEGER NOT NULL,
  to_position      INTEGER NOT NULL,
  is_class         INTEGER NOT NULL DEFAULT 0,
  opponent_car_idx INTEGER,
  opponent_name    TEXT,
  cause            TEXT    NOT NULL,   -- OnTrack | OpponentPit | OpponentOffWorld | Unknown
  recorded_at      TEXT    NOT NULL,
  uploaded_at      TEXT
);

CREATE INDEX idx_pos_session ON position_events(session_id, lap_number);
CREATE INDEX idx_pos_cause   ON position_events(cause);
```

### 11.4 schema_version

```sql
CREATE TABLE schema_version (version INTEGER NOT NULL);
```

Migrations are embedded SQL files applied in order inside a transaction. Downgrade is not supported; the application refuses to start against a newer schema than it knows.

### 11.5 Sync affordances

`uuid` on every table and `uploaded_at` on every table exist solely so that a future hub uploader can be added without a migration. Nothing writes `uploaded_at` in this version. `updated_at` on `sessions` is monotonic and suitable as a sync cursor.

### 11.6 Representative queries

```sql
-- hours per category, for the dashboard stacked bar
SELECT strftime('%Y-%W', started_at) AS week,
       session_type, event_context,
       SUM(driving_seconds) / 3600.0 AS hours
FROM sessions
WHERE started_at >= ?
GROUP BY week, session_type, event_context;

-- calendar heatmap
SELECT date(started_at) AS day, SUM(driving_seconds) / 3600.0 AS hours
FROM sessions
WHERE started_at >= ?
GROUP BY day;

-- utilisation
SELECT SUM(driving_seconds) * 1.0 / NULLIF(SUM(connected_seconds), 0) AS utilisation
FROM sessions WHERE started_at >= ?;

-- lap time trend at one track in one car, valid laps only
SELECT l.recorded_at, l.lap_time_s
FROM laps l JOIN sessions s ON s.id = l.session_id
WHERE s.track_id = ? AND s.car_id = ? AND l.is_pit_lap = 0 AND l.incidents_on_lap = 0
ORDER BY l.recorded_at;

-- pass/passed ratio, human races only
SELECT SUM(CASE WHEN pe.to_position < pe.from_position THEN 1 ELSE 0 END) AS made,
       SUM(CASE WHEN pe.to_position > pe.from_position THEN 1 ELSE 0 END) AS conceded
FROM position_events pe JOIN sessions s ON s.id = pe.session_id
WHERE pe.cause = 'OnTrack' AND s.event_context <> 'AI';

-- grid-to-finish movement per race, human races only
SELECT started_at, track_name, field_size,
       qualify_position, starting_position, finish_position,
       starting_position - finish_position AS places_gained
FROM sessions
WHERE session_type = 'Race' AND event_context <> 'AI'
  AND finish_position IS NOT NULL
ORDER BY started_at DESC;
```

## 12. HTTP API

`net/http`, bound to `127.0.0.1:47047`. Loopback-only binding is the security model: the server is unreachable from other machines, so there is no authentication. The bind address is not configurable — making it configurable would let a user expose their data unintentionally.

All list endpoints accept the same filter parameters: `from`, `to`, `session_type`, `event_context`, `track_id`, `car_id`, `league_id`.

| Method | Path | Purpose |
|---|---|---|
| GET | `/api/status` | Connection state, poll rate, active session, version, capture folder size |
| GET | `/api/summary` | Aggregates for the KPI row; `group_by` one of `type`, `context`, `track`, `car`, `week`, `month` |
| GET | `/api/daily` | Per-day driving hours for the calendar heatmap |
| GET | `/api/sessions` | Filtered session list with a total match count |
| GET | `/api/sessions/{id}` | One session with its three counters |
| GET | `/api/sessions/{id}/laps` | Laps for one session |
| GET | `/api/sessions/{id}/positions` | Position events for one session |
| GET | `/api/laps` | Flat filtered lap list across sessions |
| GET | `/api/positions/summary` | Pass/passed aggregates |
| GET | `/api/facets` | Distinct tracks, cars, leagues, types for filter controls |
| GET | `/api/settings` | Current configuration |
| PUT | `/api/settings` | Update configuration; response states which fields need a restart |
| GET | `/api/export` | `scope` one of `sessions`, `laps`, `positions`; `format` `csv` or `json`; same filters |

Export streams directly from a SQL cursor with `encoding/csv` or `encoding/json` — no full result set in memory, so a multi-year export does not balloon RSS. Exports honour the active filter, which is why the filter parameters are shared.

## 13. Frontend

React 19, Vite, TypeScript. Built to `web/dist` and embedded with `embed.FS`, so the release is a single `.exe`.

- **ECharts** for all charts, via `echarts-for-react`, with tree-shaken imports.
- **TanStack Table v8** headless for every table.
- **TanStack Query** for server state, caching and refetch.

### 13.1 Pages

| Page | Contents |
|---|---|
| Dashboard | KPI row (total hours, laps, incidents/hr, utilisation, pass/passed ratio), calendar heatmap of driving hours, hours-by-category stacked bar, lap-time trend, grid-to-finish movement |
| Sessions | Faceted filter panel with live match count → session list → detail pane with the three counters, qualifying and finish positions, lap chart, lap table, position events |
| Laps | One flat sortable, filterable table of every lap ever, for cross-session comparison |
| Export | Choose scope and format; shows the row count the current filter matches before downloading |
| Settings | Grouped as Telemetry, Recording, Application, Data, Status |

A persistent left sidebar carries navigation; the date/scope filter persists across pages.

### 13.2 Visualisation rules

The chart set follows the project's data-visualisation standard. Decisions already made:

| Data's job | Form | Colour job |
|---|---|---|
| Hours per category over time | stacked bar | categorical |
| Driving hours per day across a year | calendar heatmap | sequential, one hue |
| Headline totals with deltas | KPI row of stat tiles | text ink only, no series colour |
| Lap time trend | line, with the personal best emphasised and the rest recessive | one hue plus grey |
| driving ÷ connected | meter | sequential track |
| Grid slot → finish position per race | dumbbell | one hue, two shades |
| Session, lap, position records | table | none |

Binding constraints:

- **No dual-axis charts anywhere.** Two measures of different scale become two charts or get indexed to a common base.
- **No pie charts.** Part-to-whole is a stacked bar.
- Categorical hues are assigned in fixed order and never cycled. The category axis has up to nine values; past eight, the tail folds into "Other".
- Sequential encoding is one hue, light to dark. Diverging encoding is two hues with a neutral grey midpoint.
- Any categorical palette is validated with the project's palette validator before shipping — CVD ΔE ≥ 8 on adjacent pairs, normal-vision ΔE ≥ 15. Not eyeballed.
- For two or more series a legend is always present, and four or fewer are also directly labelled, so identity is never carried by colour alone.
- Dark mode steps come from the same ramps re-validated against the dark surface, not an automatic inversion.
- Every chart ships a hover layer: crosshair and tooltip on lines, per-mark tooltip on bars and cells.
- Every chart has a table view, so the data is reachable without reading colour.

## 14. Settings

Stored as `config.json`. Poll interval and capture settings take effect live; port changes require a restart, which the UI states explicitly.

| Group | Setting | Default | Notes |
|---|---|---|---|
| Telemetry | Poll interval | 1.0 s | Range 0.25 s – 30 s |
| Telemetry | Minimum session length | 30 s | Sessions shorter than this are discarded, dropping accidental joins |
| Recording | Record capture files | on | |
| Recording | Capture size cap | 2 GB | `0` means unlimited |
| Recording | Captures folder | — | Shows file count and total size; reveal button |
| Application | Web UI port | 47047 | Restart required |
| Application | Start with Windows | on | `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` |
| Application | Units | Metric | Initialised from the sim's `DisplayUnits` on first run |
| Application | Theme | System | Light, Dark, System |
| Data | Database | — | Shows path and size; reveal and back-up buttons |
| Status | iRacing | — | Connection state, effective poll rate, active session, incident source |
| Status | Version and log | — | Version string, log path, view-log button |

Deliberately absent, per explicit decision: no offline-testing toggle (always counted), no replay-time toggle (never counted), no anonymisation toggle (never anonymised).

## 15. Tray

`fyne.io/systray`, which is pure Go on Windows.

Left click opens the default browser at `http://localhost:47047`. Right click opens the menu:

```
LapDog · Connected                (header, non-interactive)
Race Practice · Watkins Glen
Driving 1:12 · 38 laps
─────────────────────────
Open LapDog
Pause recording
─────────────────────────
Settings…
Open data folder
─────────────────────────
Quit
```

The icon has three states: connected, disconnected, paused. "Pause recording" stops the collector and capture without exiting, and the paused state is not persisted across restarts.

## 16. Error handling

The governing rule: **iRacing not running is the normal state, not an error.** Every failure degrades toward "keep the tray alive, keep serving the UI."

| Failure | Response |
|---|---|
| Shared memory absent | Retry every 5 s. Tray shows disconnected. Logged once per state change, not per attempt. |
| `status & irsdk_stConnected` clear | Treated as disconnected. Close the active session segment. |
| Torn read | Discard the frame, retry next poll. Never partially applied. |
| Header `ver` higher than expected | Log a warning, continue. The layout has been stable. |
| Required variable missing | Refuse to record the session. Log which variable. Surface in Settings → Status. Recording wrong data is worse than recording none. |
| Session YAML unparseable | Record with `event_context = Unknown`, store the raw YAML in `classify_source_json` so `reclassify` can fix it later. |
| Poll gap > 4× interval | Credit one interval only. Log the anomaly. Prevents a machine suspend becoming ten hours of practice. |
| SQLite busy or locked | Retry with backoff. Single-writer discipline means this should be impossible, so log it as a genuine anomaly. |
| Database corrupt, or migration fails | Do not start the collector. Keep the UI up showing the error and the database path. Never auto-delete or auto-repair. |
| Database directory is a network path | Refuse to start with a clear message. WAL is unsafe there. |
| Port 47047 in use | Show a tray notification naming the conflict, and keep the collector recording — losing the UI must not lose session data. The tray menu offers to change the port. No silent fallback port, because the user would never find the UI. |
| Capture write fails, or disk full | Disable capture for the remainder of the run, keep recording to the database. Capture must never cost session data. |
| Frontend asset missing from `embed.FS` | Build-time failure, not runtime. The build must fail if `web/dist` is empty. |

Logging is levelled and rotating, to `%LOCALAPPDATA%\lapdog\lapdog.log`. Steady-state operation is silent; anything logged at warning or above is meant to be actionable.

## 17. Privacy

There is no anonymisation anywhere, by explicit decision. Two consequences are recorded here so they are not rediscovered as surprises.

Capture files contain the full session YAML, which includes every driver's `UserName`, `UserID`, `IRating` and licence class. Capture fixtures committed to the repository will therefore contain other people's personal data, publicly, in version control.

`position_events.opponent_name` stores the name of the driver involved in each swap, and exports include it.

## 18. Future work

Recorded so the reasoning is not lost, and deliberately not built now.

### 18.1 Central hub upload

Schema affordances are in place (§11.5): `uuid` on every row, `uploaded_at` on every row, monotonic `updated_at` on `sessions`. Building this means a client uploader, a server API, authentication and a conflict-resolution story. The hub is effectively a second application and needs its own spec.

### 18.2 `.ibt` import for history backfill

Users accumulate `.ibt` files from iRacing's own alt-L logging. Reading them could backfill lap history from before LapDog was installed.

It was rejected as the *capture* format for reasons that remain true and are worth recording, verified against `irsdk_diskclient.cpp:75-97` and `irsdkDiskReader.cs:308-379`:

- An `.ibt` file holds exactly **one** session-YAML snapshot, read once at open. Results fields (`ResultsPositions`, `ResultsOfficial`, `ResultsLapsComplete`) are still empty at logging start, and mid-session driver changes never appear. Classification cannot be exercised against it.
- `.ibt` is written only while the driver is in the car, and stops and restarts across garage transitions — so it cannot see `connected_seconds` at all.
- Records carry no per-record timestamp; time must be inferred from `SessionTime` plus `diskSubHeader.sessionStartDate`.
- Fixed 60 Hz, so an hour is on the order of a gigabyte. Not committable as a fixture.
- Not hand-authorable, so synthetic edge cases are unreachable.

As an *import* path none of that is disqualifying, because lap times and the fields needed for a coarse session row are present. It remains genuinely useful and should be its own spec.

### 18.3 Deferred smaller items

- True overtake detection from `CarIdxLapDistPct` ordering rather than position deltas, which would survive sub-second multi-car shuffles.
- AI difficulty and roster detail once §6.5 is resolved, if the YAML exposes a skill level. Would let AI results be compared to each other meaningfully rather than only excluded.
- ~~Per-opponent head-to-head records, since `position_events.opponent_name` is already stored.~~ **Delivered 2026-08-05**; see `2026-08-05-lapdog-cars-tracks-design.md` §5.7.
- Sector times from `SplitTimeInfo.Sectors[]`.
- Setup tracking via `DriverSetupName` and `DriverSetupIsModified`.
- Weather correlation using `TrackTemp`, `AirTemp`, `TrackWetness`.

## 19. Build and packaging

```bash
# frontend, first
cd web && npm ci && npm run build          # -> web/dist, embedded

# Windows binary, cross-compiled from macOS, no cgo
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags "-H windowsgui -s -w" -o dist/lapdog.exe ./cmd/lapdog

# dev CLI, host platform, console subsystem
go build -o dist/lapdogctl ./cmd/lapdogctl

# tests, on macOS, no sim required
go test ./...
```

`cmd/lapdog` must be linked `-H windowsgui` so no console window appears. That is precisely why `lapdogctl` is a separate binary: a GUI-subsystem executable has no console and is useless as a CLI.

`CGO_ENABLED=0` is load-bearing. It is the reason `modernc.org/sqlite` was chosen over `mattn/go-sqlite3`, and it is what makes a Mac-only toolchain sufficient.

The build must fail if `web/dist` is empty, rather than embedding nothing and shipping a blank UI.

## 20. Testing strategy

| Package | Approach |
|---|---|
| `internal/classify` | Table-driven over hand-authored YAML fixtures. Densest coverage in the project: every `session_type` × `event_context` combination, plus malformed, empty and unknown-value inputs. Highest-risk logic. |
| `internal/collector` | Driven by the replay `Source` against `.lpd` fixtures with an injected `Clock`. Asserts all three counters, lap detection, position attribution, session boundaries, and the poll-gap clamp. Runs on macOS. |
| `internal/capture` | Round-trip: write → read → NDJSON → `build` → read, asserting byte equality of variable rows. |
| `internal/irsdk` | Byte-layout decoding tested on any OS against a synthetic buffer built from a known `varHeader`, including torn-read rejection. The `MapViewOfFile` path itself requires Windows and is the only Windows-gated test. |
| `internal/store` | Migrations up from empty; every §11.6 query against a seeded database. |
| `internal/api` | `httptest` against a seeded database. CSV and JSON export compared byte-for-byte against golden files. |
| End-to-end | Replay a captured real race weekend through collector → store → API and assert the dashboard aggregates. |
| Frontend | Vitest for aggregation and formatting helpers. No browser-driving tests. |

Fixtures live in `testdata/`. At minimum, captured or authored: a public practice session, a race weekend with practice + qualify + race, a league race, an offline test session, **an AI race**, a session ending mid-lap, and a session with a missing required variable.

Two AI fixtures are required, not one: a real captured AI race to confirm the `CarIsAI` field, and a hand-authored one with the field absent to exercise the heuristic fallback path.

## 21. Risks

| Risk | Mitigation |
|---|---|
| **AI detection field name is unverified** (§6.5) | Runtime probe with a documented heuristic fallback; `ai_detection` records which was used; `reclassify` corrects history once confirmed. First implementation step is to capture one AI race and inspect the YAML. |
| Classification rules wrong in ways only real sessions reveal | `classify_source_json` plus `lapdogctl reclassify` make history fixable without re-driving |
| `event_context` heuristics may not cover every iRacing session kind | `Unknown` is a valid stored value, not a crash; the raw YAML is retained |
| Time accounting is not re-derivable | Counters kept deliberately simple and heavily unit-tested; the complex logic got the provenance instead |
| Variable set drifts as iRacing updates | Runtime discovery with an explicit required set; missing required variables refuse the session loudly rather than silently degrading |
| `modernc.org/sqlite` is a source translation, not upstream SQLite | Widely deployed; `ncruces/go-sqlite3` (official SQLite on WASM) is the fallback if a divergence bites |
| Capture files grow unattended | 2 GB size cap, prune oldest first, runs at start and after each session |
| Only `irsdk` needs Windows, but it is the least testable part | Kept deliberately thin (~200 LOC) with layout tests that run anywhere |

## 22. References

| Document | Location | Use |
|---|---|---|
| Telemetry reference and YAML schema | `documentation/telemetry_11_23_15.pdf` | Variable list (Appendix A), session string (Appendix B). Predates several variables now in the SDK. |
| SDK header, current | `documentation/irsdk_1_20/irsdk_defines.h` | Struct layout, enums, constants |
| Live client reference | `documentation/irsdk_1_20/irsdk_client.cpp` | Connection and buffer selection |
| Disk client reference | `documentation/irsdk_1_20/irsdk_diskclient.cpp` | `.ibt` layout, cited in §18.2 |
| Lap timing sample | `documentation/irsdk_1_20/irsdk_lapTiming/lapTiming.cpp` | Lap and position handling patterns |
| C# SDK port | `documentation/irsdk_csharp_2024_03_09/` | Newer variable names; confirms drift past the 2015 PDF |

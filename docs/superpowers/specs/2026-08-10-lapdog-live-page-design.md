# Live page design

**Goal:** a page that answers "is LapDog reading telemetry right now, and what is it seeing", showing enough of what is being captured to be informative without becoming a driving HUD or a raw variable dump.

## Why this exists

The last week was spent establishing whether live telemetry worked at all, and the only instrument was a 641-line log file read after the fact. Twice the answer was in the data and invisible: a `MapViewOfFile` bug that recorded nothing, and a log line that claimed a connection it had not made. Neither needed a debugger — both needed a way to see the current state.

A second problem is narrower and sharper. The 2026-08-06 test recorded 154 seconds in the car and **zero driving seconds**, which is correct — the car was in the pit box — but the only way to know that was to read `internal/collector/accounting.go`. A page that says *why* time is not being credited turns that from a mystery into an observation.

The page is therefore for confidence first and information second. It is explicitly **not** a HUD, and not `lapdogctl inspect` in a browser.

## Route and navigation

`/live`, added as the **first** item in the left nav, above Dashboard. It is the only page about *now*; every other page is about the past.

## Layout: five stacked bands

Full-width bands in a single column, reusing the dashboard's existing card stack so no new layout primitives are needed. Ordered by urgency, so the answer to "is it working" is the first thing read.

| Band | Content |
|---|---|
| **Verdict** | `Recording` / `Not reading` / `Waiting for iRacing`, and the poll interval |
| **State** | Connected · In car · Driving, plus the reason when driving is false |
| **Totals** | connected / in car / driving seconds for the active session |
| **Lap** | lap number, lap-distance bar, current lap time, last, best, laps completed |
| **Vitals** | speed, gear, fuel, incidents |

Sketch, in the pit-box case that motivated the page:

```
● Recording                                    reading every 1s
AI Practice · Okayama International Circuit · BMW M2 CS Racing
─────────────────────────────────────────────────────────────────
● Connected   ● In car   ○ Driving
Not counting driving time — car is in the pit box.
─────────────────────────────────────────────────────────────────
connected 2:35    in car 2:34    driving 0:00
─────────────────────────────────────────────────────────────────
lap 3                                                    1:38.4
▓▓▓▓▓▓▓▓░░░░░░░░░░░░
last 1:41.902        best 1:38.117        2 laps
─────────────────────────────────────────────────────────────────
speed 128 km/h    gear 4    fuel 38.2 L    inc 0
```

## Degradation: the rule that matters most

**Accumulated totals are not stale; instantaneous values are.** "2:34 in the car" stays true after frames stop, because it is a record of what happened. "128 km/h" is false the moment frames stop. The two must be treated differently, or the page joins the list of things in this project that displayed state outliving what it referred to.

Three states:

**Live** — frames arriving within the last few poll intervals. Everything shown.

**Stale** — frames were arriving and stopped. The verdict becomes `Not reading`, with the age of the last frame. Instantaneous values clear to `—`; the lap bar empties. **Totals remain**, with a note that they are what was accumulated rather than what is happening. Structure is preserved so the page does not jump when frames resume.

**Idle** — no simulator at all. The five bands collapse to a single panel: `Waiting for iRacing`, the poll interval, and a line of context from the database (when the last session was recorded, how many exist). This is the page's most common state by a wide margin, since iRacing is closed most of the time, and five empty bands would be a poor way to spend it.

Staleness threshold: **three poll intervals**, floored at two seconds. Derived from the interval rather than fixed, because a 30-second poll rate would otherwise show a permanent false `Not reading`.

**Unsupported platform** — on anything other than Windows there is no live source at all, which is a different fact from idle. The page says so, reusing the wording Settings already uses: it reports that this build cannot read telemetry on this operating system rather than implying a simulator might appear.

## What has to change behind it

### The collector must retain a frame snapshot

Today `Sample` is `{T, InCar, Driving, Replay}` and the row is discarded after `handle()`. Nothing instantaneous survives, so there is nothing for an endpoint to serve. This is the bulk of the work.

A `LiveFrame` struct, guarded by the existing `mu`, written on each handled frame and read by the API. Fields, all from variables the collector already reads or that `RequiredCoreVars` already demands:

- `At` — wall-clock time the frame was handled, so the interface can compute staleness itself
- `InCar`, `Driving`, `Replay`, and `NotDrivingReason`
- `Lap`, `LapDistPct`, `LapCurrentTimeS`, `LapLastTimeS`, `LapBestTimeS`
- `Speed`, `Gear`, `FuelLevel`, `Incidents`

Cleared by `clearActiveStatus` alongside the session fields it already blanks, so a finished session leaves nothing behind.

### "Why driving is false" has to be derived

`Driving` is a boolean; the reason is not currently computed anywhere. No new telemetry is required — the reason must be derived from **exactly the values `SampleFrom` already uses**, or the explanation could contradict the boolean it explains. That is the failure this project has spent the week fixing, so it is worth stating as a rule rather than a preference.

`SampleFrom` (`internal/collector/accounting.go:133`) reads `IsOnTrackCar`, `IsReplayPlaying`, and `CarIdxTrackSurface[driverCarIdx]` as an `irsdk.TrkLoc`. It sets `Driving = loc != NotInWorld && loc != InPitStall`. So when driving is false, `loc` is one of exactly two values.

The reason follows `Accountant.Add`'s own precedence, since that is what actually decides whether time accrues:

| Precedence | Reason | Condition | Effect on totals |
|---|---|---|---|
| 1 | `watching a replay` | `Replay` | nothing accrues, not even connected |
| 2 | `not in the car` | `!InCar` | connected accrues; in-car does not |
| 3 | `in the pit box` | `loc == InPitStall` | in-car accrues; driving does not |
| 3 | `not on track` | `loc == NotInWorld` | in-car accrues; driving does not |
| — | *(none — driving)* | otherwise | all three accrue |

Replay outranks everything because `Add` returns before crediting anything. This is the band that makes the pit-box case self-explanatory, so it is the part worth getting exactly right.

### `GET /api/live`

Separate from `/api/status`, which stays as it is for the sidebar and Settings. Two reasons: this payload is per-frame and polled hot, while status is per-session; and merging them would drag telemetry through every two-second sidebar poll.

Returns the snapshot, the session identity and classification, the three totals, and the poll interval. Absent values are `null` rather than zero — the same distinction the `driver_*` columns make, and for the same reason: a speed of zero is a real reading and an absent speed is not.

### Poll rate

The collector's own interval, clamped to **0.5–3 seconds**. Polling faster than the collector reads returns the same frame twice; polling slower than three seconds makes the "last frame *n* seconds ago" counter stop feeling live.

## Deliberate cuts

**No live lap list.** Current, last and best only. A list appending as laps complete needs lap events streamed rather than a snapshot polled — a different transport and considerably more machinery than a confidence page justifies.

**Polling, not SSE.** `App.tsx` already polls `/api/status` every two seconds, so this follows an established pattern with no new server-side plumbing. SSE would be the right answer for a HUD; it is not the right answer for this.

**No history, no charts.** Everything on the page is the present moment or a total for the active session. The rest of the application already covers the past.

## Verification

The point of the page is honesty, so that is what gets tested.

**Against real telemetry, on the Mac.** Replay `ignore/captures` through a running server and confirm the pit-box session shows `Driving ○ — in the pit box` with `0:00` driving against `2:34` in car. That capture is the exact case the page exists to explain, and it needs no simulator.

**Degradation.** Stop the source mid-replay and confirm instantaneous values clear while totals hold. Confirm the idle panel appears with no source at all, and that the unsupported-platform wording appears on macOS rather than the idle panel.

**Staleness threshold.** With the poll interval set high, confirm the page does not report `Not reading` while frames are still arriving on schedule.

**Mutation checks.** Per standing practice in this repository, each new assertion is confirmed to fail when the behaviour it covers is removed — six non-discriminating tests reached review earlier in this project, and the counter-measure is mechanical rather than a matter of care.

# Session record

A resumable account of work on LapDog: what changed, why, what broke. Written to be read cold, by someone with no memory of the conversation. **Newest first.**

Companion to `ToDo.md`, which holds the single next action and how to carry it out. This file is the history; that file is the instruction.

`git log` is the primary record — every message explains its reasoning — and this is the map to it.

## Current state

```
263 Go test functions across 19 packages   pass, including under -race
33 frontend tests                         pass
make ci                                   green
schema version                            2
dist/lapdog.exe                           14.2 MB, windows/amd64, GUI subsystem
dist/lapdogctl.exe                        13.0 MB, windows/amd64, console subsystem
dist/lapdog-0.1.0-portable.zip            11.2 MB, both executables
dist/lapdog-0.1.0-setup.exe                4.7 MB, payload confirmed present
.dataset.db                               1331 sessions, 1242.6 driving hours, schema 2
```

**Live telemetry reads a real simulator** as of 2026-08-06. Still never exercised: **driving** (the first test was conducted in the pits, so laps, driving time and position events have not run against real telemetry), **ratings from an online session**, and **installing the installer**. All three need the Windows machine.

## Picking it up next

1. **Drive a session on track, and one online.** `ToDo.md` has the procedure and the checks. The pits test proved the read path; these two exercise lap detection, driving time, position events and real ratings.
2. **Add a git remote and push.** Everything is on one disk and CI has never actually run.
3. **Install the installer** on the Windows machine — does it land correctly, register uninstall, start the tray app?
5. **Consider one calendar row per year** for the "All time" heatmap, the only way that range gets larger cells.

Deliberately excluded, and recorded as such in the specs: `.ibt` file import, sector times, true overtake detection from `CarIdxLapDistPct`, and setup tracking. Central hub upload is no longer excluded but not designed — see `docs/server-design-brainstorming.md`.

---

## 2026-08-10 — the first real telemetry data, and two bugs in it

The Windows machine produced a data drop, kept in `ignore/`: `lapdog.log`, `lapdog.db`, `config.json` and two `.lpd` captures from 2026-08-06.

**Telemetry works.** The `MapViewOfFile` fix (`6faa2c4`) is confirmed against a real simulator: 331–332 variables published, every variable the collector requires present, two sessions recorded, two replayable captures written, incidents from the live variable, identity captured. That closes the question three days of work were blocked on.

**`CarIsAI` is confirmed**, retiring an unverified guess carried since the SDK headers were read. The capture holds `CarIsAI: 1` on 24 driver entries and `CarIsAI: 0` on 2; the database recorded `ai_opponent_count = 24` with `ai_detection = field`. No code change was needed — the wiring was already right — only the four places that still called it a guess.

Three things looked like bugs and were not, which is worth as much as the two that were. `driving_seconds = 0` and `laps_completed = 0`: the test was conducted sitting in the pits. And 189 `simulator present but not connected` lines whose timestamps fall entirely in the gaps between sessions — none during 14:06, 14:07, 14:10 or 14:11 — which is the simulator at its menus, reported correctly.

#### The log claimed a connection it had not made

`live.go` logged `telemetry: connected to the simulator` at INFO the moment `OpenTraced` succeeded. Opening the mapping proves only that the shared-memory section exists, which is true whenever iRacing is running — including at the menus, where the connected bit is clear and the header is all zeroes. Because the mapping is reopened every poll, the claim fired once a second:

```
14:04:34.772 INFO   telemetry: connected to the simulator
14:04:34.772 DEBUG  telemetry: header parsed ... connected=false numVars=0 bufLen=0
14:04:34.772 DEBUG  telemetry: simulator present but not connected  status=0 wantBitSet=1
```

190 of those against 189 "not connected" — 379 of the file's 641 lines, 59% of the log, each contradicted by the line beneath it. The sting is that `ToDo.md` named that exact string as the success indicator to grep for: the instrument written for diagnosing this pointed at a line that lied. Moved into the existing once-per-connection block, which is reached only after a header reports connected.

#### Offline sessions stored fabricated ratings

iRacing does not report real ratings offline. The capture gave an established account `IRating: 1`, `LicLevel: 1`, `LicSubLevel: 1`, `LicString: "R 0.01"`, and both sessions stored them. On the progression chart added on 08-06 those render as a collapse to nothing — a fabricated cliff in the one chart whose purpose is showing how the ratings moved.

`MyIdentity` now takes ratings only when `WeekendInfo.SubSessionID` is non-zero. That marker is structural rather than a heuristic — the service allocates it, so an offline session has none — and `Official` would have been the wrong test, since a hosted or league session is online, unofficial, and rated. The customer id is kept either way: it is correct offline, and it is what says whose data a database holds.

That gate then exposed a second defect by interaction. `store.Ratings` took the customer id only from rows carrying a rating, so a database holding nothing but offline sessions reported no owner at all and the settings screen said "not yet recorded" for a known account. "Whose database is this" and "how did the rating move" are different questions over different row sets; they now have a query each.

Verified against the real captures rather than only fixtures: replaying `ignore/captures` yields `driver_user_id = 1152608` on both sessions with the rating columns NULL, and `/api/ratings` returns 0 points while still naming the owner. All three fixes were mutation-checked — removing the gate, gating on `Official` instead, and reverting the identity query each produce failures.

---

## 2026-08-06 — identity and ratings, packaging, make targets

Eleven commits, `33fc186..5081292`.

| Commit | Subject |
|---|---|
| `33fc186` | Record the driver's identity and ratings per session |
| `b89b5dd` | Show iRating and Safety Rating progression |
| `390f0f6` | Ship lapdogctl.exe in the portable zip |
| `28daa44` | Name the portable zip after what it is |
| `b4a89ff` | Upload both Windows binaries from CI |
| `1781e1c` | Name the portable-zip variable PORTABLE |
| `01d7e18` | Add a build target for every check and every artefact |
| `7f49cf7` | Slim the make target list from 26 to 22 |
| `421d765` | Record the server-side collection discussion |
| `b5f044d` | Add a ToDo for collecting the Windows telemetry logs |
| `5081292` | Add a resume section to the ToDo |

#### "Does LapDog know the user's iRacing id?" → "Identity. Also track iRating and Safety Rating."

It did, but only incidentally: the customer ID sat inside `classify_source_json`, a blob that exists so a wrong classification rule stays fixable. Reading identity out of provenance ties two unrelated concerns together.

Schema version 2 adds six nullable `driver_*` columns and `idx_sessions_driver`. Ratings are **per session, not per database**, because that is the point of having them — both move after almost every official race, so one current value would answer "what is my iRating" while discarding how it got there.

`sessionyaml.MyIdentity()` picks the driver whose `CarIdx` matches the document's own `DriverCarIdx`. The `Drivers` list holds every car in the field, so taking the first entry would record an opponent's rating as the user's own — plausible-looking and wrong. Safety Rating derives from the licence string (`"A 3.55"` → `3.55`), falling back to `LicSubLevel/100`.

`store.Ratings(Filter)` returns the progression oldest-first plus headline values taken from the ends of that same ordering, so a card cannot disagree with the chart beside it. A delta needs two observations: with one, absent and zero are different claims. Served at `GET /api/ratings`, filtered like every other aggregate.

Two charts on the dashboard, never one with two y-axes — an iRating in the thousands and a Safety Rating between 0 and 4.99 share no scale, and a second axis invites comparing slopes whose steepness means nothing relative to each other.

**Three defects caught by looking rather than reasoning:**

- The Safety Rating headline printed the raw licence string over a chart of numbers, so it read `A 3.55` above a line ending at 3.94. `licenceLabel()` now takes the class from the string and the number from the plotted value, shared between the dashboard and Settings so the two cannot drift.
- 1,284 markers rendered as a solid band that hid the line they marked. Symbols now drop past 60 observations.
- **`web/tools/shoot.mjs` was silently measuring the wrong thing.** Its default range list contained `730`, which is not a range the interface offers, so `useFilter` fell back to 90 days — a reported PASS across "three ranges" was really two, and the earlier heatmap-centring verification had the same flaw. It now rejects unknown ids.

Tests were mutation-checked rather than merely passed: dropping `driver_irating` from the `ON CONFLICT` clause and swapping two scan targets each produce a failure.

#### "Have `make portable` build the windows executables"

It already built `lapdog.exe`; what it lacked was the CLI. `lapdogctl.exe` now ships because the machine that needs diagnosing has a simulator and no Go toolchain — `lapdogctl inspect` is how a telemetry problem gets identified there.

The PE subsystem difference is load-bearing and was verified by reading the header bytes, not assumed: 2 (GUI, no console) for the tray app, 3 (console) for the CLI. A CLI linked into the GUI subsystem has nowhere to print.

Two things extended past the literal ask, both justified by mistakes this repository has already made:

- `verify-embed` checks both binaries and names the offending file. Confirmed it discriminates by feeding it a darwin build renamed `.exe`.
- `rm -f` before writing the zip. `zip` **appends** and replaces only the entries named, so a rebuild after renaming or dropping a file left the old entry shipping beside the current one. Verified by planting a stale entry.

#### "Add a make `build` uber-target" and "slim down the list of Make targets"

`make build` is `ci` first, then every artefact. Verified that a failing test stops the run before anything is packaged — broke an assertion, got exit 2 and no artefacts.

`.NOTPARALLEL:` added, and it is not cosmetic: `portable` and `installer` both write `dist/lapdog.exe`, and under `-j` both can be in that file at once. Global rather than per-target because this is GNU Make 3.81; per-target arguments arrived in 4.4.

Then 26 targets to 22. Four merged rather than hidden: `test-ci` into `test` (which now runs the web suite too, so there is one way to run the tests and it is the complete one), `build-windows-ctl` into `build-windows`, `vet` and `fmt-check` into `lint`, and `ui-clean` deleted since `make clean ui` does it. **`run-ctl` renamed to `run`.**

#### The installer, which the previous record said had never been built

`makensis` 3.12 was in fact installed. It produces a 4.7 MB self-extractor — and 4.7 MB from a 14 MB input could equally be an installer that packages nothing, so the payload was verified rather than assumed: rebuilt with zlib instead of LZMA, scanned for a raw deflate stream, found one at offset 56,860 decompressing to 14,274,929 bytes containing both the icon set and the telemetry strings.

What remains untested is *installing* it.

#### Two questions answered without building anything

**"Can the local web app open directories on the local system?"** Not from the page — browsers block `file://` navigation from an `http://` origin. Via the server, yes, and `internal/tray/tray.go:106` already does it; the tray has had "Open data folder" all along. An endpoint would need care the API currently lacks — no `Origin`, `Sec-Fetch-Site` or `Content-Type` checks — because loopback binding stops remote attackers but not other web pages. Declined; not built.

**"Would SQLite suit 5,000 users?"** Measured rather than estimated, and written up in `docs/server-design-brainstorming.md`. The finding that reframes it: 61% of the 26 MB database is `classify_source_json`, local provenance with no reason to be transmitted, so the payload is ~10 MB per heavy user and ~52 GB across 5,000. At 3 writes/second against 52 GB, SQLite is nowhere near a limit — the case for Postgres is the single writer, backup rehearsal and live migrations. Also flagged: `position_events` holds opponent names, and centralising those means processing personal data of people who never used the service.

#### One correction worth recording

The `ToDo.md` resume notes first claimed the rating panels would be absent on `.dataset.db`. Checking instead of asserting showed the opposite — it was re-ingested at schema 2 with all 1,331 sessions carrying identity. That check then surfaced something real: **the generator writes a fixed `LicString` for every session**, so the Safety Rating delta is exactly 0 across the whole dataset. The query is correct on the values it is given, but that chart is unexercised by the development data.

---

## 2026-08-05 — Cars and Tracks, and the first Windows test

Forty-two commits, `4338da6..6faa2c4`. Brainstormed and researched, then executed subagent-driven from a 2,616-line plan across 10 tasks, merged as `db83d59`.

#### Cars and Tracks pages

New left-column entries, each a per-entity dashboard: headline stats, pace by month, consistency, rivals, qualifying versus race, racecraft, and progression. Web research informed which metrics sim racers actually care about. One shared `Entity.tsx` renders both dimensions. Five new parameterised store queries in `internal/store/entities.go`, five endpoints, one vendored icon.

Plus a dashboard heatmap of the top 10 car-and-track pairings by time, honouring the filter.

**Ten defects originated in the plan rather than the implementation, and six of those were tests that could not fail.** The counter-measure that works is mechanical: delete the line the test covers, confirm the test fails, restore it. Among the more instructive:

- **A fan-out trap.** Joining `laps` into a session-level aggregate multiplied sums by lap count — 28,895 hours reported against 1,242.6 actual.
- **The GROUP BY remedy was backwards.** A reviewer said add the name to `GROUP BY`; that split a renamed car into two rows with its hours divided. Correct is `MAX(nameExpr)`, grouping by id alone.
- **Consistency must be per session then averaged, never pooled.** The difference is 2.5 percentage points, which crosses the band boundary.
- **`?car=` collided with the filter's own parameter space,** collapsing the left list to one row on first click. My spec had specified that collision.
- **"Wins" was labelled human races but counted AI** — 3 of 55 for one car.
- **`limit` collided with `Filter.limit`**, silently clobbered by `URLSearchParams.set`. Renamed to `top`.
- **`make ci` never ran `gofmt`**, and four files on main were unformatted.

#### The first Windows test, and the bug it found

The build recorded nothing. Diagnosing it from logs alone produced the session's most valuable find:

**`MapViewOfFile` was called with a fixed 2 MiB length, and it fails when asked for more bytes than the section contains.** The reference SDK passes 0, meaning the whole section. `VirtualQuery` now establishes the real size. A comment had asserted the fixed length was safe. Compounding it, `live.Next` flattened every error into `ErrDisconnected`, so a genuine mapping failure and an absent simulator were indistinguishable — the interface said only "not connected" and the log said nothing.

Also that day: `lapdogctl inspect` added, so a capture can be read without a Go toolchain. The telemetry source and paths shown read-only in Settings. "Available" changed to **"Supported"** — it only ever meant `GOOS == windows`, and was being read as "working". And a deep-research pass established that `.ibt` files cannot substitute for live reading: 7 of the 26 required variables are `Disk=0/Live=1`, verified against the bundled SDK PDF.

---

## 2026-08-04 — becoming a program

Start of day: the backend and interface existed, but LapDog was not yet a program — `cmd/lapdog` was an empty directory. End of day: it is a program, it cross-compiles to a signed-optional Windows executable, and CI builds the frontend.

> Make targets were renamed on 2026-08-06: `run-ctl` → `run`, `test-ci` → `test`, `vet` and `fmt-check` → `lint`. Commands quoted below are what was written that day, and some no longer exist by that name.

#### Commits

| Commit | Subject |
|---|---|
| `1387bb4` | Make the dashboard charts actually animate |
| `cf8fda9` | Centre the calendar heatmap and stop it clipping |
| `ac27822` | Enlarge the calendar heatmap cells |
| `5085839` | Commit the frontend bundle that .gitignore was hiding |
| `500e822` | Build the frontend in CI instead of committing it |
| `dbf08b5` | Add Windows shared-memory reader and live telemetry source |
| `ece2e5e` | Add tray, autostart, logging and application wiring |
| `428c6dd` | Stop a missing certificate from failing the release |
| `f8b3b97` | Add make run-ctl for looking at the interface locally |

#### What was asked, and what happened

#### 1. "The UI graphs still jump, rather than animate to different values"

A previous attempt had changed the ECharts merge mode. That could not have worked, and the reason matters: **the chart was being unmounted on every filter change.** The filter is part of every query key, so changing a filter starts a *new* query rather than refetching. `isLoading` went true, the panel rendered a loading placeholder, the chart unmounted, and ECharts disposed the instance. The replacement had no previous state to tween from. No merge setting can fix a disposed instance.

Fixed in `web/src/query.ts` with two pieces: `keepPrevious` (`placeholderData: (prev) => prev`) so something is always on screen, and `viewState()` so panels decide what to render from *whether data exists* rather than *whether a fetch is in flight*. A query that is refetching but already holds data is `ready`.

Deliberately **not** applied to single-entity queries such as one session's laps — showing the previous session's data under the new session's heading would be briefly wrong rather than merely stale.

Verified with `web/tools/verify-animation.mjs`: 6/6 canvases survive a filter change, 7 of 10 sampled frames are states matching neither the old nor the new rendering.

**A dead end worth remembering:** the first version of that tool measured the longest bar's width and reported inconclusive. A value axis rescales to its data, so the longest bar occupies nearly the same width whatever the numbers are. It now checksums painted pixels.

#### 2. "Change the heatmap to center rather than left justify"

`calendar.left: 'center'`, and the visual map follows so the scale stays attached to the grid.

Centring alone made one case *worse*. ECharts derives the calendar's box from the cell size, so with cells pinned at 14px a two-year range wanted more width than the card had and simply overflowed. Left-aligned it lost days off the right; centred it lost them off **both** ends — "All time" started in October 2024 and ended in May 2026 despite holding data from August 2024 to August 2026, with nothing indicating the ends were missing.

So the cell size became a **cap rather than a constant**: 14px when the range fits, shrinking only as far as avoiding the clip requires. That needs the width actually available, hence `useElementWidth` in `web/src/components/Chart.tsx`, which reuses the resize observation the chart wrapper already does.

Verified with `web/tools/shoot.mjs` (`npm run verify-layout`), which finds the leftmost and rightmost painted columns and compares the margins at every range preset.

**Two dead ends:** counting any non-white pixel measured the *labels* rather than the grid — the year label sits well left of the grid, so a correctly placed grid reported as touching the edge; the probe now requires a blue cast. And a clipped grid reports a skew of **zero**, because it overflows equally in both directions, so balance had to be checked separately from fit or the broken case scored as the best possible result.

#### 3. "The time heatmap is a bit small, expand 20%"

Cap 14px → 17px, panel height 158px → 179px (seven rows plus labels and scale; raising one without the other crops the bottom rows).

**This did not affect "All time"**, and that was reported rather than glossed over: there the cell is width-constrained at 10px, so the cap never applies. 104 week columns at 17px would want ~1770px against ~1210px available. Making that range larger needs a different layout — one calendar row per year — not a bigger constant.

#### 4. "Commit all code"

The tree was already clean, but checking turned up a real defect. `Makefile` said the built bundle was committed so that `go build` works without Node. It was not: a bare `dist/` ignore rule matches a directory of that name at *any* depth, so it caught `internal/web/dist` alongside the top-level build directory it was written for.

`index.html` escaped the rule, having been added before it existed, and that is what made the failure quiet. A clone held HTML referencing hashed assets that were absent; the binary built, started, passed a bundle check that only looked for `index.html`, and served a blank page.

Fixed the rule, committed the bundle, and strengthened `web.Check()` to read the asset names back out of the HTML and confirm each is embedded — the names are content-hashed per build, so they cannot come from a fixed list. Proved against a reconstruction of the broken state: previously the whole suite passed; now start-up fails naming the missing file.

#### 5. "I'd rather not have 1Mb of generated JS" → build the frontend in CI

Reversed the previous commit's decision. `internal/web/dist` is now gitignored apart from `.gitkeep`, and `.github/workflows/ci.yml` builds it.

`.gitkeep` is tracked deliberately: `//go:embed` fails at **compile** time when its pattern matches nothing, so without a tracked file there every Go build on a clean clone breaks, including `go vet` and binaries that never serve an interface.

Four things had to be fixed to make this safe rather than merely smaller:

1. **`web.Check()` was never called.** It existed, was documented as running at start-up, and nothing invoked it. `api.ListenAndServe` now checks and refuses — not `Handler()`, because `Handler` is how tests assemble the API, and requiring a Node toolchain to test a JSON endpoint would be absurd.
2. **Tests that serve the interface now skip** when the bundle is absent, via `internal/web/webtest`. `LAPDOG_REQUIRE_BUNDLE=1` turns skips into failures; CI and `make test-ci` set it.
3. **Vite's `emptyOutDir` deleted the tracked placeholder** on every build, showing as a deleted file in git and leaving the tree unable to compile after `make clean`. Now off; the make rule clears only the generated paths.
4. **Two type errors were sitting in code that appeared to typecheck.** `npm run build` is `tsc -b && vite build`, but the checks being run were `npx vite build` (skips tsc) and `npx tsc --noEmit` *without* `-b` (does not descend into the app project). Neither was checking anything. Both errors were real index-accesses that can be undefined.

#### 6. "Do task 23, then start 22"

Done in the opposite order, and this is why: Task 23's `main()` calls `source.NewLive()`, which is Task 22. 23 could not compile without it.

**Task 22 — Windows shared-memory reader.** Split so almost none of it needs Windows to verify: `snapshotFrom` takes raw mapped bytes and returns layout, newest untorn row and YAML, carries no build tag, and is tested against synthetic mappings. The Windows file holds only the mapping. Every untrusted offset is bounds-checked — it is a 2 MiB window over another process's memory, so a bad header field must be an error rather than a panic.

Two plan errors, both surfacing only on cross-compilation:

- `golang.org/x/sys/windows` has **no `OpenFileMapping` wrapper**, so the syscall is declared in `live_windows.go`. `CreateFileMapping` is not a substitute: it would *create* the region when absent and report a connected simulator publishing zeroes.
- The plan set `Frame.T` from the sim's `SessionTime`, contradicting what `Frame.T` documents. `SessionTime` restarts at each session in a weekend, and `Accountant.Add` credits nothing for a negative interval — so **practice time would have silently vanished at exactly the moment a session changed**, with nothing logged. Now monotonic since source start, with a test, because both are plausible-looking float seconds and nothing else would notice.

**Task 23 — tray, autostart, logging, wiring.** The tray uses the vendored racing-helmet icon rather than the plan's generated coloured circle; the icon set landed after the plan and a bare dot says nothing about which application it belongs to. `systray` is confined to `tray_windows.go` because it needs CGO off Windows.

Two bugs here:

- **Ctrl-C during start-up hung the process.** I gave the tray its own signal handler alongside `main`'s. A signal arriving in the ~250 ms gap went only to the context; the tray then waited forever for a signal already consumed, and only SIGKILL would stop it. Found by interrupting at a sweep of delays — 0.1s and 0.2s hung reproducibly. Fixed by having the tray watch a `Done` channel the caller owns. Tests pin it, because the symptom is a program that stops exiting rather than anything that fails.
- **`make build-windows` never cross-compiled.** It set no `GOOS`/`GOARCH`, and `-H windowsgui` only asks the linker for a PE *header* — so it emitted a **darwin/arm64** binary that `file` called "PE32+ executable (GUI) Aarch64, for MS Windows". Latent until now because `cmd/lapdog` was empty. **`verify-embed` passed it**, because grepping for strings says nothing about the compilation target. Both fixed: the build pins `windows/amd64`, and `verify-embed` reads `GOOS`/`GOARCH` back out of the binary. Confirmed the new check rejects the old artefact.

#### 7. "What is necessary to sign windows executables?" → "Do not invest in signing"

Answered, then found that the no-signing path was broken. `make sign` with no certificate is meant to warn and succeed; it failed. **Make runs each recipe line in its own shell**, so the `exit 0` in the no-certificate branch ended only that line — make then ran the next one, which requires `osslsigncode`, and the target failed on any machine without a certificate. `make release` therefore could not complete without buying something, which is precisely the coupling the optional path exists to avoid. The recipe is now a single shell command.

Also recorded: public CAs stopped issuing downloadable PKCS#12 files in June 2023, so the `SIGN_PKCS12` path in the spec only works for a pre-2023 certificate or a private CA. Not pursued, per instruction; flagged so a future attempt does not start from a stale premise.

#### 8. "Add a make target for testing, something like run-ctl"

`make run-ctl` builds the CLI and serves `.dataset.db` on 47047, overridable with `DEV_DB` and `DEV_PORT`. Added `dataset-db` too, which was a missing link — `make dataset` wrote captures but nothing turned them into a database, so that step was an unwritten manual command. A missing database prints the two commands that create it rather than failing inside SQLite.


## Method notes that paid off

Accumulated across every day above, because each was learned by getting it wrong first.

- **Reasoning is not evidence for anything visual.** The chart properties fixed on 08-04 were verified by driving real Chrome over the DevTools Protocol and sampling pixels. Both had regressed silently, and both "fixes" were wrong on the first attempt in a way only measurement caught.
- **A verification that cannot fail is not a verification.** `verify-embed` passed a darwin binary. The animation probe passed axis rescaling. In both cases the tool was measuring something that was true regardless.
- **Check the broken state deliberately.** Reconstructing a clone with the bundle removed proved both that the old check passed it and that the new one catches it. Without that, "I added a check" is an assertion.
- **A skip is a silent failure unless something forbids it.** Hence `LAPDOG_REQUIRE_BUNDLE`.
- **A test that cannot fail is worse than no test**, and the counter-measure is mechanical rather than a matter of care: delete the line the test covers, confirm the test fails, restore it. Six non-discriminating tests reached review on 08-05, and two mutations were run deliberately against the identity work on 08-06.
- **A verification tool can under-test silently.** `shoot.mjs` accepted `730` as a range, which the interface does not offer, and measured the 90-day layout instead — reporting a PASS over fewer ranges than it named. Tools that take parameters should reject ones they cannot honour.
- **Size is not evidence of contents.** A 4.7 MB installer built from a 14 MB binary is equally consistent with packaging nothing. Decompressing it and finding the strings is the check.
- **Comments assert; they do not verify.** A comment claimed the fixed 2 MiB `MapViewOfFile` length was safe. It was the bug.
- **A log line is a claim, and can be false.** `telemetry: connected to the simulator` fired on mapping-open rather than on connecting, so 59% of a real log was a statement contradicted by the line beneath it — and the debugging notes named that line as proof of success. Log what was actually established, at the point it becomes true.
- **Real data disagrees with synthetic data in ways fixtures cannot predict.** The generator gave every driver a plausible iRating; the simulator reports `IRating: 1` and `"R 0.01"` offline. No amount of local testing would have surfaced that — only a capture from the real thing.
- **Look for what is correct, not only what is broken.** Three of the five anomalies in the first real data drop were the system behaving properly: zero driving time from sitting in the pits, and "not connected" lines that fell entirely between sessions. Chasing any of them would have been wasted work, and "fixing" them would have introduced bugs.


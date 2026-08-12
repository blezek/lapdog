# ToDo: install the telemetry repair and rebuild the database

The 2026-08-12 UTC captures did exercise real driving, laps, position events and online sessions. They also exposed lost time in the old ingestion path. After installing a build containing the repair, stop LapDog and rebuild into a separate file before replacing anything:

```cmd
lapdogctl.exe ingest "%LOCALAPPDATA%\lapdog\captures" "%LOCALAPPDATA%\lapdog\lapdog-recovered.db"
lapdogctl.exe summary "%LOCALAPPDATA%\lapdog\lapdog-recovered.db"
```

For the 12 captures reviewed on the Mac, the repaired 2026-08-12 UTC totals are 5,378.782 connected seconds, 5,373.749 in-car seconds, 5,329.640 driving seconds (1.4805 hours), and 50 lap rows. Keep `lapdog.db` as the backup until the recovered database has been opened in the interface and checked. Replacing the live database is deliberately not automated here because that is the only copy on Windows and may contain sessions without surviving captures.

The original collection procedure remains below for the next live verification.

## Collection procedure

**Telemetry works.** Confirmed 2026-08-06 from the data drop in `ignore/`: 331 variables published, every required one present, two sessions recorded, two captures written, identity captured, `CarIsAI` confirmed. The `MapViewOfFile` fix was the bug. The procedure below still applies — it is how the next round of data gets collected — but it is no longer chasing a failure.

Two things remain unexercised, and one session of each settles both.

**On track.** The 2026-08-06 test was conducted sitting in the pits, so it recorded 154s and 133s in-car with zero driving time and zero laps. All correct for the conditions, and none of it exercised lap detection, the driving counter or position events. Drive some laps and check: `driving_seconds` non-zero and below `in_car_seconds`, one `laps` row per completed lap, `best_lap_time_s` set, and for a race, rows in `position_events`.

**Online.** Offline sessions report placeholder ratings — an established account came back as `IRating: 1`, `LicString: "R 0.01"` — so those are now discarded, and the rating progression has never had a genuine data point. One official or hosted session settles it. Check that `driver_irating` and `driver_lic_string` are populated and plausible, and that the dashboard's iRating panel appears at all; on an offline-only database it is correctly absent.

There is no development environment on the Windows machine, so the log is the only instrument.

## 1. Build, on the Mac

```bash
make portable
```

Produces `dist/lapdog-0.1.0-portable.zip` containing `lapdog.exe` and `lapdogctl.exe`. Copy the zip to the Windows machine and unzip anywhere; both executables are self-contained, with no installer and no runtime needed.

## 2. Run it, on Windows

Debug logging is already on by default, so nothing needs enabling. If it was ever switched off, the toggle is in the interface under Settings → Debug logging, and it applies immediately without a restart.

1. Start `lapdog.exe`. It appears in the system tray, no window.
2. Start iRacing and **get out on track** — not just to the menus. The single most likely benign explanation for silence is that the simulator was never in a session, and the log distinguishes the two.
3. Drive for a few minutes, so there is more than one poll to look at.
4. Right-click the tray icon → Quit, so the log is flushed and the session closed.

## 3. Collect

Everything under `%LOCALAPPDATA%\lapdog`:

| File | Why |
|---|---|
| `lapdog.log` | The step-by-step read trace. The main artefact. |
| `lapdog.log.1` | Previous generation, if the log passed 4 MB and rotated. |
| `captures\*.lpd` | Only exist if recording partly worked — but if any do, they contain the real session YAML. |
| `lapdog.db` | Small. Says whether anything was actually written. |

Paste that path straight into Explorer's address bar; `%LOCALAPPDATA%` expands.

## 4. Bring it back to the Mac

Over the Windows App remote desktop session, redirected folders arrive as `\\tsclient\<FolderName>`, **not** as a mapped drive letter — that is the thing that was not obvious last time. Set it up under the PC entry → Edit → Folders → Redirect folders → add the Mac folder, then reconnect.

```cmd
robocopy "%LOCALAPPDATA%\lapdog" "\\tsclient\lapdog-2\from-windows" lapdog.log lapdog.log.1 lapdog.db
robocopy "%LOCALAPPDATA%\lapdog\captures" "\\tsclient\lapdog-2\from-windows\captures" /E
```

Do not point LapDog's data directory at `\\tsclient\...` — it is a network path, SQLite WAL is unsafe there, and `config.CheckLocalFilesystem` rejects it. Copy files out; do not run from the share.

## 5. What to look for in the log

The read path emits one line per step. Find the first one that stops, since everything after it is a consequence:

```
opening telemetry mapping          → about to open Local\IRSDKMemMapFileName
mapping opened                     → the simulator is running and published the region
view mapped                        → the fix being tested; reports how many bytes mapped
header parsed                      → every header field, incl. numVars, bufLen, sessionInfoLen
simulator present but not connected → normal at the menus; NOT normal on track
variable headers parsed            → count should be in the hundreds
session YAML read                  → classification has what it needs
telemetry: connected to the simulator   → logged once per connection, with tickRate and numVars
```

That last line is trustworthy as of 2026-08-10. It previously fired on mapping-open, before the connected bit was read, so it appeared once a second while iRacing sat at its menus and contradicted the line beneath it.

Failure lines worth grepping for directly:

```powershell
Select-String -Path "$env:LOCALAPPDATA\lapdog\lapdog.log" -Pattern "could not|failed|not connected|unavailable"
```

- `mapping could not be opened` — the simulator was not running, or was not publishing.
- `view could not be mapped` — the original bug's signature. If this still appears after the fix, the size logic is still wrong and the reported byte counts say by how much.
- `region smaller than a header` / `region size query failed` — `VirtualQuery` fell back; the bound it used is logged.
- `simulator present but not connected` while on track — the status bit is not what is expected, and the raw value is logged beside it.
- `variable headers unparseable` — the 144-byte `irsdk_varHeader` stride may be wrong for this build of iRacing.

## Settled on 2026-08-06 — `CarIsAI`

Confirmed, no longer a guess. `ignore/captures/20260806T190535Z-0-0.lpd` holds `CarIsAI: 1` on 24 driver entries and `CarIsAI: 0` on 2, and the database recorded `ai_opponent_count = 24` with `ai_detection = field`. The heuristic fallback stays for documents that omit the field, but it is no longer the primary path and no `reclassify` is needed.

## Resuming this session

`SESSION.md` is the full history, newest first, covering all three days. What follows is the short version for this last one.

Eleven commits, `33fc186..5081292`. The working tree is clean and `make ci` passes.

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

### What changed, in one paragraph each

**Identity and ratings.** Schema version 2 (`internal/store/migrations/0002_driver_identity.sql`) adds six nullable `driver_*` columns and `idx_sessions_driver`. `sessionyaml.MyIdentity()` picks the driver whose `CarIdx` matches the document's own `DriverCarIdx` — taking the first entry in `Drivers` would record an opponent's rating as the user's own. Safety Rating derives from the licence string, falling back to `LicSubLevel/100`. `store.Ratings(Filter)` returns the progression plus headline values computed over the same range, so a card cannot disagree with the chart beside it. Served at `GET /api/ratings`; shown as two dashboard cards and a read-only row in Settings.

**Packaging.** `make portable` now builds and zips both Windows binaries; `lapdogctl.exe` ships because the machine that needs diagnosing has no Go toolchain. `make build` is the new uber-target: `ci` first, then every artefact. The zip is `lapdog-<version>-portable.zip`, matching `-setup.exe`.

**Make targets slimmed, 26 to 22.** `test-ci` folded into `test` (which now also runs the web suite), `build-windows-ctl` into `build-windows`, `vet` and `fmt-check` into `lint`, `ui-clean` deleted. `run-ctl` renamed to **`run`**.

### Getting back to a running system

```bash
make run        # http://127.0.0.1:47047 against .dataset.db
make ci         # every check
make build      # every check, then every artefact
```

`.dataset.db` was re-ingested on 2026-08-06 and is at schema version 2, so all 1,331 sessions carry identity: customer 271828, iRating 1405 rising to 2249, licence `A 3.55`. The rating panels therefore render, and `/api/ratings` returns 1,331 points with a delta of +809.

Note that Safety Rating shows a delta of exactly 0 there, which is a property of the generator rather than a bug — `internal/synth/yaml.go` writes a fixed `LicString: A 3.55` for every session while varying iRating. If the Safety Rating chart ever needs to be seen moving, the generator has to vary the licence string. iRating exercises the progression path fully in the meantime.

On a database that predates the migration the panels are **absent** rather than empty, which is deliberate: a card asserting "no rating" would read as a rating of nothing.

### Things learned the hard way, worth not rediscovering

- `web/tools/shoot.mjs` silently measured the wrong thing: `730` is not a range the interface offers, so `useFilter` fell back to 90 days and a "three range" PASS was really two. It now rejects unknown ids. Legal values: `7 30 90 365 all`.
- The Safety Rating card printed the raw licence string over a chart of numbers, so it could read `A 3.55` above a line ending at 3.94. `licenceLabel()` in `web/src/format.ts` now takes the class from the string and the number from the plotted value.
- Rating series drop their markers past 60 points; two years of practice is over a thousand observations and the markers merge into a band that hides the line.
- Recipes are serial now (`.NOTPARALLEL:`), because `portable` and `installer` both write `dist/lapdog.exe` and under `-j` both could be in it at once. Global rather than per-target because this is GNU Make 3.81.
- Tests were mutation-checked, not merely passed: dropping `driver_irating` from the `ON CONFLICT` clause and swapping two scan targets each produce a failure. Worth repeating for new tests — six non-discriminating tests shipped earlier in this project.

## Also outstanding, unrelated to telemetry

- **No git remote.** Everything is local only, and the CI workflow has never actually run — it is verified to parse and `make ci` passes, nothing more.
- **The installer has never been installed.** `make build` produces `lapdog-0.1.0-setup.exe` and the payload is confirmed present, but whether it installs, registers uninstall and starts the tray app is untested. Needs the same Windows machine.
- **Server-side collection** is parked in `docs/server-design-brainstorming.md`. No action pending.

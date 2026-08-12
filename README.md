# LapDog

Records how much time you actually spend in iRacing, and what you spend it on.

A Windows tray application that reads the simulator's telemetry once a second, writes sessions to a local SQLite database, and serves a web interface on `http://127.0.0.1:47047` for looking at the result. Time is split by what you were doing — public practice, race practice, qualifying, racing — and by the context it happened in, so league racing is distinguishable from official racing and from driving against AI.

Each session also records who was driving and where their iRating and Safety Rating stood at the time, so the dashboard can show how both moved over any range rather than only what they are now.

Everything ships as a single `.exe`. No runtime, no redistributable, no sidecar files: the interface, the icons and the database migrations are all compiled in.

## Status

The backend and interface are complete and exercised against two years of synthetic data.

**Live telemetry reads a real simulator.** First confirmed 2026-08-06 on a Windows machine with iRacing: 331 variables published, every variable the collector requires present, two sessions recorded, two replayable captures written, incidents from the live variable, and the driver identity captured. `CarIsAI` — long carried as an unverified guess from the SDK headers — is confirmed, with `CarIsAI: 1` on 24 driver entries against a recorded `ai_opponent_count = 24`.

What remains unexercised is **driving**: that test was conducted sitting in the pits, so lap detection, the driving counter and position events have still never run against real telemetry. See [Outstanding work](#outstanding-work).

## Running it on a Mac

macOS is the development platform. There is no simulator and no system tray, but everything else runs, and the Windows binary cross-compiles from here with no Windows machine involved.

### Look at the interface

The quickest thing worth doing. Two years of generated sessions, so every chart has data in it.

```bash
make run        # http://127.0.0.1:47047, Ctrl-C to stop
```

On a fresh clone the database does not exist yet — it is generated, not committed. `run` will tell you so and print these:

```bash
make dataset        # generate capture files (~250 MB, a few minutes, gitignored)
make dataset-db     # replay them into .dataset.db (~25 MB)
make run
```

The database is built by *replaying captures*, not by writing rows directly. That is deliberate: the development data has been through the same decode, classify and accounting path as a real session, so a bug in that path shows up while you are looking at the interface rather than only on a race weekend.

Point it at any database, including a real one copied off a Windows machine:

```bash
make run DEV_DB=~/.local/share/lapdog/lapdog.db
make run DEV_PORT=48000
```

### Work on the frontend

```bash
make run        # terminal 1: the Go API and data
make ui-dev         # terminal 2: http://localhost:5173
```

Vite serves the interface and proxies `/api` and `/icons` to the Go server, so edits appear immediately against real data.

**Without `ui-dev`, frontend edits will not show up.** The bundle is compiled into the binary, so a change needs `make ui` and a restart. This catches people out.

### Run the actual application

```bash
go run ./cmd/lapdog
```

Serves the interface, Ctrl-C exits. Two differences from Windows: there is no tray (the systray library needs CGO, and `CGO_ENABLED=0` is load-bearing here), so it logs `no system tray on this platform; interrupt to stop` and blocks; and with no simulator it sits disconnected with an empty database. It mainly proves the wiring.

It writes to `~/.local/share/lapdog`. To leave that alone:

```bash
XDG_DATA_HOME=/tmp/lapdog-test go run ./cmd/lapdog
```

### Build the Windows binaries

```bash
make build-windows  # dist/lapdog.exe (~14 MB) and dist/lapdogctl.exe (~13 MB)
make verify-embed   # assert both are windows/amd64 with the interface inside
make portable       # zip both
```

`portable` builds both and zips them. `lapdogctl.exe` ships because the machine that needs diagnosing is a Windows machine with a simulator and no development environment — `lapdogctl inspect` on a capture is how a telemetry problem gets identified there, and requiring a Go toolchain to obtain it puts the tool out of reach exactly when it is needed.

The two differ in PE subsystem, which is the load-bearing part: `lapdog.exe` is linked `-H windowsgui` (subsystem 2, no console window), `lapdogctl.exe` is not (subsystem 3, a console program that can print).

`verify-embed` reads `GOOS` and `GOARCH` back out of each binary rather than trusting the build. This is not paranoia: the target once produced a `darwin/arm64` binary with a Windows PE header that `file` happily called a Windows executable.

### Everything CI runs

```bash
make ci
```

Vet, frontend build, typecheck, both test suites, Windows cross-compile, embedding check. Run this before pushing.

### Everything, including the artefacts

```bash
make build
```

`make ci` first, then every binary: `lapdog.exe`, `lapdogctl.exe`, the host `lapdogctl` and `lapdog-gen`, the portable zip and the installer. `ci` runs first deliberately — a tree that does not pass has no business producing an installer, and a failing test stops the run before anything is packaged.

`make build` needs `makensis` for the installer leg (`brew install makensis`, or `make tools`). It differs from `make release` only in that release also Authenticode-signs and writes `SHA256SUMS`.

## All make targets

| Target | What it does |
|---|---|
| `build` | Every check, then every artefact: binaries, zip, installer |
| `ci` | Every check and nothing else — what CI runs |
| `test` | The Go and web test suites |
| `run` | Serve `.dataset.db` on 47047 for local testing |
| `ui-dev` | Vite dev server with hot reload |
| `dataset` | Generate synthetic capture files into `.dataset` (~250 MB) |
| `dataset-db` | Replay those captures into `.dataset.db` |
| `release` | `build`, then Authenticode-sign and write `SHA256SUMS` |
| `release-snapshot` | Local GoReleaser release without publishing |
| `goreleaser-check` | Validate `.goreleaser.yaml` |
| `tools` | Install the macOS packaging toolchain via brew |
| `clean` | Remove build output and the generated bundle |

Plumbing, invocable but normally reached as a prerequisite: `lint`, `ui`, `verify-embed`, `portable`, `installer`, `sign`, `validate`, `fixtures`, `build-windows`, `build-ctl`, `build-gen`.

To force a frontend rebuild when the inputs look unchanged: `make clean ui`.

## lapdogctl

A console CLI, separate from the tray app because a GUI-subsystem executable has no console to print to. It ships in the portable zip as `lapdogctl.exe`, so it is available on the Windows machine that has the simulator.

```bash
make build-ctl      # this machine
make build-windows  # dist/lapdogctl.exe, alongside the tray app
./dist/lapdogctl ingest <captures-dir> <lapdog.db>   # replay captures into a database
./dist/lapdogctl summary <lapdog.db>                 # print what a database contains
./dist/lapdogctl reclassify <lapdog.db>              # re-derive classification from stored provenance
./dist/lapdogctl serve <lapdog.db> [port]            # serve the API and interface
```

`reclassify` is the remedy for a classification rule that turns out to be wrong. Every session stores the inputs its label was derived from, so labels can be recomputed later without the original captures. On correct data it changes nothing, which is how the round trip is checked.

## How it fits together

```
iRacing ──shared memory──► internal/irsdk ──► internal/source ──► internal/collector
                                                    │                      │
                                          .lpd capture files       internal/store (SQLite)
                                                    │                      │
                                          replay for testing        internal/api ──► internal/web
                                                                                          │
                                                                              React + ECharts on :47047
```

| Package | Responsibility |
|---|---|
| `internal/irsdk` | The telemetry format: header, variable layout, row decoding, torn-read detection, and the Windows memory mapping |
| `internal/source` | Frames in time order, live or replayed. Pacing lives here, not in the collector |
| `internal/capture` | The `.lpd` capture format: write, read, prune |
| `internal/sessionyaml` | Parsing the simulator's session-info YAML |
| `internal/classify` | Turning session info into a session type and an event context |
| `internal/collector` | The poll loop: time accounting, lap detection, position events, segment lifecycle |
| `internal/store` | SQLite schema, migrations, writes and aggregation queries |
| `internal/api` | The JSON API and CSV/JSON export |
| `internal/web` | Serving the embedded interface |
| `internal/tray` | Tray icon and menu (systray confined to Windows) |
| `internal/config` | Settings, data paths, autostart |
| `internal/applog` | The application log, with rotation |
| `internal/synth` | The synthetic dataset generator |
| `internal/ui/icons` | Vendored Material Design Icons, and SVG-to-PNG/ICO rasterising |
| `web/` | React + TypeScript + ECharts frontend |
| `web/src/pages/Entity.tsx` | The Cars and Tracks pages: one component, two dimensions |
| `web/src/entity.ts` | Consistency banding and dimension labels, kept pure for testing |
| `web/src/pages/Live.tsx` | The `/live` page: whether telemetry is being read right now, and why driving time is or is not accruing |

Read the specs for the reasoning: [design](docs/superpowers/specs/2026-08-04-lapdog-design.md) and [packaging](docs/superpowers/specs/2026-08-04-lapdog-packaging.md). The [backend plan](docs/superpowers/plans/2026-08-04-lapdog-backend.md) is the task-by-task implementation record.

## Things that will surprise you

**`CGO_ENABLED=0` is load-bearing, not a preference.** It is why `modernc.org/sqlite` was chosen over the C bindings, and it is what lets the Windows binary cross-compile from a Mac with no mingw toolchain. It is also why the systray dependency is confined to a Windows-only file — systray needs CGO to reach Cocoa and D-Bus, but on Windows it is pure Go.

**The frontend bundle is generated, not committed.** `internal/web/dist` is gitignored apart from a `.gitkeep`, which is tracked because `//go:embed` fails at *compile* time when its pattern matches nothing — without it every Go build on a clean clone breaks, including `go vet`. CI builds the bundle and fails if it is ever committed again.

**Offline sessions carry no ratings.** iRacing reports placeholders offline — a real AI-practice capture gave an established account `IRating: 1` and `LicString: "R 0.01"`. `MyIdentity` therefore records the ratings only when `WeekendInfo.SubSessionID` is non-zero, which is the structural marker of a session the service registered. The customer id is kept either way, since it is correct offline.

**Bundle-dependent tests skip when the frontend is not built**, so a clone without Node still runs the Go suite. `LAPDOG_REQUIRE_BUNDLE=1` turns those skips into failures; `make test` and CI set it, because a skip that is reachable in CI is a test that has quietly stopped running.

**`lapdog.exe` is a GUI-subsystem binary.** Linked `-H windowsgui` so no console window appears, which also makes it useless as a CLI — hence `lapdogctl`.

**The database is written by exactly one connection.** The collector owns the writer with `MaxOpenConns=1`; readers are pooled. `SQLITE_BUSY` is impossible by construction rather than retried.

**Replay time is never counted.** Watching a replay is not driving, and there is deliberately no setting to change that.

**Other drivers' names are never anonymised.** They are public information within a session, and a race is not a race without who you raced.

**Consistency is computed per session, then averaged.** Pooling every lap a car has ever done at a track measures something else — it mixes repeatability with two years of improvement — and on real data the two forms differ by about 2.5 percentage points, enough to move a driver across the 98% threshold. Both return a plausible percentage, so a test pins the difference.

## Testing

```bash
make test                  # Go and web, 298 Go tests across 14 tested packages
cd web && npm run test     # frontend
make ci                    # everything
```

Go tests run with no simulator and no network. The synthetic dataset is deterministic — the same seed and a fixed base date produce byte-identical captures — so `make fixtures` only changes output when the generator genuinely changes.

**One fixture comes from a real simulator:** `internal/collector/testdata/real-build-vars.json`, the variable layout a 2026 iRacing build published — 331 names, the tick rate and the buffer length. It is committed because it is the one part of a real capture that carries nothing personal, and because the question it answers cannot be answered locally: `RequiredCoreVars` and `RequiredRaceVars` were transcribed from 2015 documentation, and asserting them against `internal/synth` would only prove the generator agrees with itself, since `layout.go` declares whatever LapDog asks for. A renamed variable would otherwise surface as a refused session on a user's machine.

The rest of that capture is **not** committed and should not be: it carries a real customer id, the driver's name, 24 real people's names from the licensed AI roster, and iRacing's own setup and track content. `/ignore/` is gitignored for that reason.

**The generator reports what the simulator reports, including the awkward parts.** Offline weekends carry `IRating: 1` and `LicString: "R 0.01"` rather than plausible values, because that is what a real offline session publishes. Fidelity here is load-bearing: it is what stops the offline-ratings gate from being removable with every test still green.

**The Live page's honesty is tested where it can be.** `web/src/live.test.ts` covers the staleness rule — three poll intervals floored at two seconds, and the switch to `Not reading` driven by the clock alone while the payload is unchanged, which is the transition a stalled collector produces — and which of the four explanations an absent frame is given. The other half of the rule, that accumulated totals keep their values while instantaneous ones clear to `—`, lives in the page component; there is no DOM test harness in this repository, so it is verified in a browser rather than by a unit test. Its agreement with the accounting — that a reason is present exactly when driving time is not accruing — is pinned in `internal/collector/live_test.go` over a replayed frame stream rather than against constructed rows, which is worth more than a hand-built row because every value takes the same decoding path production uses. Those captures come from `internal/synth`, not from a simulator.

Two verification tools drive a real browser over the Chrome DevTools Protocol, with no dependencies beyond Node's built-in WebSocket:

```bash
cd web
npm run verify-animation   # charts tween between filters rather than snapping
npm run verify-layout      # the calendar is centred and unclipped at every range
```

`verify-animation` takes the page and its chart-bearing card as optional arguments, so it can check the Cars and Tracks pages as well as the dashboard, all of which keep their charts mounted across a filter change:

```bash
node tools/verify-animation.mjs                                                           # dashboard, default card
node tools/verify-animation.mjs http://127.0.0.1:47047 "/cars?range=90" "BEST LAP BY MONTH"
node tools/verify-animation.mjs http://127.0.0.1:47047 "/tracks?range=90" "BEST LAP BY MONTH"
```

Both need a server running (`make run`). They exist because both properties are invisible to unit tests and both regressed silently once.

## Releases

```bash
make release       # test, build, zip, installer, sign, checksums
make goreleaser-check
make release-snapshot
```

Signing is optional: with no certificate configured the build emits unsigned artefacts and says so. Unsigned executables show a one-time SmartScreen warning, which `SHA256SUMS` is the answer to. `make installer` needs `brew install makensis`.

Note that public certificate authorities stopped issuing downloadable PKCS#12 files in June 2023 — keys must now live in hardware or a cloud service — so the `SIGN_PKCS12` path only works for a pre-2023 certificate or a private CA. Signing is not currently being pursued.

GitHub Releases are built by GoReleaser from semantic version tags:

```bash
git tag -a v0.0.1 -m "LapDog v0.0.1"
git push origin v0.0.1
```

The tag-triggered workflow publishes `lapdog-windows-amd64.zip` for self-update, `lapdog-<version>-portable.zip` for manual portable installs, the NSIS setup executable, and `SHA256SUMS`.

Self-update is not implemented yet. The release pipeline now creates the updater
asset that future code will consume, but the app still has no updater package,
tray/settings update action, download path, checksum validation or executable
replacement flow. That work is tracked in
`docs/superpowers/plans/2026-08-10-lapdog-github-self-update.md`.

## Where the rest of the writing lives

| File | What it holds |
|---|---|
| `ToDo.md` | The next action: how to collect Windows logs and find why telemetry is not read |
| `SESSION.md` | Running history, newest first — what changed, why, and what broke |
| `docs/server-design-brainstorming.md` | Parked: would SQLite suit 5,000 users, with measurements |
| `docs/superpowers/specs/` | Design specs |
| `docs/superpowers/plans/` | Implementation plans |

## Outstanding work

- **Driving time and laps are unexercised.** Live telemetry now reads a real simulator (2026-08-06), but that test was conducted sitting in the pits: two sessions recorded 154s and 133s in-car with zero driving time and zero laps, all correct for the conditions and none of it exercising lap detection, the driving counter or position events. That needs a session actually on track.
- **Ratings from an online session are unverified.** Offline sessions report placeholders, so they are now discarded (see below). What a real official or hosted session reports has never been seen, which means the rating progression has never had a genuine data point.
- **The NSIS installer has been run, and its upgrade path exposed a bug.** The first upgrade attempt could not replace a running `lapdog.exe`: the installer looked for a visible window titled `LapDog`, but the application has only a tray window. The installer now asks that tray window to close and waits for the process to finish flushing before replacing or deleting the executable. That shutdown path still needs to be re-run on Windows.
- **The installer carries only `lapdog.exe`.** The portable zip ships `lapdogctl.exe` too; the installer does not.
- **No git remote.** Nothing is pushed, and the CI workflow has therefore never run.

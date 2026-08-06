# LapDog

Records how much time you actually spend in iRacing, and what you spend it on.

A Windows tray application that reads the simulator's telemetry once a second, writes sessions to a local SQLite database, and serves a web interface on `http://127.0.0.1:47047` for looking at the result. Time is split by what you were doing — public practice, race practice, qualifying, racing — and by the context it happened in, so league racing is distinguishable from official racing and from driving against AI.

Each session also records who was driving and where their iRating and Safety Rating stood at the time, so the dashboard can show how both moved over any range rather than only what they are now.

Everything ships as a single `.exe`. No runtime, no redistributable, no sidecar files: the interface, the icons and the database migrations are all compiled in.

## Status

The backend and interface are complete and exercised against two years of synthetic data. **Reading live telemetry has never been run against a real simulator** — that needs a Windows machine with iRacing, and it is the one part of the system with genuine unknowns. See [Outstanding work](#outstanding-work).

## Running it on a Mac

macOS is the development platform. There is no simulator and no system tray, but everything else runs, and the Windows binary cross-compiles from here with no Windows machine involved.

### Look at the interface

The quickest thing worth doing. Two years of generated sessions, so every chart has data in it.

```bash
make run-ctl        # http://127.0.0.1:47047, Ctrl-C to stop
```

On a fresh clone the database does not exist yet — it is generated, not committed. `run-ctl` will tell you so and print these:

```bash
make dataset        # generate capture files (~250 MB, a few minutes, gitignored)
make dataset-db     # replay them into .dataset.db (~25 MB)
make run-ctl
```

The database is built by *replaying captures*, not by writing rows directly. That is deliberate: the development data has been through the same decode, classify and accounting path as a real session, so a bug in that path shows up while you are looking at the interface rather than only on a race weekend.

Point it at any database, including a real one copied off a Windows machine:

```bash
make run-ctl DEV_DB=~/.local/share/lapdog/lapdog.db
make run-ctl DEV_PORT=48000
```

### Work on the frontend

```bash
make run-ctl        # terminal 1: the Go API and data
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
make build-windows      # dist/lapdog.exe, windows/amd64, ~14 MB
make build-windows-ctl  # dist/lapdogctl.exe, the console CLI, ~13 MB
make verify-embed       # assert both are windows/amd64 with the interface inside
make portable           # zip both
```

`portable` builds both and zips them. `lapdogctl.exe` ships because the machine that needs diagnosing is a Windows machine with a simulator and no development environment — `lapdogctl inspect` on a capture is how a telemetry problem gets identified there, and requiring a Go toolchain to obtain it puts the tool out of reach exactly when it is needed.

The two differ in PE subsystem, which is the load-bearing part: `lapdog.exe` is linked `-H windowsgui` (subsystem 2, no console window), `lapdogctl.exe` is not (subsystem 3, a console program that can print).

`verify-embed` reads `GOOS` and `GOARCH` back out of each binary rather than trusting the build. This is not paranoia: the target once produced a `darwin/arm64` binary with a Windows PE header that `file` happily called a Windows executable.

### Everything CI runs

```bash
make ci
```

Vet, frontend build, typecheck, both test suites, Windows cross-compile, embedding check. Run this before pushing.

## All make targets

| Target | What it does |
|---|---|
| `run-ctl` | Serve `.dataset.db` on 47047 for local testing |
| `dataset` | Generate synthetic capture files into `.dataset` (~250 MB) |
| `dataset-db` | Replay those captures into `.dataset.db` |
| `ui` | Build the frontend into `internal/web/dist` |
| `ui-dev` | Vite dev server with hot reload |
| `ui-clean` | Rebuild the frontend from scratch |
| `test` | Go tests |
| `test-ci` | Go tests with the frontend bundle required rather than skipped |
| `vet` | `go vet` |
| `ci` | Everything CI runs |
| `build-windows` | Cross-compile the tray app to `dist/lapdog.exe` |
| `build-ctl` | Build `lapdogctl` for this machine |
| `build-windows-ctl` | Cross-compile `lapdogctl` to `dist/lapdogctl.exe` |
| `build-gen` | Build `lapdog-gen`, the dataset generator |
| `verify-embed` | Prove the interface is inside both windows/amd64 binaries |
| `fixtures` | Regenerate the committed test fixtures (~1.7 MB) |
| `validate` | Replay `.dataset` back through decode, parse and classify |
| `portable` | Zip both executables |
| `installer` | Build the NSIS installer (needs `brew install makensis`) |
| `sign` | Authenticode-sign, when a certificate is configured |
| `release` | Test, build, portable, installer, sign, checksums |
| `tools` | Install the macOS packaging toolchain via brew |
| `clean` | Remove build output and the generated bundle |

## lapdogctl

A console CLI, separate from the tray app because a GUI-subsystem executable has no console to print to. It ships in the portable zip as `lapdogctl.exe`, so it is available on the Windows machine that has the simulator.

```bash
make build-ctl          # this machine
make build-windows-ctl  # dist/lapdogctl.exe
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

Read the specs for the reasoning: [design](docs/superpowers/specs/2026-08-04-lapdog-design.md) and [packaging](docs/superpowers/specs/2026-08-04-lapdog-packaging.md). The [backend plan](docs/superpowers/plans/2026-08-04-lapdog-backend.md) is the task-by-task implementation record.

## Things that will surprise you

**`CGO_ENABLED=0` is load-bearing, not a preference.** It is why `modernc.org/sqlite` was chosen over the C bindings, and it is what lets the Windows binary cross-compile from a Mac with no mingw toolchain. It is also why the systray dependency is confined to a Windows-only file — systray needs CGO to reach Cocoa and D-Bus, but on Windows it is pure Go.

**The frontend bundle is generated, not committed.** `internal/web/dist` is gitignored apart from a `.gitkeep`, which is tracked because `//go:embed` fails at *compile* time when its pattern matches nothing — without it every Go build on a clean clone breaks, including `go vet`. CI builds the bundle and fails if it is ever committed again.

**Bundle-dependent tests skip when the frontend is not built**, so a clone without Node still runs the Go suite. `LAPDOG_REQUIRE_BUNDLE=1` turns those skips into failures; `make test-ci` and CI set it, because a skip that is reachable in CI is a test that has quietly stopped running.

**`lapdog.exe` is a GUI-subsystem binary.** Linked `-H windowsgui` so no console window appears, which also makes it useless as a CLI — hence `lapdogctl`.

**The database is written by exactly one connection.** The collector owns the writer with `MaxOpenConns=1`; readers are pooled. `SQLITE_BUSY` is impossible by construction rather than retried.

**Replay time is never counted.** Watching a replay is not driving, and there is deliberately no setting to change that.

**Other drivers' names are never anonymised.** They are public information within a session, and a race is not a race without who you raced.

**Consistency is computed per session, then averaged.** Pooling every lap a car has ever done at a track measures something else — it mixes repeatability with two years of improvement — and on real data the two forms differ by about 2.5 percentage points, enough to move a driver across the 98% threshold. Both return a plausible percentage, so a test pins the difference.

## Testing

```bash
make test                  # Go, 206 tests across 19 packages
cd web && npm run test     # frontend
make ci                    # everything
```

Go tests run with no simulator and no network. The synthetic dataset is deterministic — the same seed and a fixed base date produce byte-identical captures — so `make fixtures` only changes output when the generator genuinely changes.

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

Both need a server running (`make run-ctl`). They exist because both properties are invisible to unit tests and both regressed silently once.

## Releases

```bash
make release       # test, build, zip, installer, sign, checksums
```

Signing is optional: with no certificate configured the build emits unsigned artefacts and says so. Unsigned executables show a one-time SmartScreen warning, which `SHA256SUMS` is the answer to. `make installer` needs `brew install makensis`.

Note that public certificate authorities stopped issuing downloadable PKCS#12 files in June 2023 — keys must now live in hardware or a cloud service — so the `SIGN_PKCS12` path only works for a pre-2023 certificate or a private CA. Signing is not currently being pursued.

## Outstanding work

- **Live telemetry has never read a real simulator.** Everything up to the memory mapping is tested against synthetic mappings on macOS; the mapping itself needs Windows.
- **`CarIsAI` is unverified.** The field name is a guess from the SDK headers. The procedure is to drive one AI race, read the session YAML out of the resulting capture, correct the field name, then `lapdogctl reclassify` — the provenance columns exist precisely so this is a recomputation rather than lost data.

  There is a gap here: the specs and the backend plan both describe this step as `lapdogctl inspect`, and **that subcommand was never built**. `lapdogctl` has `ingest`, `summary`, `reclassify`, `serve` and `version`. Confirming the field needs `inspect` adding first, or a throwaway program that opens a `.lpd` with `internal/capture` and prints the session record.
- **The NSIS installer has never been built.** `makensis` is not installed here.
- **No git remote.** Nothing is pushed, and the CI workflow has therefore never run.

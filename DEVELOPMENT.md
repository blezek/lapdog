# Developing LapDog

LapDog is developed on macOS and shipped for Windows. The simulator and system
tray are unavailable on the development machine, but capture replay, the API,
the web interface, tests, and Windows cross-compilation all run locally.

Read [AGENTS.md](AGENTS.md) before changing the project. It records constraints
that arose from verified failures, including the requirement to keep
`CGO_ENABLED=0`.

## Run the interface locally

The normal development server uses a generated database:

```bash
make dataset        # generate about 250 MB of synthetic captures
make dataset-db     # replay them into .dataset.db
make run            # http://127.0.0.1:47047; Ctrl-C to stop
```

The database is built by replaying captures rather than inserting rows. This
exercises the same decode, classification, and accounting path used for live
sessions.

Point the server at another database or port when needed:

```bash
make run DEV_DB=~/.local/share/lapdog/lapdog.db
make run DEV_PORT=48000
```

Real captures belong under `ignore/`, which is gitignored because they contain
customer identifiers and driver names. Replay them into the development
database with:

```bash
make ingest CAPTURES=ignore/captures
```

## Frontend development

Run the API and Vite in separate terminals:

```bash
make run
make ui-dev         # http://localhost:5173
```

Vite proxies `/api` and `/icons` to the Go server and hot-reloads frontend
changes. Without `ui-dev`, the interface is compiled into the Go binary; run
`make ui` and restart the server to see a change. Use `make clean ui` to force a
bundle rebuild when timestamps make the inputs appear unchanged.

## Run the application wiring

```bash
go run ./cmd/lapdog
```

On macOS there is no simulator or tray. The process serves an empty local
database and waits until interrupted. Keep normal application data untouched
with:

```bash
XDG_DATA_HOME=/tmp/lapdog-test go run ./cmd/lapdog
```

## Tests and CI

```bash
make test
make ci
CGO_ENABLED=0 go test -race ./internal/collector/
```

`make ci` runs formatting checks, vet, the Go and web suites, typechecking,
Windows cross-compilation, and embedded-bundle verification. Go package and
in-binary test concurrency are both set explicitly by the Makefile.

Bundle-dependent Go tests skip when the frontend has not been built.
`LAPDOG_REQUIRE_BUNDLE=1` turns those skips into failures; `make test` and CI set
it so the full suite cannot silently prove less than expected.

The synthetic dataset is deterministic for a fixed seed and end date. The
committed fixtures change only when the generator changes:

```bash
make fixtures
make validate
```

`internal/collector/testdata/real-build-vars.json` is a real simulator variable
layout with no personal fields. It checks the expected variable names against
what a 2026 iRacing build actually published. Full real captures must never be
committed.

### Browser verification

The browser tools use Chrome's DevTools protocol without an additional browser
automation dependency. Start `make run`, then:

```bash
cd web
npm run verify-animation
npm run verify-layout
```

The animation verifier accepts a page and chart title:

```bash
node tools/verify-animation.mjs \
  http://127.0.0.1:47047 "/cars?range=90" "BEST LAP BY MONTH"
node tools/verify-animation.mjs \
  http://127.0.0.1:47047 "/tracks?range=90" "BEST LAP BY MONTH"
```

Visual changes must be inspected in a real browser. Unit tests cannot prove
that a chart is centered, unclipped, readable, or animating.

## Make targets

| Target | What it does |
|---|---|
| `build` | Run every check, then build every artifact |
| `ci` | Run every CI check without packaging |
| `test` | Run the Go and web test suites |
| `run` | Serve `.dataset.db` on the configured development port |
| `ui-dev` | Run Vite with hot reload against the Go API |
| `dataset` | Generate the full synthetic capture dataset into `.dataset` |
| `dataset-db` | Replay `.dataset` into `.dataset.db` |
| `ingest` | Replay captures from `CAPTURES` into the development database |
| `release` | Build, optionally Authenticode-sign, and write checksums |
| `release-snapshot` | Exercise the GoReleaser pipeline without publishing |
| `goreleaser-check` | Validate `.goreleaser.yaml` |
| `tools` | Install the macOS packaging toolchain with Homebrew |
| `clean` | Remove build output and the generated frontend bundle |

Plumbing targets, normally reached as prerequisites, include `lint`, `ui`,
`verify-embed`, `portable`, `installer`, `sign`, `validate`, `fixtures`,
`build-windows`, `build-ctl`, and `build-gen`.

## Command-line diagnostics

`lapdogctl` is separate from the GUI-subsystem tray executable so it can print
to a console. It ships as `lapdogctl.exe` in the portable archive.

```bash
make build-ctl
make build-windows
./dist/lapdogctl inspect <capture.lpd>
./dist/lapdogctl ingest <captures-dir> <lapdog.db>
./dist/lapdogctl summary <lapdog.db>
./dist/lapdogctl reclassify <lapdog.db>
./dist/lapdogctl serve <lapdog.db> [port]
```

`reclassify` recomputes labels from the provenance stored with each session. It
does not require the original capture. Settings' **Re-index saved captures** is
broader and destructive: it deletes historical rows and replays every retained
capture through the current collector. The same replay implementation backs
`lapdogctl ingest`.

## Build Windows artifacts

```bash
make build-windows  # dist/lapdog.exe and dist/lapdogctl.exe
make verify-embed   # verify windows/amd64 and the embedded interface
make portable       # zip both executables
make installer      # build the NSIS installer
make build          # CI checks followed by every artifact
```

`lapdog.exe` uses the Windows GUI subsystem and has no console. `lapdogctl.exe`
uses the console subsystem. `verify-embed` reads the target OS and architecture
from the binaries rather than trusting their names or headers.

The installer requires `/usr/local/bin/makensis` on this development machine.
Run commands that invoke it outside the Codex sandbox; a sandboxed exit is not a
valid installer result.

## Architecture

```text
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
| `internal/irsdk` | Telemetry headers, variable layout, row decoding, torn-read detection, and Windows shared-memory mapping |
| `internal/source` | Ordered live or replayed frames and replay pacing |
| `internal/capture` | Reading, writing, and pruning `.lpd` captures |
| `internal/sessionyaml` | Parsing simulator session-info YAML |
| `internal/classify` | Deriving session type and event context |
| `internal/collector` | Polling, time accounting, laps, position events, and segment lifecycle |
| `internal/store` | SQLite schema, migrations, writes, and aggregation queries |
| `internal/api` | JSON API, settings operations, and CSV/JSON export |
| `internal/web` | Serving the embedded React interface |
| `internal/tray` | Windows tray icon and menu |
| `internal/config` | Settings, paths, autostart, and installation state |
| `internal/applog` | Rotating application logs |
| `internal/synth` | Deterministic synthetic captures |
| `internal/ui/icons` | Raster and ICO generation for embedded icons |
| `web/` | React, TypeScript, TanStack Query/Table, and ECharts frontend |

The [design spec](docs/superpowers/specs/2026-08-04-lapdog-design.md),
[packaging spec](docs/superpowers/specs/2026-08-04-lapdog-packaging.md), and
[backend plan](docs/superpowers/plans/2026-08-04-lapdog-backend.md) contain the
reasoning and implementation history.

## Load-bearing constraints

- Keep `CGO_ENABLED=0`. It enables pure-Go SQLite and Windows
  cross-compilation without a MinGW toolchain.
- Preserve the distinction between absent values and real zeroes in Go and
  TypeScript.
- Never count replay playback as driving.
- Ignore offline placeholder ratings; a non-zero iRacing subsession identifies
  a registered online session.
- Treat captures as private. They contain customer identifiers and opponent
  names.
- Keep accumulated totals across telemetry gaps, but clear stale instantaneous
  values.
- Verify claims mechanically. A plausible build name, log line, or test is not
  evidence of the behavior it describes.

The frontend bundle is generated and not committed. `internal/web/dist/.gitkeep`
exists because `go:embed` must match something even on a fresh clone. The
collector owns one SQLite writer connection while readers are pooled, avoiding
`SQLITE_BUSY` by construction.

## Releases

Validate locally before creating a tag:

```bash
make goreleaser-check
make release-snapshot
```

Semantic version tags matching `v*.*.*` trigger `.github/workflows/release.yml`.
The workflow runs CI and GoReleaser, then publishes:

- `lapdog-windows-amd64.zip` for automatic updates
- `lapdog-<version>-portable.zip` for manual use
- `lapdog-<version>-setup.exe`
- `SHA256SUMS`

Annotated tag notes are the public release notes. Follow the repository's
`bump-version` workflow rather than creating an unreviewed tag: tags publish
software immediately.

Signing is optional. Without a configured certificate, artifacts remain
unsigned and Windows may show SmartScreen. `SHA256SUMS` verifies integrity but
does not establish publisher identity.

## Project records and outstanding work

| File | What it holds |
|---|---|
| `ToDo.md` | The next concrete Windows verification work |
| `SESSION.md` | Running history of changes, reasoning, and failures |
| `docs/server-design-brainstorming.md` | Parked multi-user SQLite investigation |
| `docs/superpowers/specs/` | Design specifications |
| `docs/superpowers/plans/` | Implementation plans |

The live reader, session classification, identity and online rating capture,
and `CarIsAI` have been verified against iRacing. Recorded on-track captures now
replay with laps, driving time, race results, and position events. The complete
installer/update replacement and rollback path still requires its targeted
Windows fake-release exercise. See [ToDo.md](ToDo.md) for the current procedure
rather than relying on a copied checklist here.

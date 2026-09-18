# Brake-it Integration Plan

**Date:** 2026-09-16

**Status:** Implemented with a synthetic built-in scenario. A Garage 61-derived
catalog remains gated on written permission, and the serial path still requires
a Windows hardware reality check.

## Goal

Incorporate Brake-it into the LapDog Windows application as a first-class,
locally served module under `/brake-it/*`. Users must be able to move between
LapDog and Brake-it in both directions, while both interfaces continue to ship
inside the single `lapdog.exe`.

The integration must preserve LapDog's existing constraints:

- `CGO_ENABLED=0` remains mandatory.
- The HTTP server remains bound to `127.0.0.1` only.
- Absent values remain distinct from zero.
- Other drivers' identifying data must not enter the public repository or
  release artifacts.
- Claims about browser, serial-device, and generated-scenario behavior must be
  limited to behavior that has actually been verified.

## Reviewed state

LapDog is currently clean. Brake-it has six uncommitted files containing
relevant work on generated brake tolerances and provenance display:

- `src/components/ScenarioEditor.tsx`
- `src/components/TraceChart.tsx`
- `src/data/generatedScenarios.json`
- `src/styles.css`
- `src/types.ts`
- `tools/garage61_generate.py`

The current Brake-it working tree, rather than only its last commit, is the
source for the integration. Those changes must be preserved without altering
the separate Brake-it repository during the port.

Brake-it is presently a browser-only React application. IndexedDB holds its
scenarios, settings, and results; Web Serial reads a pedal controller; Web
Audio produces cues; `requestAnimationFrame` drives the exercise and sampling.

## Target architecture

```text
http://127.0.0.1:<configured-port>/
├── /live, /dashboard, ...       LapDog interface
├── /brake-it                    redirects to /brake-it/simulator
├── /brake-it/simulator          pedal exercise
├── /brake-it/scenarios          scenario editor
├── /brake-it/devices            controller setup
├── /brake-it/results            practice history
├── /api/...                     existing LapDog API
└── /api/brake-it/...            Brake-it persistence API
```

Use one React application, one Vite build, one embedded asset tree, and one Go
HTTP server. Do not use an iframe, a second listener, a second Vite project, or
a separately bundled React runtime.

LapDog's existing extensionless-route fallback can serve every Brake-it route
on a direct request or browser refresh. The Go router needs new JSON endpoints,
not a second static-file handler.

## Data and permission gate

The current 1.1 MB Brake-it generated scenario pack contains 759 source-lap
records, 101 identifiable drivers, 102 lap identifiers, and 306 provenance
URLs. It must not be copied into LapDog history or a public release as-is.

Before importing any Garage 61-derived catalog:

1. Obtain express written permission for the authenticated API access,
   aggregation, redistribution, attribution, and source-link behavior described
   in `Garage61-Permission.md`.
2. Do not import names, driver IDs, driver slugs, raw telemetry, authenticated
   API URLs, or raw coaching transcripts. A local ignored catalog may retain a
   lap ID and Garage61 app link solely to provide the requested citation UI.
3. If permission is granted, produce a screened catalog containing the approved
   track/car labels, aggregate brake trace or parameters, aggregate percentiles,
   sample count, method, generated-at time, generator version, and whatever
   Garage61 lap-link attribution it approves.
4. If permission is not granted, ship only synthetic scenarios and scenarios
   derived from the user's own telemetry or LapDog captures.
5. Add an automated privacy check that rejects driver identity, credentials,
   raw telemetry, API URLs, and non-Garage61 citation URLs in every catalog.

The application and persistence work can proceed with the default synthetic
scenario while this decision is pending.

## Phase 1: Establish a reproducible baseline

1. Save the Brake-it working-tree diff outside the porting work so its six
   modified files cannot be lost or accidentally replaced with `HEAD`.
2. From a clean dependency installation, run Brake-it's TypeScript check and
   production build.
3. Exercise all four existing tabs with keyboard input and record desktop and
   narrow-width screenshots.
4. Record the current scenario count and the shape of a saved result without
   recording or publishing identifying provenance values.
5. Import only runtime source and approved data. Exclude Brake-it's `.git`,
   `node_modules`, `dist`, standalone package files, standalone `index.html`,
   caches, and raw source documents.

## Phase 2: Add durable storage to LapDog

Do not retain IndexedDB as the authoritative store. Browser storage is scoped
to hostname and port, so changing LapDog's configured port, clearing site data,
or opening another browser would make Brake-it history appear to vanish while
LapDog's SQLite history remained intact.

Add `internal/store/migrations/0004_brake_it.sql` and increment
`CurrentSchemaVersion`. Use these tables:

### `brake_scenarios`

- Stable text ID as the primary key.
- Name and description.
- Typed duration and pedal-target fields.
- Nullable transition duration and brake target where they are genuinely
  absent.
- Origin: `builtin` or `custom`.
- Catalog version and retired flag for built-ins.
- Optional, sanitized aggregate source metadata.
- Created and updated timestamps.

Built-in scenarios are read-only. A user edits one by duplicating it into a
custom scenario. Reconcile built-ins by stable ID and catalog version without
overwriting custom scenarios. Retire removed built-ins rather than deleting a
scenario referenced by historical runs.

### `brake_runs`

- Stable text ID.
- Scenario ID plus a scenario snapshot.
- Device label.
- Scoring-version identifier.
- Score and typed metrics, with nullable fields for measurements that were not
  observed.
- Creation timestamp.

The scenario snapshot and scoring version make a historical score interpretable
after a built-in scenario or scoring algorithm changes.

### `brake_samples`

- Run ID with `ON DELETE CASCADE`.
- Sequence number.
- Elapsed milliseconds.
- Accelerator and brake percentages.
- Input source.

Use a transaction for each complete run so a failed write cannot leave a run
without all of its samples.

### `brake_settings`

- Singleton settings row.
- Preferred scenario.
- Baud rate.
- Optional USB vendor and product filters.

Do not serialize `SerialPort` objects or the current time/index-based device IDs.
The active connection is transient React state. If a preferred device is saved,
identify it only through stable information the browser exposes.

Add store tests for migration, reopen, CRUD, uniqueness, nullable values,
transactions, built-in reconciliation, custom-scenario preservation, and run
sample cascades.

## Phase 3: Add the Brake-it API

Register specific routes before the existing `/api/` catch-all:

```text
GET    /api/brake-it/scenarios
POST   /api/brake-it/scenarios
PUT    /api/brake-it/scenarios/{id}
DELETE /api/brake-it/scenarios/{id}
GET    /api/brake-it/results
POST   /api/brake-it/results
GET    /api/brake-it/settings
PUT    /api/brake-it/settings
```

Use the existing same-origin JSON mutation protection. Apply a request-body
limit and validate:

- UUIDs and timestamps.
- Percentages within 0–100.
- Positive and bounded durations.
- Recognized transition modes.
- Monotonically increasing sample times.
- Bounded sample count and total run duration.
- Immutable built-in scenarios.
- References to existing or retired scenarios.

Return JSON errors with correct 400, 404, 405, 409, and 500 distinctions.
Add API tests for every successful method, rejected method, invalid value,
missing resource, oversized payload, and cross-site mutation attempt.

## Phase 4: Port the frontend

Create `web/src/brake-it/` and port the current Brake-it runtime code into it.
Keep LapDog's existing React, TypeScript, and Vite versions; do not turn this
feature into an incidental toolchain upgrade.

Refactor the current LapDog `App` into a top-level route switch with:

- `LapDogApp` for the existing interface.
- `BrakeItApp` at `/brake-it/*`.

Do not set a global BrowserRouter basename. Use nested relative links under the
Brake-it route instead.

Replace Brake-it's in-memory tab selector with routes:

- `/brake-it/simulator`
- `/brake-it/scenarios`
- `/brake-it/devices`
- `/brake-it/results`

Unknown Brake-it paths redirect to the simulator. Existing unknown LapDog paths
retain their current dashboard behavior.

Replace IndexedDB calls with a namespaced API client. Add explicit loading,
empty, and error states rather than leaving the application on an indefinite
`Loading` screen.

Keep these capabilities in the browser:

- Keyboard pedal simulation.
- Web Serial permission, connection, and live reads.
- Web Audio cues.
- Animation-frame timing and live sample capture.

Harden them during the port:

- Close and release an active serial reader when leaving Brake-it or unmounting
  the component.
- Close the audio context when it is no longer needed.
- Surface serial permission, open, read, and disconnect errors.
- Validate baud rate and USB filter input.
- Do not persist unstable array-index or timestamp-generated port IDs.
- State accurately that Web Serial requires a supporting browser; keyboard
  practice remains available otherwise.

## Phase 5: Navigation, styling, and identity

### Navigation

- Add a visually separated `Brake-It` entry near the bottom of LapDog's sidebar.
- Link it to `/brake-it/simulator` without carrying historical filter query
  parameters.
- Put `Back to LapDog` in the Brake-it header and link it to `/dashboard`.
- Use the Brake-it logo beside both navigation links.
- Set the document title to identify the active module.

### CSS isolation

Do not concatenate Brake-it's current stylesheet unchanged. It has global
element rules and generic class names that collide with LapDog.

- Give the module a `.brake-it` root.
- Scope every element selector under that root.
- Prefix generic structural classes with `brake-` where practical.
- Replace generic custom properties such as `--red` and `--surface` with scoped
  Brake-it names or LapDog design tokens.
- Make direct entry into `/brake-it/*` honor LapDog's light, dark, and system
  theme preference.
- Preserve Brake-it's wide simulator layout rather than putting it inside the
  narrow LapDog page column.
- Inspect every responsive breakpoint in a real browser.

### Icons and logo

Reuse LapDog's vendored Material Design icon system. Add the few missing SVGs
and typed names rather than adding Lucide as a second icon library and licensing
surface.

Create the logo according to `Brake-it-Logo.md`. Retain a 1024×1024 master and
verify its silhouette at 160, 32, and 16 pixels before using it in navigation.

## Phase 6: Catalog tooling and documentation

If Garage 61 grants permission:

1. Move the generator to `tools/brake-it/` as a developer-only tool.
2. Do not invoke it from ordinary builds, CI, or release builds.
3. Document its Python environment, dedicated token, rate limits, and approved
   endpoints.
4. Split private generation output from the screened distributable catalog.
5. Add schema/version validation and deterministic generation where possible.
6. Add a privacy test that fails on driver identity, API URLs, bearer tokens,
   unapproved raw telemetry, or citation links outside Garage61's app.

Update `README.md` and `DEVELOPMENT.md` with:

- The `/brake-it` entry point.
- What the simulator teaches.
- Browser and serial-controller requirements.
- Where scenarios and results are stored.
- The built-in/custom scenario distinction.
- The scoring-version behavior.
- Any approved Garage 61 attribution.

Do not import raw coaching transcripts into the public repository. If the
teaching rationale belongs in LapDog, write a concise original explanation.

## Phase 7: Verification

### Unit and contract tests

- Scenario timing at every phase boundary.
- Target accelerator and brake curves.
- Score calculation, including absent crossings.
- Time-in-band integration.
- Serial line parsing, normalization, and capability messages.
- Built-in catalog reconciliation.
- Store migration and CRUD behavior.
- API validation and mutation protection.
- Navigation targets and module route mapping.
- Privacy validation of distributable catalogs.

For every new assertion, temporarily break or remove the behavior, confirm the
test fails for the intended reason, then restore it.

### Browser verification

- Load and refresh every `/brake-it/*` route directly.
- Use both cross-application links.
- Complete a keyboard run and confirm it survives reload.
- Confirm the same result remains after changing the LapDog port and opening a
  different supported browser.
- Verify the unsupported-Web-Serial state without disabling keyboard input.
- Verify serial and audio resources are released when returning to LapDog.
- Capture and inspect desktop and narrow screenshots in light and dark themes.
- Confirm LapDog's existing pages have no CSS or layout regressions.

Extend the existing Chrome tooling rather than relying on JSX inspection for
visual claims.

### Build and platform verification

Run:

```bash
make ci
CGO_ENABLED=0 go test -race ./internal/collector/
```

Extend the embedded-interface check so both shipped Windows binaries contain a
Brake-it-specific marker as well as the LapDog interface and icon set.

Finally, perform a Windows Chrome or Edge reality check with an actual serial
pedal device. Verify permission, connection, capability negotiation, live
accelerator/brake input, disconnect, route-away cleanup, audio cues, result
persistence, and restart behavior. Until that succeeds, documentation and
release notes must describe hardware input as awaiting real-device verification.

## Completion criteria

The integration is complete when:

- Brake-it is reachable only through the LapDog executable at `/brake-it/*`.
- Direct route loads and refreshes work.
- Navigation works in both directions and is keyboard accessible.
- Brake-it data lives in LapDog's SQLite database and survives port and browser
  changes.
- Catalog upgrades do not overwrite custom scenarios.
- Leaving Brake-it releases browser hardware and audio resources.
- Brake-it styling does not affect LapDog pages.
- No unapproved Garage 61 data or identifying source-lap data appears in tracked
  files or release artifacts.
- Automated suites, cross-builds, embed checks, visual checks, and the final race
  run pass.
- The Windows hardware path has either been verified or is described without
  claiming that it has.

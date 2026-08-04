# LapDog — Packaging, Installer and Iconography Specification

**Date:** 2026-08-04
**Status:** Approved design; NSIS installer implemented, MSI deferred
**Companion to:** `2026-08-04-lapdog-design.md`

## 1. Purpose

This document covers how LapDog is built, packaged, signed and shipped, and where its iconography comes from. It exists separately from the main design spec because packaging changes on a different cadence from the application and has its own toolchain concerns.

Three requirements drive it:

- The web interface must be entirely inside the Go executable. No sidecar files, no install-time asset extraction.
- There must be a Windows installer, with a documented build process.
- Iconography must come from an open or Creative Commons set, used for the tray icon and throughout the interface.

## 2. Single-executable embedding

### 2.1 What is embedded

Everything the browser needs is compiled in with `embed`:

| Asset | Package | Source |
|---|---|---|
| Interface HTML, JS, CSS | `internal/web` | `internal/web/dist/`, produced by the frontend build (generated, not committed — see 2.3) |
| Icon set (25 SVGs) | `internal/ui/icons` | `internal/ui/icons/mdi/` |
| Icon licence text | `internal/ui/icons` | `internal/ui/icons/mdi/LICENSE` |
| Database migrations | `internal/store` | `internal/store/migrations/*.sql` |

`internal/web` also serves the icons at `/icons/`, so the frontend bundle does not carry a second copy of them. One copy in the binary, one licence to account for.

### 2.2 How it is enforced

Embedding is a verified property, not an intention.

- **`web.Check()`** runs at start-up and returns `ErrNoBundle` unless the bundle is present *and complete*: it reads the asset names back out of `index.html` and confirms each one is embedded. A build that forgot to run the frontend bundler fails loudly instead of serving a blank page that reads as a runtime fault.

  Checking only that `index.html` exists is not sufficient, and this is not hypothetical. A `dist/` line in `.gitignore` matched `internal/web/dist` as well as the intended build directory, while `index.html` itself was tracked from before that rule existed. A clone therefore held HTML referencing hashed assets that were absent, the binary built and started cleanly, and the interface was blank. The asset names are content-hashed per build, so they cannot be checked against a fixed list — they have to be read from the HTML that loads them.
- **No runtime file reads.** No code path in `internal/web` or `internal/ui/icons` opens a path on disk; every read goes through `embed.FS`.
- **Verified against a real Windows binary.** Building `internal/web` for `windows/amd64` and searching the output finds the interface HTML, the icon path data and the licence text inside it.

### 2.3 The bundle is generated, not committed

`internal/web/dist/` is gitignored apart from a `.gitkeep`, and CI builds it (`.github/workflows/ci.yml`).

The bundle is roughly a megabyte of minified JavaScript whose entire contents change on every UI edit, so committing it buries real diffs under regenerated noise. The cost is that `go build` alone is no longer sufficient to produce a working executable: `make ui` must run first. `make build-windows` and `make build-ctl` depend on the bundle so this cannot be forgotten, and CI fails if the bundle is ever committed again.

`.gitkeep` is tracked deliberately. `//go:embed all:dist` fails at *compile* time when its pattern matches nothing, so without a tracked file in that directory every Go build on a clean clone breaks — including `go vet` and binaries that never serve the interface. A directory holding only the placeholder still fails `web.Check()`, so the placeholder cannot be mistaken for an interface.

The Go tests that need the bundle skip when it is absent, so a clone without a Node toolchain can still run the suite. A skip that is reachable in CI is a test that has silently stopped running, so `LAPDOG_REQUIRE_BUNDLE=1` converts the skip into a failure; `make test-ci` and CI both set it.

### 2.4 Consequence for the installer

Because the executable is genuinely self-contained, the installer's only jobs are placing one file, creating shortcuts, registering an uninstaller, and optionally setting the startup entry. There is no dependency to install, no runtime to bundle, no VC++ redistributable. This is what makes a portable zip a legitimate distribution channel alongside the installer rather than a degraded one.

## 3. Iconography

### 3.1 Choice

**Material Design Icons**, from the Pictogrammers project, vendored at version 7.4.47.

Licence: the Pictogrammers Free License, under which the icons are Apache 2.0. That permits commercial use in a closed-source application and imposes no in-application attribution requirement. The licence text is redistributed inside the binary, as Apache 2.0 requires, and the settings screen displays it.

### 3.2 Why this set

The deciding factor was that it genuinely contains motorsport iconography rather than requiring generic substitutes. Verified present: `racing-helmet`, `flag-checkered`, `car-sports`, `go-kart`, `steering`, `tire`, `podium`, `speedometer`, `timer-outline`, `trophy`.

Alternatives considered and rejected:

| Set | Licence | Why not |
|---|---|---|
| **game-icons.net** | CC BY 3.0 | Richer racing flavour, but attribution is mandatory and must appear in the product, and the collection has no vehicle or motorsport category to draw a coherent set from. |
| **Font Awesome Free** | CC BY 4.0 (icons) | Attribution required; motorsport coverage thinner than MDI's. |
| **Tabler, Lucide, Feather** | MIT / ISC | Cleanest licences, but essentially no motorsport icons — a racing app would end up with a generic car and a generic flag. |
| **Commissioned or stock** | varies | Cost and licence-tracking burden for no functional gain. |

### 3.3 Vendoring rather than fetching

The SVGs are committed under `internal/ui/icons/mdi/` rather than pulled from a CDN or an npm dependency at build time. A release must be reproducible years later without depending on a CDN still being up or still serving identical bytes.

### 3.4 Tray icon rendering

The Windows tray needs an `.ico`. Rather than committing a binary icon file that no reviewer can inspect, the tray icon is rendered from the vendored `racing-helmet` SVG at start-up:

```
racing-helmet.svg
  └─ oksvg + rasterx (pure Go)   → image.RGBA alpha mask
       └─ tinted by state         → connected / paused / disconnected
            └─ PNG encode
                 └─ ICO container (hand-written, 22 bytes of header per image)
```

Sizes 16 and 32 are emitted, covering standard and scaled DPI. The ICO container is written directly because it is a trivial format and a dependency for it is not worth taking; its structure is unit-tested, since a malformed header shows up as a blank tray icon on Windows and is awkward to diagnose from a Mac.

`oksvg` and `rasterx` are pure Go, so they do not compromise `CGO_ENABLED=0`.

**Colour never carries meaning alone.** The three tray tints come from the project's status palette, but the tooltip and the menu header always state the connection state in words as well.

## 4. Build process

### 4.1 The toolchain runs on macOS

This is the significant finding, and it shapes everything below: **the complete Windows release — compile, package and sign — can be produced from a Mac with no Windows machine and no CI runner.**

| Step | Tool | Install |
|---|---|---|
| Compile | Go 1.26, `CGO_ENABLED=0` | already present |
| Installer | NSIS 3.12 | `brew install makensis` |
| MSI (optional, deferred) | msitools 0.106 (`wixl`) | `brew install msitools` |
| Authenticode signing | osslsigncode 2.14 | `brew install osslsigncode` |

`CGO_ENABLED=0` is load-bearing rather than a preference: it is why `modernc.org/sqlite` was chosen over the C bindings, and it is what removes the need for a mingw toolchain.

### 4.2 Stages

```
1. frontend      make ui                                -> internal/web/dist/
2. verify        make test-ci                           -> web.Check passes, no skips
3. compile       GOOS=windows GOARCH=amd64              -> dist/lapdog.exe
                 -ldflags "-H windowsgui -X …Version=…"
4. portable      zip                                    -> dist/lapdog-<ver>-windows-amd64.zip
5. installer     makensis packaging/windows/lapdog.nsi  -> dist/lapdog-<ver>-setup.exe
6. sign          osslsigncode (both exe and setup)      -> *-signed
7. checksums     shasum -a 256                          -> dist/SHA256SUMS
```

Stage 3 must be linked `-H windowsgui` so no console window appears. This is precisely why `lapdogctl` and `lapdog-gen` are separate binaries: a GUI-subsystem executable has no console and is useless as a CLI.

### 4.3 Architectures

`windows/amd64` is the shipped target. `windows/arm64` compiles cleanly and is built in CI as a regression check, but is not published until there is demand — iRacing itself does not run on Windows on ARM, so a native ARM build has no user today.

## 5. Installer

### 5.1 Choice: NSIS

**NSIS**, producing `lapdog-<version>-setup.exe`.

Chosen because `makensis` runs natively on macOS, so the release does not require a Windows machine or a Windows CI runner. Given the whole point of the `CGO_ENABLED=0` decision was to keep the build on one machine, adopting an installer that reintroduces a Windows dependency would undo that.

Alternatives:

| Option | Verdict |
|---|---|
| **Inno Setup** | Nicer default output and a more polished wizard, but `iscc` is Windows-only. It would need a Windows box or Wine, reintroducing exactly the dependency the toolchain choice removed. |
| **WiX / MSI** | MSI is what enterprise deployment expects — Group Policy, Intune, SCCM. `wixl` from msitools does build MSIs on macOS, so this is viable. Deferred rather than rejected: LapDog is a personal tool for one driver's own machine, so per-machine managed deployment is not a real use case yet. Section 5.4 records how to add it. |
| **MSIX** | Clean install and uninstall, Store distribution, built-in update. Requires a signing certificate, Windows-only tooling, and its startup-task model differs from the `HKCU` Run key the application already uses. Disproportionate for a tray utility. |
| **Portable zip only** | Legitimate, because the executable really is self-contained — and it ships alongside the installer for exactly that reason. On its own it gives no Start Menu entry, no uninstaller and no upgrade path, so it is the secondary channel, not the only one. |

### 5.2 Installer behaviour

| Aspect | Decision | Reason |
|---|---|---|
| Scope | Per-user (`HKCU`), installing to `%LOCALAPPDATA%\Programs\LapDog` | No elevation prompt, and it matches the app's existing per-user data directory and `HKCU` Run key. A tray app has no reason to need administrator rights. |
| Elevation | None requested | Follows from per-user scope. |
| Shortcuts | Start Menu always; Desktop optional, default off | A tray app is launched at login, so a desktop icon is clutter for most users. |
| Start with Windows | Checkbox, default on, writes the same `HKCU` Run key the app manages | The app already owns this setting, so the installer writes the identical value rather than a competing mechanism. |
| Uninstaller | Registered under `HKCU` Uninstall so it appears in Settings → Apps | |
| Data on uninstall | **Kept by default**, with an explicit opt-in checkbox to delete | Years of racing history must not be destroyed by an uninstall. Deleting it is the user's decision, made deliberately. |
| Running instance | Detected and the user asked to quit before proceeding | Overwriting a running executable fails on Windows. |
| Upgrade | Same product key overwrites in place; settings and database untouched | |
| Architecture guard | Refuses to run on non-64-bit Windows | |

### 5.3 What the installer does *not* do

- No bundled runtime, redistributable or dependency. The executable is self-contained.
- No file associations. LapDog owns no document type.
- No service installation. The design is a single user-session process (main spec §3).
- No firewall rule. The server binds loopback only and needs no inbound exception.

### 5.4 Adding an MSI later

If enterprise deployment is ever wanted, the addition is small because the payload is one file:

1. `brew install msitools`
2. Author `packaging/windows/lapdog.wxs` — a single `Component` for `lapdog.exe`, a shortcut, and the Run-key `RegistryValue`.
3. `wixl -o dist/lapdog-<ver>.msi packaging/windows/lapdog.wxs`
4. Sign with the same `osslsigncode` invocation.

Per-machine MSI installs would need the Run key to move from `HKCU` to `HKLM`, or the startup entry to be left entirely to the application. That is the only real design consequence.

## 6. Code signing

Unsigned Windows installers trigger SmartScreen warnings, and a new certificate has no reputation, so warnings persist until download volume accrues.

- Signing is **optional in the build**: absent a certificate, `make installer` succeeds and emits an unsigned artefact with a warning. A missing certificate must not block a development build.
- When present, both `lapdog.exe` and the installer are signed. Signing the inner executable matters because a user who runs it from the portable zip never sees the installer's signature.
- `osslsigncode` signs PE and MSI from macOS, given a PKCS#12 certificate and an RFC 3161 timestamp server.
- Certificate material is supplied by environment variable and never committed. `SIGN_PKCS12` and `SIGN_PASSWORD`.
- EV certificates bypass SmartScreen reputation-building but require hardware tokens, which are awkward to use from a Mac. Recorded as a known trade-off, not solved.

## 7. Versioning and release artefacts

Version is a single `VERSION` variable stamped into the binary through `-ldflags -X`, surfaced by `internal/version`, and reported by `/api/status` and `lapdogctl version`. Semantic versioning; tags are annotated, never squashed.

A release publishes:

```
lapdog-<version>-setup.exe                 installer, signed when a certificate is available
lapdog-<version>-windows-amd64.zip         portable, containing lapdog.exe and a README
SHA256SUMS                                 checksums for both
```

`lapdogctl` and `lapdog-gen` are development tools and are deliberately not published.

## 8. Decisions taken

**Installer format: NSIS only.** An MSI was considered and declined — LapDog is installed by the person driving, not deployed by a management tool, so the enterprise channel MSI exists to serve has no user. Section 5.4 records how to add one if that ever changes; nothing in the design forecloses it.

**Status:** `2026-08-04`, confirmed by the project owner.

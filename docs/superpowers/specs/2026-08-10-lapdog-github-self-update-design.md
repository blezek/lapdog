# LapDog GitHub Self-Update Specification

**Date:** 2026-08-10, revised 2026-08-16

**Status:** Implemented; Windows end-to-end verification pending

**Companion to:** `2026-08-04-lapdog-packaging.md`

## Purpose and release contract

LapDog updates its single installed `lapdog.exe` from stable semantic GitHub Releases
in `blezek/lapdog`. Drafts, prereleases and non-semantic tags are ignored. Development
builds and targets other than `windows/amd64` report updating disabled.

Each release supplies:

| Asset | Contract |
|---|---|
| `lapdog-windows-amd64.zip` | Self-update archive containing only `lapdog.exe` |
| `lapdog-<version>-portable.zip` | Manual portable archive with both executables |
| `lapdog-<version>-setup.exe` | NSIS installer |
| `SHA256SUMS` | SHA-256 integrity values for release assets |

GoReleaser builds the two Windows executables with `CGO_ENABLED=0`, gives only the tray
application the GUI subsystem, and stamps both semantic version and source commit.

## Discovery, consent and suppression

Release builds check shortly after startup when the persisted last check is at least 24
hours old, then every 24 hours. A check never opens a browser. A release is prompted once;
manual checks still display a skipped release. “Ask me later” persists a 24-hour
deferral. “Skip” persists that exact version, and a newer version clears the skip.

Installation always requires consent. “Upgrade after session” and “Upgrade now” mean the
same durable authorization: download and verify now, then replace and restart
automatically once recording and capture re-indexing are idle. Consent, last check,
deferral, skip, selected release, staging and pending restart live in an atomically
replaced updater JSON file under the data directory, not in user preferences.

## Artifact validation

`go-selfupdate` supplies stable semantic discovery and rollback-capable replacement.
LapDog owns staging because it additionally requires bounded archive/checksum responses,
an exact `SHA256SUMS` entry, no duplicate checksum, safe ZIP paths, no unexpected entries,
exactly one root `lapdog.exe`, no links, and bounded extracted size. The verified binary
is staged in the updater directory.

HTTPS plus the published SHA-256 file is integrity checking only. It does not authenticate
a publisher if a compromised release replaces both files. Detached signatures or
Authenticode verification remain future hardening.

## Recording and restart safety

`collector.Status.Recording` means a session segment is actively being written,
independent of optional capture-file retention. `TryQuiesce` atomically succeeds only
when no segment is active and prevents subsequent telemetry frames from opening one.
Re-index cannot begin after update consent, and apply waits while re-index is running.

The staged executable launches with internal handoff arguments and waits for the old PID
to exit normally, releasing SQLite, log, loopback port and tray resources. It preserves
the prior executable in the updater directory and uses rollback-aware replacement before
launching the installed path. A rollback-success path relaunches the old executable so
recording resumes; a rollback failure is reported distinctly. Failure to launch the
helper leaves the current process running and reports restart required.

Successful normal startup clears accepted/staged state and removes staging/backup files.
It updates the existing HKCU uninstall `DisplayVersion` only when that entry's
`InstallLocation` matches the running executable directory. Portable copies never create
installer registry entries. `lapdogctl.exe` remains a portable/installer update only.

## Interface and API

`GET /api/update` exposes current version, nullable revision, coordinator state, nullable
release and timestamps, prompt eligibility, recording/re-index/restart safety, and a
nullable actionable error. While transferring, it also exposes the archive bytes received
and the nullable server-reported total; verification is a separate progress phase. `POST
/api/update/check` refreshes metadata. `POST /api/update/action` accepts `install`,
`later`, `skip` or `shown`.

States are `disabled`, `checking`, `current`, `available`, `deferred`, `skipped`,
`downloading`, `waiting`, `applying`, `restart-required` and `failed`. Local mutation
routes require JSON and reject cross-origin browser fetch metadata or Origin, including
the pre-existing settings and capture re-index routes.

The sidebar shows a version badge and opens the update popdown once per discovered
version. Tray selection opens that popdown directly. Release notes support safe Markdown
without raw HTML. The popdown offers **Upgrade now**, **Ask me later**, and **Skip this
version**, and shows a determinate progress bar when a content length is known or an
indeterminate bar when it is not. Measured download percentage is also visible in the
tray. Settings shows version, nullable short revision, last check, and a manual check
action. Background discovery failures do not prompt; accepted download or apply failures
remain visible in the popdown and tray.

## Verification boundary

Local automated verification covers semantic selection, scheduling/suppression,
persistence, network and limits, checksum/ZIP rejection, quiescing, mutation protection,
rollback relaunch and safe note rendering. Cross-build, PE/stamp inspection, full CI,
snapshot release, race and real-Chrome checks are recorded in `SESSION.md` when run.

Production verification still requires the Windows fake-release procedure in `ToDo.md`
and a separate NSIS running-application upgrade test. Neither follows merely from local
cross-compilation.

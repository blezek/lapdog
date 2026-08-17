# LapDog GitHub Self-Update Implementation Record

**Status:** Implemented on 2026-08-16; Windows end-to-end verification remains.

The original manual-only plan was superseded by the automatic, consent-gated design in
`docs/superpowers/specs/2026-08-10-lapdog-github-self-update-design.md`.

## Implemented

- Stable semantic GitHub discovery through `go-selfupdate`, fixed to
  `blezek/lapdog` and `windows/amd64`.
- Startup/daily checks, durable 24-hour deferral, exact-version skip and newer-version
  override.
- Bounded archive/checksum downloads; SHA-256 verification; strict ZIP extraction;
  atomically persisted consent and staging state.
- Collector `Recording` and atomic `TryQuiesce`; re-index/update mutual exclusion.
- Delayed helper, old-PID wait, normal shutdown, preserved backup, rollback-aware
  replacement and relaunch.
- Version/revision stamping, update API, local mutation protection, sidebar popdown,
  Settings manual check, safe notes and dynamic Windows tray cue.
- Installer-only HKCU version reconciliation based on `InstallLocation`.

## Still requires Windows

- Exercise a local fake release through download, active-session waiting, graceful
  restart, version/revision change, registry reconciliation, permission/checksum failure
  and rollback.
- Separately re-test the NSIS running-app upgrade path. This is not implied by a
  successful self-update test.

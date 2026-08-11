# LapDog GitHub Self-Update Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub-backed self-update path for the Windows tray app while preserving the single-binary runtime model.

**Spec:** `docs/superpowers/specs/2026-08-10-lapdog-github-self-update-design.md`.

**Current preparation:** The GoReleaser release assets and GitHub release workflow are in place. The application updater code is not implemented by this plan-prep change.

## Global Constraints

- Preserve `CGO_ENABLED=0`.
- Preserve a single runtime executable for `lapdog.exe`.
- Ship only `windows/amd64` update assets until the packaging spec changes.
- Do not update from `dev` builds.
- Do not install updates silently in the first implementation.
- Validate the downloaded artifact before replacing the executable.
- Keep failures non-destructive: the current executable must remain runnable.

## Task 1: Release Asset Contract

**Files:**
- `.goreleaser.yaml`
- `.github/workflows/release.yml`
- `Makefile`
- `README.md`
- `docs/superpowers/specs/2026-08-04-lapdog-packaging.md`

**Status:** Implemented as release preparation.

- [x] Add GoReleaser OSS configuration for `cmd/lapdog` and `cmd/lapdogctl`.
- [x] Limit release builds to `windows/amd64`.
- [x] Produce `lapdog-windows-amd64.zip` containing only `lapdog.exe`.
- [x] Produce `lapdog-<version>-portable.zip` containing `lapdog.exe` and `lapdogctl.exe`.
- [x] Attach the existing NSIS installer as `lapdog-<version>-setup.exe`.
- [x] Emit `SHA256SUMS`.
- [x] Add a tag-triggered GitHub Actions release workflow.

## Task 2: Add Updater Package

**Files:**
- Create: `internal/updater/updater.go`
- Test: `internal/updater/updater_test.go`

- [ ] Add `github.com/creativeprojects/go-selfupdate`.
- [ ] Define a small local interface around release discovery and executable replacement so tests do not call GitHub.
- [ ] Return a typed result for current, available, updated and failed states.
- [ ] Treat `version.Version == "dev"` as non-updatable.
- [ ] Configure the GitHub source for `blezek/lapdog`.
- [ ] Configure checksum validation against `SHA256SUMS`.

## Task 3: Implement Manual Check

**Files:**
- Modify: `internal/updater/updater.go`
- Test: `internal/updater/updater_test.go`

- [ ] Compare the running semantic version with the latest stable release.
- [ ] Ignore draft and prerelease releases by default.
- [ ] Select only the `windows/amd64` asset.
- [ ] Report the new version, release URL and expected asset name without downloading during a check-only call.
- [ ] Surface GitHub rate-limit and network errors without retry loops.

## Task 4: Implement Apply

**Files:**
- Modify: `internal/updater/updater.go`
- Test: `internal/updater/updater_test.go`

- [ ] Download the selected release archive.
- [ ] Verify the archive checksum.
- [ ] Replace the running executable using the updater library's rollback-capable path.
- [ ] Return a restart-required result after replacement.
- [ ] Exercise permission-denied, checksum mismatch and missing-asset failures.

## Task 5: Expose User Controls

**Files:**
- Modify: `internal/tray/tray.go`
- Modify: `internal/tray/tray_windows.go`
- Modify: `internal/tray/tray_other.go`
- Modify: `web/src/pages/Settings.tsx`
- Modify as needed: `internal/api/handlers.go`, `web/src/api.ts`

- [ ] Add a tray "Check for updates" command on Windows.
- [ ] Add a settings action that calls the same updater path.
- [ ] Show current, available, failed and restart-required states.
- [ ] Disable update controls on non-Windows platforms and `dev` builds.
- [ ] Avoid checking automatically on app startup.

## Task 6: Release Verification

**Files:**
- Modify as needed: `.github/workflows/release.yml`
- Modify as needed: `README.md`

- [x] Run `goreleaser check`.
- [x] Run a local snapshot release and confirm asset names.
- [x] Confirm `lapdog-windows-amd64.zip` contains only `lapdog.exe`.
- [x] Confirm the portable archive contains both shipped binaries.
- [x] Confirm `SHA256SUMS` contains every uploaded artifact.
- [ ] On Windows, install v0.0.1, update to a later test release and verify the app restarts into the new version.

## Task 7: Future Hardening

- [ ] Add detached signature verification with an embedded public key, or verify Authenticode signatures after download.
- [ ] Add an optional background check interval with user control.
- [ ] Add a channel setting only if prereleases become useful to real users.

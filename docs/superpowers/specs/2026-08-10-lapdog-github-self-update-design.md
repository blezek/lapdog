# LapDog GitHub Self-Update Specification

**Date:** 2026-08-10
**Status:** Draft design; release asset preparation implemented
**Companion to:** `2026-08-04-lapdog-packaging.md`

## 1. Purpose

LapDog should be able to update itself from GitHub Releases without changing the
application's deployment model. The installed application remains one
self-contained `lapdog.exe`; GitHub is only the distribution point for release
archives, checksums and the human installer.

This document defines the release asset contract and the updater behaviour that
future implementation must follow.

## 2. Release Channel

Releases are GitHub Releases created from semantic version tags named
`vMAJOR.MINOR.PATCH`, for example `v0.0.1`.

The updater uses the stable release channel by default:

- Draft releases are ignored.
- Prereleases are ignored unless a future setting explicitly enables them.
- Development builds whose stamped version is `dev` do not offer self-update.
- Versions are compared as semantic versions, not as strings.

The repository currently has no configured remote. The release workflow assumes
the eventual GitHub remote is the same repository named by the Go module:
`github.com/blezek/lapdog`.

## 3. Targets

The only release target is `windows/amd64`.

That matches the packaging spec: iRacing is a Windows application, the shipping
LapDog binary is a Windows tray app, and Windows on ARM is not useful until
iRacing supports that environment. Cross-platform or `windows/arm64` update
assets are explicitly deferred.

## 4. Assets

Every GitHub release publishes these assets:

| Asset | Contents | Audience |
|---|---|---|
| `lapdog-windows-amd64.zip` | `lapdog.exe` only | Self-update client |
| `lapdog-<version>-portable.zip` | `lapdog.exe`, `lapdogctl.exe` | Human portable install |
| `lapdog-<version>-setup.exe` | NSIS installer for `lapdog.exe` | Human installer |
| `SHA256SUMS` | SHA-256 checksums for release artifacts | Humans and updater |

The self-update archive deliberately has no version in its filename. GitHub
already scopes assets by release tag, and a stable OS/architecture filename makes
asset discovery unambiguous for the updater. The archive contains only the GUI
tray executable because replacing `lapdog.exe` is the update operation.

The portable archive keeps `lapdogctl.exe`; it remains the diagnostic path for a
Windows machine with no Go toolchain.

## 5. GoReleaser

GoReleaser OSS is used to build and publish GitHub release assets. It is
configured to:

- Build `cmd/lapdog` as `windows/amd64` with `-H windowsgui`.
- Build `cmd/lapdogctl` as `windows/amd64` without the GUI subsystem flag.
- Stamp `internal/version.Version` from the semantic release tag.
- Run the existing frontend build before compiling so the embedded interface is
  present.
- Use the existing NSIS script to create the setup executable and attach it as a
  release extra file.
- Emit one `SHA256SUMS` file so the updater can validate the downloaded archive.

GoReleaser's native NSIS pipe is not used because it is a GoReleaser Pro feature.
The existing `makensis` path remains the installer authority.

## 6. Updater Behaviour

The updater should be implemented around `github.com/creativeprojects/go-selfupdate`.
It already provides GitHub release discovery, OS/architecture asset selection,
checksum validation and executable replacement/rollback helpers while preserving
LapDog's single-binary runtime model.

Expected user flow:

1. The user chooses "Check for updates" from the tray menu or settings page.
2. LapDog checks the latest stable GitHub Release.
3. If no newer version exists, it reports that the app is current.
4. If a newer version exists, it shows the version and asks before downloading.
5. The update archive is downloaded to a temporary file.
6. The archive checksum is verified against `SHA256SUMS`.
7. `lapdog.exe` is replaced in place.
8. The user is told to restart LapDog to run the new version.

Automatic background checks may be added later, but the first implementation is
manual. That avoids surprising replacement of a tray process while the release
pipeline and Windows installer path are still young.

## 7. Failure Handling

Update failures must leave the currently running executable intact.

The updater reports clear, user-actionable failures for:

- No network connection.
- GitHub API or rate-limit errors.
- No matching `windows/amd64` asset.
- Missing or mismatched checksum.
- Filesystem permission errors when replacing the executable.
- Running from a protected location that the current user cannot write.

Portable installs may live in arbitrary directories. A failed portable update is
therefore normal and must not corrupt the existing executable.

## 8. Security

The first implementation validates SHA-256 checksums from the release
`SHA256SUMS` asset over HTTPS. That protects against transfer errors and
accidental asset mismatches.

It does not protect against a compromised GitHub release that replaces both the
archive and checksum. A stronger future version should add detached signatures
with an embedded public key, or verify Authenticode signatures after download.

## 9. Out of Scope

- Silent automatic update installation.
- Multiple release channels.
- Downgrade support.
- Updating `lapdogctl.exe` from inside the running tray app.
- Enterprise package feeds such as winget, Chocolatey or MSI deployment.

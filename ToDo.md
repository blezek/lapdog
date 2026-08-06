# ToDo: get logs off the Windows machine to find why telemetry is not read

The one open question is why the Windows build recorded nothing from iRacing. Since the last attempt, `MapViewOfFile` was fixed to map the whole shared-memory section rather than a fixed 2 MiB — a request larger than the section fails outright, which is very likely the original cause — and the read path now logs every step. This task is to confirm it, or to find what else is wrong.

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
telemetry: connected to the simulator
```

Failure lines worth grepping for directly:

```powershell
Select-String -Path "$env:LOCALAPPDATA\lapdog\lapdog.log" -Pattern "could not|failed|not connected|unavailable"
```

- `mapping could not be opened` — the simulator was not running, or was not publishing.
- `view could not be mapped` — the original bug's signature. If this still appears after the fix, the size logic is still wrong and the reported byte counts say by how much.
- `region smaller than a header` / `region size query failed` — `VirtualQuery` fell back; the bound it used is logged.
- `simulator present but not connected` while on track — the status bit is not what is expected, and the raw value is logged beside it.
- `variable headers unparseable` — the 144-byte `irsdk_varHeader` stride may be wrong for this build of iRacing.

## Second task, if a capture file exists

`CarIsAI` is still an unverified guess from the SDK headers, and it drives whether a session is classified as AI. With a real capture in hand:

```cmd
lapdogctl.exe inspect -grep AI path\to\capture.lpd
lapdogctl.exe inspect -grep CarIsAI path\to\capture.lpd
```

That prints the matching session-YAML lines with line numbers. If the field is named something else, correct it and then run `lapdogctl reclassify` — the provenance columns exist precisely so this is a recomputation rather than lost data.

## Also outstanding, unrelated to telemetry

- **No git remote.** Everything is local only, and the CI workflow has never actually run — it is verified to parse and `make ci` passes, nothing more.
- **The installer has never been installed.** `make build` produces `lapdog-0.1.0-setup.exe` and the payload is confirmed present, but whether it installs, registers uninstall and starts the tray app is untested. Needs the same Windows machine.
- **Server-side collection** is parked in `docs/server-design-brainstorming.md`. No action pending.

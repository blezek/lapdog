# Agent instructions

For [Codex](https://openai.com/codex) or any agent without Claude Code's skill system. Part one orients you in the project; part two describes three git workflows that were originally Claude Code slash commands.

---

# Part one: orientation

## What this is

LapDog records how much time you actually spend in iRacing, and what you spend it on. A Windows tray application reads the simulator's shared memory once a second, writes sessions to a local SQLite database, and serves a React interface on `http://127.0.0.1:47047`. Everything ships as one `.exe` — interface, icons and migrations all compiled in.

macOS is the development platform. There is no simulator here, but everything else runs and the Windows binary cross-compiles.

## Where it stands

**Working and verified against a real simulator** (first confirmed 2026-08-06): telemetry reading, session classification, identity and rating capture, `CarIsAI` detection.

**Never exercised against reality:** driving itself. That first real test was conducted sitting in the pit box, so lap detection, the driving counter and position events have never run on real telemetry. Ratings from an online session have never been seen either — offline sessions report placeholders. `ToDo.md` holds the procedure for both.

**Not built:** self-update. The release pipeline produces the asset a future updater will consume; nothing consumes it.

## Constraints that are load-bearing

Each of these caused a real bug. None is stylistic.

- **`CGO_ENABLED=0` everywhere.** It is why `modernc.org/sqlite` was chosen over the C bindings and why the Windows binary cross-compiles from macOS. Never add a dependency needing cgo.
- **Absent and zero are different facts.** Pointers in Go, `| null` in TypeScript. A speed of zero is a real reading from a stationary car; an absent speed is not. An iRating of zero is a real value for an unrated licence.
- **Replay time is never counted.** There is deliberately no setting for it.
- **Offline sessions carry placeholder ratings.** iRacing reports `IRating: 1` and `LicString: "R 0.01"` offline, so ratings are recorded only when `WeekendInfo.SubSessionID` is non-zero. The customer id is kept either way.
- **Other drivers' data stays local.** The interface shows real opponent names, which is correct for your own capture of a session you were in. It must not be published — `/ignore/` is gitignored because recorded data holds real customer ids and every opponent in the field.
- **Accumulated totals are not stale; instantaneous values are.** A session total records what happened and survives a telemetry gap; a speed describes a moment that has passed and must clear.
- **Nothing may claim more than it knows.** This applies to code, comments, docs, interface copy and commit messages equally.

## The failure mode this project keeps hitting

**Asserting instead of verifying.** Not carelessness — plausible-looking claims that nobody checked. A representative sample, all real:

- A log line said "connected to the simulator" on mapping-open, before reading the connected bit. It fired once a second at the menus; 59% of a real log was a claim the next line contradicted.
- A comment called a fixed 2 MiB `MapViewOfFile` length safe. That length was the bug that recorded nothing for a week.
- `verify-embed` passed a `darwin/arm64` binary named `.exe`, because grepping for strings says nothing about the compilation target.
- A dashboard card printed `A 3.55` above a chart whose last point was 3.94.
- Six tests reached review that could not fail. Later, four of five task specs in one plan contained a test that could not fail or a claim that outran the code.

**The counter-measure is mechanical, not attentive:** delete the line a test covers, confirm the test fails, restore it. Do this for every new assertion. A `git status --short && echo clean` is the same mistake in miniature — `git status` exits 0 whether or not it prints, so that check always says clean.

## Verifying your work

```bash
make ci                    # fmt, vet, both suites, typecheck, cross-build, embed check
make test                  # Go and web suites
CGO_ENABLED=0 go test -race ./internal/collector/   # ~5 minutes; run once at the end
make run                   # serve .dataset.db on 47047 to look at the interface
```

Anything visual needs looking at, not reasoning about. `web/tools/shoot.mjs` and `verify-animation.mjs` drive real Chrome over the DevTools protocol with no dependencies beyond Node's built-in WebSocket.

## Where to read more

| File | What it holds |
|---|---|
| `README.md` | How to run it, all make targets, what will surprise you |
| `ToDo.md` | The next action, with the exact commands |
| `SESSION.md` | Running history, newest first — what changed, why, and what broke |
| `docs/superpowers/specs/` | Design specs, including the parked server-collection one |
| `docs/superpowers/plans/` | Implementation plans |

`git log` is the primary record; every message explains its reasoning.

---

# Part two: git workflows

Invoke them by name — "commit staged", "bump version", "push and PR" — or by the original slash names `/commit-staged`, `/bump-version`, `/push-and-pr`.

## Rules that bind all three

These are not stylistic. Each exists because its absence caused a real problem.

**Never stage files the user did not stage.** `commit-staged` commits the index as it finds it. If you think something is missing, say so and stop; do not run `git add` to be helpful. The user curates the index deliberately, often to split one working tree across several commits.

**Never commit or push without explicit approval,** unless the invocation carried a pre-approval argument. Approval for one commit is not approval for the next.

**Verify before you claim.** Do not report a tree clean, a suite green, or a push succeeded without having run the command and read its output. Note that `git status --short` exits 0 whether or not it prints anything, so `git status --short && echo clean` always prints "clean" — a check of that shape is worse than none.

**A push publishes.** Before the first push to any remote, check what the remote is and whether the history contains anything that should not be public — credentials, tokens, real customer or account identifiers, personal data. GitHub caches, forks and mirrors index commits, so deleting a value later does not reliably unpublish it. Check `git remote -v` and grep the tracked tree; if the remote is public and something sensitive is present, stop and report before pushing.

**Commit messages follow the [seven rules](https://cbea.ms/git-commit/).** Subject in the imperative under 50 characters, capitalised, no trailing period, blank line, body wrapped at 72 explaining *what* and *why* rather than *how*. The subject completes the sentence "When applied, this commit will ___".

---

# 1. `commit-staged` — commit what is already staged

**Optional argument:** `yes` means pre-approved; skip the approval prompt and commit directly. Any other argument, or none, requires explicit approval.

## Steps

**Gather state.** Run these three, and read all of them:

```bash
git status
git diff --staged
git log --oneline -10
```

The log is not decoration — match the repository's existing message style, which may be more or less verbose than your default.

**Verify something is staged.** If the index is empty, say so and stop. Do not suggest files to stage.

**Write the message** per the seven rules above, then append a `Files:` section listing every file with a one-line description of what changed in it:

```
Files:
- path/to/file — what changed
- path/to/other — what changed
```

**Present it** in a code block.

**Ask for approval** — "Shall I commit these changes? You can also suggest edits to the message." — unless the argument was `yes`. If the user requests edits, revise and show the message again before committing.

**Commit** using a heredoc, so the body's newlines and any special characters survive:

```bash
git commit -m "$(cat <<'EOF'
<subject>

<body>
EOF
)"
```

Confirm with `git log --oneline -1`.

**Push if an upstream exists.** Check `git rev-parse --abbrev-ref --symbolic-full-name @{u}`; if it resolves, `git push` and confirm. If it does not, skip silently — do not create an upstream unasked.

## Worth knowing

If a staged file also has *unstaged* changes on top, you are committing an older snapshot of it than the working tree holds. That is legitimate and usually intentional, but it is worth verifying the staged snapshot compiles on its own. `git checkout-index -a --prefix=/tmp/staged/` writes the index to a scratch directory without touching the working tree, so you can build and test exactly what will be committed.

---

# 2. `bump-version` — tag a release with synthesized notes

**Optional argument:** `major`, `minor`, or `patch`.

## Steps

**Sync tags** so version arithmetic is not stale:

```bash
git fetch --tags
```

**Find the current tag:**

```bash
git tag --list 'v*' | sort -V | tail -1
```

If there are no `v*` tags, **do not silently pick one.** Ask: "No existing `v*` tag found. What starting version should I use? (e.g. `v0.1.0`, `v1.0.0`)". Validate the reply against `vMAJOR.MINOR.PATCH`, digits only, leading `v`, and treat it as the baseline the bump applies to.

**Determine the release type.** Use the argument if given, otherwise ask "Release type: major, minor, or patch?". Then compute:

| Type | Effect | Example |
|---|---|---|
| `major` | increment MAJOR, reset MINOR and PATCH | `v1.2.3` → `v2.0.0` |
| `minor` | increment MINOR, reset PATCH | `v1.2.3` → `v1.3.0` |
| `patch` | increment PATCH | `v1.2.3` → `v1.2.4` |

Show `Current tag: v1.2.3 → Proposed: v1.3.0`.

**Gather context** over `<prev>..HEAD`, where `<prev>` is the current tag or the root commit if there is none:

```bash
git log <prev>..HEAD --no-merges --pretty=format:"%h %s%n%b%n---"
git log <prev>..HEAD --oneline
git diff --stat <prev>..HEAD
git diff <prev>..HEAD
```

If the oneline log is empty, stop: "No commits since `<prev>`. Nothing to tag."

**Synthesize thorough release notes.** Not a one-liner — these are read in `git show <tag>` and published to release pages. Read the full log, the stat and the diff. Omit any section with no items:

```
<3–6 sentence narrative: the theme of the release, why it matters, user-visible impact>

## Breaking Changes
- <change requiring user action>

## Features
- <new capability, with file or package hint where useful>

## Bug Fixes
- <defect corrected>

## Performance
- <speedup, allocation reduction, throughput>

## Refactoring
- <internal restructuring with no behaviour change>

## Documentation
- <docs, comments, README>

## Build & Tooling
- <Makefile, CI, dependencies, release pipeline>

## ToDo
- <housekeeping, or a TODO surfaced in commit messages>
```

Prefer detail over brevity, and quote commit subjects literally where they are already descriptive.

**Present the tag name and full notes,** then ask: "Shall I create tag `<version>` with these release notes and push to all remotes? You can also suggest edits to the notes." Wait for explicit approval. Revise and re-show if asked. **Do not create the tag before approval.**

**Create the annotated tag:**

```bash
git tag -a <version> -m "$(cat <<'NOTES'
<release notes>
NOTES
)"
```

**Push to every remote,** not just `origin`:

```bash
for remote in $(git remote); do
  git push "$remote" <version>
done
```

If any push fails, report which remote and stop. Do not delete the local tag without asking.

**Confirm:**

```bash
git show <version> --stat --no-patch
git tag --list 'v*' | sort -V | tail -3
git remote -v
```

Report the new tag, the remotes it reached, and the previous tag for context.

## Worth knowing

A tag is far more visible than a commit and often triggers a release pipeline. Before tagging, check whether the repository has a tag-triggered workflow (`.github/workflows/*.yml` matching `on: push: tags:`) — if so, the tag *publishes artefacts*, and the pre-push sensitivity check in the shared rules applies with more force.

**This repository has one:** `.github/workflows/release.yml` fires on `v*.*.*` and runs GoReleaser, publishing Windows executables, a portable zip, an NSIS installer and `SHA256SUMS` to a public GitHub Release. So `bump-version` here is not a bookkeeping operation — the tag ships software. `make goreleaser-check` and `make release-snapshot` exercise that pipeline locally, without publishing, and are worth running before the tag rather than after.

---

# 3. `push-and-pr` — push the branch and open a prefilled PR

**Optional argument:** `force` — proceed non-interactively, answering "yes" to concerns this workflow would otherwise raise, chiefly the dirty-tree stop. `force` does **not** override the genuine aborts below, since those make a PR impossible rather than merely risky. When `force` is set, still list what would have stopped you, note that you are continuing anyway, then continue.

## Hard rules

- **Never commit or stage,** with or without `force`. A dirty tree warns and stops; under `force` it warns and continues, but you still never commit.
- **Do not hardcode the base branch or the repository.** Detect both at runtime.
- **Do not use the `gh` CLI.** Open the PR by launching a GitHub compare URL in the browser. No API token, no auth handling — it rides the browser's existing session.

## Steps

**Preflight.**

1. `git rev-parse --abbrev-ref HEAD`. If it prints `HEAD`, abort — detached HEAD, nothing to PR.
2. Detect the base: `git symbolic-ref --short refs/remotes/origin/HEAD` gives e.g. `origin/develop`; strip `origin/`. If it fails, ask the user for the base branch. **In this repository it does fail** — `origin/HEAD` is unset, so asking is the live path, not the fallback. `git remote set-head origin -a` fixes it permanently if the user wants that.
3. If the current branch equals the base, abort — nothing to compare.
4. `git status --porcelain`. If non-empty, list the changes and stop with: "Uncommitted changes present — commit first, then re-run." Under `force`, list them, note that only committed work reaches the PR, and continue.

**Push and set upstream:**

```bash
git push -u origin <branch>
```

If the push fails, surface the error and stop — do not open the page. The head branch must exist on `origin` for the compare view to work.

**Summarize the branch** relative to the base:

```bash
git log <base>..HEAD --oneline
git diff --stat <base>..HEAD
git diff <base>..HEAD -- <path>   # for files with functional, non-formatting changes
```

Then write:

- **Title** — what actually changed, imperative, roughly 70 characters or fewer. Use the branch name as a hint to intent, not as text to transcribe.
- **Body** — `## Summary`, one short paragraph on what the branch does and why; then `## Changes`, bullets grouped by area, marking formatting-only changes as such. **No "Test plan" section.**

**Open the PR** by building the compare URL from the remote:

```bash
remote=$(git remote get-url origin)
slug=$(printf '%s' "$remote" | sed -E 's#^git@[^:]+:##; s#^https?://[^/]+/##; s#\.git$##')
host=$(printf '%s' "$remote" | sed -E 's#^git@([^:]+):.*#\1#; s#^https?://([^/]+)/.*#\1#')

title_enc=$(python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1]))' "$title")
body_enc=$(python3 -c 'import urllib.parse,sys;print(urllib.parse.quote(sys.argv[1]))' "$body")

url="https://$host/$slug/compare/$base...$branch?expand=1&title=$title_enc&body=$body_enc"
open "$url"        # macOS; xdg-open on Linux
```

The browser opens the compare view prefilled; the user reviews and clicks "Create pull request".

## Worth knowing: the host derivation breaks on SSH aliases, including this repo's

The derivation above handles SSH (`git@host:owner/repo.git`) and HTTPS (`https://host/owner/repo.git`) remotes. It does **not** handle an SSH host alias, and this repository has one.

`origin` is `git@public.github.com:blezek/lapdog.git`, which yields host `public.github.com`. That is an alias defined in `~/.ssh/config` pointing at `HostName github.com`; it exists only for SSH key selection and **does not resolve over HTTPS** — `curl https://public.github.com/...` fails to connect. So the compare URL built naively is dead.

Resolve the alias before building the URL:

```bash
host=$(printf '%s' "$remote" | sed -E 's#^git@([^:]+):.*#\1#; s#^https?://([^/]+)/.*#\1#')
# An SSH alias is not necessarily a web host. Prefer ssh config's real HostName.
real=$(ssh -G "$host" 2>/dev/null | awk '/^hostname /{print $2; exit}')
[ -n "$real" ] && host=$real
```

`ssh -G` prints the effective configuration for a host, so this works for any alias without parsing `~/.ssh/config` by hand. Verify the page loads before reporting success; a compare URL on an unreachable host fails silently as a browser error the agent never sees.

Very long bodies can exceed browser or server URL limits and get silently truncated in the prefill. If the opened page looks short, shorten the body and paste the remainder into the PR manually.

---

## Choosing between them

| Situation | Workflow |
|---|---|
| Index curated, want it committed | `commit-staged` |
| Work committed, want it reviewed | `push-and-pr` |
| Work merged, want it released | `bump-version` |

They compose in that order. `push-and-pr` refuses a dirty tree, so `commit-staged` runs first; `bump-version` tags what is already on the branch, so it runs last.

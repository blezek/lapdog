---
name: bump-version
description: Prepare, annotate, and publish the next semantic-version release tag from committed LapDog history. Use when the user says "bump version", invokes $bump-version, or requests a major, minor, or patch release. A tag publishes public GoReleaser artifacts, so approval and release verification are mandatory.
---

# Bump Version

Treat a release tag as a software publication, not bookkeeping. Never commit,
tag, or push unasked.

## Select the version

Run `git fetch --tags`, then find the newest semantic tag:

```bash
git tag --list 'v*' | sort -V | tail -1
```

If no tag exists, ask for a starting baseline such as `v0.1.0`. Require the
exact form `vMAJOR.MINOR.PATCH`, with digits only. The requested bump applies
to that baseline; do not silently choose an initial release.

Use a supplied `major`, `minor`, or `patch` argument. Otherwise ask which type
to use. Compute the next version conventionally and show:

```text
Current tag: v1.2.3 → Proposed: v1.3.0
```

## Verify release readiness

This repository publishes on tags matching `v*.*.*` through
`.github/workflows/release.yml`. Before proposing the tag:

1. Run `make goreleaser-check` and `make release-snapshot` when practical;
   these exercise the publishing pipeline without publishing. If either is not
   run, say so before asking for tag approval.
2. Inspect `git remote -v`.
3. Inspect the commits and tracked diff being published for credentials,
   tokens, real customer identifiers, private captures, and personal data.
4. Stop if a performed check fails or anything sensitive would become public.

## Read the release history

Use the current tag as `<prev>`, or the root commit when no tag exists. Run and
read all of:

```bash
git log <prev>..HEAD --no-merges --pretty=format:"%h %s%n%b%n---"
git log <prev>..HEAD --oneline
git diff --stat <prev>..HEAD
git diff <prev>..HEAD
```

Stop with `No commits since <prev>. Nothing to tag.` when the oneline log is
empty.

Check whether `<prev>` produced a published release and its expected artifacts.
If that release failed before publication, its changes are still unreleased:
read its annotated notes and include those changes in the new release notes.

## Draft and approve release notes

Write a 3–6 sentence narrative covering the release theme, rationale, and
user-visible effect. Add only non-empty sections from this list:

```markdown
## Breaking Changes
## Features
## Bug Fixes
## Performance
## Refactoring
## Documentation
## Build & Tooling
## ToDo
```

Prefer useful detail and quote already-descriptive commit subjects literally.
Present the tag and full notes, then ask:
"Shall I create tag `<version>` with these release notes and push to all
remotes? You can also suggest edits to the notes."

Wait for explicit approval. Re-present revised notes before tagging.

## Tag, publish, and confirm

Create an annotated tag with a heredoc. `--cleanup=verbatim` is required because
Git otherwise removes Markdown headings beginning with `#` from the annotation:

```bash
git tag -a <version> --cleanup=verbatim -m "$(cat <<'NOTES'
<release notes>
NOTES
)"
```

Push the tag to every remote:

```bash
for remote in $(git remote); do
  git push "$remote" <version>
done
```

If a push fails, identify the remote and stop. Do not delete the local tag
without permission. Confirm with:

```bash
git show <version> --stat --no-patch
git tag --list 'v*' | sort -V | tail -3
git remote -v
```

The release workflow extracts the annotated tag contents and passes them to
GoReleaser with `--release-notes`. Follow the tag-triggered workflow through
completion, then verify that the public release body matches the annotation and
that all expected artifacts exist. Report the new and previous tags, every
remote reached, workflow result, release-note result, and published assets.

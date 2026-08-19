---
name: push-and-pr
description: Push the current committed branch to origin and create a GitHub pull request with a generated title and body, falling back to a prefilled compare page when the GitHub CLI cannot create it. Use when the user says "push and PR", invokes $push-and-pr, or asks to publish a branch for review. Accept optional `force` to continue with a dirty tree while never staging or committing its changes.
---

# Push and PR

Push only committed history and create a pull request with `gh` when possible.
If `gh` is unavailable or errors, open a prefilled GitHub compare page instead.
Never stage or commit, including in force mode.

## Interpret force

Without `force`, list a dirty tree and stop with:
`Uncommitted changes present — commit first, then re-run.`

With `force`, list what would have stopped the workflow, state that only
committed work reaches the PR, and continue. Force never overrides a detached
HEAD, being on the base branch, a failed push, sensitive content, or another
condition that makes publication impossible.

## Preflight

1. Record the branch once with `git rev-parse --abbrev-ref HEAD`. Stop on
   detached `HEAD`.
2. Detect the base with
   `git symbolic-ref --short refs/remotes/origin/HEAD` and strip `origin/`.
   Ask for the base if detection fails; never hardcode it.
3. Stop if branch equals base.
4. Read `git status --porcelain` and apply the dirty-tree rule above.
5. Inspect `git remote -v` and the committed `<base>..HEAD` history for
   credentials, tokens, real customer identifiers, private captures, and
   personal data. Stop before publishing sensitive material.

Because another process can switch a shared worktree, verify the current branch
still equals the recorded branch immediately before pushing and before creating
or opening the pull request. Stop on a mismatch; never substitute the new branch
silently.

## Push

Run:

```bash
git push -u origin <recorded-branch>
```

Surface a failure and stop without opening a page.

## Summarize the committed branch

Read:

```bash
git log <base>..HEAD --oneline
git diff --stat <base>..HEAD
git diff <base>..HEAD -- <functional-path>
```

Create an imperative title of about 70 characters or fewer. Write a body with
`## Summary` followed by a short rationale, then `## Changes` with bullets
grouped by area. Mark formatting-only work. Do not add a test-plan section.

## Create with GitHub CLI

Recheck the current branch against the recorded branch. If `gh` is available,
attempt the non-interactive creation first. Remove `GITHUB_TOKEN` from every
`gh` invocation so an invalid injected token cannot override the CLI's stored
authentication; scope the removal to that command rather than changing the
calling shell:

```bash
env -u GITHUB_TOKEN gh pr create \
  --base <base> \
  --head <recorded-branch> \
  --title <title> \
  --body <body>
```

Capture the command output and exit status. On success, retain the pull-request
URL printed by `gh`. If creation reports an error, make one read-only attempt to
resolve an already-created PR with:

```bash
env -u GITHUB_TOKEN gh pr view <recorded-branch> --json url --jq .url
```

If that returns a URL, use it rather than opening a duplicate. Otherwise report
the concise `gh` failure and continue immediately to the compare-URL fallback.
Do not repeatedly retry a failing external mutation.

## Fall back to the compare URL

Use this path only when `gh` is unavailable or both CLI commands above fail.

Derive the repository slug and host from `git remote get-url origin`. For SSH
remotes, resolve aliases with `ssh -G` and prefer its `hostname` result; an SSH
alias is not necessarily a valid HTTPS host.

URL-encode the title and body, then build:

```text
https://<host>/<slug>/compare/<base>...<recorded-branch>?expand=1&title=<title>&body=<body>
```

Recheck the current branch against the recorded branch. Open the URL with
`open` on macOS or `xdg-open` on Linux. Verify the page when possible before
reporting that the creation page opened. A private repository may return 404 to
an anonymous HTTP check even though the authenticated browser works; report that
limitation instead of claiming verification.

If URL length truncates the body, shorten it and tell the user to paste the
remainder manually. This fallback does not itself create a pull request: the
user reviews the page and clicks Create pull request. Never describe the compare
URL as an already-created PR.

## Final response

State whether the push succeeded and whether `gh` created or found the pull
request. End the response with a Markdown link on its own final line:

```text
Pull request: [<owner>/<repo>#<number>](<pull-request-url>)
```

When the browser fallback was necessary and no PR exists yet, end honestly with
the creation link instead:

```text
Create pull request: [Open the prefilled GitHub page](<compare-url>)
```

---
name: push-and-pr
description: Push the current committed branch to origin and open a GitHub compare page with a generated PR title and body. Use when the user says "push and PR", invokes $push-and-pr, or asks to publish a branch for review. Accept optional `force` to continue with a dirty tree while never staging or committing its changes.
---

# Push and PR

Push only committed history and open a prefilled GitHub compare page. Never
stage or commit, including in force mode. Do not use the `gh` CLI.

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
still equals the recorded branch immediately before pushing and before opening
the URL. Stop on a mismatch; never substitute the new branch silently.

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

## Build and open the compare URL

Derive the repository slug and host from `git remote get-url origin`. For SSH
remotes, resolve aliases with `ssh -G` and prefer its `hostname` result; an SSH
alias is not necessarily a valid HTTPS host.

URL-encode the title and body, then build:

```text
https://<host>/<slug>/compare/<base>...<recorded-branch>?expand=1&title=<title>&body=<body>
```

Recheck the current branch against the recorded branch. Open the URL with
`open` on macOS or `xdg-open` on Linux. Verify the page when possible before
reporting success. A private repository may return 404 to an anonymous HTTP
check even though the authenticated browser works; report that limitation
instead of claiming verification.

If URL length truncates the body, shorten it and tell the user to paste the
remainder manually. The user reviews the page and clicks Create pull request.

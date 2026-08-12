---
name: worktree
description: Turn a short description of intended work into a branch name and check that branch out as a git worktree under ./worktrees. Use when the user says "worktree", invokes $worktree, or asks to start a piece of work in an isolated worktree. The rest of the prompt is the work description.
---

# Worktree

Take the work description that follows the invocation, derive a branch name from
it, and create a git worktree for that branch **under `./worktrees/` in the
repository**. The location is not negotiable: every worktree this skill creates
lives beneath `./worktrees/`, which is gitignored and never repository content.

## Rules

- The worktree path is always `worktrees/<dir>` relative to the repository root.
  Never create it anywhere else, and never create a branch without a worktree.
- Derive the branch and directory from the description, do not invent unrelated
  scope. If the description is empty or too vague to name, ask for one and stop.
- Do not clobber. If the branch or the target directory already exists, stop and
  report rather than overwriting or reusing it.
- Never commit, push, or modify tracked files here. This skill only creates a
  branch and its worktree; the work happens afterward.

## Derive the branch name

Read everything after the invocation as the description of the work.

1. Choose a type prefix from the intent: `feat/` for a new capability, `fix/`
   for a defect, `chore/` for tooling, config or docs, `refactor/` for internal
   restructuring. When unsure between two, prefer the one the description leads
   with.
2. Summarise the description into two to four words, lowercase, kebab-case, no
   trailing punctuation. Aim for under ~40 characters total.
3. The result is `<type>/<summary>`, e.g. "$worktree add a dark mode toggle" →
   `feat/dark-mode-toggle`, "$worktree the export button 404s" → `fix/export-404`.

State the chosen branch name before creating anything.

## Choose the base

Start the branch from the latest default branch, not from whatever is currently
checked out, so the worktree begins clean and independent of in-progress work.

```bash
root=$(git rev-parse --show-toplevel)
cd "$root"
git fetch origin --quiet 2>/dev/null || true
# Default branch, e.g. origin/main; fall back to main, then to HEAD.
base=$(git symbolic-ref --quiet --short refs/remotes/origin/HEAD 2>/dev/null)
[ -z "$base" ] && git show-ref --verify --quiet refs/heads/main && base=main
[ -z "$base" ] && base=HEAD
```

If the description explicitly names a different base ("off the release branch"),
honour that instead.

## Create the worktree

The directory name is the branch with any `/` replaced by `-`, so the tree stays
flat under `worktrees/`.

```bash
branch="<derived branch>"
dir="worktrees/$(printf '%s' "$branch" | tr '/' '-')"

# Refuse to clobber an existing branch or directory.
if git show-ref --verify --quiet "refs/heads/$branch"; then
  echo "branch $branch already exists — stopping"; exit 1
fi
[ -e "$dir" ] && { echo "$dir already exists — stopping"; exit 1; }

git worktree add "$dir" -b "$branch" "$base"
```

## Confirm gitignore

Ensure `/worktrees/` is ignored so a worktree never becomes repository content.
It usually already is; add it only if missing:

```bash
grep -qxF '/worktrees/' "$root/.gitignore" || printf '\n/worktrees/\n' >> "$root/.gitignore"
```

## Report

Tell the user the branch, the base it was cut from, and the path, with how to
enter it:

```bash
git worktree list
echo "created worktree at $dir on $branch (from $base)"
echo "cd $dir"
```

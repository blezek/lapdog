---
name: commit-staged
description: Commit exactly the files and snapshots already in the Git index. Use when the user says "commit staged", invokes $commit-staged, or asks to commit the curated index without staging other work. Accept an optional `yes` argument as advance approval.
---

# Commit Staged

Commit the index exactly as the user curated it. Never stage another file.

## Safety rules

- Never run `git add` or otherwise change the index before the commit.
- Never commit without explicit approval unless the invocation includes `yes`.
- Approval for this commit does not authorize another commit.
- Verify every reported result from command output.
- Before the first push to a remote, inspect the remote and committed content
  for credentials, tokens, real customer identifiers, or personal data. Stop
  before publishing anything sensitive.

## Inspect the index

Run and read all three commands:

```bash
git status
git diff --staged
git log --oneline -10
```

Stop if nothing is staged. Do not suggest or stage missing files.

If a staged file also has unstaged changes, explicitly note that the index is
an older snapshot. When compilation risk warrants it, export the index to a
temporary directory with `git checkout-index -a --prefix=<temp>/` and verify
that snapshot without touching the working tree.

## Draft the message

Follow the repository's recent message style and the seven commit-message
rules: imperative capitalized subject under 50 characters, no trailing period,
a blank line, and a body wrapped at 72 columns explaining what and why.

Append a `Files:` section listing every staged file and one concise description:

```text
Files:
- path/to/file — what changed
- path/to/other — what changed
```

Present the complete message in a code block. Unless `yes` was supplied, ask:
"Shall I commit these changes? You can also suggest edits to the message."
Wait for explicit approval and re-present any revision before committing.

## Commit and confirm

Use a heredoc so special characters and newlines survive:

```bash
git commit -m "$(cat <<'EOF'
<message>
EOF
)"
```

Confirm with `git log --oneline -1`.

Check for an upstream with:

```bash
git rev-parse --abbrev-ref --symbolic-full-name '@{u}'
```

If no upstream exists, stop silently without creating one. If it exists,
perform the sensitivity check, run `git push`, and verify the push output.

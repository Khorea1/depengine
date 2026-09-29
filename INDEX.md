# .dev/

Persistent working context shared by humans and agents across sessions: quick
notes, drafts, plans, research, decisions. It is **not** a source of truth and is
not auto-injected into sessions. Link the specific file that matters from a task
prompt, or from `AGENTS.md` when the reference is durable and broadly useful.

By default, `.dev/` is not merged or committed to `main`, unless the repository
defines a different policy. It often lives on its own orphaned branch/worktree.

## Access from another worktree

Do not assume the current code worktree contains a nested `.dev/` checkout. All
worktrees share the repository's refs, so first resolve the checkout that owns
`dev-notes`:

```sh
dev_notes_root="$(
  git worktree list --porcelain |
  awk '
    $1 == "worktree" { sub(/^worktree /, ""); wt = $0 }
    $1 == "branch" && $2 == "refs/heads/dev-notes" { print wt; exit }
  '
)"
```

When that prints a path, read working context through
`"$dev_notes_root"` (for example
`"$dev_notes_root/architecture/index.yaml"`). This keeps `wt switch` and other
sibling worktrees independent without making agents reach into some guessed
primary-checkout path.

If the branch exists but is not checked out as a worktree, single files remain
available through Git itself:

```sh
git show dev-notes:INDEX.md
git show dev-notes:architecture/index.yaml
```

For tools that need the whole architecture directory, materialize a disposable
snapshot instead of copying it into the code worktree:

```sh
tmp="$(mktemp -d)"
git archive dev-notes architecture | tar -x -C "$tmp"
python "$tmp/architecture/tools/validate.py" --root "$tmp/architecture" --repo .
```

## File map

| File | Purpose |
|------|---------|
| `cleaning.md` | Procedure for consolidating scratch material and promoting verified knowledge |
| `TODO.md` | Session-specific queue and unresolved editorial/CLI ideas |
| `bad-writing-findings.md` | Two unresolved wording findings in ADR-002 |
| `architecture/` | archmap: selective semantic architecture model with code-revision checks, symbol-navigation anchors, and derived visual views. Start at `architecture/README.md` |

## Lifecycle

Keep active files current. When something is finished, superseded, or no longer
actionable, delete it, or move it to `archive/` if the history has expected future
value. Do not let archived material look active. See `cleaning.md` for the full
cleanup procedure.

Cached external material records its source URL or document ID, retrieval date,
and relevant product/API/version. Refresh it before relying on it for freshness-
sensitive decisions.

## Conventions

- Use descriptive lowercase kebab-case names, prefixed by kind (`plan-`,
  `retrieved-`) where relevant.
- Put current state at the top of long-lived files, not buried in chronology.
- Do not promote `.dev/` content to `AGENTS.md` automatically. Only verified,
  durable, repo-specific facts qualify (see `cleaning.md`).

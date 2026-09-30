# .dev/

Persistent working context shared by humans and agents across sessions: quick
notes, drafts, plans, research, decisions. It is **not** a source of truth and is
not auto-injected into sessions. Link the specific file that matters from a task
prompt, or from `AGENTS.md` when the reference is durable and broadly useful.

In Depengine, `.dev/` is a worktree of the orphaned `dev-notes` branch nested
inside the primary repository checkout. The filesystem workspace may also contain
ignored or symlinked control metadata that is intentionally absent from the
branch itself. In particular, `cleaning.md` is supplied from local dotfiles and
must not be committed to `dev-notes`.

## Access from another worktree

The nested `.dev/` path exists only in the primary checkout that contains that
worktree. Sibling code worktrees created by helpers such as `wt switch` should
resolve the checkout that owns `dev-notes` instead of assuming their own
`.dev/` path exists:

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
`"$dev_notes_root/architecture/index.yaml"`). This preserves the intentional
nested-worktree layout without hard-coding the location of the primary checkout.

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
| `cleaning.md` | Local-only cleanup procedure supplied from dotfiles; intentionally ignored by the `dev-notes` branch |
| `TODO.md` | Session-specific queue and unresolved editorial/CLI ideas |
| `architecture/` | archmap: selective semantic architecture model with code-revision checks, symbol-navigation anchors, and derived visual views. Start at `architecture/README.md` |

## Lifecycle

Keep active files current. When something is finished, superseded, or no longer
actionable, delete it, or move it to `archive/` if the history has expected future
value. Do not let archived material look active. The local `cleaning.md` control
file defines the full cleanup procedure when present in the workspace.

Cached external material records its source URL or document ID, retrieval date,
and relevant product/API/version. Refresh it before relying on it for freshness-
sensitive decisions.

## Conventions

- Use descriptive lowercase kebab-case names, prefixed by kind (`plan-`,
  `retrieved-`) where relevant.
- Put current state at the top of long-lived files, not buried in chronology.
- Do not promote `.dev/` content to `AGENTS.md` automatically. Only verified,
  durable, repo-specific facts qualify; the local `cleaning.md` control file
  defines that promotion procedure.

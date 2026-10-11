# .dev/

Persistent working context shared by humans and agents across sessions: quick
notes, drafts, plans, research, decisions, and Beads task state. General
notes are **not** a source of truth for project behavior, and the directory is
not auto-injected into sessions. Beads is authoritative only for the state of
tracked agent tasks. Link the specific file that matters from a task prompt
or from local agent instructions.

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

### Beads task state

The shared agent-task workspace lives only in this branch's `.beads/`
(normally `.dev/.beads/` in the primary checkout). The primary checkout
provides automatic discovery through a native `br` redirect:
`.beads/redirect` contains the absolute path of `.dev/.beads/`, so a plain
`br` invocation from any worktree of this repository resolves to the shared
database — no `BEADS_DB` export, wrapper, or per-harness configuration.
Verify with:

```sh
br where --json
br ready --json
```

One-time setup (already done in the primary checkout; repeat only if the
redirect is missing or the checkout moved — note `pwd -P` is used instead
of a `cd` subshell so shell directory hooks cannot pollute the file):

```sh
# From the primary checkout root
mkdir -p .beads
printf '%s\n' "$(pwd -P)/.dev/.beads" > .beads/redirect
exclude="$(git rev-parse --git-path info/exclude)"
grep -qxF '/.beads/redirect' "$exclude" ||
  printf '\n/.beads/redirect\n' >> "$exclude"
br where --json
```

The redirect path is absolute on purpose: helpers that copy ignored files
into new worktrees (e.g. `wt step copy-ignored`) keep a valid pointer.
`.beads/redirect` is worktree-local (kept in `info/exclude`, not
`.gitignore`) and never committed. The checked-out `dev-notes` worktree
must exist for read/write operations; a `git show` of `issues.jsonl` is
only a read-only fallback, not an active Beads workspace. If the worktree
is absent, do not create a second `.beads/` under `master` or a feature
worktree. If a worktree already has its own `.beads/` database, inspect it
before replacing it with a redirect.

`br init --prefix dep` was run here once to bootstrap the workspace. The
tracked files are `.beads/config.yaml`, `.beads/metadata.json`,
`.beads/.gitignore`, and `.beads/issues.jsonl`. SQLite, lock, sync, and
recovery files are local-only and ignored. A new checkout/worktree reconstructs
its database from versioned JSONL on first access; if needed, explicitly run
`br sync --import-only`.

Before sharing task updates, run `br sync --flush-only`, review the changed
JSONL, and commit it on `dev-notes` separately from implementation branches.
`br` does not commit, push, pull, or merge Git revisions. Synchronize the
`dev-notes` Git branch between machines before importing fresh JSONL; claims
are atomic only for agents sharing the same SQLite database, not across
machines. `docs/roadmap.md` stays authoritative for product work; Beads tracks
execution and handoffs, not product specifications.

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
| `TODO.md` | Unvetted/session-specific ideas; track persistent actionable work in Beads |
| `.beads/` | Structured agent task state and handoffs; only JSONL and metadata are versioned |
| `architecture/` | archmap: selective semantic architecture model with code-revision checks, symbol-navigation anchors, and derived visual views. Start at `architecture/README.md` |

## Lifecycle

Keep active files current. When a note is finished, superseded, or no longer
actionable, delete it, or move it to `archive/` if the history has expected future
value. Close completed Beads tasks rather than duplicating them in `TODO.md`.
Do not let archived material look active. The local `cleaning.md` control
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

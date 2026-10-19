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

## Layout contract

One note per concern, in the directory for its kind. The root holds only control
files and long-lived aggregators; no other note belongs there. This is the
contract enforced by `Templates/tools/check-dev-layout.sh` from my dotfiles.

| Path | Kind it holds | Naming |
|------|---------------|--------|
| `plans/` | Implementation plans, one per non-trivial change | `plan-<slug>.md` |
| `reports/` | Audits, reviews, verification records, spikes, council/investigation output | `<kind>-<slug>-<YYYY-MM-DD>.<ext>` with `kind` in `report` `audit` `verification` `spike` `council` `investigation` |
| `references/` | Cached external material, research notes, derived captures, fixtures | `<origin>-<topic>[-<YYYY-MM-DD>].<ext>` with `origin` in `retrieved` `research` `capture` `fixture` |
| `assets/` | Local-only binary fixtures, ignored by `.gitignore` | — |
| `archive/` | Finished or superseded material, mirroring the kind directories | `archive/<kind>/<YYYY-MM-DD>_<original-name>` |
| `architecture/` | archmap: selective semantic architecture model with code-revision checks, symbol-navigation anchors, and derived visual views. Start at `architecture/README.md` | — |
| `.beads/` | Structured agent task state and handoffs; only JSONL and metadata are versioned | — |

Control files (never treated as notes): `INDEX.md` (this file), `cleaning.md`
(local-only, intentionally absent from `dev-notes`), `archmap.log`.

Aggregators allowed at the root: `TODO.md` (open follow-ups; persistent
actionable work belongs in Beads). The convention also allows `ideas.md`,
`decisions.md`, and optionally `specs.md` and `not-planned.md`.

`INDEX.md` describes categories, not files. When a kind directory grows past a
few entries, file-level drift stays local in its own index. The active files and
their purpose live in [`plans/README.md`](plans/README.md) and
[`reports/README.md`](reports/README.md).

## Lifecycle

Keep active files current. When a note is finished, superseded, or no longer
actionable, delete it, or move it under `archive/<kind>/` with a date-first name
(`git mv` when it is tracked) if the history has expected future value. Close
completed Beads tasks rather than duplicating them in `TODO.md`. Do not let
archived material look active. The local `cleaning.md` control file defines the
full cleanup procedure when present in the workspace.

Cached external material records its source URL or document ID, retrieval date,
and relevant product/API/version. Refresh it before relying on it for freshness-
sensitive decisions.

## Conventions

- Use lowercase kebab-case names, kind-prefixed (`plan-`, `report-`,
  `retrieved-`). An active dated artifact is kind-first with the date last; an
  archived file is date-first and does not repeat the original date suffix.
- Put current state at the top of long-lived files, not buried in chronology.
- Do not promote `.dev/` content to `AGENTS.md` automatically. Only verified,
  durable, repo-specific facts qualify; the local `cleaning.md` control file
  defines that promotion procedure.

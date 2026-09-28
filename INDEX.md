# .dev/

Persistent working context shared by humans and agents across sessions: quick
notes, drafts, plans, research, decisions. It is **not** a source of truth and is
not auto-injected into sessions. Link the specific file that matters from a task
prompt, or from `AGENTS.md` when the reference is durable and broadly useful.

By default, `.dev/` is not merged or committed to `main`, unless the repository
defines a different policy. It often lives on its own orphaned branch/worktree.

## File map

| File | Purpose |
|------|---------|
| `cleaning.md` | Procedure for consolidating scratch material and promoting verified knowledge |
| `TODO.md` | Session-specific queue and unresolved editorial/CLI ideas |
| `bad-writing-findings.md` | Two unresolved wording findings in ADR-002 |
| `archive/lint-triage-2026-09-23.md` | Historical uncapped lint audit; current status is tracked in `docs/roadmap.md` and `docs/gosec-triage.md` |
| `archive/preparation-exec-map-2026-09-24.md` | Archived preparation/execution mapping |
| `archive/author-audit-2026-09-23.md` | Archived author audit; result is recorded in `docs/roadmap.md` |
| `archive/preparation-map-2026-09-23.md` | Archived preparation mapping |
| `archive/2026-09-16-triaged-work.md` | Archived triaged work and historical decisions |
| `archive/plans/plan-graph-terminal-renderer.md` | Archived graph terminal renderer plan |
| `archive/plans/plan-secret-transport.md` | Archived secret transport plan |
| `archive/adapterv2/retrieved-adapter-v2-adapters.md` | Retrieved adapter-v2 adapter research |
| `archive/adapterv2/retrieved-adapter-v2-callers.md` | Retrieved adapter-v2 caller research |
| `archive/adapterv2/retrieved-adapter-v2-contracts.md` | Retrieved adapter-v2 contract research |

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

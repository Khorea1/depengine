# .dev/ — índice do que existe aqui agora

Not committed/pushed to main. Shared between agents and humans across sessions.
NOT auto-injected into every session — link to a specific file from
AGENTS.md or a task prompt when it's relevant to the current work.

Naming conventions and the "where things belong" rule live in
[`docs/development.md`](../docs/development.md) §`.dev/` — this file does not
duplicate them. This file is only the live inventory: one concern per file,
and a file whose concern has finished moves to `.dev/archive/` rather than
being deleted.

Last reconciled: 2026-09-26.

## Root

| File | Purpose | State |
|------|---------|-------|
| `INDEX.md` | This inventory | current |
| `TODO.md` | Active task queue for the current work session/sprint | active |
| `lint-triage-2026-09-23.md` | Full-repository `golangci-lint` backlog: totals, verdicts, per-linter policy blockers, green list. Refresh sections appended by date. | active |
| `bad-writing-findings.md` | Open documentation-writing audit findings (ADR-002, ADR-005) with proposed rewrites | open — none applied yet |

## `archive/` — concern finished, kept as history

| File | Why it finished |
|------|-----------------|
| `2026-09-16-triaged-work.md` | Original triage queue, reconciled against the repo on 2026-09-17; `docs/roadmap.md` is now the live backlog |
| `author-audit-2026-09-23.md` | `.mailmap` verified complete (7 identities, 6 aliases); roadmap item marked done |
| `plan-secret-transport.md` | Implemented and merged; plan closed 2026-09-23 |
| `plan-graph-terminal-renderer.md` | All 5 scope items shipped (`--format graph`, `internal/term`, renderer, docs); roadmap item marked done |
| `preparation-map-2026-09-23.md` | Split verdict `DO NOT SPLIT` for `internal/plan/preparation.go`; consumed by the roadmap item |
| `preparation-exec-map-2026-09-24.md` | Split verdict `DO NOT SPLIT` for `internal/exec/preparation.go`; consumed by the roadmap item |
| `retrieved-adapter-v2-contracts.md` | Premise obsolete: predates `type AdapterV2 interface` and the removal of the legacy contract (`cb59374`) |
| `retrieved-adapter-v2-callers.md` | Same — the `TODO-0 §2` plan and `feat/resolved-install-execution` branch it fed no longer exist |
| `retrieved-adapter-v2-adapters.md` | Same — optional capability interfaces and the legacy `Install()` strangler are gone |

## Conventions

- Prefix by kind (`plan-`, `retrieved-`) so an agent can glob for relevant
  context without reading everything.
- Nothing here is promoted to `AGENTS.md` automatically. Promotion is a
  deliberate act: when a local note becomes a project rule, move it into
  `docs/`, an ADR, a spec, or `AGENTS.md` per `docs/development.md`, and avoid
  keeping two canonical copies.
- Generated reports and raw tool logs are scratch. Their content is promoted
  into `TODO.md` or the relevant triage file before the raw output is dropped;
  regenerate the tool rather than storing its output.

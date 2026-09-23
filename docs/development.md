# Project documentation

Keep information in Git when a future clone needs it to understand, change, or
review the project. Keep temporary task notes in `.dev/`.

## Where things belong

| Content | Location |
|---|---|
| Architecture and engineering constraints | `docs/` |
| Accepted design decisions | `docs/design/adr-*.md` |
| Behavioral and compatibility contracts | `docs/specs/` |
| Research that supports a lasting decision | `docs/research/` |
| Shared agent/project instructions | `AGENTS.md` |
| Long-lived unfinished work | `docs/roadmap.md` |
| Task plans, scratch notes, temporary TODOs, generated reports | `.dev/` |

A simple rule: if the note should be reviewed with a code change because future
work depends on it, commit it. If it only helps the current task, keep it in
`.dev/`.

## `.dev/`

`.dev/` is ignored by Git. Useful names include:

- `TODO.md` for local work still in progress;
- `plan-<feature>.md` for an implementation plan;
- `ideas.md` for exploration;
- `retrieved-<topic>.md` for cached external material;
- `archive/` for local notes worth keeping around.

When a local note becomes a project rule, move the rule into `docs/`, an ADR, a
spec, or `AGENTS.md`. Avoid keeping two canonical copies.

## ADR or spec?

Use an ADR when the important artifact is a decision and why it was made. ADRs
are historical and can remain after they are superseded.

Use a spec when code or validation must follow an active contract. Update the
spec when that contract changes.

Benchmark output, logs, and generated analysis normally belong in CI artifacts
or `.dev/`. Commit a benchmark baseline only when it is an intentional
acceptance criterion with enough environment data to interpret it.

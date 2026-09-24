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
| Local agent/operator instructions | `AGENTS.local.md` (optional, ignored) |
| Long-lived unfinished work | `docs/roadmap.md` |
| Task plans, scratch notes, temporary TODOs, generated reports | `.dev/` |

A simple rule: if the note should be reviewed with a code change because future
work depends on it, commit it. If it only helps the current task, keep it in
`.dev/`.

## `.dev/`

`.dev/` is ignored by Git. It is for temporary task material, not project instruction discovery. Useful names include:

- `TODO.md` for local work still in progress;
- `plan-<feature>.md` for an implementation plan;
- `ideas.md` for exploration;
- `retrieved-<topic>.md` for cached external material;
- `archive/` for local notes worth keeping around.

When a local note becomes a project rule, move the rule into `docs/`, an ADR, a
spec, or `AGENTS.md`. Avoid keeping two canonical copies.

## `AGENTS.local.md`

`AGENTS.local.md` is an optional operator-local instruction layer beside
`AGENTS.md` at the project root. It is not version controlled.

Use it for machine and workflow preferences such as preferred tools, local paths,
or development shortcuts. It may refine execution details but must not override
project contracts defined in `AGENTS.md`.

Keep the discovery order deterministic:

1. Load `AGENTS.md`.
2. Load `AGENTS.local.md` when present.
3. Apply local instructions with the project instructions as the contract.

Do not store `AGENTS.local.md` under `.dev/`; `.dev/` is scratch space and is
not an instruction boundary.

## ADR or spec?

Use an ADR when the important artifact is a decision and why it was made. ADRs
are historical and can remain after they are superseded.

Use a spec when code or validation must follow an active contract. Update the
spec when that contract changes.

Benchmark output, logs, and generated analysis normally belong in CI artifacts
or `.dev/`. Commit a benchmark baseline only when it is an intentional
acceptance criterion with enough environment data to interpret it.

# Development metadata

Project knowledge lives with the code when a future clone needs it to understand,
change, review, or reproduce the project. Temporary coordination stays outside the
versioned contract.

## Where information belongs

| Content | Location | Versioned |
|---|---|---|
| Architecture and public engineering constraints | `docs/` | yes |
| Accepted architectural decisions | `docs/design/adr-*.md` | yes |
| Durable behavioral or compatibility specifications | `docs/specs/` | yes |
| Research that supports a lasting decision | `docs/research/` | selectively |
| Shared agent/project instructions | `AGENTS.md` | yes |
| Executable benchmarks | next to the relevant package or under a benchmark package | yes |
| Long-lived project roadmap | `docs/roadmap.md` | yes |
| Active task queue | issue tracker; `.dev/TODO.md` for local/session work | no |
| Implementation plans, scratch notes, cached research, generated reports | `.dev/` | no |

A useful test is whether a change to the file should be reviewed atomically with a
change to the code. If yes, keep it in Git. If it describes work in progress or can
be regenerated cheaply, keep it out of the repository history.

## `.dev/` working memory

`.dev/` is ignored by Git. It is for local or short lived context shared between
agents and humans on the same checkout. Suggested names:

- `TODO.md` for the current work queue;
- `plan-<feature>.md` for implementation plans;
- `ideas.md` and `not-planned.md` for uncommitted exploration;
- `decisions.md` for decisions still being evaluated;
- `retrieved-<topic>.md` for cached external material;
- `archive/` for finished local notes worth retaining.

When a decision or specification becomes a project invariant, promote it from
`.dev/` into `docs/design/`, `docs/specs/`, or `AGENTS.md` and review it with the
code it governs. Avoid keeping two canonical copies.

## Decisions and specifications

Use an ADR when the useful artifact is the decision and its rationale. Use a spec
when future implementation or validation must conform to a behavioral contract.
ADRs are historical and normally remain after supersession; specs describe the
current contract and should be updated when that contract changes.

Generated benchmark outputs, logs, and analysis reports are normally CI artifacts
or `.dev/` files. Commit benchmark baselines only when they are an intentional,
reproducible acceptance criterion and include enough environment information to
interpret them.

# ADR-005: ResolvedInstallPlan as the shared execution projection

Status: Accepted

## Context

Install, upgrade, status, remove, validation, explanation, and dry-run must
agree on candidate identity. A host-dependent resolution can enrich an intent
with versions, revisions, digests, artifact URLs, and placement paths. Later
observations and mutations must use that resolved target.

## Decision

Each selected candidate starts with a validated, host-independent plan intent
projected from the merged schema and manifest. Candidate ordering and method
capability checks determine which intent can be reached. Dependency scheduling
and lockfile constraints remain separate inputs to execution.

The adapter resolves that intent once when the candidate is reached. The
executor validates the resolution and uses the concrete plan for observation,
reconciliation, reporting, and execution. Validation checks static intent
without probing the host. Explanations may show static intent for candidates
that are filtered before host resolution.

Commands may project information from the plan:

- `install`: execute transitions;
- `dry-run`: render transitions without mutation;
- `why`: explain candidate selection;
- `status`: compare installed state with planned identity;
- `remove` and `undo`: verify the tracked target, then project its resolved
  identity into the removal adapter; ownership release uses persisted state;
- `upgrade`: verify the tracked candidate and install the exact resolved new
  target after removing the previously tracked installation.

Native batch installation uses the same resolved-target verification as serial
execution before deciding a tool is already installed and after the batch
command. No command should independently repeat adapter selection rules.

## Non-goals

This ADR does not require every adapter to support every lifecycle operation.
Missing capabilities remain explicit in the resolved plan.

This ADR does not define lockfile format changes.

## Consequences

Positive:

- command behavior becomes consistent;
- testing can assert plan invariants once;
- adapter-specific behavior stays behind capability boundaries.

Trade-off:

- adapters with legacy removal signatures receive a method configuration
  projected from the verified plan; that projection remains an internal
  compatibility boundary.

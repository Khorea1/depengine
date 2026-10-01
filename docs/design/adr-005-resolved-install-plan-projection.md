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

Commands use the plan and, for lock v2, the universal `LockDocument` as the
resolved-identity authority. When producing v2, `update` resolves candidates
through the executor/`AdapterV2`; a profiled update over v1 remains on the v1
compatibility path:

- `install`: execute transitions;
- `dry-run`: render transitions without mutation;
- `why`: explain candidate selection;
- `status`: resolve/observe the selected candidate against the v2 document and
  reconcile observed identity; v1 retains its bounded pin-application path;
- `remove` and `undo`: verify the tracked target, then project its resolved
  identity into the removal adapter; ownership release uses persisted state;
- `upgrade`: capture tracked state during discovery, resolve and verify the
  exact target, then call `ExecuteResolvedUpgradeCandidate` with that mandatory
  snapshot. The API deep-clones the snapshot config before initialization,
  recovery, or prerequisites; the executor compares it with current state under
  lock before WAL creation/removal and uses `config.FindMethodCandidate` to
  resolve the old candidate by its persisted kind and label, then journals and
  installs/verifies the exact target.

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

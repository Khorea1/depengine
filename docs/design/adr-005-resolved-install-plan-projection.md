# ADR-005: ResolvedInstallPlan as the shared execution projection

Status: Proposed

## Context

The roadmap identifies a need to use `ResolvedInstallPlan` consistently across
install, upgrade, status, remove, validation, explanation, and dry-run paths.
The current architecture already separates declaration, resolution, and
execution concerns, but every command must consume the same resolved projection
or the observable behavior can diverge.

## Decision

Commands should consume a single resolved install plan produced after:

1. schema and manifest merge;
2. validation of declared intent;
3. adapter capability filtering;
4. candidate selection and ordering;
5. dependency expansion;
6. lockfile constraints where applicable.

The plan is the boundary between user intent and adapter execution.

Commands may project information from the plan:

- `install`: execute transitions;
- `dry-run`: render transitions without mutation;
- `why`: explain candidate selection;
- `status`: compare installed state with planned identity;
- `remove`: derive owned resources and lifecycle actions;
- `upgrade`: resolve mutable upgrades against the same model.

No command should independently repeat adapter selection rules.

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

- migration requires replacing command-specific resolution paths gradually.

## Migration order

1. Identify command paths that resolve adapters independently.
2. Replace them with resolved plan consumers.
3. Add invariant tests asserting equivalent plans across commands.
4. Remove duplicated selection logic.

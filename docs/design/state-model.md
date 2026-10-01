# State model

This is the normative model for desired-state reconciliation and durable ownership. It names existing boundaries; it does not define new state or persistence formats. See [schema reference](../schema-reference.md), [support boundary](../support-boundary.md), and the linked ADRs for format and operational details.

## Conceptual pipeline

```text
schema + manifest
    ↓
declared intent
    ↓
resolved install plan
    ↓
lock projection
    ↓
prepared sources / prerequisites
    ↓
observed machine state
    ↓
recorded ownership / installed state
```

This is an order of authority, not a requirement that every command materialize every stage. `status`, `why`, `remove`, and `update` use the applicable subset. Configuration merges project schema and manifest according to the [schema contract](../schema-reference.md); project values win conflicts.

## Invariant matrix

| Boundary | Invariant | Readers | Producer or mutator | If the invariant fails |
|---|---|---|---|---|
| Declared intent | Normalized user intent; construction does not probe or mutate the host. | Planner, executor, explain | Configuration and planner | Reject before candidate resolution. |
| Resolved plan | Adapter resolution enriches allowed concrete identity but preserves candidate and stable intent; `ValidateResolution` checks this boundary. | Observation, execution, lock, report | Adapter `ResolvePlan`, validated by `internal/plan` | Reject the candidate; do not observe or execute a rewritten intent. |
| Lock projection | Immutable identity and requested intent are checked for the covered tool set; v2 universal projection is all-or-nothing. Lock coverage is limited to its supported closure and adapter resolution guarantees. | Install, upgrade, update | Lock projection from resolved plans | Fail closed on missing coverage or mismatch; legacy v1 retains only its documented method-specific guarantees. |
| Preparation (state-tracked execution) | Persist the write-ahead journal before each host mutation; retain exact plan and ownership evidence for recovery. | Executor and recovery | Locked `internal/state` transaction APIs | Block on ambiguous outcomes; never infer that an in-flight mutation did not happen. |
| Observation | Read-only report of presence and only authoritative known identity fields for the resolved target. | Reconciliation, status, remove, upgrade | Adapter `Observe` | Invalid observations become `broken`; insufficient authoritative evidence remains `unknown`. |
| Reconciled verification | `plan.Reconcile` distinguishes all five states below; presence alone is not proof of desired identity. | Executor and command workflows | `internal/plan` | Apply the operation-specific policy; destructive operations fail closed for `unknown` or `broken`. |
| Recorded state / ownership (state-tracked execution) | Persist only completed or evidence-backed transitions; resource ownership and references govern cleanup, not inferred host presence. | Status, remove, recovery, reports | `internal/state` under its lock | Persistence/recovery failure leaves the run incomplete and prevents unsafe cleanup. |

## Reconciliation states

`internal/plan/verification.go` (`Reconcile`) is authoritative:

- **`satisfied`** — target is present and every desired identity field is authoritatively known and equal.
- **`absent`** — the adapter authoritatively reports no target. This is not equivalent to an uncertain probe.
- **`drifted`** — target is present and at least one known identity field differs; known drift takes precedence over other unverifiable fields.
- **`unknown`** — presence cannot be established, or a present target lacks authoritative values for one or more desired identity fields, with no known mismatch.
- **`broken`** — desired identity, presence value, observation field set, or verification probe is invalid or failed.

A missing known field is not an empty value: `KnownFields` determines which reported values are authoritative. The support boundary describes command-specific behavior; notably, remove and upgrade stop on `unknown`/`broken`, while proven absence can release tracked ownership without invoking a remover.

## Authority and precedence

1. The merged schema and manifest define requested intent; project declarations win conflicts under the configuration contract.
2. A candidate resolver may enrich concrete version, revision, digest, source, or artifact identity, but may not redefine stable intent (`ADR-005`).
3. A lock constrains that resolution only where its immutable entry and coverage are available; it also detects requested-intent and resolved-identity drift (`ADR-001`).
4. Observation describes the host and does not rewrite desired identity.
5. Persisted state records depengine's proven install and ownership transitions. It is not an assertion of absolute machine truth; ownership evidence governs depengine cleanup (`ADR-002`).

## Preparation, ambiguity, and failure domains

In state-tracked execution, preparation and candidate commit use a write-ahead journal. The journal records an applying/committing boundary before host mutation; recovery may finalize a commit only when reconciliation proves `satisfied`. An ambiguous external outcome remains blocked until explicit evidence resolves it. Do not replay commit or rollback mutations merely because a process restarted. Ownership is finalized with the commit and shared resources are removed only under their recorded ownership/refcount rules.

Library callers that leave `schemaPath` empty use the existing best-effort preparation branch instead: it does not persist the durable WAL and provides no restart recovery for those preparations. These state-tracking and recovery guarantees do not apply to that untracked branch.

The failure domain of a failed tool is its transitive dependent closure, not its prerequisite closure: declared and lazy prerequisites propagate failure to dependents, and the failed tool's descendants are blocked. Independent siblings and unrelated branches continue; failure is not a global abort. In state-tracked execution, a recovered, proven commit may satisfy a dependency without replaying its host operations. This documents the executor/recovery contract, not a scheduler or retry policy.

## References

- [ADR-001 — Universal lock projection](adr-001-universal-lock-projection.md)
- [ADR-002 — Transactional preparation](adr-002-transactional-preparation.md)
- [ADR-005 — Resolved install plan projection](adr-005-resolved-install-plan-projection.md)
- [Support boundary](../support-boundary.md)
- [Schema reference](../schema-reference.md)

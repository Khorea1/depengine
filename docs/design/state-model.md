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

This is an order of authority, not a requirement that every command materialize every stage. `status`, `why`, `remove`, and `update` use the applicable subset. Configuration merges project schema and manifest according to the [schema contract](../schema-reference.md); explicit project values win field conflicts, while parser-added platform defaults follow candidate-aware merge rules.

## Invariant matrix

| Boundary | Invariant | Readers | Producer or mutator | If the invariant fails |
|---|---|---|---|---|
| Declared intent | Normalized user intent; construction does not probe or mutate the host. | Planner, executor, explain | Configuration and planner | Reject before candidate resolution. |
| Resolved plan | Adapter resolution enriches allowed concrete identity but preserves candidate and stable intent; `ValidateResolution` checks this boundary. | Observation, execution, lock, report | Adapter `ResolvePlan`, validated by `internal/plan` | Reject the candidate; do not observe or execute a rewritten intent. |
| Lock projection | Immutable identity and requested intent are checked for the covered tool set; v2 universal projection is all-or-nothing. Lock coverage is limited to its supported closure and adapter resolution guarantees. | Install, upgrade, update | Lock projection from resolved plans | Fail closed on missing coverage or mismatch; legacy v1 retains only its documented method-specific guarantees. |
| Preparation (state-tracked execution) | Persist the write-ahead journal before each host mutation; retain exact plan and ownership evidence for recovery. | Executor and recovery | Locked `internal/state` transaction APIs | Block on ambiguous outcomes; never infer that an in-flight mutation did not happen. |
| Observation | Read-only report of presence and only authoritative known identity fields for the resolved target. Ecosystem checks may probe the package-manager executable for runtime availability; installed tool identity comes from that manager’s version metadata or install target, not the tool executable. | Reconciliation, status, remove, upgrade | Adapter `Observe` | Invalid observations become `broken`; insufficient authoritative evidence remains `unknown`. |
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

1. The merged schema and manifest define requested intent; explicit project values win field conflicts under the configuration contract. Parser-added platform defaults are not unconditional constraints and are preserved across layers only for matching candidates.
2. A candidate resolver may enrich concrete version, revision, digest, source, or artifact identity, but may not redefine stable intent (`ADR-005`).
3. A lock constrains that resolution only where its immutable entry and coverage are available; it also detects requested-intent and resolved-identity drift (`ADR-001`).
4. Observation describes the host and does not rewrite desired identity.
5. Persisted state records depengine's proven install and ownership transitions. It is not an assertion of absolute machine truth; ownership evidence governs depengine cleanup (`ADR-002`).

## Preparation, ambiguity, and failure domains

In state-tracked execution, preparation and candidate commit use a write-ahead journal. The journal records an applying/committing boundary before host mutation; recovery may finalize a commit only when reconciliation proves `satisfied`. An ambiguous external outcome remains blocked until explicit evidence resolves it. Do not replay commit or rollback mutations merely because a process restarted. Ownership is finalized with the commit and shared resources are removed only under their recorded ownership/refcount rules.

State-tracked upgrades pass the discovery-time `ToolState` into the required
`ExecuteResolvedUpgradeCandidate` API. At API entry it deep-clones the snapshot
config before initialization, recovery, or prerequisite execution can mutate
shared input. Under the state lock, the executor deep-compares it with current
durable state before creating replacement WAL or crossing the removal
boundary; stale or missing state fails closed. `config.FindMethodCandidate`
looks up the previous schema candidate by its persisted method kind and label.
The WAL stores the previous tracked state, exact immutable replacement target,
and resource-use claims before removal. The executor journals removal and
installation boundaries before those mutations, then verifies the exact target.
Installed `ToolState`, release of the old dependent resource claims, new
replacement ownership claims, preparation completion, and the `Installed`
phase are persisted together while the replacement WAL remains active. Recovery independently observes old and desired identities and
resumes only a phase justified by those observations; unknown, broken,
conflicting, or ambiguous states block. For legacy transactions, resource
claims may be recovered from a matching preparation plan; if required claims
cannot be established, recovery fails closed. The after-upgrade hook is marked
`PostHookRunning` before execution. The postinstall-completion flag and WAL
removal are saved atomically, including when the hook reports failure; a
restart in `PostHookRunning` blocks rather than replaying an arbitrary hook
whose outcome is unknown. A post-hook error leaves the new
installation committed and does not trigger compensating removal.

Library callers that leave `schemaPath` empty use the existing best-effort preparation branch instead: it does not persist the durable WAL and provides no restart recovery for those preparations. These state-tracking and recovery guarantees do not apply to that untracked branch.

The failure domain of a failed tool is its transitive dependent closure, not its prerequisite closure: declared and lazy prerequisites propagate failure to dependents, and the failed tool's descendants are blocked. Independent siblings and unrelated branches continue; failure is not a global abort. In state-tracked execution, a recovered, proven commit may satisfy a dependency without replaying its host operations. Caller cancellation stops new host mutations, but does not cancel persistence of the final completed state and journal needed to record already-completed work. This final persistence retains the existing blocking lock-acquisition behavior and may wait indefinitely for the state lock; it is not abandoned merely because the request context was canceled.

Subprocess failure and incomplete captured output are distinct: a direct process failure is reported as such, while incomplete output capture does not by itself establish that the process failed. This documents the executor/recovery contract, not a scheduler or retry policy.

## References

- [ADR-001 — Universal lock projection](adr-001-universal-lock-projection.md)
- [ADR-002 — Transactional preparation](adr-002-transactional-preparation.md)
- [ADR-003 — Hook lifecycle](adr-003-hook-lifecycle.md)
- [ADR-005 — Resolved install plan projection](adr-005-resolved-install-plan-projection.md)
- [Support boundary](../support-boundary.md)
- [Schema reference](../schema-reference.md)

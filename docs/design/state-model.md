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

A missing known field is not an empty value: `KnownFields` determines which reported values are authoritative. The support boundary describes command-specific behavior; notably, remove and undo stop on `drifted`/`unknown`/`broken` (a projected removal identity that no longer matches the host could target another version or package), upgrade stops on `unknown`/`broken`, and proven absence can release tracked ownership without invoking a remover.

## Adapter verification after install

After `InstallResolved` succeeds, the executor verifies the same effective resolved target with the method's existing `EffectVerify` observation projection before it finalizes installed state or runs post-install hooks. Only `satisfied` completes the install. `absent`, `drifted`, `unknown`, `broken`, or an observation error fails the candidate terminally: no fallback, retry, rollback, further post-install hooks, or state adoption. If a preparation WAL is active, it remains unresolved in `Committing`; absence after a successful command is not proof that mutation did not occur. Without a transaction, the failure report may note that external effects remain. Adapter-error reconciliation is a separate path and is unchanged. Android cached-APK verification limitations and immutable-native replay remain unchanged.

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

## CLI mutation isolation (interim)

`install`, `upgrade`, `remove`, `undo` (except `--list`), `forget`, and
non-dry-run `update` first acquire an owner-only, cross-process
`host.mutations.lock` in the OS-specific per-user private temp/cache root,
then the original `state.json.mutations.lock` in the chosen state root.
The first lock serializes cooperating CLI processes even when their
`XDG_STATE_HOME` values differ; the second preserves coordination with older
clients that only use the state-root lock. Both precede actionable state reads
and schema lockfile transactions. `update` participates because
refreshing the desired lockfile while `upgrade` executes a physical replacement
could change the target during reconciliation.
Both locks remain held through physical operations, final persistence,
and journal reporting. The lock order is **per-user host → state-root mutation →
lockfile/state**; short-lived state locks and write-ahead recovery still
apply. Dry runs acquire neither mutation lock.
A canceled acquisition fails before touching host state. OS lock release
after a process crash does not resolve ambiguous WAL outcomes.
On Unix, lock-file opening refuses symbolic links, multiply linked files and
non-regular targets before tightening permissions; an unexpected pathname
change during acquisition fails rather than proceeding on a stale inode.
The private per-user and state directories and their lock pathnames must
not be replaced while a mutation is active. The OS releases both locks on
process exit; the per-user location must resolve consistently across
processes (e.g. identical temp-root configuration). Neither lock constrains
other programs that do not cooperate with this protocol.

Cross-process regression coverage exercises the vulnerable upgrade-style gap
between an initial state read and a later state commit, with a controlled
physical adapter running while the ordinary state lock is free. A concurrent
`forget` times out on the mutation guard, then succeeds on retry after the
physical phase finishes, with both state updates preserved. Another process
using a different state root also blocks during that physical phase; direct
cross-root lock acquisition and release are independently tested. A separate
cross-process update regression holds the host mutation guard and verifies
that a lockfile refresh times out without writing; after release, the retry
writes a valid lock. An `update --dry-run` still runs without acquiring the
guard or writing a lockfile. A separate
process-kill test confirms that an external effect completed before state
commit does not fabricate a commit and that the OS releases the lock for
subsequent commands. These tests are controlled process simulations, not
full CLI upgrade integration or recovery proof for ambiguous host effects.

This deliberately coarse guard prevents these CLI writers from interleaving
against each other within **one per-user temp/cache namespace**, including
different state roots that share that namespace. It does not establish
physical resource ownership, validate cross-schema compatibility, coordinate
different OS users or separate temp/cache namespaces, cover external package
managers, or protect library callers that invoke executor APIs directly.
It is an interim concurrency safeguard, **not**
completion of the global claims/admission protocol in ADR-010. Long-running
installs can delay unrelated mutations; fine-grained concurrency remains open.

## Claim arbitration is not persisted ownership

`internal/state.InstallationClaim` and `ClaimLedger` are a **planning
primitive**, not a state record. The ledger validates an explicit claim set,
indexes it by observed `scope/kind/identity` keys, and answers whether one
proposed claim would create, share, transition, remain unchanged, or conflict.
It never reads the host, never grants ownership, and is not persisted: state v5
stores no installation claims, and install/remove paths do not consult it.

The persisted "resource-use claims" retained by `ToolState` and the preparation
WAL remain the authoritative record of depengine-proven ownership and refcount
release under ADR-002. A shared name is not shared authority; do not treat a
`ClaimLedger` decision as permission to mutate the host, and do not implement
recovery from planning claims. See
[ADR-010](adr-010-global-resource-effects-and-claims.md) for the staged gate.

## References

- [ADR-001 — Universal lock projection](adr-001-universal-lock-projection.md)
- [ADR-002 — Transactional preparation](adr-002-transactional-preparation.md)
- [ADR-003 — Hook lifecycle](adr-003-hook-lifecycle.md)
- [ADR-005 — Resolved install plan projection](adr-005-resolved-install-plan-projection.md)
- [ADR-010 — Global resource effects and claims](adr-010-global-resource-effects-and-claims.md)
- [Support boundary](../support-boundary.md)
- [Schema reference](../schema-reference.md)

# ADR-010: Resource effects and project claims for shared host installations

Status: **Proposed — design gate, not approved for runtime enforcement** (2026-11-18)
Tracking: `dep-vd0`; related: `dep-fv6`, `dep-xfg`, `dep-1xv`, `dep-q43`.
Test/rollout plan: `.dev/plans/plan-global-resource-effects-and-claims.md`.

Integration checkpoint (2026-11-18): catalog-import security controls and
per-schema lock transactions have been consolidated and validated alongside
the pure claims/prospective-effect prototypes. This does **not** approve or
activate global claims enforcement. The integrated tree passed `go build`,
`go vet` and `go test -race ./... -count=1`; cross-host installation tests
remain outside this checkpoint. See
`.dev/reports/2026-11-18_checkpoint-consolidacao.md`.

Post-consolidation integration review (2026-11-18): prospective `remove` envelopes
no longer inherit install-only prerequisites, artifacts, preparation or
operations. They retain `unknown` completeness and opaque removal effects;
cleanup of persisted owned resources and actual removal hook execution still
require contract-level verification before enforcement. See
`.dev/reports/verification-integration-checkpoint-2026-11-18.md`.

## Question

How can independently locked schemas safely install, reuse, upgrade and remove
resources on the **same physical host**, when a method depends on facts,
`when`, distributions, lazy prerequisites, mutable sources, hooks and fallback?

The engine cannot promise to enumerate all effects of an arbitrary installer.
This ADR chooses a **bounded evidence and admission protocol** over either a
combinatorial list of platform variants or an invented universal package model.
It specifies safety requirements for a future implementation; it does **not**
claim that the current executor enforces them.

## Evidence from the current checkout

- `internal/config/condition.go` (`Condition.Match`): conjunction across
  condition fields, alternatives within lists; unknown WSL/container traits
  are not ordinary `false` facts. `internal/config/model.go`
  (`EffectiveRequires`) filters conditional top-level dependencies by facts.
- `internal/config/specialize.go` (`SpecializeDistribution`): zero matches
  skips a method, overlapping matches fail, and one match specializes a **copy**.
  ADR-007 requires a host-independent candidate sequence; it also records a
  known violation involving placeholders in default `method_order`.
- `internal/exec/execute.go` (`attemptMethod`) runs nine gates;
  `internal/exec/attempt.go` runs preinstall hooks and lazy method prerequisites
  **before** the final installation. `internal/exec/batch.go` has a separate
  native batch path. ADR-002 defines preparation and replacement WAL contracts;
  ADR-005 defines the single resolved-plan projection.
- `internal/exec/adapter.go` (`AdapterV2`) exposes `ResolvePlan`, `Observe`,
  `InstallResolved` and `Remove`, but not a prospective resource write-set.
- `internal/plan/verification.go`: `Observation.Resources` is **optional
  evidence of an already observed location**; it is neither a prospective
  footprint nor proof of ownership. `internal/httpdownload/observe_integrity.go`
  emits a canonical file slot only for constrained *present* raw-file targets;
  absent/archive/PATH-only/installer cases do not establish the same evidence.
- `internal/state/claims.go` is a pure planner (`create`, `unchanged`, `share`,
  `transition`), **not connected** to the executor or persistent state.
  `internal/state/state.go` v5 has one `SchemaPath` (last writer),
  `Tools[name]`, and a per-tool `RootRequested` boolean; these cannot represent
  independent requests by projects. Existing `OwnedResources` primarily cover
  managed preparation/prerequisite resources, not this new project ledger.
- `internal/lock/transaction.go` serializes transactions on **one lockfile**.
  Separate schema locks do not serialize physical resources on the host.
  The lock v2 constrains resolved intents, not project ownership of the host.

## Definitions: do not conflate these layers

| Layer | Meaning | What it does **not** prove |
| --- | --- | --- |
| Declared candidate | Host-independent entry in a recipe | Eligibility, installation destination |
| Effective candidate | Selected `when` + distribution + host facts | That facts remain true after preparation |
| Resolved plan | Concrete target: provider/version/URL/digest/placement where known | Complete side effects or ownership |
| Prospective effect envelope | What this operation **may read/write/remove** in the host namespace; completeness and evidence grade | That a mutation succeeded |
| Observation | What the adapter can prove about the host at the observation instant | Exclusive write rights or absence of other effects |
| Claim | A project's durable request for a confirmed resource identity | That depengine originally installed it |
| Ownership | Independent evidence authorizing physical cleanup | Merely matching desired bytes or possessing a lock |

### Resource identity

The future resource key must include **actual host/namespace, type and
canonical identity**, not a logical tool name: e.g. a named native package
under a particular manager database, a canonical filesystem *slot* (not
necessarily its inode), or a proved exclusive capability. Scope must distinguish
user/system, containers and independent roots where relevant. Host-wide
process aliases, symlinks, bind mounts and case-insensitive paths need explicit
rules; a canonical string alone is **not** a proof of disjointness.

A manager-package key, an executable path and a virtual capability may
represent overlapping effects without having identical keys. A claim engine
must not conclude that resources are disjoint merely because key types differ.
An adapter/probe must provide supported equivalence/overlap evidence or retain
an **unknown overlap**; a per-manager lock provides serialization but does not
prove compatibility.

### Prospective effects and evidence grades

Each *concrete operation*, not each generic adapter, eventually produces an
immutable effect envelope containing:

- operation (observe, prepare source, install prerequisite, pre-hook, install,
  post-hook, upgrade/remove/undo/compensation), candidate and resolved-plan
  identity; target namespace(s), resource identifiers and operation type;
- evidence basis (validated resolved configuration, manager inventory,
  canonical existing/absent destination, or other adapter-specific proof);
- completeness class: **exact**, **bounded**, or **unknown**; and any
  unresolved overlaps. Exact means the declared write-set is exhaustive for
  the particular contract, **not** that arbitrary downstream scripts cannot
  affect other resources. Bounded describes a known namespace with possible
  additional effects (e.g. package-manager hooks); unknown means no defensible
  footprint bound.

A missing envelope means **unknown**, never an empty write-set. An empty
observed resource list similarly never proves independence. These classes
must not be synthesized from `CheckAvailable`, `PATH`, package display names,
`State.SchemaPath`, lock checksum or the presence of an executable.

### Checkpoint: conservative target-overlap diagnostic (2026-11-18)

`internal/plan/resource_overlap.go` adds a **pure, read-only** tri-state
comparison of partial prospective targets and effect envelopes. Its current
proof boundary is intentionally narrow:

- `same` means one *declared resource target key* (scope, kind, identity)
  occurs in both envelopes; it does **not** mean identical write-sets,
  installed bytes, a durable identity, or ownership.
- `unknown` covers missing, incomplete or different target keys, even when
  both envelopes claim `exact` completeness. A different filesystem path may
  alias the same inode through hardlinks or mounts; different kinds may share
  effects. The comparison never treats distinct scopes as isolated by name.
- `disjoint` is reserved for future verified, exhaustive non-overlap evidence;
  **the current comparator never returns it**. An adapter must not manufacture
  such evidence from names, paths or a checksum alone.

The comparison does not read the host, re-evaluate `when`, admit mutations,
create claims or modify `AdapterV2`. If later consumed by an admission path,
resource identities and namespace separation must be revalidated under the
host transaction; the pure result alone is not an authorization. This is a
partial Phase 2 diagnostic, **not** its release gate.

### Checkpoint: positive host file-slot alias evidence (2026-11-19)

`internal/hostresource/overlap.go` adds an **optional, read-only host probe**
separate from the pure `internal/plan` comparator. For prospective targets
whose scope is `host-filesystem` and kind is `file-slot`, it can report `same`
when:

- the existing parent directories are the same filesystem object (including
  symlinked parent paths and detectable bind-mount aliases) **and** the final
  filename agrees, including when that file is verified absent; or
- two existing regular files have the same filesystem identity (`os.SameFile`),
  covering hardlinks even when their path strings differ.

The probe deliberately returns `unknown` for different filesystem objects,
invalid/missing parent directories, final symlinks, non-regular targets,
relative paths, different target kinds and non-host-filesystem scopes.
It **never returns `disjoint`**, never interprets differing namespace labels as
isolation, and never infers ownership. Parent resolution and filesystem
identities can change immediately after the read: `same` is a point-in-time
*collision hint*, not a durable claim or permission. Only a future host
transaction could safely revalidate it for admission. Native manager package
namespaces, containers and verified `disjoint` evidence remain unresolved.

The runtime executor and `AdapterV2` do not call this probe, so installation
behavior and the proposed ADR enforcement gate are unchanged. Tests cover
hardlinks, parent aliases, absent slots and unsupported/ambiguous paths.

### Checkpoint: native-manager state-root alias evidence (2026-11-19)

`internal/hostresource/manager_state.go` adds another **optional, read-only**
positive probe: `CompareHostManagerStateRoots`. A target with scope
`host-filesystem`, kind `manager-state-root`, and an existing absolute file or
directory path can be compared against another such target. If `os.SameFile`
confirms the same physical object (including symlinked directories and
hardlinked database files), the probe reports `same`. Missing, inaccessible,
relative or semantically mismatched inputs return `unknown`. The result is
symmetric and **never `disjoint`**.

This is **state-root object equality, not manager namespace equality**. Real
package managers may use several files, execute install scripts, touch global
paths or write outside a database root. Two different database roots, mount
namespaces or user scopes are therefore not proven disjoint. Even two roots
that compare equal do not establish compatible installed packages, full
write-set confinement, or ownership. Symlink/mount identity may change after
probing; results must not authorize a mutation without locked revalidation.

No adapter emits these targets yet: their paths must eventually come from a
**trusted adapter inventory**, never an unvalidated recipe label or display
name. This checkpoint does not change `AdapterV2`, `state v5`, the executor,
container namespace detection or admission policy. Tests cover file/directory
aliases and conservative failure cases. Filesystem object identity from the
current process is not a durable cross-host or cross-container namespace ID.

### Checkpoint: native observation completeness (2026-11-19)

The explicit native-manager adapter now treats an incomplete subprocess output
capture (`WaitErr`) as `unknown` in `Observe` and never as an affirmative
result in boolean `Check`. Previously `brew` and `winget` could report a
package present or absent despite incomplete output. Structured dpkg/rpm
sentinel protocols were already fail-closed on incomplete output.

This is a **package observation integrity fix**, not certification of manager
state roots or write-set confinement. Other manager protocols still rely on
exit codes and cannot uniformly prove absence on operational failures. The
protocol comparison and unresolved cases are recorded in
`.dev/reports/2026-11-19_native-observation-evidence-audit.md`.
Neither the executor's admission logic nor `AdapterV2`, `state v5`, claims,
namespace isolation or `disjoint` semantics changed.

### Checkpoint: reusable validated claim index (2026-11-19)

`internal/state/claims.go` separates one-time ledger validation
(`LoadClaimLedger`) from repeated decisions (`(*ClaimLedger).Plan`). This is
the plan's "reusable validated indexes" Phase 2 item, **not** the Phase 2 exit
gate.

- The complete ledger is validated once: duplicate logical claims, incomplete
  or duplicate resource keys, mutually inconsistent owners and claims that
  collide with legacy-unassigned resources are rejected before any request is
  evaluated. A damaged ledger still cannot authorize an unrelated claim.
- Claim and resource ordering is canonical (schema/tool; scope/kind/identity),
  so equivalent permuted inputs yield identical dispositions and identical
  diagnostics. Overlap selection no longer depends on map iteration order.
- The index owns a copy of its input, so later caller mutation cannot
  desynchronize the index from the claims it validated.
- `PlanInstallationClaim` remains a stateless wrapper with the established
  semantics; every prior planner test passes unchanged.
- Verification is independent of the implementation: a deterministic seeded
  differential test judges 300 generated host views with both the index and a
  naive reference model written directly from the documented rules, and
  requires the reused index to agree with the stateless entry point. A
  concurrent test under the race detector holds the documented read-only
  guarantee, not just the single-threaded result.

Effect envelopes are still **not** accepted as claim evidence. Every current
envelope is `unknown` completeness with tentative targets, so it cannot
populate a claim's resource set; `unknown != empty` continues to apply. There
is no host read, executor caller, `state v5` persistence, admission decision or
ownership grant.

Local microbenchmark (20k iterations, 1/10/100 claims sharing one slot):
0.13/0.31/1.97 µs per indexed decision versus 0.64/2.59/19.6 µs for the
stateless wrapper. The 100-claim case scales with *overlapping* claims, not
total schemas, and this is not a host-level admission benchmark.

## Proposed protocol

### A. Select and resolve one candidate, not the Cartesian product

1. Preserve ADR-007's full candidate sequence and identity across hosts.
   Evaluate existing `when`/`requires_when` rules and specialize a **copy**
   with a single matching distribution; ambiguity is a hard error.
2. Resolve only the reached candidate via the existing ADR-005 projection.
   Pin the concrete target against the selected schema lock as today.
3. Construct a prospective effect envelope **from that resolved candidate**.
   Include source setup, lazy method prerequisites and every hook; explicitly
   mark unknown effects. Plan these separately if their targets are resolved
   lazily. Do not run preparatory hooks to *discover* effects.
4. The initial facts snapshot is input to selection, **not** a perpetual
   authorization. Under the host transaction, recheck facts/compatibility and
   resource observations needed for the chosen effects. If eligibility,
   effective target, or write-set changes, stop and replan outside the locks.
   Never silently reinterpret an existing frozen lock as a different target.
5. A fallback is a **new admission decision**. Release/compensate the previous
   candidate's preparation under ADR-002, then resolve/revalidate the next
   candidate and its *own* envelope. No permission or resource claim carries
   over simply because both methods install the same logical tool.

No second fact matcher, duplicate host-filtered catalog or separate candidate
ordering system is authorized by this ADR. The known ADR-007 default-order
placeholder violation needs its own resolution before asserting identity
invariance in a stronger admission path.

### B. Admission is risk-specific; unknown is not absent

| Situation | Proposed decision |
| --- | --- |
| Read-only inspection with uncertain resources | Report uncertainty; no ownership grant |
| `satisfied`, independently verified, no mutation | Report existing state; no new physical ownership assumed |
| Exact write-set, compatible existing claims, no unassigned overlap | Eligible for a transaction **after** locked revalidation |
| Bounded write-set (native manager, hooks with documented effects) | Lock the appropriate whole namespace; require proven non-conflict or a safe, explicit policy; never infer disjointness from names |
| Unknown effect, potentially shared/mutable resource | Refuse automated mutation; report the missing evidence |
| Known incompatible claim, conflicting or stale observations | Refuse, including attempts to fall back to another provider for the same contested resource |
| Legacy-unassigned resource | No automatic adoption, replacement or cleanup; require explicit evidence-backed resolution |

An explicit arbitrary-code authorization is **not** a project-ownership grant
or an assurance that effects are bounded. Whether a separately authorized
*unmanaged* escape hatch can operate on truly isolated, user-owned targets is
an **open product/security decision**; this ADR does not introduce a flag or
silently change existing CLI behaviour. In particular, `--allow-arbitrary-code`
cannot override a known conflicting claim.

The proof requirement applies before **the first mutation**, not just before
`InstallResolved`: pre-hooks, source setup, lazy prerequisites, batching and
removal must be covered. A read-only plan that cannot establish safety must
say so, not claim that its dry-run guarantees the host outcome.

### C. One host transaction governs each physical conflict domain

1. Acquire stable OS-backed guards for the canonical namespace/resources in a
   deterministic order. Establish and test a lock hierarchy with existing
   per-schema lock transactions, state locks, manager locks and WAL recovery.
   No mutually waiting lock acquisition is permitted.
2. Keep the guard from **fresh observation + claim lookup + decision** through
   mutation, verification, WAL/claim commit or durable ambiguous outcome.
   A lock around only `Save(state)` is insufficient.
3. If runtime resolution reveals an additional resource/namespace, do **not**
   expand a partially held lock-set in an order that can deadlock. Abort,
   release, replan and reacquire the complete sorted set (or take a
   conservatively broader guard before starting). Revalidate after reacquiring.
4. The same admission path must cover the native batch path, serial attempt,
   fallback, upgrade, `remove`, `remove --all`, `undo` and recovery. A batch
   commits only if every member has an admitted, compatible envelope; otherwise
   use an admitted serial path or refuse.
5. A host lock prevents cooperating depengine processes from racing. It
   **cannot** prevent external package managers, direct filesystem writes or
   postinstall scripts from modifying resources. Reobserve and validate
   immediately before destructive transitions and at verification boundaries;
   never describe this as complete TOCTOU elimination.

### D. Claims are durable only after verified effects

A portable per-schema lock remains a declaration; the host-local ledger
records independent project claims. The proposed durable record distinguishes:

- canonical project/schema identity with rename/clone/adoption policy (not a
  raw `SchemaPath` used as the owner), logical tool, concrete resolved target;
- resource/namespace keys, evidence/completeness, source of ownership and
  claim lifecycle (`pending`, `confirmed`, `unknown`/`recovering` as needed);
- connection to the exact candidate preparation/replacement WAL boundary.

Writing an intent before mutation is necessary for crash recovery, but is
**not** a confirmed ownership grant. Verification and claim confirmation must
be coordinated atomically in the durable state transition; interrupted host
mutations remain explicitly pending/unknown until independently reconciled.
Removal first releases the requesting project's claim and physically uninstalls
only after the *last* relevant claim is released **and** depengine-owned
cleanup rights plus unchanged identity have been independently verified.
A project-specific `undo` reverts that project's claim deltas, not the host's
entire `Tools` map.

### E. Legacy migration and interoperability

- Do not backfill an owner from v5 `SchemaPath`, `RootRequested`, a matching
  artifact checksum or a single observed file. Preserve these installations
  as **legacy-unassigned** until explicit, verifiable adoption.
- Do not replace existing v5 `OwnedResources` refcounts with project claims:
  preparation ownership and project demand are different dimensions.
- Define the on-disk revision and migration only **after** crash recovery,
  rollback, concurrent-write and cross-schema compatibility tests pass.
- A successful schema lock verification is never an assertion that the host
  resource is owned by that schema. Support external managers without
  pretending their hooks and install scripts have exhaustive footprints.

## Invariants for acceptance

The complete matrix, scenarios and corresponding planned tests live in
`.dev/plans/plan-global-resource-effects-and-claims.md` (IDs `GRA-01` through
`GRA-22`). The following are non-negotiable:

1. **No mutation on an unproven shared conflict**; `unknown != absent`.
2. **No successful claim from intent or observation alone**; only independently
   verified outcomes and ownership evidence authorize release/removal.
3. **One admission protocol across all mutating entry points**, including
   preparation, batch, fallback and crash recovery.
4. **No stale admission**: revalidate within the host transaction, and
   replan on changed target, facts, resource set or competing claim.
5. **No false project owners**: migrate legacy records as unassigned.
6. **No deadlock or partial global commit**: ordered locks and durable,
   recoverable boundaries around host mutations and claim confirmation.

## Alternatives rejected or postponed

- **Enumerate every `(adapter × facts × when × distribution × state)` path**:
  not scalable and misses runtime-only effects. Test invariants and selected
  interactions instead.
- **Assume one package manager owns a tool's filesystem effects**: wrong for
  mixed providers, generated files, hooks and tools that expose `PATH` aliases.
- **Treat equal logical names as conflicts and unequal ones as disjoint**:
  rejects safe sharing and misses `rust`/`rustup` overlap.
- **Reuse `Observation.Resources` as the future write-set**: it only describes
  supported observed locations and often is intentionally absent.
- **Use schema file paths or `RootRequested` as the owner**: current v5 fields
  do not store independent project claims.
- **Global mutex for all operations**: might be a temporary diagnostic guard,
  but damages concurrency without solving unknown effects or compatibility.
- **Immediately extend every `AdapterV2` implementation**: a breaking contract
  migration without a validated effects vocabulary. Prototype an isolated
  internal effects planner/registry; revise the public adapter contract only
  after contract tests show a stable, necessary interface.
- **Immediate state migration or CLI enforcement**: postponed until the
  evidence protocol, transaction and recovery cases are validated.

## Consequences, trade-offs and open decisions

**Benefits:** no cross-product recipe language, explicit uncertainty, reusable
pure arbitration, honest dry-run/report semantics and a staged safety rollout.

**Costs:** additional adapter-specific evidence work, more refusal cases,
transaction overhead, and a host-local ledger needing careful recovery.

**Open decisions (require explicit review before implementation):**

1. Exact canonical **host namespace and project identity** representation,
   including schema rename/copy and container/mount namespace boundaries.
2. How providers express **cross-kind overlap** (package database versus
   binary slot versus shell capability) without making unverifiable claims.
3. Precise policy for **bounded/unknown effects**, administrator overrides and
   temporary compatibility mode; `--allow-arbitrary-code` is insufficient.
4. Host guard ordering with existing per-lock and state/WAL locks; platform-
   specific crash/failure semantics and external-manager interference.
5. v5 migration/adoption UX and a future state version; exact rollback and
   claim-delta semantics for `remove --all` and `undo`.

## Rollout gate

No behavioural mutation guard, new claim persistence, or state-version bump is
approved by this document alone. First complete the staged tests and explicit
policy decisions in the companion plan. Existing ADR-002/005/007 contracts stay
in force unless separately amended with evidence.

## Phase 1 prototype checkpoint (2026-11-18)

`internal/plan/effects.go` now contains a **pure, non-enforcing prototype**:
`DescribeProspectiveEffects(resolved, transition)` enumerates potentially
mutating phases for **one** validated resolved plan. It conservatively covers
lazy prerequisites, declared sources, artifact handling, preparation and its
rollback, source mutations, transition-local lifecycle hooks, ensures, explicit
mutating operations and the *implicit* `InstallResolved`/remove action even
when the plan contains no `Operations`. It does not interpret `when` conditions
again; conditionally reachable phases remain in the envelope. Enumeration
order is diagnostic, **not** a replacement for the executor's hook/prepare
schedule.

All emitted phases are currently **unknown**, without physical keys or
claimed owners. This is deliberate: resolved URLs, checksums, `OwnedPaths`,
`ResourceIdentity` for preparation, and the existing HTTP observed file slot
are not proof of an exhaustive prospective write-set. `exact` and `bounded`
are vocabulary and monotonic combination rules only; they are **not yet
produced by trusted adapter-specific evidence**. An empty set of
classifications combines to `unknown`, not `exact`.

No `AdapterV2` signature, state version, CLI output, authorization or executor
branch has changed. Phase 1 is **not complete** until real evidence providers
can safely classify a bounded/exact operation, with contract tests covering
absent HTTP targets, install scripts, archives and mutable registries. Do not
use this prototype to admit mutations or persist project claims.

## Phase 1 raw-HTTP destination checkpoint (2026-11-18)

`ProspectiveEffectStep.Targets` carries **tentative, non-exhaustive** host
resource destinations, distinct from `Observation.Resources`. The isolated
`HTTPAdapter.DescribeProspectiveEffects` helper combines the pure planner's
phase inventory with one limited form of pre-install target evidence: a
**single raw HTTP artifact** with a concrete resolved URL, an explicitly named
binary and an explicit, absolute `extract_to` directory that already exists.
The file itself may be absent or regular. Existing parent symlinks are
canonicalized; an occupied non-regular destination, missing installation root,
PATH-based discovery, archives, installers, invalid paths and incompatible
candidate/plan identities emit no target. The concrete resolved artifact is
used rather than an earlier/stale method URL.

This identifies a **possible final file slot only**. It does not describe
staging files, HTTP caches, newly created directories, launcher links,
privileged operations, hooks or third-party effects. The primary install step
and the whole envelope therefore remain `unknown`, **not** `exact` or
`bounded`. This evidence is racy; no mutation may be admitted on its basis
without revalidation inside a future host transaction. Cross-kind overlaps
and ownership are completely unresolved.

The helper is read-only, not part of `AdapterV2` or the production executor;
there is no change to CLI, lock, persistence or installation authorization.
The Phase 1 exit gate and ADR status remain unchanged. Adapter and provider
contract tests cover absent/present direct raw targets, directory aliases,
refused symlink/occupied destinations, archive/installer/relative cases and
unchanged overall uncertainty. Further adapters and bounded/exact claims
require separate evidence and review.

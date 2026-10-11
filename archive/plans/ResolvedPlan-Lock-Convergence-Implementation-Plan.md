# Depengine: Canonical Resolved Plan + Universal Lock Convergence
## Implementation Plan and Task Breakdown

**Repository:** `Khorea1/depengine`  
**Source snapshot:** `depengine.tar(20261001-170220).gz`  
**Pinned commit:** `6bb3223c8a4ffb8894deeb9552761b90f587c6c3` (`master`)  
**Plan purpose:** finish the architectural migration already present in the codebase so `ResolvedInstallPlan` + `plan.LockDocument` become the canonical resolved identity used by operational v2 flows, while legacy v1 lock behavior remains an isolated compatibility path. Then move destructive upgrade replacement semantics into the executor so upgrade consumes the same resolution, observation, reconciliation, preparation, hook, prerequisite, and durability machinery as install.

---

# 0. How to use this document

This plan is intentionally over-structured. It is designed to be executed by multiple lower-capability agents without requiring any individual agent to reconstruct the architecture from scratch.

Each implementation task has:

- a stable task ID;
- a narrow goal;
- explicit files and approximate line references;
- prerequisites;
- a concrete change description;
- tests to add or update;
- acceptance criteria;
- explicit “do not do” constraints where a likely wrong turn exists.

Agents should complete tasks in dependency order. Tasks marked **parallel-safe** may be delegated concurrently only after their common prerequisite gate has passed.

## 0.1 Mandatory agent operating rules

Every agent must follow these rules:

1. **Work from the pinned snapshot or a descendant commit only.** If the branch has moved, rebase the references mentally, but do not silently plan against a different architecture.
2. **Do not redesign the project.** The target architecture already exists in the code. This plan finishes a migration; it does not introduce a new service layer, planner framework, repository abstraction, command bus, or dependency-injection system.
3. **Keep `internal/plan` pure.** `plan.Reconcile`, `plan.TransitionForVerification`, `LockDocument`, and related types must remain host-I/O-free.
4. **Effectful replacement belongs in `internal/exec`, not `internal/plan`.** Do not implement a mutating `plan.Reconciliation.Replace(...)` API.
5. **Do not delete v1 lock support while implementing v2 convergence.** v1 remains readable and must remain a bounded compatibility path.
6. **Do not allow v2 execution to silently fall back to v1 pins.** Once a valid v2 `LockDocument` is present, it is authoritative for resolved identity.
7. **Do not re-resolve mutable identity below the canonical executor resolution seam.** After `AdapterV2.ResolvePlan`, lower layers must consume the resolved plan.
8. **Do not weaken fail-closed behavior for unknown/broken observations, corrupt lock/state, ambiguous recovery, or mismatched immutable identity.**
9. **Do not remove current `sha256:auto` / materialized-integrity behavior without an explicit replacement.** It is one of the reasons the legacy lock path cannot simply be deleted.
10. **Do not broaden a task because nearby code is ugly.** Record unrelated cleanup separately.
11. **Prefer a characterization test before changing behavior that is currently undocumented or subtle.**
12. **Run `gofmt` only on touched Go files.**
13. **Commit one coherent task or tightly coupled task group at a time.** Do not mix migration phases in one giant commit.
14. **If a task uncovers a contradiction with an invariant in this document, stop that task and report the contradiction. Do not improvise a new architecture.**

## 0.2 Evidence hierarchy

When an implementation detail is unclear, use this order:

1. current code at the pinned commit;
2. accepted ADRs in `docs/design/`;
3. `docs/architecture.md` and `docs/support-boundary.md`;
4. `.dev/architecture/` maps;
5. README/dev notes;
6. this implementation plan.

This plan is a roadmap, not permission to contradict executable behavior without tests.

---

# 1. Verified current-state architecture

The following statements were checked against the pinned code. They are the basis of the plan.

## 1.1 The canonical resolved plan already exists

`internal/plan/plan.go:260-279` defines `ResolvedInstallPlan`, including candidate identity, resolved identity, artifacts, prerequisites, sources, preparation, hooks, ownership, operations, removal metadata, and secret references.

`internal/exec/resolution.go:29-103` already implements the executor's single read-only candidate resolution seam:

```text
static candidate intent
    -> optional LockDocument hydration
    -> AdapterV2.ResolvePlan
    -> plan.ValidateResolution
    -> verify result against lock, when locked
```

The comments in this file explicitly prohibit lower layers from resolving mutable tags/releases/assets again.

## 1.2 AdapterV2 is already the backend seam

`internal/exec/adapter.go:39-77` defines the uniform contract:

```text
ResolvePlan
Observe
InstallResolved
Remove / CanRemove
CheckAvailable
CheckHostCompatibility
```

The executor already owns policy; adapters own backend mechanics. The migration must preserve this boundary.

## 1.3 LockDocument is already the universal adapter-neutral lock model

Key references in `internal/plan/lock.go`:

- `ProjectLock`: approximately `372+`
- `VerifyResolvedPlanAgainstLock`: approximately `475+`
- `LockDocument`: `784+`
- `BuildLockDocument`: `830+`
- `EntryForPlan`: `872+`
- `VerifyCoverage`: `999+`
- `VerifyResolvedPlansAgainstLock`: `1035+`
- `PinnedPlanFor`: `1090+`

`PinnedPlanFor` hydrates immutable identity from the lock into the current operational intent. It deliberately leaves operational semantics such as hooks/prerequisites/current configuration in the current plan.

## 1.4 The legacy resolver is still an independent resolution engine

`internal/lock/lock.go:275-419` implements `ResolveAll`, which directly resolves every selector class supported by legacy lock v1, including:

- GitHub latest releases;
- mutable Git revision selectors;
- container tag to digest;
- mutable package selectors;
- checksum/materialized artifact cases;
- method/source identity metadata.

That is a real parallel resolution path, not merely serialization code.

## 1.5 v2 deliberately coexists with legacy pins today

`internal/lock/universal.go:13-27` states that `SetProjection` stores the universal projection while retaining method-specific pins.

`internal/lock/lock.go:421-513` explicitly preserves the universal projection while merging legacy pins.

Therefore the dual representation is not accidental corruption. It is migration scaffolding. The implementation must reduce its operational authority without pretending it never had a purpose.

## 1.6 Update currently performs both old and new resolution paths

`internal/app/update.go:51-181` currently does, in order:

```text
load/filter schema
    -> lock.ResolveAll(...)              [legacy remote resolution]
    -> load old lock
    -> lock.Merge(...)
    -> apply identity/hash policy
    -> lock.Apply(schema, newLock)       [legacy hydration]
    -> resolveUniversalLockDocument(...) [executor read-only resolution]
    -> SetProjection(...)
    -> save
```

This is the clearest duplication to remove.

## 1.7 Universal lock generation already uses the executor, but through Explain

`internal/app/universal_lock.go:16-105` builds a `LockDocument` through an install executor in dry-run mode, currently by calling `ExplainToolWithSourceRevisions` and selecting the first suitable attempt.

This is already close to the desired architecture. The main issue is that lock generation is consuming a human/explanation-oriented projection rather than a small canonical “resolve selected candidate for persistence” API.

## 1.8 Install still crosses the legacy lock path

References:

- `internal/app/install.go:245+` `resolveInstallLock`
- `internal/app/install.go:351-427` `runInstall`
- `internal/app/helpers.go:244-268` `loadLockfile`
- `internal/app/helpers.go:272-294` `saveLockfile`
- `internal/app/helpers.go:338+` `mergeInstallLock`

Install can call `ResolveAll` before execution and `saveLockfile` can call it again after execution. Existing v2 projection is preserved rather than regenerated.

Therefore the real current architecture is:

> modern executor + universal lock core, with a legacy lock resolver still crossing install/update/status/upgrade paths.

It is not simply “modern install, legacy update.”

## 1.9 Status still derives desired version from legacy ToolPin

`internal/app/status.go:197+` and `210+` use legacy pin lookup to establish the desired version, then call the executor verification path.

A v2 lock should instead provide desired immutable identity directly through `LockDocument`.

## 1.10 Upgrade is the most special-cased operational path

Important references:

- `internal/app/upgrade.go:210+` `collectOutdatedTools`
- `internal/app/upgrade.go:314+` `upgradeSingleTool`
- `internal/app/upgrade.go:441+` `removeInstalledTool`
- `internal/app/upgrade.go:458+` `reinstallUpgradeTool`
- `internal/app/upgrade.go:548+` `runUpgrade`
- `internal/app/upgrade.go:681+` `preflightDirectUpgrade`

Current direct upgrade behavior:

```text
legacy pin discovery
    -> executor resolve/verify preflight
    -> app directly calls adapter.Remove
    -> app calls Executor.InstallResolvedCandidate
       (thin wrapper around AdapterV2.InstallResolved)
    -> app updates state
```

`preflightDirectUpgrade` intentionally rejects sources, `method.requires`, tool dependencies, and lifecycle hooks because the direct path cannot preserve normal executor semantics.

This is a genuine architectural limitation, not a stylistic complaint.

## 1.11 Existing preparation WAL is useful infrastructure but is not a replacement transaction

`internal/state/preparation.go` and `internal/plan/preparation.go` provide durable write-ahead semantics for candidate preparation and candidate install commit.

The current WAL can distinguish preparation application, commit ambiguity, rollback, and resource ownership. It does **not** model this sequence:

```text
old installation known present
    -> old removal starts
    -> old removal completed
    -> new target install starts
    -> new target install completed
```

Do not claim replacement durability is “already done.” Reuse the patterns, not a fictional capability.

---

# 2. Target architecture

The final operational model should be:

```text
Configuration
    -> Candidate selection
    -> canonical executor resolution
    -> ResolvedInstallPlan
    -> LockDocument, when persisted/locked
    -> exact observation
    -> plan.Reconcile / TransitionForVerification
    -> executor lifecycle transition
    -> AdapterV2 backend mutation
```

The command layer should become policy/projection, not a second execution engine.

## 2.1 Desired command projections

### install

```text
select
-> resolve exact candidate
-> observe exact desired target
-> reconcile
-> prepare/prerequisites/hooks
-> execute install or replacement transition
-> persist state
-> persist lock material only through canonical resolved identity
```

### status

```text
select
-> resolve from current intent + LockDocument when v2
-> observe
-> reconcile
-> report
```

No v2 `ToolPin` version injection.

### update

```text
select
-> canonical read-only resolution
-> require immutable lockability
-> build complete LockDocument
-> persist
```

No host install/remove mutation.

### upgrade

```text
load tracked candidate
-> consume exact locked desired plan
-> observe
-> reconcile
-> if upgrade required:
       prepare safely
       durable replacement transition
       remove old
       install exact resolved target
       verify
       commit state
```

### why / explain

Already largely correct:

```text
select
-> canonical resolution
-> observation/reconciliation explanation
```

## 2.2 Final lock authority rule

After this migration:

```text
if lock.Version == 2:
    LockDocument is authoritative for resolved identity.
    Legacy ToolPin values MUST NOT alter candidate resolution.
    MethodsHash / SourceHash may temporarily remain as pure requested-intent
    validation metadata until a later lock-projection format change absorbs
    those exact invariants.

if lock.Version == 1:
    legacy v1 resolution/application behavior remains supported.
```

This distinction is important. The goal is to eliminate **parallel resolution**, not to delete every legacy field in the same patch.

---

# 3. Non-goals

The following are explicitly outside this migration unless a failing test proves they are required:

1. A new generic planner package.
2. A new service/repository layer around lock/state.
3. Changing every adapter API.
4. Redesigning the manifest format.
5. Freezing or redesigning the public lock format.
6. Removing v1 readers.
7. Converting every command to a generic command framework.
8. Rewriting the source manager.
9. Rewriting the existing preparation transaction.
10. Solving unrelated state migration concerns.
11. Replacing `plan.Reconcile` with a new reconciliation abstraction.
12. Making update install/download artifacts merely to obtain a checksum.
13. Automatically “repairing” unknown or broken host state.
14. Allowing upgrade to switch to a different candidate when the tracked candidate fails.
15. Broad cleanup of `internal/app` unrelated to lock/reconciliation convergence.

---

# 4. Critical invariants that must survive the migration

Every task must preserve these invariants.

## I-01: One resolution authority for v2

A v2 command must not resolve “latest”, Git branches, package selectors, container tags, or equivalent mutable identity via `internal/lock.ResolveAll` and then resolve again through the executor.

## I-02: v1 remains readable

Existing v1 lockfiles remain consumable according to the current documented support boundary.

## I-03: update is the operation that accepts lock identity change

A normal install using an existing v2 lock must not silently refresh the locked identity. `depengine update` is the explicit acceptance boundary.

## I-04: frozen mode is fail-closed

Missing, corrupt, unsupported, incomplete, or mismatched lock identity must fail before host mutation.

## I-05: exact resolved plan reaches the adapter

Once `ResolvePlan` produces a concrete plan, `InstallResolved` must receive that exact target. No lower resolver may replace it.

## I-06: v2 source/candidate intent must not become less protected

Legacy `MethodsHash` and `SourceHash` currently detect classes of requested-intent drift that are not fully encoded as separate requested fields in `LockProjection`.

Therefore:

- extracting them from `ResolveAll` is allowed and recommended;
- dropping them from v2 validation during this migration is **not** allowed unless equivalent universal-lock intent validation is added first.

## I-07: materialized integrity must not disappear

Existing behavior that carries a materialized checksum such as `sha256:auto` across lock operations must remain covered by tests.

## I-08: upgrade must remain exact-candidate

State currently persists enough information to disambiguate labeled same-kind candidates. Upgrade must not fall back to another same-kind candidate merely because its adapter kind matches.

## I-09: `internal/plan` remains pure

No filesystem, subprocess, registry, network, package manager, state writer, or adapter call from `internal/plan`.

## I-10: replacement is durable before destructive removal

Once executor-owned replacement exists, no old installation may be removed before a durable recovery record exists.

## I-11: unknown replacement outcome is never guessed

After a crash or ambiguous adapter failure, observe and reconcile. Do not automatically replay destructive steps unless the persisted phase proves replay is safe.

## I-12: hooks use the reconciliation transition

Upgrade hooks must execute as `TransitionUpgrade`, not as fake install hooks and not from duplicated CLI-specific hook logic.

## I-13: prerequisites/sources are prepared before destructive replacement

An upgrade may not remove a working installation and only afterward discover that a required source, prerequisite, permission, or candidate capability cannot be prepared.

## I-14: command code does not own adapter mutation after migration

Once the upgrade migration is complete, `internal/app/upgrade.go` must not directly call `AdapterV2.Remove` or use `InstallResolvedCandidate` as its operational replacement implementation.

---

# 5. Dependency graph

```text
P0 Baseline + characterization
 |
 +--> P1 Separate pure lock metadata from legacy remote resolution
 |      |
 |      +--> P2 Make v2 consumers independent from ToolPin mutation
 |             |\
 |             | +--> P4 Status consumes LockDocument directly
 |             |
 |             +--> P3 Canonical update resolution API + update migration
 |                    |
 |                    +--> P5 Isolate legacy v1 resolver/application
 |
 +--> P6 Characterize replacement semantics
        |
        +--> P7 Durable replacement WAL/state machine
               |
               +--> P8 Executor owns upgrade replacement
                      |
                      +--> P9 Upgrade command becomes a thin client

P2 + P3 + P4 + P5 + P9
        |
        +--> P10 Install/post-install lock cleanup
                |
                +--> P11 Final cleanup/docs/architecture verification
```

Do not start P7/P8 by editing upgrade CLI first. The state and executor primitives must exist before the command is rewired.

---

# 6. Phase P0: Baseline and characterization

**Goal:** create a reliable safety net before moving authority between lock layers.

**Production behavior change:** none.

## P0-T01: Record the implementation baseline

**Files:** no production files required.  
**References:** repository HEAD `6bb3223c...`.

Actions:

1. Record the exact commit in the implementation branch/PR description.
2. Confirm `git diff` is clean except intentionally untracked development artifacts.
3. Record Go version from `go.mod`.
4. Record which test commands can run in the execution environment.

Acceptance:

- baseline commit is explicit;
- no production edit is mixed into this task.

## P0-T02: Run the smallest available unit-test baseline

**Primary packages:**

```text
./internal/plan
./internal/lock
./internal/exec
./internal/app
./internal/state
```

If the environment is offline and the module cache is incomplete, record the exact missing dependencies rather than treating setup failure as a code failure.

Known snapshot-specific note from plan preparation: the bundled Go toolchain is Go 1.27.1; `internal/plan` can run with the available cache, while broader packages may require module-cache entries not present in the sandbox.

Acceptance:

- test baseline result is recorded;
- no failed package is misreported as a regression if compilation never began because a module is unavailable.

## P0-T03: Characterize v1 frozen validation

**Files:**

- `internal/lock/lock.go:516-631`
- `internal/lock/lock_test.go`
- app frozen-lock tests in `internal/app/install_phases_test.go`

Add or identify tests proving v1 frozen validation rejects:

- changed method ordering/labels;
- changed source identity;
- missing required mutable-selector pin;
- changed Git selector;
- changed container tag;
- changed mutable package selector;
- missing materialized checksum when required.

Acceptance:

- every behavior that v2 might accidentally stop inheriting from v1 is visible in a test.

## P0-T04: Characterize v2 lock consumption during install

**Files:**

- `internal/app/install.go:245-270`, `351-427`
- `internal/app/helpers.go:244-268`
- `internal/exec/resolution.go:29-103`
- `internal/app/install_phases_test.go`

Add/confirm tests proving:

1. v2 `ProjectionDocument()` is loaded;
2. executor receives it through `exec.WithLockDocument`;
3. a mismatched resolved identity fails;
4. install does not rewrite the universal projection to a newly resolved identity;
5. profile/subset install preserves entries outside its current execution scope.

## P0-T05: Characterize update's current dual-resolution behavior

**Files:**

- `internal/app/update.go:51-181`
- `internal/app/universal_lock.go:16-105`
- `internal/app/update_test.go`

Do not assert implementation details in user-facing behavior. Instead write tests around outcomes:

- full update can promote a complete lock to v2;
- profile update over v1 does not produce incomplete v2 projection;
- profile update over existing v2 retains valid omitted entries;
- removed tools are pruned from current full coverage;
- materialized auto checksum behavior is preserved;
- accepted source identity changes are represented after update.

## P0-T06: Characterize v2 status behavior before migration

**Files:**

- `internal/app/status.go:55+`, `197+`, `210+`
- status tests

Create tests that clearly distinguish:

```text
legacy pin says version A
universal lock says version B
```

Current behavior should expose which one status is actually using. This is a characterization test that will intentionally change later.

Purpose: prevent an agent from “fixing” status without proving authority changed.

## P0-T07: Characterize direct-upgrade restrictions

**Files:**

- `internal/app/upgrade.go:681+`
- `internal/app/upgrade_test.go`

Ensure tests exist for current rejection of:

- candidate sources;
- `method.requires`;
- tool dependencies;
- pre/post lifecycle hooks;
- unsupported removal;
- unknown/broken/absent tracked state;
- arbitrary-code gate.

These tests are not all permanent. Some become expected-success tests after executor replacement owns the transition.

## P0-T08: Characterize exact-candidate tracking

**Files:**

- `internal/app/lock_pin.go`
- `internal/app/lock_pin_test.go`
- `internal/app/upgrade_test.go`
- `internal/app/graph_why.go:217-300`

Cover:

- same-kind candidates with unique labels;
- unlabeled same-kind ambiguity;
- duplicate label ambiguity;
- persisted label selection.

**Important:** do not change `CandidateIdentity` or lock-projection format in this task.

## P0-T09: Baseline gate

P0 is complete only when:

- all applicable current tests pass or known environment failures are documented;
- no production behavior has changed;
- the above compatibility cases have direct test coverage.

---

# 7. Phase P1: Separate pure lock metadata from legacy remote resolution

**Goal:** stop making `MethodsHash`/`SourceHash` dependent on running the old resolver. These hashes are requested-intent validation metadata, not remote resolution results.

This is the first mechanical extraction that allows v2 to remain protected without calling `ResolveAll`.

## P1-T01: Extract pure intent-metadata construction

**Files:**

- `internal/lock/lock.go:228-273`
- `internal/lock/lock.go:275-419`
- `internal/lock/lock_test.go`

Introduce one small pure helper, recommended shape:

```go
// Name may differ, but responsibility must remain this narrow.
func SnapshotIntentMetadata(s *config.Schema) (methods map[string]string, sources map[string]string, err error)
```

Behavior:

```text
for tools in deterministic order:
    validate non-nil tool/method inputs as current resolver does
    MethodsHash[tool] = computeMethodsHash(tool.Methods)
    for each method using same legacy key convention:
        hash := computeSourceHash(method)
        if hash != "": SourceHash[key] = hash
```

Constraints:

- no network;
- no subprocess;
- no registry access;
- no mutation of schema;
- no `ToolPin` creation;
- reuse existing `computeMethodsHash` and `computeSourceHash`.

Acceptance:

- helper output equals the metadata produced by current `ResolveAll` for the same schema;
- deterministic tests cover same-kind labeled methods and source identity.

## P1-T02: Make legacy ResolveAll consume the pure helper

**Files:** `internal/lock/lock.go:275-419`.

Refactor `ResolveAll` so it obtains `MethodsHash` and `SourceHash` from P1-T01 instead of building them inline.

Do not change selector-resolution behavior.

Acceptance:

- v1 lock golden/behavior tests remain unchanged;
- diff is mostly deletion/movement of metadata construction.

## P1-T03: Add an explicit lock-envelope constructor for v2 persistence

**Files:**

- `internal/lock/universal.go`
- `internal/lock/lock_test.go`

Avoid making app code hand-assemble a half-valid `Lock` repeatedly.

Recommended narrow API:

```text
NewUniversal(document, methodsHash, sourceHash, optionalLegacyPinsToCarry)
```

or equivalent small constructor.

Responsibilities:

- set envelope version 2;
- encode/set the universal projection using existing `SetProjection` validation;
- attach pure method/source metadata;
- optionally carry existing legacy `Tools` unchanged for transitional readability;
- do not resolve anything.

Do **not** make this constructor accept a schema and resolve it internally.

## P1-T04: Document legacy pins as compatibility payload, not v2 authority

**Files:**

- comments in `internal/lock/lock.go`
- comments in `internal/lock/universal.go`

Update comments only after P1-T03 establishes the distinction.

Required wording concept:

```text
v1 ToolPin values remain readable/migratable.
When a v2 universal projection exists, ToolPin is compatibility data and must
not be used to override the universal resolved identity in operational v2 paths.
```

Do not claim legacy fields can be deleted yet.

## P1-T05: P1 gate

Search:

```bash
rg -n "MethodsHash|SourceHash" internal/lock internal/app
```

Acceptance:

- metadata can be constructed without `ResolveAll`;
- legacy `ResolveAll` still behaves identically;
- no v2 consumer has been changed yet.

---

# 8. Phase P2: Make v2 consumption independent from legacy ToolPin mutation

**Goal:** when a v2 lock exists, `LockDocument` becomes the operational identity source. Legacy pins may remain serialized, but they no longer patch the schema or inject desired versions into v2 execution.

## P2-T01: Split frozen validation by lock envelope version

**Files:**

- `internal/lock/lock.go:516-631`
- `internal/lock/lock_test.go`

Refactor without changing the exported entry point initially:

```text
ValidateFrozen(schema, lock):
    validate arguments/version
    if v1:
        validateFrozenV1(schema, lock)
    if v2:
        validateFrozenV2(schema, lock)
```

### v1 branch

Copy current semantics exactly.

### v2 branch

Must validate:

1. universal projection decodes and validates;
2. current requested method/source identity metadata is unchanged, using the pure metadata from P1;
3. every tool in the effective schema that should have a concrete candidate is represented by the universal projection sufficiently for executor consumption;
4. **do not require legacy `ToolPin` fields merely because a selector is mutable.** Those concrete values are now represented by the universal projection.

Do not use exact whole-document coverage for a filtered install if the persisted v2 lock legitimately has extra entries. Validate “required subset is present”; exact coverage remains appropriate when **writing** a new full projection.

### Suggested pseudocode

```text
validateFrozenV2(schema, lock):
    doc = lock.ProjectionDocument()
    currentMethods, currentSources = SnapshotIntentMetadata(schema)

    for each tool in effective schema:
        require stored MethodsHash[tool] == currentMethods[tool]
        for each current source-hash key:
            require stored SourceHash[key] == currentSources[key]
        require doc contains tool if tool has install candidates

    do NOT inspect ToolPin.Latest/Revision/Digest/PackageVersion
    return success
```

### Why method/source hashes remain here

`LockProjection` currently stores resolved source identity but does not independently encode every requested source/candidate discriminator that legacy requested-intent hashes protect. Removing those hashes in the same migration would widen scope into a lock-format redesign.

Acceptance:

- all P0 v1 tests still pass;
- new v2 test passes even when `Tools` is empty but universal projection + intent metadata are complete;
- v2 fails when method/source requested identity changes;
- v2 subset frozen install is not rejected merely because the full lock has extra entries.

## P2-T02: Prevent `loadLockfile` from applying legacy pins to v2 schemas

**Files:** `internal/app/helpers.go:244-268`.

Change conceptual flow:

```text
load lock
if frozen: ValidateFrozen
if lock is v1: Apply legacy pins
if lock is v2: do not Apply; caller must feed LockDocument to executor
```

Recommended helper boundary:

```text
if lk.Version == 1 {
    lock.Apply(...)
}
```

Do not silently skip `ProjectionDocument` validation. `ValidateFrozen` handles frozen mode; normal v2 consumers should validate when loading projection.

Acceptance:

- v1 install behavior unchanged;
- v2 schema config/transient `LockedRevision`, `LockedDigest`, `LockedVersion`, `_resolved_version`, and legacy checksum substitutions are no longer populated through `lock.Apply`.

## P2-T03: Make `resolveInstallLock` branch before legacy resolution

**Files:** `internal/app/install.go:245+`.

Desired behavior:

```text
lk = load existing lock

if lk is v2:
    validate/read universal projection
    return lk

if lk is v1 or missing:
    retain current legacy compatibility behavior for now
```

Critical rule:

> Existing v2 install must never call `ResolveAll` just because the current schema contains `{latest}`, a branch, a tag, or another mutable selector.

Add a test with an instrumented runner/resolver proving the legacy remote resolver is not reached for a valid v2 lock.

## P2-T04: Limit package-lock identity precheck to v1

**Files:**

- `internal/app/helpers.go:296-327`
- callers in install path

`validateInstallPackageLockIdentity` is specifically built around legacy `ToolPin.PackageSelector`.

For v2, candidate/requested identity validation must occur through the universal plan/lock path, not this helper.

Acceptance:

- v1 retains existing fail-closed mismatch behavior;
- v2 does not consult `ToolPin.PackageSelector`.

## P2-T05: Introduce a read-only universal entry lookup suitable for reporting

**Files:** `internal/plan/lock.go`.

Status/install reporting occasionally needs the locked concrete version without reconstructing legacy pins.

Add the smallest safe API if no existing one is sufficient, for example:

```go
func (d LockDocument) EntryForTool(name string) (LockProjection, bool)
```

Requirements:

- validate or operate on already validated document consistently with existing APIs;
- return a clone, not aliased slices;
- no resolution;
- no candidate guessing beyond unique tool key, because LockDocument already enforces one entry per tool.

Do not create a generic lock query service.

## P2-T06: Make install version synchronization read v2 projection first

**Files:** `internal/app/install.go:439+`.

Current `syncInstalledVersions` reads legacy pins.

Refactor:

```text
if lock v2:
    desiredVersion = LockDocument entry Identity.Version
    use that for the existing backfill/warning behavior
else:
    preserve legacy ToolPin logic
```

Do not treat revision/digest as a semantic package version if existing state/reporting does not do so.

Acceptance:

- v2 version synchronization works with an empty legacy `Tools` map;
- v1 tests remain unchanged.

## P2-T07: Add a no-legacy-authority v2 integration fixture

**Files:** app test helpers / install tests.

Construct a valid v2 lock containing:

- universal projection;
- method/source metadata;
- empty or deliberately stale legacy `Tools` values.

Expected:

- install resolution follows universal projection;
- stale legacy values do not change the desired plan;
- a mismatch in universal projection still fails.

This fixture will be reused by status/update/upgrade tests.

## P2-T08: P2 gate

Run/search checks:

```bash
rg -n "lock\.Apply\(" internal/app
rg -n "ResolveAll\(" internal/app/install.go internal/app/helpers.go
```

Expected state after P2:

- v1 branches may still call legacy functions;
- v2 branches do not.

---

# 9. Phase P3: Give update a canonical read-only resolution API

**Goal:** update builds the universal lock from the executor's real candidate resolution seam without first running the legacy remote resolver.

## P3-T01: Extract a machine-oriented “resolve selected candidate” executor API

**Files:**

- `internal/exec/explain.go:69+`
- `internal/exec/resolution.go:29+`
- `internal/exec/capability.go:31+`
- new file allowed: `internal/exec/lock_resolution.go`

Do not make `app` parse `MethodAttempt` statuses forever.

Recommended responsibility:

```text
ResolveLockCandidate(ctx, tool, clan)
    -> select candidates in same executor order
    -> apply static intent/when/adapter availability gates
    -> run canonical resolveCandidatePlan
    -> host compatibility checks
    -> perform only read-only source probing needed for selection
    -> return exactly one resolved candidate suitable for ProjectLock
    -> include resolved source revisions in the returned plan
```

A possible result type:

```go
type ResolvedCandidate struct {
    Method *config.MethodCandidate
    Plan   plan.ResolvedInstallPlan
}
```

Prefer applying source revisions inside `exec` so `app` receives a final `ResolvedInstallPlan`, not source-manager internals.

### Hard constraints

- no install/remove;
- no source mutation;
- no prerequisites mutation;
- no lifecycle hooks;
- no state write;
- reuse `resolveCandidatePlan`, do not duplicate adapter resolution;
- candidate ordering must match normal executor selection.

## P3-T02: Refactor Explain to share the machine-oriented primitive

**Files:** `internal/exec/explain.go`.

Do not make `ResolveLockCandidate` call a formatter-oriented `ExplainTool` and scrape strings.

Preferred direction:

```text
shared internal candidate evaluation primitive
       /                         \
ResolveLockCandidate          ExplainTool
(machine result)              (diagnostic projection)
```

Not:

```text
ResolveLockCandidate -> ExplainTool -> inspect "would_install" string
```

Keep this refactor small. If extraction becomes large, preserve existing Explain implementation and add only enough shared lower-level helpers to remove string/status coupling.

## P3-T03: Add parity tests for candidate selection

**Files:** executor tests.

Test at least:

1. first applicable resolvable candidate is selected;
2. `when` mismatch moves to next candidate;
3. unavailable adapter moves to next candidate;
4. failed resolution moves/fails according to existing explain/install semantics;
5. host-incompatible resolved candidate is rejected;
6. same-kind labeled candidates preserve exact selected method pointer/label;
7. source revision is projected into returned plan;
8. no mutating runner operation occurs.

## P3-T04: Rewrite `resolveUniversalLockDocument` around resolved plans, not MethodAttempt

**Files:** `internal/app/universal_lock.go:16-105`.

Desired core:

```text
for each in-scope tool:
    selected = executor.ResolveLockCandidate(...)
    plans += selected.Plan

retain omitted old v2 entries only when profile semantics permit

doc = plan.BuildLockDocument(plans)
VerifyCoverage(expectedTools)
return doc
```

Delete app-level parsing of:

- `attempt.Error == ""`;
- `attempt.Status == "would_install"`;
- `attempt.Status == "already_installed"`;
- direct `SourceRevisions` application.

Those are presentation details and source-manager details, respectively.

## P3-T05: Load the old lock before fresh update resolution

**Files:** `internal/app/update.go:51-181`.

Current sequence resolves legacy pins before loading old lock. New sequence needs the old envelope early to choose the migration path.

Reorder safely:

```text
load full schema + effective closure
load existing lock
choose v1 compatibility vs universal update path
resolve
persist
```

No host mutation is introduced.

## P3-T06: Define update path selection explicitly

**Files:** `internal/app/update.go`, possibly a small private helper.

Policy:

```text
CASE A: no previous lock, full-scope update
    -> canonical universal resolution
    -> create v2 lock

CASE B: previous v2, full-scope update
    -> canonical universal resolution
    -> create refreshed v2 projection

CASE C: previous v2, profile update
    -> canonical resolution for profile
    -> retain still-valid omitted entries from previous v2
    -> verify full current expected coverage
    -> write v2

CASE D: previous v1 or no lock, profile update
    -> remain on legacy v1 compatibility path
       because complete universal coverage cannot be proven
```

This preserves the rationale already documented in `canPersistProjection` at `internal/app/universal_lock.go:123-145`.

## P3-T07: Build v2 method/source metadata without ResolveAll

**Files:** `internal/app/update.go`.

Use P1's pure metadata helper.

For a full v2 document, metadata should represent the current full effective install closure used by the lock.

For profiled update over existing v2:

- current in-scope metadata is refreshed;
- out-of-scope metadata must be retained only for tools still in the expected closure;
- removed tools must not remain merely because the old map had them.

Write a small deterministic merge helper if needed. Do not use `lock.Merge` blindly for this because its semantics were designed around legacy pin resolution.

## P3-T08: Preserve materialized integrity without reintroducing legacy resolution

**Files:**

- `internal/app/update.go`
- `internal/app/universal_lock.go` or a small lock/app helper
- tests in `internal/app/update_test.go`

This task is critical.

Current behavior can retain a materialized checksum that `update` cannot derive without downloading the artifact.

Implement a **narrow carry-forward step**, not generic legacy `Apply`:

```text
for each newly resolved candidate whose artifact integrity is still unresolved:
    if previous v2 projection has a compatible immutable artifact checksum:
        carry only that integrity datum
    else if migrating from v1 and matching legacy pin has materialized checksum:
        carry only that checksum
    else:
        leave unresolved and let BuildLockDocument fail closed as today
```

Compatibility checks must include enough candidate/artifact identity that an old digest is not attached to a different artifact accidentally.

Do not carry:

- old latest version;
- old Git revision;
- old container digest;
- old package version;

Those are exactly the mutable values update is supposed to refresh canonically.

## P3-T09: Stop full-scope v2 update from calling ResolveAll

**Files:** `internal/app/update.go`.

After P3-T06 through T08:

```text
if output will be v2:
    do not call ResolveAll
    do not call lock.Apply
```

Legacy `ResolveAll` remains only in Case D until later isolation/rename.

Add a test that makes the legacy resolver impossible/failing while canonical executor resolution succeeds; full v2 update must still succeed.

## P3-T10: Preserve optional legacy payload without making it authoritative

When writing a v2 lock, choose one conservative policy and test it:

Recommended policy for this migration:

```text
carry existing legacy Tools values unchanged when migrating an existing lock;
do not freshly remote-resolve them for a v2 write;
do not require them for current v2 execution.
```

For a brand-new v2 lock, an empty `Tools` map is acceptable once P2 validation/consumption is complete.

Rationale: old fields remain migration/debug payload; refreshing them would require keeping the parallel resolver alive.

## P3-T11: Preserve update drift reporting using universal identity

**Files:** `internal/app/update.go:232+` and associated tests.

Current `reportVersionDrift` is pin-oriented.

For v2 update:

- compare installed state version to `LockDocument` entry `Identity.Version` when version is meaningful;
- do not invent a version from revision/digest;
- keep warning-only semantics.

Leave v1 reporting path intact.

## P3-T12: Update tests for canonical update semantics

Required cases:

1. missing lock + full update => v2 with complete projection;
2. v1 + full update => v2;
3. v1 + profile => remains v1;
4. v2 + profile => retained omitted entries, exact current closure;
5. removed tool pruned;
6. changed source accepted only by update;
7. mutable Git selector resolves through adapter path;
8. mutable package selector resolves through adapter path;
9. container tag resolves through adapter path;
10. GitHub/latest artifact resolves through adapter path;
11. materialized auto checksum carry-forward survives;
12. unresolved required integrity fails closed;
13. no install/remove/source mutation is executed;
14. stale legacy ToolPin disagrees with fresh universal result => universal result wins.

## P3-T13: P3 gate

Search:

```bash
rg -n "ResolveAll\(" internal/app/update.go internal/app/universal_lock.go
rg -n "lock\.Apply\(" internal/app/update.go internal/app/universal_lock.go
```

Expected:

- only the explicitly bounded v1/profile fallback may still invoke legacy resolution;
- universal v2 update path has zero legacy remote resolution.

---

# 10. Phase P4: Make status a direct LockDocument client

**Goal:** status uses the same desired resolved identity as install/explain instead of injecting legacy desired versions.

This phase is relatively self-contained after P2 and is a good parallel workstream with later P3 tasks.

## P4-T01: Load universal projection into the status executor

**Files:** `internal/app/status.go:55+`.

If the loaded lock is v2:

```text
doc = lk.ProjectionDocument()
exec.WithLockDocument(doc)(ex)
```

Do not call `lock.Apply` for v2.

## P4-T02: Split status desired-state logic by lock version

**Files:** `internal/app/status.go:197+`, `210+`.

For v1:

- retain current `lockPinForCandidate` + `pinnedVersion` compatibility behavior.

For v2:

- call canonical `ResolveAndVerifyCandidate` or equivalent with the lock-aware executor;
- do not call `ResolveAndVerifyCandidateAtVersion` with a legacy pin-derived version.

Pseudocode:

```text
if v2:
    resolved, verification = ex.ResolveAndVerifyCandidate(...)
else:
    desiredVersion = legacyPinnedVersion(...)
    resolved, verification = ex.ResolveAndVerifyCandidateAtVersion(...)
```

## P4-T03: Derive outdated/drift state from reconciliation result for v2

Avoid comparing only strings when the universal plan has richer identity.

Use the existing verification state:

```text
Satisfied -> current
Drifted   -> outdated/drifted
Absent    -> missing
Unknown   -> unknown/fail-closed reporting
Broken    -> broken
```

Preserve current CLI vocabulary unless a test/docs change is explicitly intended.

## P4-T04: Add stale-legacy-pin disagreement test

Fixture:

```text
ToolPin version = old
LockDocument version = new
observed installed = new
```

Expected v2 status: satisfied/current.

Inverse fixture:

```text
ToolPin version = new
LockDocument version = old
observed installed = new
```

Expected v2 status: drifted relative to universal lock.

This proves authority moved.

## P4-T05: P4 gate

For v2 status, `internal/app/lock_pin.go` must not be on the operational desired-state path.

---

# 11. Phase P5: Isolate the v1 compatibility engine

**Goal:** make legacy resolution visibly and mechanically a compatibility subsystem, reducing the chance that later code accidentally reuses it for v2.

Do this only after P2/P3/P4 tests prove v2 no longer needs it.

## P5-T01: Rename the legacy resolver

**Files:** `internal/lock/lock.go`, all callers/tests.

Preferred name:

```go
ResolveLegacyV1
```

Keep a temporary unexported/compat wrapper only if necessary to keep a staged branch compiling. Final code should not advertise `ResolveAll` as a general-purpose current resolver.

Comment must say:

```text
Compatibility resolver for lock envelope v1 only.
New v2 code must resolve through internal/exec AdapterV2.ResolvePlan.
```

## P5-T02: Rename legacy application semantics

Preferred name:

```go
ApplyLegacyV1
```

or equivalent explicit name.

Before applying, verify lock version expectations. Avoid applying a v2 lock accidentally.

## P5-T03: Rename legacy pin helpers where clarity improves

**Files:** `internal/app/lock_pin.go`.

Do not mechanically rename every helper if it creates noise. At minimum comments and call sites must make it obvious that:

- `ToolPin` lookup is a v1 compatibility mechanism;
- v2 paths use `LockDocument`.

## P5-T04: Add version guards

Legacy mutation/resolution functions should reject or no-op clearly when handed a v2 envelope in a way that could make them authoritative.

Prefer fail-fast errors for resolver APIs; avoid panics.

## P5-T05: Call-site audit

Search:

```bash
rg -n "ResolveLegacyV1|ApplyLegacyV1|ToolPin|lockPinFor|pinnedVersion" internal/app internal/exec
```

Classify every remaining call as one of:

1. v1 read compatibility;
2. v1/profile migration fallback;
3. legacy-format test;
4. bug that still makes v2 depend on legacy state.

No unclassified call may remain.

## P5-T06: P5 gate

A reviewer should be able to understand from function names alone that v2 has one resolution engine and v1 has a quarantined compatibility resolver.

---

# 12. Phase P6: Specify replacement semantics before coding them

**Goal:** make the destructive transition explicit and testable before moving it into the executor.

**Production mutation change:** none yet.

## P6-T01: Define the replacement lifecycle in comments/tests

Use this state sequence as the implementation contract:

```text
A. Resolve target, observe current state, reconcile => TransitionUpgrade
B. Complete every non-destructive/fail-fast prerequisite possible
C. Run before-upgrade hook
D. Persist replacement WAL BEFORE remove
E. Remove old installation
F. Persist "old removed"
G. Persist new-install commit boundary
H. Install exact ResolvedInstallPlan
I. Observe/verify desired target
J. Atomically finalize tool state/resource ownership and close replacement WAL
K. Run after-upgrade hook according to existing lifecycle policy
```

If existing post-hook ordering requires state finalization before post-hook, preserve the existing executor invariant and document it. Do not reorder hooks casually.

## P6-T02: Enumerate crash/failure points

Write table-driven tests first for desired recovery decisions:

| Persisted phase | Observed host | Required behavior |
|---|---|---|
| before removal | old still present | safe to retry/continue from pre-removal |
| removal in-flight | old present | removal did not establish target absence; reconcile conservatively |
| removal in-flight | absent | old likely removed; advance only with authoritative evidence |
| removed | absent | safe to proceed toward exact install |
| install in-flight | desired satisfied | finalize commit |
| install in-flight | absent | do not guess whether partial install side effects exist; method-specific evidence or blocked recovery |
| install in-flight | unknown/broken | fail closed |
| finalization complete | desired satisfied | no active replacement journal |

The exact status names may differ. The semantics may not.

## P6-T03: Decide the minimum persisted replacement identity

Do not persist secrets.

Recommended persisted fields:

```text
tool name
tracked method kind
tracked method label / durable candidate discriminator available from state
previous ToolState snapshot or equivalent removal identity
immutable desired LockProjection (not mutable selector)
replacement phase/journal
```

Rationale:

- recovery can identify what was being replaced;
- the desired target is immutable and credential-free;
- current schema can reconstruct operational semantics;
- if current schema no longer matches persisted identity, recovery fails closed rather than guessing.

## P6-T04: Explicitly reject putting replacement I/O in plan package

No code task here. This is a review gate.

Allowed in `internal/plan`:

- replacement phase enum;
- pure transition validation;
- pure recovery decision helpers.

Not allowed:

- adapter calls;
- state writes;
- subprocesses;
- filesystem mutation.

## P6-T05: P6 gate

Do not begin destructive implementation until table-driven desired recovery behavior is agreed and represented in tests or a dedicated small design comment/ADR amendment.

---

# 13. Phase P7: Add a durable replacement transaction

**Goal:** provide the minimum crash-safe persistence needed before executor can own `Remove -> InstallResolved`.

## P7-T01: Add pure replacement journal types

**Files:** recommended new file `internal/plan/replacement.go`.

Keep it small.

Possible states:

```text
pending/removal_planned
removing
removed
installing
```

Terminal success should normally remove the active journal rather than persist a terminal record indefinitely, matching preparation transaction style.

Pure methods should validate legal transitions, e.g.:

```text
PlanRemove
RecordRemoved
PlanInstall
```

If an ambiguous phase needs explicit resolution, provide a pure decision method rather than setting fields ad hoc.

## P7-T02: Add persistence-safe replacement record to state

**Files:**

- `internal/state/state.go:30-70`
- new `internal/state/replacement.go`
- state validation/secrets tests

Recommended shape conceptually:

```text
ReplacementTransactions map[toolName]ReplacementTransaction
```

where each record contains the minimal identity from P6-T03 plus pure journal state.

Requirements:

- current state checksum includes it automatically;
- `ValidateNoSecrets` checks it;
- deterministic JSON;
- at most one active replacement per tool;
- invalid/terminal active records rejected.

Because state format is explicitly pre-freeze but exact-versioned, follow the repository's version policy. If adding the field changes persisted semantics enough to require a state-version bump under current project convention, increment `formatversion.CurrentStateVersion` and update fixtures deliberately. Do not guess; follow neighboring state-version tests/comments.

## P7-T03: Persist “begin replacement” before removal

Add state API resembling existing preparation transaction methods:

```text
BeginReplacement(...)
```

It must:

- require exclusive state lock;
- verify no active replacement for that tool;
- persist previous tracked state + desired immutable target;
- save before returning success.

No adapter call belongs here.

## P7-T04: Persist removal write-ahead boundary

Add:

```text
PlanReplacementRemoval(tool)
```

Semantics:

1. transition journal to removal-in-flight;
2. persist state;
3. caller may then invoke adapter removal.

This boundary is mandatory before host deletion.

## P7-T05: Persist successful removal

Add:

```text
RecordReplacementRemoved(tool)
```

Call only after adapter removal returned success and, if the adapter contract permits, absence verification confirms the old target is gone.

Persist before starting new install.

## P7-T06: Persist install write-ahead boundary

Add:

```text
PlanReplacementInstall(tool)
```

Persist immediately before exact target install mutation.

Coordinate this with existing preparation `PlanPreparationCommit` rather than creating two contradictory truths.

Recommended ordering:

```text
replacement says old removed
-> preparation PlanCommit (if preparation tx exists)
-> replacement PlanInstall
-> AdapterV2.InstallResolved
```

or one atomic state-save helper that advances both WAL records together.

Choose one order and test crash semantics between every boundary.

## P7-T07: Add atomic successful finalization

The successful path must update in one durable state save:

- new `ToolState`;
- ownership/resource claims resulting from prepared dependencies/sources;
- removal of preparation journal/plan if active;
- removal of replacement transaction.

Reuse the internals of `FinalizePreparationCommitWithTool` (`internal/state/preparation.go:172+`) instead of duplicating ownership logic.

Recommended refactor:

```text
private finalizeCandidateCommit(... optional replacement transaction ...)
```

Both regular install and replacement finalizers call it.

Do not regress existing install state finalization.

## P7-T08: Add replacement recovery query API

State layer returns persisted record. Executor owns observation.

Avoid a state API that reaches into adapters.

Example boundary:

```text
ReplacementTransaction(tool) -> record
ResolveReplacementObservation(record, observation) -> pure decision
```

Pure decision logic may live in `plan`.

## P7-T09: Recovery: old still present before confirmed removal

When journal indicates pre-removal/removing and observation authoritatively proves the old tracked target is still present:

- do not install new target over it unless the adapter/transition explicitly supports in-place replacement;
- return/retry at the removal phase safely according to the state machine.

## P7-T10: Recovery: old removed, desired absent

When persisted state proves removal completed and desired target is absent:

- recovery may safely resume toward installation because destructive removal is not ambiguous anymore;
- reconstruct current operational intent;
- require it to match persisted desired immutable identity;
- if schema drift prevents reconstruction, fail closed with remediation rather than selecting another candidate.

## P7-T11: Recovery: install in-flight and desired satisfied

Observation proving the exact desired identity is satisfied allows finalization without replaying install.

This mirrors existing preparation commit reconciliation principles.

## P7-T12: Recovery: install in-flight and outcome ambiguous

Unknown/broken or partial identity must remain blocked.

Do not infer success from simple presence.

## P7-T13: Add state persistence tests

Required tests:

- begin persists and round-trips;
- duplicate active transaction rejected;
- illegal transition rejected;
- each write-ahead phase survives reload;
- checksum detects corruption;
- secret validation rejects forbidden material;
- successful finalize removes transaction;
- previous tool state is not lost before replacement commits;
- ownership remains correct after successful replacement;
- recovery paths from P6-T02 produce expected decisions.

## P7-T14: P7 gate

No production code may call adapter `Remove` under the new replacement path until these persistence tests pass.

---

# 14. Phase P8: Move replacement execution into the executor

**Goal:** make `TransitionUpgrade` a first-class executor transition using the same candidate pipeline as install.

## P8-T01: Add transition-specific mutation branching in candidate execution

**Files:** `internal/exec/attempt.go:144-455`.

The executor already sets:

```go
ac.transition = decision.Transition
```

at `gateAlreadyInstalled`.

Change the final mutation stage conceptually from:

```text
always InstallResolved
```

to:

```text
switch ac.transition:
    install:
        existing install commit
    upgrade:
        durable replace commit
    repair:
        existing/explicit repair semantics, if already supported
```

Do not duplicate all preceding candidate phases for upgrade.

## P8-T02: Reuse pre-existing source/prerequisite/hook phases for upgrade

Because the normal attempt pipeline already executes candidate selection, source handling, lifecycle hooks, and prerequisites, replacement should enter only at the final mutation boundary.

Before destructive removal, ensure these have completed:

- exact resolution;
- observation/reconciliation yielding `TransitionUpgrade`;
- source viability/preparation;
- capability checks;
- pre-upgrade hook;
- method prerequisites.

**Note:** current phase ordering in `internal/exec/execute.go:207-216` must be preserved or deliberately adjusted with tests. Do not rely on an outdated prose description of the pipeline.

## P8-T03: Add `replaceCandidate` executor primitive

**Files:** recommended `internal/exec/replacement.go`.

Responsibility:

```text
assert TransitionUpgrade
assert exact resolved target exists
assert adapter CanRemove
open/coordinate durable state transaction
persist begin/remove WAL
remove tracked old candidate
persist removed
persist install WAL / preparation commit
InstallResolved exact target
observe exact target
reconcile exact target
finalize state + resources + WAL
return result
```

It must receive the exact `ac.resolved` / `ac.reported` target; it must not call `ResolvePlan` again.

## P8-T04: Project the correct removal target

Current app removal logic reconstructs the tracked candidate from state/config.

Move or reuse the minimum helper necessary so executor removal targets the **old tracked installation**, not blindly the new desired plan.

Inputs available:

- current `ToolState`;
- exact tracked method identity;
- current schema candidate;
- adapter.

Do not remove by “first method with same kind.”

## P8-T05: Keep credentials scoped to the appropriate mutation

Removal and install may have different credential requirements.

Reuse existing execution credential context logic; do not persist secret values or expose them to unrelated subprocesses.

## P8-T06: Verify exact desired state after replacement install

After `InstallResolved` returns success:

```text
verification = VerifyResolvedCandidate(... exact desired resolved plan ...)
```

Expected:

- `StateSatisfied` => eligible to finalize;
- `StateDrifted`, `StateAbsent`, `StateUnknown`, `StateBroken` => do not claim successful upgrade.

If current install semantics intentionally do not post-verify, this stricter check is specific to destructive replacement and must be covered by replacement tests.

## P8-T07: Preserve state root intent and candidate identity

Replacement final state must preserve/update:

- `RootRequested` from previous state;
- technical `MethodKind`;
- human/exact method label representation used by current state semantics;
- fresh definition/desired-state hashes;
- observed version where authoritative, otherwise locked concrete version when existing semantics permit;
- postinstall state according to hook execution outcome.

Use existing `toolStateForResult`/state projection helpers where possible. Do not maintain a second state-builder in app.

## P8-T08: Integrate replacement recovery into executor startup

**Files:**

- `internal/exec/preparation.go:673+` or adjacent recovery file
- executor initialization path

Recovery order must be deterministic.

Recommended:

```text
recover active replacement transactions
recover/coordinate candidate preparation transactions
then permit new host mutations
```

If replacement and preparation refer to the same candidate, recovery must understand them as one coordinated transition rather than two independent tasks racing each other.

## P8-T09: Add exact-candidate executor entry point for upgrade command

The app needs a way to request execution of one already selected/tracked candidate without allowing method fallback.

Use existing internal `candidateResolutionSeed` machinery (`internal/exec/attempt.go:51+`, `internal/exec/execute.go:109+`) rather than inventing another execution engine.

Recommended exported API shape conceptually:

```go
func (ex *Executor) ExecuteResolvedCandidate(
    ctx context.Context,
    schema *config.Schema,
    clan string,
    tool *config.Tool,
    method *config.MethodCandidate,
    resolved *plan.ResolvedInstallPlan,
) (ToolResult, error)
```

Requirements:

- exact method only, no fallback;
- same attempt phases as normal execution;
- same source/prerequisite/hook/security semantics;
- same replacement branch when reconciliation yields upgrade;
- initialize/recover executor state correctly;
- do not re-resolve the seeded plan;
- validate seed corresponds to the provided method/tool.

If a smaller existing public API can provide all of this, use it instead. Do not export internal types unnecessarily.

## P8-T10: Remove the low-level InstallResolvedCandidate dependency from upgrade

`internal/exec/github_secret.go:77+` currently exposes `InstallResolvedCandidate`, which is a thin adapter install wrapper and bypasses the full lifecycle.

After upgrade is migrated:

- no upgrade app code should use it;
- retain it only if another legitimate caller needs it;
- otherwise reduce/delete it in final cleanup.

Do not delete it before call-site audit.

## P8-T11: Convert former upgrade-preflight restriction tests

The following current failures should become supported **only because the executor now preserves their semantics**:

- sources;
- `method.requires`;
- tool dependencies;
- pre/post lifecycle hooks.

For each former restriction:

1. replace “preflight rejects” test with an executor-backed successful or correctly failing lifecycle test;
2. prove prerequisite/source/hook happens in correct order relative to removal;
3. prove failure before removal leaves old tool intact.

Arbitrary-code policy and unsupported `CanRemove` remain legitimate gates.

## P8-T12: Add replacement ordering tests

Use a recording adapter/runner.

Expected successful ordering, simplified:

```text
ResolvePlan
Observe old/desired
prepare source/prereqs
before-upgrade hook
WAL remove boundary
Remove
WAL removed
WAL install boundary
InstallResolved(exact target)
Observe desired
state finalize
after-upgrade hook (according to established executor ordering)
```

No test should depend on incidental logging order.

## P8-T13: Add destructive-failure tests

At minimum:

1. source preparation fails => Remove never called;
2. prerequisite fails => Remove never called;
3. pre-hook fails => Remove never called;
4. WAL save before remove fails => Remove never called;
5. Remove fails => Install never called and old tracked state retained/recovery record correct;
6. state save after remove fails => no unjournaled install attempt;
7. InstallResolved fails after removal => transaction remains recoverable/fail-closed;
8. post-install verification fails => not reported as upgraded;
9. final state save fails => recovery journal remains enough to reconcile;
10. crash-recovery desired satisfied => no duplicate install.

## P8-T14: P8 gate

A direct executor replacement test must demonstrate a candidate with sources + prerequisite + upgrade hooks can replace successfully with exact locked identity.

---

# 15. Phase P9: Reduce upgrade command to a client of canonical reconciliation

**Goal:** remove app-owned replacement mechanics and legacy v2 discovery.

## P9-T01: Split v1 and v2 upgrade discovery

**Files:** `internal/app/upgrade.go:210+`.

### v1

Keep current legacy pin/version discovery as compatibility behavior.

### v2

For each tracked tool:

1. resolve exact tracked candidate from state;
2. use lock-aware executor to obtain exact locked desired plan;
3. observe/reconcile through canonical executor path;
4. include tool when `TransitionForVerification` requires `TransitionUpgrade`.

Do not derive v2 target from `ToolPin`.

## P9-T02: Replace `upgradeOutdatedTool.pinnedVer` as v2 authority

If the internal struct currently assumes a string target version, introduce a field carrying the resolved desired plan for v2.

Conceptually:

```text
legacyTargetVersion string   // v1 only
resolvedTarget *ResolvedInstallPlan // v2
verification VerificationResult
```

Avoid one overloaded string that pretends every adapter identity is a version.

## P9-T03: Make dry-run use the same resolved target

For v2 dry-run:

- no remove/install;
- report `would_upgrade` from reconciliation transition;
- display target version if `resolved.Identity.Version` is non-empty;
- otherwise use existing non-version reporting conventions.

No separate version resolver.

## P9-T04: Replace `upgradeSingleTool` mutation body with executor call

**Files:** `internal/app/upgrade.go:314+`.

Target shape:

```text
upgradeSingleTool:
    validate/report command policy
    call ex.ExecuteResolvedCandidate(... exact tracked method, resolved target ...)
    translate ToolResult into upgradeResult
```

The function must no longer:

- call `adapter.Remove`;
- call `InstallResolvedCandidate`;
- manually run source/prerequisite/hook restrictions;
- manually update durable ToolState after successful executor commit.

## P9-T05: Delete `removeInstalledTool` after no callers remain

**Reference:** `internal/app/upgrade.go:441+`.

Only delete after executor owns removal and tests pass.

If remove command uses a different helper, do not conflate them.

## P9-T06: Delete `reinstallUpgradeTool` after no callers remain

**Reference:** `internal/app/upgrade.go:458+`.

Its behavior is specifically the bypass being removed.

## P9-T07: Delete or collapse `preflightDirectUpgrade`

**Reference:** `internal/app/upgrade.go:681+`.

The following checks should now belong to canonical executor phases:

- candidate static capability;
- `when`;
- adapter availability;
- resolved target;
- observation/reconciliation;
- transition capabilities;
- source/prerequisite/hook support;
- package availability;
- removal support.

App may retain only command-level policy checks not otherwise represented.

## P9-T08: Remove app-owned failed-removal state workaround when replacement WAL supersedes it

**Reference:** `internal/app/upgrade.go:663-678` `recordFailedUpgradeRemoval`.

Do not delete until replacement failure/recovery tests prove the WAL/state finalizer covers the same ownership concern.

Expected final behavior: state transitions are owned by executor/state transaction, not manually patched by upgrade command after partial failure.

## P9-T09: Release outer upgrade state lock ownership

Current real upgrade keeps an exclusive state lock over the app-level read-modify-write sequence (`loadUpgradeState`, approximately `130+`).

Once executor replacement owns durable transactions:

- app discovery may read a snapshot/shared state;
- executor must acquire/revalidate exclusive state at each destructive transition;
- app must not hold an exclusive state lock while calling executor code that needs its own locked state, or deadlock is possible.

Add concurrency/state-staleness tests as appropriate.

## P9-T10: Revalidate state immediately before destructive replacement

Because app no longer owns one long exclusive lock, executor must ensure the tracked state used for replacement still matches the expected candidate before removing anything.

If another process changed state after discovery:

- abort/re-resolve that tool;
- do not remove based on stale discovery data.

## P9-T11: Preserve confirmation/reporting at app layer

The CLI remains responsible for:

- printing proposed upgrades;
- confirmation prompt;
- `--only`;
- `--force` policy as currently defined;
- `--dry-run` presentation;
- JSON/plain output translation;
- aggregate exit status.

Do not push CLI formatting into executor.

## P9-T12: v2 disagreement test

Construct:

```text
legacy ToolPin = target A
universal LockDocument = target B
state installed = target A
```

Expected v2 upgrade target: B.

No app branch should choose A because the old pin happened to be present.

## P9-T13: v1 compatibility test

A genuine v1 lock must still upgrade using documented legacy pin semantics.

This test prevents “universal-only” cleanup from silently dropping promised readability.

## P9-T14: P9 gate

Search:

```bash
rg -n "\.Remove\(|InstallResolvedCandidate|preflightDirectUpgrade|lockPinForCandidate|pinnedVersion" internal/app/upgrade.go
```

Expected:

- no direct adapter mutation in upgrade app;
- pin helpers appear only inside explicit v1 compatibility branch, if at all;
- canonical v2 replacement goes through executor.

---

# 16. Phase P10: Finish install/post-install lock convergence

**Goal:** remove remaining accidental v2 dependence on legacy lock regeneration while preserving v1/missing-lock behavior deliberately.

This phase should happen after update/status/upgrade authority is clean. Otherwise it is too easy to erase the only persistence path for a subtle identity.

## P10-T01: Split `saveLockfile` by lock mode

**Files:** `internal/app/helpers.go:271+`.

Desired policy:

```text
if existing lock is v2:
    do not call legacy resolver
    do not rewrite universal identity during install
    persist only explicitly allowed post-install materialization, if any

if v1/missing:
    compatibility behavior remains until canonical post-install persistence is implemented
```

## P10-T02: Determine whether successful executor results already contain all persistable identity

**Files:**

- `internal/exec/types.go:43-78`
- `internal/exec/attempt.go:343-352`, `381+`
- adapter-specific materialization, especially HTTP

`ToolResult.PlanIntent` carries the resolved/executed plan, but `sha256:auto` can be materialized by HTTP install into method config after `InstallResolved` (`internal/httpdownload/adapter.go:182-199`).

Characterize whether `PlanIntent` itself receives the final checksum. It likely does not because the adapter mutates an effective method/config, not the plan artifact.

Do not assume report plan is sufficient until a test proves it.

## P10-T03: Add explicit post-install resolved-integrity projection if needed

If P10-T02 proves runtime materialized integrity is absent from `ToolResult.PlanIntent`, add the smallest explicit executor result field/API needed to return persistence-safe final identity.

Possible shape:

```text
ToolResult.LockPlan *ResolvedInstallPlan
```

or an updated clone of `PlanIntent` with materialized checksum applied.

Constraints:

- no secret values;
- do not expose arbitrary mutable method config;
- exact executed target only;
- adapters should not independently write the lockfile.

## P10-T04: Persist new materialized integrity into existing v2 without refreshing mutable identity

This is subtle.

If v2 lock contains an unresolved integrity field that can legally be materialized during install, the post-install persistence step may fill that field **only if all other immutable identity matches the existing lock entry**.

It may not update:

- version;
- revision;
- digest;
- source;
- registry;
- candidate identity.

If identity differs, instruct user to run update; do not bless drift during install.

## P10-T05: Decide missing-lock/full-install behavior explicitly

Current install can produce/update legacy pins automatically.

Recommended low-risk policy for this migration:

```text
install without an existing lock may continue to use the v1 compatibility
persistence path; `depengine update` is the canonical promotion point to v2.
```

This avoids turning install into another universal-lock writer before all runtime-integrity cases are proven.

A later focused change can make full install emit v2 directly if desired.

The architectural requirement is narrower:

> once v2 exists, install no longer invokes a parallel legacy resolver.

## P10-T06: Rename compatibility save helper

If v1 save behavior remains, make the name explicit, e.g.:

```text
saveLegacyInstallLock
```

so later contributors do not call it from v2 code casually.

## P10-T07: Remove v2 calls to mergeInstallLock

`mergeInstallLock` is built around legacy pins and preservation rules.

After v2 post-install persistence is explicit, v2 must not use this merge as a generic lock update function.

Keep it for v1 compatibility as needed.

## P10-T08: P10 gate

Search:

```bash
rg -n "ResolveLegacyV1|saveLegacyInstallLock|mergeInstallLock" internal/app/install.go internal/app/helpers.go
```

Expected:

- existing v2 install path never invokes remote legacy resolution;
- only v1/missing-lock compatibility branch does.

---

# 17. Phase P11: Cleanup, documentation, and architectural verification

## P11-T01: Remove dead dual-path helpers

Only after all call-site gates pass, delete helpers that are truly unused:

- direct-upgrade reinstall wrapper usage;
- app-specific removal wrapper superseded by executor;
- obsolete pin-to-v2 adapters;
- duplicate resolution/status helpers.

Do not delete v1 compatibility helpers merely because v2 no longer calls them.

## P11-T02: Re-audit `internal/app/lock_pin.go`

Every surviving helper must have a documented reason:

- v1 compatibility;
- state exact-candidate lookup independent of lock pins;
- remove command behavior.

Split state-candidate helpers from legacy-pin helpers if their mixed file makes the boundary misleading.

A reasonable cleanup is:

```text
candidate_state.go  // state -> exact schema candidate
legacy_lock_pin.go  // v1 ToolPin only
```

Do this only if it reduces conceptual mixing without changing behavior.

## P11-T03: Update ADR/support docs

At minimum review/update:

- `docs/design/adr-001-universal-lock-projection.md`
- `docs/design/adr-002-transactional-preparation.md`
- `docs/design/adr-005-resolved-install-plan-projection.md`
- `docs/design/state-model.md`
- `docs/architecture.md`
- `docs/support-boundary.md`
- `.dev/architecture/` relevant flow/unit files

Documentation must state:

1. v2 operational identity authority is `LockDocument`;
2. legacy pins are v1 compatibility/migration payload;
3. update resolves v2 through executor/AdapterV2;
4. status consumes universal lock directly;
5. upgrade replacement is executor-owned and durable;
6. v1 remains readable;
7. any remaining `MethodsHash`/`SourceHash` role is requested-intent validation, not concrete resolution.

## P11-T04: Update architecture map only for real flow changes

Reflect:

- update no longer `ResolveAll -> Apply -> universal resolve` for v2;
- status v2 no longer pin-version injects;
- upgrade no longer app `Remove -> InstallResolvedCandidate`;
- replacement WAL/state ownership.

Do not claim removal of v1 support if it remains.

## P11-T05: Full static call-site audit

Run at least:

```bash
rg -n "ResolveAll|ResolveLegacyV1" .
rg -n "lock\.Apply|ApplyLegacyV1" .
rg -n "ToolPin|lockPinForCandidate|pinnedVersion" internal/app internal/exec
rg -n "InstallResolvedCandidate" .
rg -n "\.Remove\(" internal/app/upgrade.go internal/exec
rg -n "ResolveAndVerifyCandidateAtVersion" internal/app
```

For every result, write a one-line classification in PR notes if its purpose is not obvious from code.

## P11-T06: Full test matrix

Run, where environment supports it:

```bash
go test ./internal/plan
go test ./internal/state
go test ./internal/lock
go test ./internal/exec
go test ./internal/app
go test ./...
```

Then project-standard linters/formatters.

Pay special attention to BSD/portable subprocess assumptions because the project has recently had cross-platform CI failures. Do not add shell-dependent test setup when Go helpers suffice.

## P11-T07: End-to-end scenario matrix

Manually or via integration tests cover:

### Lock lifecycle

- no lock -> install compatibility behavior;
- no lock -> full update -> v2;
- v1 -> frozen install;
- v1 -> full update -> v2;
- v1 -> profile update remains safely v1;
- v2 -> normal install;
- v2 -> frozen install;
- v2 -> profile install;
- v2 -> profile update with retained coverage;
- v2 -> source/method drift rejected by frozen install;
- v2 -> update accepts intended drift.

### Status

- v2 satisfied;
- v2 version drift;
- v2 non-version identity drift;
- v2 stale legacy pin disagreement;
- v1 compatibility.

### Upgrade

- ordinary version upgrade;
- source-backed upgrade;
- method prerequisite upgrade;
- tool dependency upgrade;
- pre/post hook upgrade;
- same-kind labeled candidate;
- unsupported removal;
- removal failure;
- install failure after removal;
- crash after removal;
- crash during install;
- successful recovery/finalization;
- unknown/broken observation remains blocked.

## P11-T08: Final architecture acceptance check

A reviewer should be able to answer “yes” to all:

1. Does v2 have exactly one mutable-identity resolution path?
2. Is that path executor + AdapterV2?
3. Does LockDocument provide v2 concrete identity to install/status/upgrade?
4. Is `internal/lock` for v2 primarily persistence/validation rather than remote resolution?
5. Is legacy remote resolution visibly v1-only?
6. Does update use canonical read-only resolution?
7. Does status use canonical observation/reconciliation?
8. Does upgrade use canonical observation/reconciliation and executor mutation?
9. Does app upgrade avoid direct adapter removal/install?
10. Is replacement durably journaled before old installation removal?
11. Can recovery distinguish “old still present”, “old removed”, and “new install in flight” without guessing?
12. Do v1 lockfiles remain readable?
13. Are materialized integrity and profile coverage still tested?
14. Did the migration avoid adding an unnecessary new abstraction layer?

---

# 18. Detailed task delegation map

This section groups tasks into delegation-sized packets. Each packet should generally fit one agent/PR-sized unit.

## Packet A: Baseline tests

**Tasks:** P0-T01 through P0-T09  
**Can run in parallel with:** nothing initially.  
**Expected code churn:** mostly tests.

Deliverable: characterization coverage, no production behavior change.

## Packet B: Pure lock metadata extraction

**Tasks:** P1-T01 through P1-T05  
**Depends on:** P0.  
**Can run in parallel with:** early replacement design P6 after P0.

Deliverable: `MethodsHash`/`SourceHash` can be built without remote resolution.

## Packet C: v2 install consumption cleanup

**Tasks:** P2-T01 through P2-T08  
**Depends on:** P1.  
**Can run in parallel with:** P3-T01 through P3-T04 after shared interfaces settle.

Deliverable: existing v2 install is independent from ToolPin mutation/resolution.

## Packet D: Executor lock-resolution API

**Tasks:** P3-T01 through P3-T04  
**Depends on:** P0.  
**Risk:** medium because Explain candidate semantics must remain intact.

Deliverable: update has a machine-oriented canonical resolver.

## Packet E: Update migration

**Tasks:** P3-T05 through P3-T13  
**Depends on:** P1, P2 core validation, Packet D.

Deliverable: full/current v2 update performs no legacy remote resolution.

## Packet F: Status migration

**Tasks:** P4-T01 through P4-T05  
**Depends on:** P2.  
**Parallel-safe with:** most Packet E.

Deliverable: v2 status ignores legacy concrete pins.

## Packet G: Legacy isolation

**Tasks:** P5-T01 through P5-T06  
**Depends on:** P2, P3, P4.

Deliverable: legacy resolver/application visibly v1-only.

## Packet H: Replacement design/tests

**Tasks:** P6-T01 through P6-T05  
**Depends on:** P0.  
**Parallel-safe with:** P1-P4.

Deliverable: agreed state machine and failure matrix before destructive code.

## Packet I: Replacement persistence

**Tasks:** P7-T01 through P7-T14  
**Depends on:** P6.

Deliverable: crash-safe state substrate with no adapter calls yet.

## Packet J: Executor replacement

**Tasks:** P8-T01 through P8-T14  
**Depends on:** P7.

Deliverable: executor can execute exact upgrade transition with sources/prereqs/hooks and WAL.

## Packet K: Upgrade app migration

**Tasks:** P9-T01 through P9-T14  
**Depends on:** P2, P4 concepts, P8.

Deliverable: app upgrade is a client of executor reconciliation/replacement.

## Packet L: Install persistence cleanup

**Tasks:** P10-T01 through P10-T08  
**Depends on:** P2/P3, preferably P9 complete.

Deliverable: existing v2 install never invokes the legacy remote resolver post-run.

## Packet M: Cleanup/docs/final audit

**Tasks:** P11-T01 through P11-T08  
**Depends on:** all prior packets.

Deliverable: no accidental dual authority remains.

---

# 19. Recommended commit sequence

A safe history would look approximately like this:

```text
1. test: characterize lock v1/v2 and direct upgrade behavior
2. refactor(lock): extract pure method/source intent metadata
3. refactor(lock): split frozen v1/v2 validation
4. refactor(install): make v2 lock document authoritative
5. refactor(exec): expose canonical lock-candidate resolution
6. refactor(update): build universal lock without legacy resolution
7. refactor(status): consume universal lock directly
8. refactor(lock): isolate legacy v1 resolver/application
9. test(plan/state): specify replacement journal transitions
10. feat(state): persist durable replacement transactions
11. feat(exec): execute upgrade replacement transaction
12. refactor(upgrade): delegate exact replacement to executor
13. refactor(install): remove remaining v2 legacy save resolution
14. docs: update architecture/support/ADRs
15. chore: remove dead compatibility glue after call-site audit
```

Do not squash all of these into one change while developing. Bisectability is unusually valuable in transaction/refactor work.

---

# 20. Implementation pseudocode: key end states

## 20.1 Frozen lock validation

```text
function ValidateFrozen(schema, lock):
    require schema
    require lock

    switch lock.version:
        case 1:
            return ValidateFrozenLegacyV1(schema, lock)

        case 2:
            doc = lock.ProjectionDocument()
            require doc structurally valid

            methods, sources = SnapshotIntentMetadata(schema)
            require stored requested-intent metadata matches current schema

            for each non-virtual tool in effective schema:
                require doc has an entry for tool

            return success

        default:
            fail unsupported lock version
```

## 20.2 Install lock consumption

```text
function resolveInstallLock(...):
    lock = load lock

    if lock is v2:
        validate if frozen
        do NOT ResolveLegacyV1
        do NOT ApplyLegacyV1
        return lock

    // v1 or missing compatibility path
    validate legacy identity as required
    if mutable selectors require a legacy lock:
        fresh = ResolveLegacyV1(...)
        merge without accepting identity drift
    ApplyLegacyV1(...)
    return lock
```

## 20.3 Update v2

```text
function update(...):
    fullSchema = load + effective closure
    oldLock = load existing

    if profile selected AND old lock is not v2:
        return updateLegacyV1Profile(...)

    scope = profile-filtered resolution scope or full scope
    executor = read-only canonical executor

    freshPlans = []
    for tool in scope:
        plan = executor.ResolveLockCandidate(tool)
        plan = carryOnlyAllowedMaterializedIntegrity(oldLock, plan)
        require plan lockable
        freshPlans += plan

    if profiled existing-v2 update:
        freshPlans += retained still-valid old entries outside scope

    document = BuildLockDocument(freshPlans)
    require document exact coverage of current full expected closure

    methodsHash, sourceHash = SnapshotIntentMetadata(fullSchema)
    newLock = NewUniversal(document, methodsHash, sourceHash, carryLegacyPayload(oldLock))
    save atomically
```

## 20.4 Status v2

```text
function statusToolV2(tool, trackedState, lockDocument):
    method = exact tracked schema candidate
    executor.WithLockDocument(lockDocument)

    resolved, verification = executor.ResolveAndVerifyCandidate(tool, method)

    return projectVerificationToStatus(verification, resolved, trackedState)
```

## 20.5 Upgrade v2 discovery

```text
function discoverUpgradeV2(toolState, tool, lockDocument):
    method = exact tracked candidate
    resolved, verification = executor.ResolveAndVerifyCandidate(tool, method)
    decision = TransitionForVerification(verification)

    if decision.required == false:
        return already-current

    if decision.transition != Upgrade:
        return fail/route according to existing command semantics

    return upgradeTarget(method, resolved, verification)
```

## 20.6 Executor replacement

```text
function replaceCandidate(attempt):
    require attempt.transition == Upgrade
    require exact resolved target
    require adapter.CanRemove

    // All ordinary candidate selection/preparation phases have already passed.

    oldState = load/revalidate tracked ToolState under exclusive state lock
    require oldState still identifies expected tracked candidate

    persist BeginReplacement(oldState, immutableProjection(resolved))
    persist PlanRemoval

    adapter.Remove(old tracked target)

    persist RecordRemoved

    persist candidate preparation/install commit boundary
    persist PlanReplacementInstall

    adapter.InstallResolved(exact resolved target)

    verification = VerifyResolvedCandidate(exact resolved target)
    require verification == Satisfied

    atomically:
        finalize preparation ownership
        write new ToolState
        delete replacement transaction

    run/finalize lifecycle semantics according to existing hook ordering
    return success
```

## 20.7 Replacement recovery

```text
function recoverReplacement(tx):
    reconstruct exact current candidate from persisted tracked identity
    require current intent compatible with persisted immutable target

    switch tx.phase:
        pre-removal/removing:
            observe old + desired
            if old authoritatively present:
                resume/retry removal safely
            else if desired satisfied:
                finalize if state proves transition outcome
            else if authoritative absence and policy proves removal applied:
                advance to removed
            else:
                block; do not guess

        removed:
            observe desired
            if satisfied:
                finalize
            if absent:
                safely resume exact install
            else:
                block

        installing:
            observe desired
            if satisfied:
                finalize without replay
            else:
                block unless method-specific evidence proves install never applied
```

---

# 21. Test ownership matrix

Use this to prevent several agents from creating overlapping, contradictory tests.

| Concern | Primary package | Secondary package |
|---|---|---|
| lock projection purity | `internal/plan` | none |
| v1 selector resolution | `internal/lock` | app compatibility |
| method/source metadata | `internal/lock` | app update |
| v2 frozen validation | `internal/lock` | app install |
| canonical candidate resolution | `internal/exec` | app universal lock |
| update coverage/profile policy | `internal/app` | plan coverage helpers |
| status reconciliation | `internal/app` | exec verification |
| replacement state machine | `internal/plan` | state persistence |
| replacement WAL persistence | `internal/state` | exec recovery |
| replacement execution order | `internal/exec` | app upgrade |
| CLI upgrade reporting | `internal/app` | none |
| v1/v2 command integration | `internal/app` / integration | none |

---

# 22. High-risk areas and explicit mitigations

## R-01: Accidentally making v2 less strict than v1

Risk: deleting `MethodsHash`/`SourceHash` requirements at the same time as ToolPin authority.

Mitigation:

- extract these as pure metadata first;
- keep them in v2 validation during this migration;
- redesign them only in a separate lock-format change.

## R-02: Reusing an old auto checksum for a different artifact

Risk: naive carry-forward of legacy checksum after canonical update resolves a new release URL.

Mitigation:

- carry only when artifact/candidate compatibility is established;
- otherwise fail lockability and require materialization;
- add explicit changed-artifact tests.

## R-03: Candidate-label ambiguity

Current `plan.CandidateIdentity` contains method kind + explicitness, while config/state also distinguish labels and same-kind candidate positions.

Do not silently “solve” this by changing the persisted lock schema in the middle of the migration.

During this plan:

- retain `MethodsHash` validation;
- preserve exact tracked candidate selection from state for upgrade;
- require resolved immutable identity to match the lock;
- keep existing ambiguity fail-closed behavior.

A future dedicated lock-projection format revision may add an explicit candidate discriminator if desired.

## R-04: Deadlock after moving upgrade into executor

Risk: app holds `state.LoadLocked()` while executor tries to open preparation/replacement locked state.

Mitigation:

- remove long-lived app exclusive lock before executor mutation migration completes;
- executor revalidates state under its own exclusive lock immediately before removal.

## R-05: Treating `InstallResolvedCandidate` as equivalent to normal executor execution

It is not. It is a thin adapter invocation.

Mitigation: upgrade must call the new exact-candidate executor entry point that traverses the ordinary attempt lifecycle.

## R-06: Using `plan.Reconcile` as a mutation API

Mitigation: keep decision pure; executor interprets `TransitionUpgrade` and performs effects.

## R-07: Crash after removal but before install WAL

Mitigation: replacement WAL must record `removed` before beginning install, giving recovery an unambiguous missing-old state.

## R-08: Crash during install

Mitigation: persist install-in-flight before adapter mutation; exact observation may finalize if desired identity is satisfied; otherwise fail closed.

## R-09: Profile update writes incomplete v2

Mitigation: retain existing rule that profile update over missing/v1 lock cannot promote to v2; profile update over v2 must retain valid omitted entries and verify complete current coverage.

## R-10: Legacy payload accidentally becomes authoritative again

Mitigation: explicit version branching and call-site audits in P5/P11.

---

# 23. “Do not implement this” examples for delegated agents

These are common attractive wrong turns.

## Wrong: new global ReconciliationService

```text
app -> ReconciliationService -> ResolutionService -> LockService -> Executor
```

Why wrong: it wraps existing boundaries without eliminating duplication.

Correct direction:

```text
app -> Executor
       -> plan pure decisions
       -> AdapterV2
state/lock remain persistence/validation components
```

## Wrong: update calls both resolvers and compares them

```text
legacy = ResolveLegacyV1
modern = Executor.ResolveLockCandidate
if equal: save
```

Why wrong: preserves two authorities forever.

Correct: modern result is authority for v2; legacy resolver is v1-only.

## Wrong: put Remove in `plan.Reconcile`

Why wrong: destroys purity and makes status/explain capable of host effects by API design.

Correct: `plan.Reconcile` returns a decision; executor owns replacement effects.

## Wrong: make upgrade call `ex.Execute` with the whole unfiltered schema and hope it picks the right candidate

Why wrong: can fallback to another candidate or mutate unrelated tools.

Correct: exact-candidate executor entry point seeded with the already-resolved target.

## Wrong: remove old tool, then begin a journal

Why wrong: crash window loses durable knowledge that the old installation disappeared.

Correct: WAL first, destructive mutation second.

## Wrong: refresh legacy ToolPins while writing every v2 lock

Why wrong: keeps remote legacy resolution as a second resolver.

Correct: carry legacy payload if needed, but canonical v2 identity comes from resolved plans.

## Wrong: delete `MethodsHash`/`SourceHash` because “LockDocument replaces legacy”

Why wrong: current universal schema does not cleanly replace every requested-intent invariant they enforce.

Correct: keep them as pure validation metadata for this migration.

---

# 24. Definition of Done

The migration is complete only when all statements below are true.

## Resolution

- [ ] `ResolvedInstallPlan` remains the canonical concrete candidate identity.
- [ ] v2 install resolves through executor/AdapterV2 only.
- [ ] v2 update resolves through executor/AdapterV2 only.
- [ ] v2 status resolves/observes through executor with LockDocument.
- [ ] v2 upgrade consumes LockDocument, not ToolPin, for target identity.
- [ ] no lower layer re-resolves a resolved target.

## Lock

- [ ] full-scope update can create v2 without running legacy remote resolution.
- [ ] existing v2 update can refresh v2 without running legacy remote resolution.
- [ ] profile v2 update retains valid omitted entries and verifies full closure.
- [ ] profile update over v1/missing remains safely bounded as v1 compatibility.
- [ ] v2 frozen validation does not require concrete ToolPins.
- [ ] v2 requested method/source drift remains protected.
- [ ] materialized integrity semantics remain tested.
- [ ] v1 lock remains readable.

## Status

- [ ] v2 status ignores conflicting legacy pin values.
- [ ] v2 status derives drift from canonical verification.
- [ ] v1 status compatibility remains.

## Upgrade

- [ ] upgrade app does not directly call adapter Remove.
- [ ] upgrade app does not call thin `InstallResolvedCandidate` as its replacement engine.
- [ ] exact tracked candidate is preserved.
- [ ] sources can be prepared before removal.
- [ ] prerequisites can be satisfied before removal.
- [ ] transition-specific hooks run through executor.
- [ ] removal support is checked before destructive transition.
- [ ] replacement WAL exists before removal.
- [ ] removal completion is persisted before install begins.
- [ ] install-in-flight is persisted before target mutation.
- [ ] exact desired target is verified before successful replacement finalization.
- [ ] crash recovery never guesses unknown state.
- [ ] state/resource ownership is finalized atomically with successful replacement.

## Maintainability

- [ ] legacy resolver has an explicitly v1 name/boundary.
- [ ] no new generic service layer was added.
- [ ] `internal/plan` remains pure.
- [ ] app commands are thinner than before.
- [ ] duplicated upgrade preflight restrictions are removed where executor now supplies equivalent semantics.
- [ ] dead helpers are deleted only after call-site audit.
- [ ] architecture/support docs match the code.

---

# 25. Final expected conceptual shape

After the plan is executed, the codebase should read approximately like this to a new maintainer:

```text
internal/plan
    Pure model:
        ResolvedInstallPlan
        LockDocument
        Observation / Verification
        Reconcile
        TransitionForVerification
        preparation/replacement state machines

internal/exec
    Operational engine:
        candidate selection
        AdapterV2 resolution
        exact observation
        reconciliation
        source/prerequisite preparation
        hooks
        install transition
        replacement/upgrade transition
        recovery coordination

internal/lock
    Persistence + compatibility:
        read/write lock envelope
        universal projection encoding
        requested-intent metadata
        legacy v1 resolver/application, explicitly quarantined

internal/state
    Durable machine state:
        ToolState
        owned resources
        preparation WAL
        replacement WAL

internal/app
    Command policy/projection:
        install: choose scope/options, invoke executor, render
        update: canonical read-only resolve -> build/save lock
        status: canonical verify -> render
        upgrade: choose confirmed targets -> invoke exact executor transition -> render
        why: canonical explanation -> render
```

The important simplification is not the number of types. It is the number of **authorities**:

```text
Mutable identity resolution authority: 1  -> Executor + AdapterV2
Desired-state decision authority:       1  -> plan.Reconcile / TransitionForVerification
Host mutation authority:                1  -> Executor
v2 persisted resolved identity:         1  -> LockDocument
legacy v1 resolution:                   isolated compatibility path only
```

That is the architectural endpoint this plan is trying to reach. Everything else is implementation mechanics.

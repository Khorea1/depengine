# Depengine — Post-Audit Remediation Implementation Plan

**Target:** `.worktrees/resolved-plan-lock-convergence`

**Baseline audited:** `6bb3223c` → current `resolved-plan-lock-convergence` worktree

**Purpose:** fix only the defects found in the post-implementation audit of `.dev/plans/ResolvedPlan-Lock-Convergence-Implementation-Plan.md` without reopening the completed lock/executor architecture migration.

---

## 0. Scope and execution rules

This is a **remediation plan**, not a second implementation of the original convergence plan.

Preserve the following already-correct architecture:

- v2 operational identity comes from `plan.LockDocument` / `ResolvedInstallPlan`;
- v1 `ToolPin` access remains compatibility-only;
- `upgrade` delegates destructive mutation to `internal/exec`;
- upgrade discovery occurs without holding the long-lived state lock;
- replacement is WAL-backed and fail-closed;
- sources, prerequisites, hooks, credentials, and verification stay in the normal executor pipeline;
- do not reintroduce app-layer `Remove` / `Install` mutation;
- do not add a new lockfile version merely to repair replacement recovery.

### Global implementation constraints

1. Prefer local changes over new abstraction layers.
2. Do not change `plan.CandidateIdentity` merely to fix replacement recovery. It is shared with the universal lock projection.
3. Every destructive replacement decision must be justified by durable identity or a fresh exact comparison.
4. Unknown/ambiguous recovery must fail closed. Never choose “the first matching candidate”.
5. Do not silently retain stale v2 projection data when the metadata required to prove it current is missing.
6. Do not preserve a legacy checksum unless artifact compatibility is actually provable.
7. Keep backward readability of replacement transactions written by the current implementation when possible. Missing newly-added discriminator fields may fall back only to an unambiguous lookup.
8. Run `gofmt` on every touched Go file.

### Recommended execution order

```text
P0  Characterization tests / baseline
 |
 v
P1  Stale-state destructive-boundary protection
 |
 +---------> P2  Legacy checksum safety
 |
 v
P3  Replacement WAL identity + resource ownership
 |
 v
P4  Replacement finalization / hook durability
 |
 v
P5  Secondary v2 regressions: SBOM + profile update + constructor
 |
 v
P6  Dead-code cleanup + full verification
```

P1 must land before any further replacement behavior is trusted.
P3 should land before P4 because P4 changes recovery state transitions and should build on the corrected persisted transaction shape.

---

# P0 — Freeze the audited failures as regression tests

## Goal

Before changing behavior, add focused tests demonstrating each blocker. These tests should initially fail for the reason described in the audit.

Do not start with broad integration tests. First create narrow tests that isolate the broken invariant.

## P0-T01 — Stale tracked state test

**Primary files**

- `internal/app/upgrade_phases_test.go`
- `internal/exec/resolved_candidate.go`
- `internal/exec/replacement.go`
- optionally a new focused `internal/exec/replacement_stale_state_test.go`

**Scenario**

1. Discovery records `ToolState A`.
2. Before executor replacement begins, persisted state is changed to `ToolState B`.
3. A and B have the same `MethodKind` so the existing kind-only check cannot save us.
4. Invoke the exact resolved upgrade candidate using the discovery snapshot A.
5. Assert:
   - replacement fails before `Adapter.Remove`;
   - replacement WAL is not created;
   - state B remains unchanged;
   - `InstallResolved` is not called.

Include at least one variant where only the tracked candidate label/config changed while the method kind stayed the same.

## P0-T02 — Legacy v1 checksum/artifact mismatch test

**Primary file**

- `internal/app/universal_lock_test.go`

Construct a v1 lock whose `MethodsHash` matches the current value but whose historical materialized checksum corresponds to another HTTP URL.

The important test property is:

```text
same kind + same label + same legacy MethodsHash
old URL != newly resolved URL
```

Assert that the old checksum is **not** attached to the new artifact.

Do not use artificial strings such as `same-intent` as the sole compatibility proof. The test must exercise the real metadata/hash semantics.

## P0-T03 — Duplicate same-kind recovery identity test

**Primary file**

- `internal/exec/replacement_recovery_test.go`

Schema:

```text
http.mirror-a
http.mirror-b
```

Both explicit and both kind `http`.

Persist a replacement transaction for `mirror-b`.

Assert recovery identifies `mirror-b` exactly instead of failing because two explicit `http` candidates exist.

Also add a compatibility test for an old transaction with no new label discriminator:

- unique kind => recovery remains possible;
- duplicate kind => recovery remains fail-closed.

## P0-T04 — Recovery resource-claim test without preparation WAL

**Primary file**

- `internal/exec/replacement_recovery_test.go`

Create a replacement where:

- source already exists;
- `probeCandidateSources` therefore produces a `ResourceUse`;
- no source needs creation;
- no preparation transaction is created;
- replacement reaches recovery finalization.

Assert the recovered final state still claims the source for the upgraded tool.

Add a prerequisite resource to a second case if test setup is reasonably small.

## P0-T05 — Post-upgrade hook crash boundary characterization

**Primary files**

- `internal/plan/replacement_test.go`
- `internal/exec/replacement_recovery_test.go`
- `internal/exec/executor_test.go` or a focused replacement lifecycle test

Freeze these desired properties:

1. recovery must not silently convert “install verified but after-upgrade lifecycle unresolved” into a completed replacement;
2. if recovery can prove the hook never started, it may safely continue to the hook;
3. if a crash may have occurred while the hook was running, recovery must not blindly execute it again;
4. successful hook completion eventually clears the replacement transaction;
5. failed hook completion leaves installed state/resource ownership coherent.

Do not implement the journal phase names yet. The tests should express behavior, not a guessed implementation.

### P0 acceptance

- Each audited blocker has a narrow regression test.
- Tests fail before the corresponding fix.
- No production behavior changes in P0.

---

# P1 — Prevent stale discovery state from authorizing destructive replacement

## Problem being fixed

Current flow:

- `internal/app/upgrade.go:208-293` discovers the tracked candidate and captures `upgradeOutdatedTool.ts`;
- `internal/app/upgrade.go:337-346` sends only schema/tool/method/resolved plan to the executor;
- `internal/exec/replacement.go:43-58` reloads current state but only verifies method kind;
- `BeginReplacement` compares `previous` to the state value that was just read, not to the discovery snapshot.

This means a concurrent state change between discovery and replacement can authorize removal of a different tracked installation as long as the method kind still matches.

## P1-T01 — Make expected previous state explicit in the executor API

**Files**

- `internal/exec/resolved_candidate.go:13+`
- `internal/exec/attempt.go:53+`
- `internal/app/upgrade.go:337-346`
- affected tests calling `ExecuteResolvedCandidate`

Preferred shape: make the API explicitly upgrade/replacement-specific rather than hiding an extra invariant inside a generic-looking method.

Suggested interface:

```go
func (ex *Executor) ExecuteResolvedUpgradeCandidate(
    ctx context.Context,
    schema *config.Schema,
    clan string,
    tool *config.Tool,
    method *config.MethodCandidate,
    resolved *plan.ResolvedInstallPlan,
    expectedPrevious state.ToolState,
) (ToolResult, error)
```

If retaining the old name is materially simpler, the critical requirement is still that the expected tracked state is mandatory for an upgrade replacement.

Do **not** make the expectation optional for the upgrade path.

Clone `expectedPrevious.Config` before storing it in attempt state so later mutation cannot alter the comparison target.

Extend `candidateResolutionSeed` with the expected prior state, e.g. conceptually:

```text
candidateResolutionSeed
  method
  resolved
  requireUpgrade
  expectedPreviousState
```

## P1-T02 — Compare current durable state to the discovery snapshot before WAL creation

**File**

- `internal/exec/replacement.go:29-75`

Immediately after loading locked state:

```text
currentPrevious := locked.State().Tools[tool]
expectedPrevious := attempt.expectedPrevious

if expected is missing:
    fail closed

if currentPrevious != expectedPrevious:
    fail stale-state error

ONLY THEN:
    resolve exact old method
    persist BeginReplacement
    persist removal boundary
    call Remove
```

Use the same semantic equality currently relied upon by `BeginReplacement`. `ToolState` contains a map, so plain `==` is unavailable; `reflect.DeepEqual` or an explicit equality helper is acceptable.

Error message should identify the tool and state that tracked state changed after upgrade discovery. Do not dump secret-bearing config values.

## P1-T03 — Stop synthesizing the old removal candidate from the desired candidate

**Current problematic code**

- `internal/exec/replacement.go:55-58`

Current behavior copies `*ac.method` and replaces only label/config from state. Other schema fields may therefore come from the wrong candidate.

Introduce one exact candidate lookup primitive based on:

```text
method kind
optional durable label
```

Recommended location:

- `internal/config/` as a small neutral helper operating only on `*config.Tool` and strings;
- or an `internal/exec` helper if extraction would unnecessarily widen scope.

Preferred contract:

```text
Find exact candidate(tool, kind, label)

if label is non-empty:
    exactly one candidate must match both kind and label
else:
    exactly one candidate of kind must exist

0 matches -> error
>1 matches -> ambiguity error
```

Then:

- app-side `findStateMethodCandidate` may delegate to this helper;
- replacement removal reconstructs the old method from the **expected tracked state**;
- recovery in P3 uses the same exact matching semantics.

Do not choose by ordinal unless the ordinal is itself durable and validated. It currently is not.

## P1-T04 — Keep `BeginReplacement` as a second defensive check

**File**

- `internal/state/replacement.go:30-75`

Do not remove the existing state equality check.

P1 should create two barriers:

```text
executor compares durable current state to discovery snapshot
        ↓
BeginReplacement compares the supplied previous state to still-locked current state
```

The second protects against accidental future call-site misuse while holding the state lock.

## P1-T05 — Add destructive-boundary assertions

Tests must assert operation order explicitly:

```text
fresh state check
BeginReplacement save
PlanReplacementRemoval save
Remove
```

For stale state:

```text
fresh state check -> fail
NO BeginReplacement
NO Remove
NO Install
```

### P1 acceptance

- A state mutation after discovery aborts replacement before any WAL/destructive mutation.
- Same-kind candidate changes cannot slip through.
- Old candidate removal is reconstructed from tracked identity, not from the new candidate object.
- Normal replacement still succeeds.

---

# P2 — Make legacy checksum carry-forward provably safe

## Problem being fixed

`internal/app/universal_lock.go:113-140` currently treats unchanged `MethodsHash` as proof that a legacy v1 HTTP checksum still belongs to the currently resolved artifact.

But `internal/lock/lock.go:243-251` hashes only method kind + label. URL/config changes therefore do not change the hash.

A v1 lock does not persist enough immutable artifact identity to prove that its checksum belongs to a newly resolved URL.

## Design decision

Do **not** expand this remediation into a new historical lock fingerprint protocol.

For existing v1 locks, if the previous lock does not contain the artifact URL/immutable identity, compatibility is unprovable. Therefore the safe behavior is:

> do not carry a materialized v1 artifact checksum into the v2 projection solely from `MethodsHash` equality.

The v2→v2 carry-forward path remains supported because it compares concrete artifact identity fields (`URL`, local path, checksum URL/format, signature data, etc.).

This intentionally prefers correctness over preserving an optimization from an under-specified legacy format.

## P2-T01 — Remove the unsafe v1 checksum carry-forward branch

**File**

- `internal/app/universal_lock.go:100-156`

Refactor `carryForwardArtifactIntegrity` so:

```text
previous v2:
    compare exact persisted artifact identity
    carry materialized checksum only on exact compatibility

previous v1:
    do not transfer artifact checksum unless a future explicit proof mechanism exists
```

Remove comments claiming `MethodsHash` represents the full requested method identity. It does not.

Do not change `computeMethodsHash` in this remediation merely to rescue v1 carry-forward. That hash is used for other compatibility checks and changing its semantics would invalidate/mutate broader lock behavior.

## P2-T02 — Preserve v2 exact-match carry-forward

Tests in `internal/app/universal_lock_test.go` must prove:

1. identical v2 artifact identity + materialized checksum => checksum carried;
2. URL changed => not carried;
3. checksum URL/format changed => not carried;
4. signature identity changed => not carried;
5. already-concrete new checksum => new checksum wins;
6. `sha256:auto` may receive the compatible prior materialized checksum.

## P2-T03 — Update v1 migration test expectations

Replace any test that treats same legacy method hash as sufficient proof.

Required regression:

```text
v1 checksum = sha256:OLD
legacy MethodsHash happens to match
new resolved HTTP URL differs
=> new projection must NOT contain sha256:OLD
```

Document this as a deliberate safety property, not an accidental loss of behavior.

### P2 acceptance

- No v1 checksum can become attached to a newly resolved artifact solely because kind/label hash matches.
- v2→v2 compatible checksum carry-forward still works.
- No network/download is introduced merely to recover the old checksum.

---

# P3 — Persist enough replacement identity and resource state for deterministic recovery

P3 fixes two independent blockers in the same WAL schema:

1. duplicate same-kind labeled candidates cannot be recovered exactly;
2. recovery finalization can lose `ResourceUse` claims when no preparation transaction exists.

---

## P3-A — Durable candidate discriminator

### P3-T01 — Add replacement-local candidate labels

**File**

- `internal/state/replacement.go:12-30`

Do not modify `plan.CandidateIdentity`.

Extend `ReplacementTransaction` with local, backward-compatible fields:

```go
PreviousCandidateLabel string `json:"previous_candidate_label,omitempty"`
CandidateLabel         string `json:"candidate_label,omitempty"`
```

Keep existing:

```go
PreviousCandidate plan.CandidateIdentity
Candidate         plan.CandidateIdentity
```

Reason:

- lock projection format remains untouched;
- old state JSON remains readable;
- new journals can resolve duplicate same-kind candidates exactly.

### P3-T02 — Populate both labels at transaction creation

**Files**

- `internal/exec/replacement.go`
- `internal/state/replacement.go`

Change `BeginReplacement` inputs so it receives the exact old and desired candidate labels rather than guessing later.

Conceptually:

```text
previous candidate:
    identity = kind + explicitness
    label    = exact old schema candidate label

desired candidate:
    identity = resolved.Candidate
    label    = exact desired schema candidate label
```

Even if both are identical today, persist them separately. That avoids baking the current same-candidate restriction into the transaction format.

### P3-T03 — Make recovery label-aware with legacy fallback

**File**

- `internal/exec/run.go:268-283`

Change:

```go
exactReplacementMethod(tool, identity)
```

to conceptually:

```go
exactReplacementMethod(tool, identity, label)
```

Rules:

```text
label present:
    require exact kind + explicitness + label
    0 => fail
    >1 => fail

label absent (journal written by current/older implementation):
    allow current kind + explicitness matching
    require exactly one match
    duplicate => fail closed with remediation message
```

Use:

- `tx.CandidateLabel` for desired method;
- `tx.PreviousCandidateLabel` for old method.

### P3-T04 — Validate label consistency

**File**

- `internal/state/replacement.go:257+` (`validateReplacementTransaction`)

Validation should reject malformed labels but not reject an empty label because:

- an unlabeled method is valid;
- old journals have no new fields.

Do not infer that empty label means old format. It may simply be an unlabeled candidate. Exact identity remains safe when kind+explicitness is unique.

---

## P3-B — Persist replacement resource uses

### P3-T05 — Store resource uses in the replacement transaction before removal

**File**

- `internal/state/replacement.go`

Add:

```go
ResourceUses []plan.ResourceUse `json:"resource_uses,omitempty"`
```

At `BeginReplacement`, persist a deep copy of `ac.resources`.

This must happen before `Remove` because recovery needs the intended post-replacement ownership even if the process dies after removal.

Validate the slice using the existing plan resource validation primitives. Duplicate resource identities must be rejected before destructive mutation.

### P3-T06 — Use persisted resource uses during recovered finalization

**File**

- `internal/exec/run.go:388-416`

Current behavior:

```text
release old dependent claims
FinalizeReplacementWithPreparation(release.Updated, ...)
```

Required behavior:

```text
release := ReleaseDependentResources(...)
owned  := ClaimResourceUses(release.Updated, toolName, tx.ResourceUses)
FinalizeReplacementWithPreparation(..., owned, ...)
```

If a preparation transaction also exists, its commit uses may be projected again inside finalization. `ClaimResourceUses` is idempotent for the same dependent/resource pair, but tests should prove there is no duplicated dependent/refcount corruption.

### P3-T07 — Backward handling for old WALs without `ResourceUses`

An old active transaction may deserialize with `ResourceUses == nil`.

Safe behavior:

- if recovery can reconstruct all uses from a preparation plan and that plan exists, continue;
- if no preparation transaction exists, do **not** invent resource claims;
- choose between:
  - fail closed with an explicit “legacy replacement WAL lacks resource ownership identity” error, or
  - only continue when the resolved plan is provably resource-free.

Recommended rule:

```text
old tx has no persisted ResourceUses
AND desired plan/current schema implies no source/prerequisite ownership
    => recovery may continue
otherwise
    => block recovery
```

This affects only in-flight transactions created by the buggy implementation; safety matters more than magical recovery.

### P3-T08 — Clone transaction slices/maps everywhere

**Files**

- `internal/state/replacement.go`
- state clone helpers/tests

Ensure `cloneReplacementTransaction` deep-copies:

- `Previous.Config`;
- `Desired` projection;
- `ResourceUses`;
- any newly added fields requiring copies.

### P3 acceptance

- Two explicit same-kind candidates recover deterministically when label was persisted.
- Old journals without labels recover only if identity is unambiguous.
- Recovered replacements restore source/prerequisite ownership exactly.
- No resource claim is silently lost because `PreparationKey == ""`.

---

# P4 — Repair replacement finalization and after-upgrade hook durability

## Problem being fixed

Current replacement flow leaves the transaction in `ReplacementInstalling` until after the after-upgrade hook. Recovery interprets “desired target is satisfied” during `ReplacementInstalling` as enough to finalize the transaction.

Therefore a crash after install verification but before the hook can silently skip the hook.

The fix must not blindly replay a hook if a crash may have occurred while it was already running.

## Required safety model

Persist enough phase information to distinguish:

```text
A. install not yet proven successful
B. install verified; post-upgrade hook has definitely not started
C. post-upgrade hook may be/running or may have been interrupted
D. post-upgrade hook returned success
```

Recovery rules:

- A: use existing install recovery rules;
- B: safe to proceed into the hook lifecycle;
- C: fail closed; do not guess or duplicate arbitrary hook side effects;
- D: safe to complete transaction finalization if not already closed.

## P4-T01 — Extend `ReplacementPhase`

**File**

- `internal/plan/replacement.go:5-118`

Recommended phases:

```go
ReplacementPlanned
ReplacementRemoving
ReplacementRemoved
ReplacementInstalling
ReplacementInstalled       // exact desired install verified; hook has not started
ReplacementPostHookRunning // write-ahead boundary before after-upgrade hook
```

A separate persisted “hook succeeded” phase is optional if success can immediately perform the final atomic state/WAL close.

Do not call a phase `committed` if the active transaction still exists; name phases by observable lifecycle boundaries.

## P4-T02 — Add journal transitions

Add pure transitions such as:

```text
RecordInstalled():
    Installing -> Installed

PlanPostHook():
    Installed -> PostHookRunning
```

All phase transitions must validate current phase and return a new journal rather than mutating silently.

Update table tests first.

## P4-T03 — Persist verified install before entering the hook

**Files**

- `internal/exec/replacement.go:104-151`
- `internal/state/replacement.go`

After `InstallResolved` and exact verification succeed:

```text
RecordReplacementInstalled(tool)
```

At this point recovery can prove the adapter mutation succeeded and the after-upgrade hook has not started.

## P4-T04 — Separate “commit installed state/resources” from “delete replacement WAL”

Current `FinalizeReplacementWithPreparation` atomically updates state/resources and deletes the replacement transaction.

For robust hook durability, add a state operation that can:

```text
commit desired ToolState
commit replacement ResourceUses
close preparation WAL if required
KEEP replacement transaction alive
advance it to Installed / post-hook-pending lifecycle
```

Then final transaction deletion occurs only after the hook path reaches a non-ambiguous terminal outcome.

Possible API shape:

```text
CommitReplacementInstall(...)
CompleteReplacement(...)
```

Do not overload one function with booleans like `keepJournal bool`. Separate names make the durability boundary reviewable.

## P4-T05 — Write ahead before running after-upgrade hook

**File**

- `internal/exec/attempt.go:555-639`

Replacement path should conceptually become:

```text
adapter install
verify exact target
persist ReplacementInstalled + installed state/resources
persist ReplacementPostHookRunning
run after-upgrade hook

hook success:
    set PostinstallDone according to result
    delete replacement WAL atomically

hook returns failure:
    installed state/resources remain committed
    PostinstallDone remains false
    close/resolve WAL in a deliberate terminal way
    return tool failure

crash while PostHookRunning:
    next recovery blocks instead of replaying an arbitrary hook
```

The normal non-replacement install ordering should remain unchanged.

## P4-T06 — Define no-hook behavior explicitly

If the resolved plan has no matching after-upgrade hook:

- `Installed` may directly complete the transaction without entering `PostHookRunning`;
- or it may enter/exit the phase through the common hook function if that remains clearer.

Prefer avoiding a persisted ambiguous running phase when there is no hook to run.

Add/derive a small pure helper that answers whether a lifecycle hook exists for:

```text
TransitionUpgrade + HookAfter
```

Do not run hooks merely to discover whether a hook exists.

## P4-T07 — Update recovery decision table

**File**

- `internal/plan/replacement.go`
- `internal/plan/replacement_test.go`

Minimum desired decisions:

```text
Installing + desired absent
    -> resume install

Installing + desired satisfied
    -> record/continue installed boundary, NOT terminal finalize

Installed + desired satisfied + old absent/drifted
    -> continue to post-hook lifecycle or complete directly if no hook

PostHookRunning
    -> blocked recovery
```

Unknown/broken host observations remain blocked.

## P4-T08 — Recovery execution for `ReplacementInstalled`

**File**

- `internal/exec/run.go:150-416`

Recovery must be able to:

1. reconstruct exact method using P3 labels;
2. reconstruct state/resource ownership using P3 resource uses;
3. persist installed state if not already persisted;
4. determine whether an after-upgrade hook exists;
5. when hook definitely never started:
   - write the hook-running boundary;
   - execute the hook;
   - complete/clear transaction;
6. when hook may already have started:
   - stop with a clear recovery error.

Never call `InstallResolved` again once exact desired state is already satisfied.

## P4-T09 — Hook failure semantics test

Assert:

- tool remains tracked at the new exact version;
- replacement resource claims remain attached;
- old claims are released;
- result is failed because lifecycle requirement failed;
- no stale preparation journal remains;
- replacement WAL is in the explicitly chosen terminal/cleared state, not an accidental `Installing` zombie.

## P4-T10 — Crash matrix

Add a compact table/integration-style test for crash points:

```text
before Remove
between Remove and RecordRemoved
between RecordRemoved and Install
inside Install / before verification
between verification and RecordInstalled
between RecordInstalled and hook-running boundary
while hook-running phase persisted
between hook success and final WAL deletion
```

Expected result for every row must be one of:

```text
safe automatic continuation
safe terminal finalization
explicit blocked recovery
```

Never “guess and continue”.

### P4 acceptance

- Recovery cannot silently skip an after-upgrade hook.
- Recovery never automatically duplicates a hook whose execution may already have started.
- Verified target installation is never repeated merely to advance hook state.
- State/resources remain coherent on hook failure.

---

# P5 — Fix smaller v2 regressions and constructor drift

These should not be mixed into the high-risk WAL commits unless necessary.

---

## P5-A — SBOM version fallback must understand v2

### P5-T01 — Branch fallback by lock version

**File**

- `internal/app/sbom.go:41-55`

Current code only calls `legacyV1PinFor`.

Required behavior:

```text
for state tool with empty Version:
    if lock v2:
        read ProjectionDocument once
        find entry for tool
        use entry.Identity.Version when compatible/available
    else v1:
        use legacyV1PinFor as today
```

Decode the v2 projection once outside the per-tool loop.

If the projection is malformed, preserve existing SBOM best-effort policy: do not fabricate a version. Whether to surface the lock error should follow current command behavior rather than introducing a new fatal path casually.

### P5-T02 — Add regression tests

Cases:

1. state version present => state wins;
2. state version empty + v2 projection concrete version => projection version used;
3. state version empty + v1 legacy pin => legacy version used;
4. neither provides version => `0.0.0` fallback remains;
5. v2 legacy `Tools` payload disagrees with projection => projection wins.

---

## P5-B — Missing v2 intent metadata must cause profile refresh

### P5-T03 — Make metadata absence conservative

**File**

- `internal/app/universal_lock.go:162-187`

Current code:

```go
previousMethods, exists := previous.MethodsHash[name]
if !exists {
    return false
}
```

For a retained/out-of-profile projected tool, missing metadata means the caller cannot prove the retained projection is current.

Change semantic contract to:

```text
previous == nil
    -> no prior drift to evaluate

previous projection contains tool but required MethodsHash is missing
    -> drift/refresh required

previous method hash differs
    -> drift

source identity added/removed/changed
    -> drift
```

If the tool is not represented in the previous projection at all, let the caller's coverage logic decide whether it must be newly resolved; do not conflate “missing tool” and “missing metadata for a retained tool”.

### P5-T04 — Parse source-hash keys canonically

Replace raw:

```go
strings.HasPrefix(key, name+"/")
```

with canonical key parsing using the existing `lockCandidateToolName` logic or move the parser to a neutral shared package if required.

This matters for tool names containing `/` and avoids prefix collisions.

Do not create a second subtly different key parser.

### P5-T05 — Profile update tests

Add cases:

```text
retained v2 tool + MethodsHash missing
    => tool must be re-resolved

retained v2 tool + source hash missing/changed
    => tool must be re-resolved

unrelated tool with similar prefix
    => must not trigger refresh

tool name containing '/'
    => source identity belongs to correct tool
```

---

## P5-C — Use the universal lock constructor consistently

### P5-T06 — Replace manual v2 construction in update

**File**

- `internal/app/update.go:125-136`

Replace:

```go
newLock = &lock.Lock{...}
newLock.SetProjection(document)
```

with:

```go
newLock, err = lock.NewUniversal(document, methodsHash, sourceHash, legacyTools)
```

Preserve the existing legacy `Tools` compatibility payload exactly.

This is cleanup/invariant centralization, not behavior change.

### P5-T07 — Constructor regression

Existing `internal/lock/universal_constructor_test.go` should remain sufficient for most invariants. Add an app-level assertion only if update behavior differs after refactor.

### P5 acceptance

- SBOM reads concrete v2 version instead of dropping to `0.0.0`.
- Missing profile metadata refreshes rather than silently retaining stale projection.
- v2 update construction has one canonical constructor path.

---

# P6 — Cleanup and final verification

## P6-T01 — Remove dead upgrade helpers

**File**

- `internal/app/upgrade.go:580+`
- `internal/app/upgrade_test.go`

Audit and remove helpers that no longer participate in production flow, especially:

- `upgradedToolState`
- `findMethodCandidate`

Move/delete tests that only exist to exercise dead helpers.

Do not remove `findTrackedMethodCandidate` or exact state-candidate matching logic if still used by discovery.

## P6-T02 — `gofmt`

Run `gofmt` on every modified Go file.

At minimum verify `internal/state/replacement.go`, which was already divergent during the audit.

## P6-T03 — Static call-site audit

Use ripgrep to prove:

```text
legacyV1PinFor
legacyV1PinForCandidate
legacyV1PinForToolState
```

are called only from explicit v1 compatibility branches.

Prove no v2 operational flow reads `Lock.Tools` to override a universal projection.

Audit:

```text
ExecuteResolvedCandidate / ExecuteResolvedUpgradeCandidate
BeginReplacement
Finalize/CommitReplacement*
exactReplacementMethod
ReplacementRecoveryFor
```

for all callers.

## P6-T04 — Replacement test matrix

Minimum matrix:

### State freshness

- unchanged expected state -> success;
- version changed after discovery -> fail before removal;
- label changed after discovery -> fail before removal;
- config changed after discovery -> fail before removal;
- tool removed from state after discovery -> fail before removal.

### Candidate identity

- one unlabeled kind -> recover;
- two same-kind labeled candidates + new WAL label -> recover exact one;
- two same-kind candidates + legacy WAL no label -> block;
- persisted label removed from current schema -> block;
- persisted explicitness no longer matches -> block.

### Resource ownership

- no resources;
- pre-existing source, no preparation WAL;
- newly added source, coordinated preparation WAL;
- prerequisite resource;
- multiple resource uses;
- recovery finalization preserves ownership and dependents.

### Hooks

- no hooks;
- before-upgrade success;
- before-upgrade failure => no remove;
- after-upgrade success;
- after-upgrade returned failure;
- crash before post-hook boundary;
- crash after post-hook-running boundary => blocked;
- desired already satisfied during recovery => no duplicate install.

### WAL/state errors

- BeginReplacement save failure => no remove;
- removal boundary save failure => no remove;
- Remove failure => install never called;
- removed-state save failure => install never called;
- install-boundary save failure => install never called;
- final state/resource save failure => WAL sufficient for recovery.

## P6-T05 — Lock/update regression matrix

- v1 frozen/apply remains readable;
- v1→v2 migration does not attach unverifiable legacy checksum;
- v2→v2 exact artifact checksum carry-forward works;
- stale legacy `ToolPin` cannot override v2 projection;
- partial profile update retains fresh unchanged tools;
- partial profile update refreshes retained tools when metadata is absent/drifted;
- `NewUniversal` is used by v2 update construction.

## P6-T06 — SBOM regression matrix

- state concrete version;
- v2 projection fallback;
- v1 pin fallback;
- unknown fallback `0.0.0`;
- stale v2 legacy pin disagreement.

## P6-T07 — Full tests and linters

When dependencies are available:

```bash
gofmt -w <touched files>
go test ./internal/plan
go test ./internal/state
go test ./internal/lock
go test ./internal/exec
go test ./internal/app
go test ./tests/integration/...
go test ./...
```

Then run the repository's configured linters/validators from the supplied tool bundle.

Do not classify missing module source/cache in the sandbox as a code failure. Record the environmental limitation separately.

## P6-T08 — Final architecture acceptance checklist

All must be true:

- [ ] Upgrade discovery snapshot is revalidated immediately before destructive replacement.
- [ ] Replacement cannot remove a same-kind but different tracked candidate.
- [ ] v1 checksum is never reused without provable artifact compatibility.
- [ ] Replacement WAL carries an exact candidate discriminator when available.
- [ ] Legacy ambiguous WAL fails closed.
- [ ] Replacement WAL persists post-replacement resource uses before removal.
- [ ] Recovery restores source/prerequisite claims even without preparation WAL.
- [ ] Recovery distinguishes install-in-flight from verified-install/hook lifecycle.
- [ ] Recovery never silently skips a required after-upgrade hook.
- [ ] Recovery never blindly replays a hook that may already have started.
- [ ] v2 SBOM fallback reads universal projection.
- [ ] Missing v2 profile metadata forces refresh rather than stale retention.
- [ ] v2 lock construction goes through `lock.NewUniversal`.
- [ ] Dead direct-upgrade-era helpers are removed.
- [ ] All touched files are gofmt-clean.

---

# Suggested commit boundaries

Keep commits independently reviewable. Recommended sequence:

```text
1. test: characterize post-convergence replacement regressions
2. fix(exec): reject stale tracked state before replacement
3. fix(lock): stop unverifiable v1 checksum carry-forward
4. fix(state): persist replacement candidate labels and resource uses
5. fix(exec): recover replacement identity and resource ownership exactly
6. fix(exec): make upgrade hook lifecycle crash-safe and fail-closed
7. fix(app): restore v2 SBOM and profile-update semantics
8. refactor(lock): use NewUniversal for v2 update construction
9. chore: remove dead upgrade helpers and format
10. test: full post-audit regression matrix
```

Avoid combining P1/P3/P4 into one giant commit. Those are three different invariants: stale-state authorization, persisted recovery identity, and lifecycle durability. Reviewers should be able to revert or reason about each independently.

---

# Delegation map for lower-capability agents

If parallelizing, use these ownership boundaries:

| Agent | Scope | May modify | Must not modify |
|---|---|---|---|
| A | P1 stale-state boundary | `internal/app/upgrade*`, `internal/exec/resolved_candidate.go`, `replacement.go`, exact candidate helper | replacement journal phases |
| B | P2 checksum safety | `internal/app/universal_lock*` | executor/state |
| C | P3 WAL identity/resources | `internal/state/replacement*`, recovery exact candidate/resource logic | hook lifecycle semantics |
| D | P4 hook durability | `internal/plan/replacement*`, `internal/exec/attempt.go`, `run.go`, state finalization APIs | lock/update/SBOM |
| E | P5 regressions | `internal/app/sbom*`, `update*`, `universal_lock*`, `internal/lock/universal*` | replacement WAL |
| F | P6 cleanup/verification | dead helpers, tests, formatting | new architecture |

Do **not** run C and D against the same branch without rebasing/coordination: both legitimately touch `internal/state/replacement.go` and `internal/exec/run.go`.

---

# Explicit non-goals

Do not use this remediation as justification to:

- create lock format v3;
- redesign `ResolvedInstallPlan`;
- redesign source ownership globally;
- replace the state WAL subsystem;
- add cross-process transaction orchestration beyond the replacement invariant;
- change ordinary install hook semantics unrelated to replacement;
- make hooks “exactly once” in the distributed-systems sense;
- reintroduce long-lived global state locks around discovery/network work;
- refactor every candidate-selection helper in the repository;
- preserve unsafe legacy behavior merely because an old test expected it.

The objective is narrower: make the already-chosen architecture truthful at its destructive and recovery boundaries.

# Depengine — Final Post-Audit Remediation Implementation Plan

**Target snapshot:** `b161bd80` (`fix: close resolved lock replacement recovery gaps`)  
**Scope:** only the issues still open after the post-implementation audit.  
**Intended executor:** low-capability coding agent operating under higher-capability review.  
**Goal:** close the remaining replacement-recovery correctness gaps without reopening or redesigning the already-working resolved-plan/lock convergence architecture.

---

## 0. Executive summary

The broad implementation is now present and structurally sound. This plan does **not** repeat P1–P8, does **not** ask for another architectural rewrite, and does **not** authorize opportunistic cleanup.

Only two behavioral gaps remain:

1. Replacement recovery reconstructs the desired immutable target from the WAL, but does **not first prove that the current schema's operational intent is still compatible with that persisted target**. A same-kind/same-label candidate whose package/source/version intent changed can therefore pass candidate lookup and reach recovery mutation logic.
2. `ReplacementRetryRemoval` does not reproduce the credential/elevation execution contract used by normal replacement removal. In particular, removal-specific elevation and method-scoped credentials can be skipped during recovery.

Two stale comments should also be corrected after the behavioral work.

The desired final invariant is:

```text
replacement recovery
    |
    +-- find exact persisted candidate identity
    |
    +-- reconstruct CURRENT static operational intent
    |
    +-- prove current intent is compatible with persisted immutable target
    |      |
    |      +-- mismatch -> FAIL CLOSED, no Remove, no Install, WAL retained
    |
    +-- observe old + desired exact identities
    |
    +-- choose recovery action
    |
    +-- if removal is retried:
           resolve removal credentials
           apply secret-env omission
           establish removal elevation if required
           remove exact tracked old candidate
```

Do not alter the replacement state machine unless a test demonstrates that it is necessary for one of these two gaps.

---

# 1. Guardrails and non-goals

The implementation agent must follow these constraints.

## 1.1 Do not redesign already-correct architecture

Do **not**:

- introduce a second replacement transaction type;
- add another WAL;
- change `ReplacementPhase` values;
- change recovery action selection in `internal/plan/replacement.go` unless an existing invariant cannot otherwise be satisfied;
- reintroduce legacy lock authority into v2;
- re-resolve mutable package/source/version identity during recovery;
- move replacement ownership back into `internal/app`;
- modify update/status/upgrade convergence logic unrelated to the two findings;
- make broad changes to adapters;
- change lock format or state format;
- add new persisted secret/credential fields;
- persist current schema operational state in the replacement WAL merely to avoid reconstruction;
- opportunistically refactor unrelated removal flows (`remove`, `undo`) unless required to keep compilation/tests correct.

## 1.2 Recovery must remain fail-closed

If current schema intent cannot be reconstructed or cannot be proven compatible with the persisted immutable target:

```text
return an error
retain replacement WAL
perform no destructive mutation
do not silently select another candidate
do not re-resolve a replacement target
```

## 1.3 Immutable identity and operational semantics have different sources

During recovery:

- immutable identity comes from `ReplacementTransaction.Desired`;
- operational semantics come from the **current schema candidate**;
- `plan.LockDocument.PinnedPlanFor` is the existing boundary intended to combine those two safely.

Do not create a hand-written field-by-field compatibility checker if `EntryForPlan` / `PinnedPlanFor` already encode the relevant lock compatibility rules.

## 1.4 Preserve mutation ordering

Normal replacement currently resolves removal credentials **before** creating/advancing the destructive WAL boundary.

Do not accidentally move credential lookup after `BeginReplacement`/`PlanReplacementRemoval` in the normal path.

For recovery, the WAL already exists. Resolve everything that can fail without mutation before invoking `Remove`.

---

# 2. Relevant existing code

These references are based on snapshot `b161bd80`.

## 2.1 Recovery entry point

`internal/exec/run.go`, approximately lines **150–203**

Current sequence:

```text
load ReplacementTransaction
find current tool
find exact desired candidate
find exact previous candidate
reconstruct desired only from tx.Desired
construct previous plan
observe old/desired
select ReplacementRecovery action
```

The current problematic line is approximately:

```go
desiredPlan, err := resolvedReplacementTarget(tx.Desired)
```

That reconstructs immutable state, but does not prove current schema intent still describes the same target.

## 2.2 Retry removal path

`internal/exec/run.go`, approximately lines **205–237**

Current `ReplacementRetryRemoval` path:

```go
locked.PlanReplacementRemoval(...)
context.WithTimeout(...)
oldAdapter.Remove(...)
```

It bypasses the credential/elevation discipline used by normal replacement.

## 2.3 Normal replacement removal

`internal/exec/replacement.go`, approximately lines **98–139**

This path already demonstrates the intended contract:

```text
executionCredentialContext
BeginReplacement
PlanReplacementRemoval
timeout
WithOmittedEnv
RemovalElevationRequirer
StartElevationSession
adapter.Remove
stop elevation
cancel context
```

Preserve its important ordering.

## 2.4 Static operational intent

`internal/exec/capability.go`, approximately lines **31–76**

Relevant APIs:

```go
candidatePlanIntent(...)
candidatePlanIntentErr(...)
```

For recovery compatibility checking, prefer `candidatePlanIntentErr` because it returns a typed error and does not convert failure into report text.

## 2.5 Existing lock compatibility boundary

`internal/plan/lock.go`, approximately lines **872–913**

```go
func (d LockDocument) EntryForPlan(intent ResolvedInstallPlan) ...
```

It validates:

- exact candidate identity;
- requested version mode/intent;
- already-known immutable identity fields;
- relevant local artifact integrity information.

`internal/plan/lock.go`, approximately lines **1103–1145**

```go
func (d LockDocument) PinnedPlanFor(intent ResolvedInstallPlan) ...
```

It:

- validates current intent against the immutable lock;
- hydrates persisted immutable identity;
- preserves current operational semantics;
- preserves current in-memory secret references rather than loading them from persisted lock data.

This is the preferred primitive for the recovery gate.

## 2.6 Existing requirements in the original plan

`ResolvedPlan-Lock-Convergence-Implementation-Plan.md`

### P7-T10

Approximately lines **1660–1667**:

```text
reconstruct current operational intent
require it to match persisted desired immutable identity
if schema drift prevents reconstruction, fail closed
```

### P8-T05

Approximately lines **1792–1796**:

```text
Removal and install may have different credential requirements.
Reuse existing execution credential context logic.
```

---

# 3. Work Package A — Gate recovery on current schema intent

## Objective

Before recovery can observe/select/mutate replacement state, prove that the current candidate's static operational intent is still compatible with `tx.Desired`.

This closes the following concrete failure class:

```text
WAL:
  candidate kind = npm
  candidate label = stable
  desired package = package-a@2

current schema after crash:
  candidate kind = npm
  candidate label = stable
  configured package = package-b
```

`exactReplacementMethod` only proves candidate discriminator identity. It must **not** be treated as proof that package/source/requested-version intent stayed compatible.

---

## A-T01 — Add a dedicated recovery-plan reconstruction helper

**Primary file:** `internal/exec/run.go`

Add one small unexported helper close to the existing recovery helpers.

Suggested responsibility:

```go
func replacementRecoveryDesiredPlan(
    tool *config.Tool,
    method *config.MethodCandidate,
    persisted plan.LockProjection,
) (*plan.ResolvedInstallPlan, error)
```

The exact name may differ, but the helper must have one responsibility:

> combine current static operational intent with the persisted immutable replacement target, failing closed on schema drift.

Do not make this helper perform host probes, adapter resolution, install, removal, or state writes.

---

## A-T02 — Reconstruct the current static intent

Inside the helper:

```text
intent, err := candidatePlanIntentErr(tool, method)
```

Required behavior:

- if the planner returns an error: return a wrapped recovery error;
- if the planner cannot produce a usable intent: fail closed;
- do not call `ResolvePlan`;
- do not invoke the network;
- do not observe the host;
- do not copy immutable identity blindly out of the WAL before validating the current intent.

Suggested error context:

```text
reconstruct replacement recovery intent for "<tool>"
```

The exact wording is not important. The failure class and absence of mutation are important.

---

## A-T03 — Build a single-entry lock document from `tx.Desired`

Use the existing lock abstraction rather than writing new comparison logic.

Conceptual pseudocode:

```go
doc := plan.LockDocument{
    Version: plan.CurrentLockVersion,
    Entries: []plan.LockProjection{persisted.Clone()},
}
```

Then:

```go
pinned, err := doc.PinnedPlanFor(*intent)
```

Why this is preferred:

- `EntryForPlan` already checks candidate identity;
- it already checks requested-version semantics;
- it already checks statically-known immutable fields;
- `PinnedPlanFor` hydrates immutable target data without re-resolution;
- operational hooks/prerequisites/current config remain sourced from current schema intent.

Do **not** call `VerifyResolvedPlanAgainstLock` on the unresolved current intent. That API compares fully concrete projections and is a different boundary.

---

## A-T04 — Treat lock mismatch as a recovery block, not a fallback opportunity

If `PinnedPlanFor` returns `plan.ErrLockMismatch` or any wrapped mismatch:

```text
return error
retain WAL
do not Remove
do not InstallResolved
do not select another candidate
```

Do not catch mismatch and fall back to `resolvedReplacementTarget(tx.Desired)`.

The persisted target is authoritative for identity, but the current schema must still be able to provide compatible operational semantics.

---

## A-T05 — Move the compatibility gate before recovery observation/action selection

In `recoverReplacementTransaction`, replace:

```go
desiredPlan, err := resolvedReplacementTarget(tx.Desired)
```

with the new current-intent + persisted-lock helper.

The gate must complete before:

```go
observeRecoveryCandidate(...)
ReplacementRecovery(...)
```

Preferred ordering:

```text
1. load tx
2. locate current tool
3. locate exact desired candidate
4. locate exact previous candidate
5. reconstruct + pin compatible current desired intent
6. reconstruct old tracked plan
7. observe old/desired
8. select recovery action
9. mutate only if action permits
```

This makes the schema-compatibility proof part of the authorization to continue recovery.

---

## A-T06 — Use the pinned recovery plan as the single desired plan for the remainder of recovery

Once the helper returns `desiredPlan`, pass that same plan through:

- desired observation;
- resumed installation;
- install verification;
- recovered state commit;
- lifecycle-hook continuation.

Avoid reconstructing a second operational plan later.

Currently `continueRecoveredReplacement` approximately lines **453–465** runs:

```go
hookPlan, mismatch := candidatePlanIntent(tool, method)
```

That creates a second reconstruction point.

Refactor it so the already-validated `desired` plan is the hook plan.

Conceptually:

```go
hooks, err := desired.HookSchedule(plan.TransitionUpgrade, plan.HookAfter)
...
postRan, hookErr := ex.runLifecycleHooks(
    postCtx,
    tool.Name,
    desired,
    plan.TransitionUpgrade,
    plan.HookAfter,
)
```

Benefits:

- one schema snapshot/intent reconstruction governs the recovery operation;
- immutable identity is already pinned;
- current operational hooks are retained;
- there is no later divergence between “plan validated for recovery” and “plan used for recovered hook”.

Do not mutate `desired` after pinning.

---

## A-T07 — Remove dead immutable-only reconstruction code if it becomes unused

`resolvedReplacementTarget` currently exists around `internal/exec/run.go:317+`.

After A-T01–A-T06, search:

```bash
rg 'resolvedReplacementTarget\(' internal/exec
```

If there are no callers:

- delete the helper;
- do not keep it as a fallback path;
- let compilation prove no dependency remains.

If it still has a legitimate caller, keep it, but document why that caller does not require the current-intent compatibility gate.

---

# 4. Work Package B — Make recovery removal use the same execution contract as normal replacement

## Objective

Ensure `ReplacementRetryRemoval` performs the same execution-scoped credential and elevation handling as normal replacement removal.

Do not change replacement phase semantics.

---

## B-T01 — Extract only the shared mutation mechanics

**Primary file:** preferably `internal/exec/replacement.go` or another existing exec removal helper file.

Create a small private helper for the actual replacement removal invocation.

Recommended shape:

```go
func (ex *Executor) runReplacementRemoval(
    ctx context.Context,
    runner run.Runner,
    tool *config.Tool,
    method *config.MethodCandidate,
    adapter AdapterV2,
) error
```

Important contract:

> `ctx` is already the method-scoped credential context.

The helper should own:

1. the two-minute removal timeout;
2. omission of secret environment names;
3. `RemovalElevationRequirer`;
4. `StartElevationSession`;
5. guaranteed elevation-session cleanup;
6. `adapter.Remove`.

Conceptual pseudocode:

```text
removeCtx = timeout(ctx, 2 minutes)
removeCtx = omit method secret env names

if adapter requires removal elevation:
    if runner supports ElevationSession:
        start elevation
        if start fails:
            return error
        defer stop elevation

return adapter.Remove(removeCtx, runner, tool, method)
```

Use `defer` where practical so every return path stops the elevation session and cancels the timeout.

---

## B-T02 — Preserve normal replacement credential ordering

In `internal/exec/replacement.go` around lines **98–139**:

Keep:

```go
removeCredentials, err := ex.executionCredentialContext(ac.toolCtx, oldMethod)
```

**before**:

```text
BeginReplacement
PlanReplacementRemoval
```

Then replace the duplicated timeout/env/elevation/remove block with the new helper.

Desired sequence remains:

```text
resolve removal credentials
persist replacement intent
persist removal boundary
run shared removal helper
record removed state
```

This is important. Do not “simplify” by moving credential resolution into a helper that is only called after `PlanReplacementRemoval`, because that would subtly change normal-path WAL semantics.

---

## B-T03 — Resolve recovery removal credentials before retrying mutation

In `internal/exec/run.go`, `ReplacementRetryRemoval` branch:

Before `locked.PlanReplacementRemoval` and before `Remove`:

```go
removeMethod := methodForResolvedTarget(oldMethod, oldPlan)
removeCredentials, err := ex.executionCredentialContext(ctx, removeMethod)
```

Or resolve credentials from `oldMethod` if the implementation keeps projection separate, provided the same secret references are preserved.

Required failure behavior:

```text
credential failure
    -> return error
    -> no Remove call
    -> WAL retained
```

Prefer performing all non-mutating preparation that can fail before advancing/reaffirming the removal boundary.

---

## B-T04 — Invoke the shared replacement removal helper from recovery

Replace the current direct call:

```go
oldAdapter.Remove(...)
```

with the same shared helper used by the normal replacement path.

The recovery path must therefore inherit:

- timeout;
- secret-env omission;
- removal-specific elevation;
- elevation cleanup.

Do not duplicate those four mechanisms again inside `run.go`.

---

## B-T05 — Preserve exact tracked target projection

Recovery must continue removing the **tracked previous target**, not the desired target and not a newly selected candidate.

Keep these inputs conceptually separate:

```text
oldMethod     = exact persisted previous candidate
oldPlan       = persisted previous concrete tracked identity
desiredMethod = exact persisted desired candidate
desiredPlan   = current intent pinned to persisted desired identity
```

The shared removal helper must receive the old candidate/method.

Do not pass `desiredPlan` to removal.

---

# 5. Work Package C — Required regression tests

All tests in this section are part of the implementation, not optional polish.

Primary file:

`internal/exec/replacement_recovery_test.go`

Reuse existing test scaffolding where practical. Do not create an elaborate new test framework.

---

## C-T01 — Schema identity drift blocks recovery before mutation

Add a test with a name similar to:

```go
TestReplacementRecoveryRejectsCurrentIntentDriftBeforeMutation
```

Scenario:

```text
persisted replacement:
  tool = demo
  kind = npm
  candidate label = stable
  desired package = package-a
  desired version = 2.0.0

current schema after crash:
  tool = demo
  kind = npm
  candidate label = stable
  package = package-b
```

The discriminator intentionally remains the same.

Put the WAL into a phase/action that could otherwise retry removal, ideally:

```text
ReplacementRemoving
old target observed present
desired target observed absent
```

Assertions:

```text
recoverAndRecord returns error
adapter.Remove calls == 0
adapter.InstallResolved calls == 0
replacement WAL still exists
tracked old ToolState still exists and is unchanged
```

Prefer also asserting that the error is or wraps a lock-mismatch/recovery-intent mismatch class without depending on a fragile full string.

This test specifically proves that `exactReplacementMethod` is not the only recovery compatibility check.

---

## C-T02 — Matching current intent still recovers normally

Add or extend a positive counterpart:

```go
TestReplacementRecoveryPinsPersistedIdentityIntoCompatibleCurrentIntent
```

Scenario:

- current schema candidate still targets the same package/requested semantics;
- WAL desired identity is concrete;
- recovery can proceed.

Assertions:

- recovery is not rejected by the new gate;
- the persisted concrete desired identity remains authoritative;
- recovery does not call a mutable resolver to choose a newer target.

If an existing test already proves all three properties after the refactor, document/reuse it instead of adding a redundant test.

---

## C-T03 — Current operational hooks survive pinning

Because A-T06 removes the second `candidatePlanIntent` reconstruction, ensure recovered hooks still work.

The existing:

```go
TestReplacementRecoveryRunsHookWithoutReinstallAfterVerifiedInstall
```

should remain green.

Strengthen it only if necessary to prove that:

```text
immutable identity = persisted tx.Desired
hook schedule       = current compatible schema intent
```

Do not change hook replay policy.

`ReplacementPostHookRunning` must still fail closed without replay.

---

## C-T04 — Retry removal starts and stops elevation session

Add a recovery-specific regression test similar to:

```go
TestReplacementRecoveryRetryRemovalUsesRemovalElevation
```

Use a test adapter implementing:

```go
RemovalElevationRequirer
```

and a runner implementing:

```go
run.ElevationSession
```

Persist a replacement transaction that resolves to `ReplacementRetryRemoval`.

Assertions:

```text
Remove executed while elevation session active
elevation session stopped afterward
Remove called exactly once
```

The existing `TestRemoveResolvedCandidateStartsElevationSession` is **not sufficient**, because the bug is specifically in replacement recovery.

---

## C-T05 — Retry removal resolves method-scoped credentials

Add a test similar to:

```go
TestReplacementRecoveryRetryRemovalUsesMethodScopedCredentials
```

Use a method kind with existing execution credential support, preferably `git`, because tests can inspect:

```go
GitCredential(ctx)
```

Suggested setup:

```text
current old candidate contains SecretRef
executor receives a small fake SecretResolver
persisted WAL requires retry removal
adapter.Remove inspects credential from context
```

Assertions:

```text
secret resolver invoked
Remove receives expected ephemeral credential through context
credential is not written to replacement WAL/state
Remove called exactly once
```

Also add the failure variant if cheap:

```text
secret resolver returns error
recoverAndRecord returns error
Remove calls == 0
WAL remains
```

Do not assert the secret value appears in error text. It must not.

---

## C-T06 — Keep existing crash-boundary tests green

At minimum re-run:

- `replacement_recovery_test.go`
- `replacement_save_boundaries_test.go`
- `replacement_stale_state_test.go`
- `remove_resolved_test.go`
- lock tests around `EntryForPlan` / `PinnedPlanFor`

Do not weaken any existing test merely to accommodate the new implementation.

---

# 6. Work Package D — Documentation/comment cleanup

Perform only after Work Packages A–C are passing.

## D-T01 — Fix stale `ResourceUse` comment

`internal/plan/preparation.go`, approximately lines **378–382**

Current text says `ResourceUse` is intentionally not persisted as transaction state.

That is no longer universally true because replacement transactions persist:

```go
ReplacementTransaction.ResourceUses
```

Rewrite the comment to describe the type rather than make a false persistence claim.

Suggested semantic wording:

```text
ResourceUse records that a committed or in-flight operation uses one shared
host resource. Created distinguishes depengine-created/re-created resources
from externally existing resources. Durable owners may persist these
observations when required for crash-safe commit/recovery.
```

Do not change the type.

---

## D-T02 — Fix stale upgrade state-lock comment

`internal/app/upgrade.go`, approximately lines **131–135**

The comment currently claims real upgrades hold the exclusive state lock for the whole read-modify-write transaction.

That is no longer accurate after mutation ownership moved to executor/replacement WAL logic.

Rewrite it to state only what `loadUpgradeState` actually guarantees:

- dry-run uses unlocked snapshot read;
- non-dry-run obtains a locked snapshot;
- caller owns and closes the returned lock;
- higher-level upgrade flow may release discovery lock before executor-owned mutation.

Do not change lock behavior as part of this documentation task.

---

# 7. Suggested implementation order

The low-capability implementation agent should execute in this exact order.

```text
1. A-T01..A-T05
   Add recovery intent compatibility gate.

2. Run focused recovery tests.
   Do not proceed if existing recovery tests regress.

3. A-T06..A-T07
   Reuse the pinned plan through recovered hook continuation
   and remove dead immutable-only helper if unused.

4. Run recovery + hook tests.

5. B-T01..B-T02
   Extract shared removal mutation mechanics and migrate normal replacement.
   Confirm behavior unchanged.

6. Run normal replacement tests.

7. B-T03..B-T05
   Migrate ReplacementRetryRemoval onto credential + elevation path.

8. Add C-T01..C-T05 regression coverage.

9. Run complete internal/exec focused suite.

10. D-T01..D-T02
    Correct comments only.

11. gofmt / vet / linter / broader tests.
```

Do not implement A and B simultaneously in one large unreviewed edit. They touch the same recovery function but protect different invariants.

---

# 8. Detailed acceptance criteria

The patch is complete only when every item below is true.

## Recovery intent

- [ ] Recovery locates the exact persisted candidate as before.
- [ ] Recovery reconstructs current static operational intent.
- [ ] Current intent is validated against `tx.Desired`.
- [ ] Same kind/label with changed package is rejected.
- [ ] Changed requested-version semantics are rejected through existing lock compatibility logic.
- [ ] No mutable resolver is invoked to choose a new target.
- [ ] A mismatch performs no Remove.
- [ ] A mismatch performs no InstallResolved.
- [ ] WAL remains after mismatch.
- [ ] The desired recovery plan uses persisted immutable identity.
- [ ] Current compatible hooks/prerequisites/operational semantics remain available.
- [ ] Recovered hook continuation uses the already-validated pinned plan rather than reconstructing another plan.

## Removal execution

- [ ] Normal replacement still resolves removal credentials before destructive WAL boundaries.
- [ ] Normal and recovery removal share timeout/elevation/env-omission mechanics.
- [ ] Recovery retry resolves method-scoped removal credentials.
- [ ] Recovery retry applies `RemovalElevationRequirer`.
- [ ] Elevation session is always stopped.
- [ ] Credential failure causes no Remove call.
- [ ] Elevation startup failure causes no Remove call.
- [ ] No credential value is persisted.
- [ ] Removal still targets the persisted previous candidate.

## State machine

- [ ] No new replacement phase added.
- [ ] No recovery action semantics changed unnecessarily.
- [ ] `ReplacementPostHookRunning` remains non-replayable/fail-closed.
- [ ] Existing installed-target recovery still avoids reinstall.
- [ ] Existing resource ownership recovery remains unchanged.

## Cleanup

- [ ] `ResourceUse` comment matches reality.
- [ ] `loadUpgradeState` comment matches reality.
- [ ] no dead helper remains from superseded recovery reconstruction.

---

# 9. Validation commands

Use the repository's provided toolchain/cache when available.

Minimum focused sequence:

```bash
gofmt -w \
  internal/exec/run.go \
  internal/exec/replacement.go \
  internal/exec/replacement_recovery_test.go \
  internal/plan/preparation.go \
  internal/app/upgrade.go
```

Then:

```bash
go test ./internal/exec
go test ./internal/plan
go test ./internal/state
```

Focused regression selection, if useful while iterating:

```bash
go test ./internal/exec -run 'ReplacementRecovery|Replacement.*Atomic|RemoveResolvedCandidate'
go test ./internal/plan -run 'EntryForPlan|PinnedPlanFor|Replacement'
```

Static validation:

```bash
go vet ./internal/exec ./internal/plan ./internal/state ./internal/app
golangci-lint run ./internal/exec/... ./internal/plan/... ./internal/state/... ./internal/app/...
git diff --check
```

If module-source availability prevents the broad commands from loading packages, record that as an environment limitation. Do **not** report unexecuted tests as passing.

---

# 10. Review checklist for the guiding agent

After Luna produces the patch, the reviewing agent should inspect the diff specifically for these failure patterns.

## Reject the patch if it does any of the following

```text
- compares only method kind/label and calls that "schema compatibility"
- manually compares two or three package/version fields instead of using lock compatibility primitives
- calls ResolvePlan during recovery to discover a replacement target
- falls back to persisted desired plan after PinnedPlanFor mismatch
- removes/recreates the WAL to escape mismatch
- clears the WAL on a blocked recovery
- stores credentials in ReplacementTransaction
- duplicates elevation code independently in run.go and replacement.go
- moves normal-path credential resolution after the removal WAL boundary
- reruns after-upgrade hooks when phase is PostHookRunning
- broad-refactors app/remove, app/undo, adapters, or lock formats without necessity
```

## Expected diff shape

The final patch should be relatively small compared with the previous remediation commit.

Likely touched files:

```text
internal/exec/run.go
internal/exec/replacement.go
internal/exec/replacement_recovery_test.go
internal/plan/preparation.go
internal/app/upgrade.go
```

A tiny additional exec helper file is acceptable if it materially improves locality, but creating multiple new abstractions is not necessary.

The patch should primarily:

```text
replace immutable-only recovery reconstruction
with current-intent + persisted-lock pinning;

replace direct recovery Remove
with shared credential/elevation-aware replacement removal.
```

Anything substantially broader deserves explicit review before acceptance.

---

# 11. Delegable task breakdown

For a multi-agent execution, use these units.

## Agent Task 1 — Recovery compatibility gate

Inputs:

- `internal/exec/run.go`
- `internal/exec/capability.go`
- `internal/plan/lock.go`
- P7-T10

Deliverables:

- current-intent reconstruction helper;
- `PinnedPlanFor` compatibility gate;
- no mutation on mismatch;
- tests C-T01/C-T02.

Must not touch removal mechanics.

## Agent Task 2 — Recovered lifecycle reuse

Inputs:

- output of Task 1;
- `continueRecoveredReplacement`.

Deliverables:

- reuse already-pinned desired plan for recovered hook schedule;
- remove second `candidatePlanIntent` reconstruction;
- existing hook recovery tests green.

Must not alter hook replay policy.

## Agent Task 3 — Removal execution convergence

Inputs:

- `internal/exec/replacement.go`
- `internal/exec/run.go`
- `internal/exec/remove_resolved.go` for reference
- P8-T05

Deliverables:

- one shared replacement removal execution helper;
- normal path behavior preserved;
- recovery credential/elevation path fixed;
- tests C-T04/C-T05.

Must not change replacement phases.

## Agent Task 4 — Cleanup and validation

Inputs:

- merged Tasks 1–3.

Deliverables:

- stale comments corrected;
- dead helper cleanup;
- gofmt;
- focused tests;
- vet/linter where environment permits;
- final requirement-to-test matrix.

No architectural changes.

---

# 12. Final requirement-to-test matrix

The implementation agent should include a completed version of this table in its handoff.

| Requirement | Implementation site | Test proving it | Result |
|---|---|---|---|
| Current schema intent must match persisted desired identity | `internal/exec/run.go` recovery helper | `TestReplacementRecoveryRejectsCurrentIntentDriftBeforeMutation` | pending |
| Persisted immutable identity remains authoritative | `PinnedPlanFor` recovery path | compatible recovery test | pending |
| No mutable re-resolution during recovery | recovery helper / code inspection | recovery tests + review | pending |
| Current hooks survive compatible pinning | `continueRecoveredReplacement` | existing recovered-hook test | pending |
| Recovery retry resolves removal credentials | retry-removal branch | credential recovery test | pending |
| Recovery retry uses removal elevation | shared removal helper | elevation recovery test | pending |
| Credential/elevation failure performs no removal | retry-removal branch | failure subtest | pending |
| WAL remains on blocked recovery | state assertions | drift/credential failure tests | pending |
| Normal replacement ordering unchanged | `replacement.go` | existing atomic/crash tests | pending |
| Comments reflect actual persistence/locking model | preparation/upgrade comments | review | pending |

---

# 13. Definition of Done

This remediation is finished when:

```text
1. a same-label/same-kind schema drift cannot authorize replacement recovery;
2. recovery obtains operational semantics only from a current schema intent
   proven compatible with the persisted immutable target;
3. retry removal uses the same credential/elevation discipline as normal
   replacement removal;
4. all new negative tests prove zero destructive calls on blocked paths;
5. existing crash/WAL/hook/resource tests remain green;
6. no new persisted format or recovery phase was introduced;
7. stale comments are corrected;
8. formatting/static checks pass wherever the offline environment permits.
```

At that point, do not start another “cleanup architecture” pass. The intended result is closure, not a fresh season of refactoring.

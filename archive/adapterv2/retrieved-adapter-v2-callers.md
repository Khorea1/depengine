# AdapterV2 research — B3: callers + test doubles (2026-09-22)

Source: read-only scout of `feat/resolved-install-execution` tip. Feeds TODO-0 §2 shim design.
No `AdapterV2`/shim symbol exists in the tree yet. Current contract: `internal/exec/adapter.go:42-61`
plus optional `PlanResolver` (:69-72), `ResolvedInstaller` (:83-86), `HostCompatibilityChecker`
(:97-100), `Remover` (:106-112), `AvailabilityChecker` (:135-145), `ElevationRequirer` (:151-154),
`Versioner` (`internal/exec/state.go:16-18`).

## 1. Callers outside `internal/exec` (root `*.go`)

Zero callers of `ResolvePlan`/`InstallResolved` outside `internal/exec`. Legacy callers use only
`Kind/Available/Check/Install/Remove/CanRemove/CheckAvailable/InstalledVersion`.

- `validate_check.go:230,234,237` — `exec.Lookup` → `Available` (skipped when `--live`) → `Check` bool → "installed"/"not-installed", exit 0/1.
- `upgrade.go:307,345,357,369,387` — `LookupAdapter` → `CanRemove` gate → `Remove` (step 1) → `Install` exact tracked candidate, no fallback (step 2). Bypasses `Executor.Execute` entirely.
- `upgrade.go:551,554,557,560` — `preflightDirectUpgrade`: bool `Available`+`Check`, optional `CheckAvailable`, `CanRemove`. Absent → fail closed.
- `upgrade.go:613-619` — `probeVersion`: optional `Versioner.InstalledVersion`, fallback to lock pin.
- `upgrade.go:529,544` — static gates `exec.CandidatePlanIntent`, `exec.CandidateRunsArbitraryCode`.
- `remove.go:167,173,177,260` — `resolveRemover`: `Lookup` → `CanRemove` → unchecked `.(exec.Remover)` assert → `Remove` with state-built `mc = {Kind, Config: toolState.Config}`, bare `&config.Tool{Name}` (no schema object).
- `undo.go:211,218,224,232` — identical shape to remove.go.
- `install.go:162-168` — NO direct adapter calls. Builds `exec.New()` + `WithAdapters(...)`, delegates to `Executor`; reads only `ExecReport` statuses (:300,322).
- `status.go` — NO adapter calls at all. Pure state-vs-schema + lock comparison. Never touches `exec.Lookup`, `Check`, `Reconcile`, or `VerificationResult`.
- `ResolvePlan`/`InstallResolved` call sites (all inside `internal/`): `internal/exec/resolution.go:43`, `internal/exec/attempt.go:306` (legacy `Install` fallback :308), `internal/msi/adapter.go:35,88` (adapter-to-adapter delegation).

## 2. Test-double inventory

- `internal/run/fake.go` — `FakeRunner`, records `FakeCall`; adapter-agnostic.
- `internal/exectest/adapter.go:18-124` — `MockAdapter` implements ONLY legacy 4-method `exec.Adapter`; conformance helper pins that surface (:163-212).
- `internal/exec/executor_test.go:38-165` — `testMockAdapter` (legacy), `blockingMockAdapter`, `availabilityMockAdapter` (+`CheckAvailable`), `compatibilityMockAdapter` (+`CheckHostCompatibility`), `resolvingCompatibilityMockAdapter:110-133` (+`ResolvePlan`/`CheckHostCompatibility`/`InstallResolved`), `elevationMockAdapter`, `sequenceRunner`, `elevationTrackingRunner`.
- `internal/exec/adapter_test.go:11-29` — `mockAdapter`, `removableMockAdapter`, `removableFalseMockAdapter` (`CanRemove` truth table).
- `internal/exec/resolved_install_test.go:20-59` — `resolvingMock` (legacy `Install` fails test — pins fail-closed); `compatRejectingMock`, `resolverWithoutInstaller` (no silent fallback).
- `internal/exec/preparation_test.go:348` — `InstalledVersion` mock for recovery-observe.
- Root `upgrade_test.go:461-490` — `upgradePreflightAdapter`: `Available/Check/CheckAvailable/Install/Remove/CanRemove` + call log; exact surface the upgrade path needs.

Net: most doubles assume only `Kind/Available/Check/Install`. Only 3 doubles know resolve/install-resolved/compat, all inside `internal/exec` tests.

## 3. upgrade/status/remove vs VerificationResult today

All three bypass executor AND verification layer:
- `status.go`: no adapter, no `Reconcile`, no `VerificationResult`.
- `remove.go`/`undo.go`: state-driven `Remover.Remove` + `plan.ReleaseDependentResources` / `source.CleanupReleasedSources`.
- `upgrade.go`: direct Remove→Install + own preflight (bool probes, string version vs lock pin + `state.VersionOutdated`). No `plan.Reconcile`, no `ReconcileLockedPlan`, no `VerificationResult`.
- Sole `Reconcile` consumers: `internal/plan/verification.go:115`, `lifecycle.go:268` (`ReconcileLockedPlan`), `internal/exec/preparation.go:448-474` (`observeRecoveryCandidate`: bool `Check` → `PresenceAbsent/Present`, optional `Versioner` → `FieldVersion`). Journal-recovery is the only bool-Check→Observation bridge.

## 4. Shim requirements + bool-Check consumers

Minimal legacy-shim surface: **`Kind, Available, Check (bool-preserving), Install` + `Remove/CanRemove` + `CheckAvailable` (fail-open) + `InstalledVersion`**. `ResolvePlan`/`InstallResolved`/`CheckHostCompatibility` stay optional — forcing them breaks remove/undo/check/upgrade-preflight (bare-Tool/state-config candidates unsuitable for resolution).

Bool-`Check` consumers (VerificationResult migration surface):
1. `validate_check.go:237` (exit 0 installed)
2. `upgrade.go:554` (preflight fail-closed)
3. `internal/exec/attempt.go:141` (already-installed hot path)
4. `internal/exec/preparation.go:451` (recovery observation)
5. `internal/exec/batch.go:60` (batch fast-path)
6. `internal/exec/run.go:334` (post-batch verification)
7. `internal/exec/explain.go:146` (`already_installed` attempt)

Internal delegations (same bool type): `internal/ecosystem/go.go:62,73`, `internal/ecosystem/base.go:87,144`, `internal/exec/native_adapter.go`, win impls.

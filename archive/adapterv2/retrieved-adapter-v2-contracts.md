# AdapterV2 research — B1: plan + executor contracts (2026-09-22)

Source: read-only scout of `feat/resolved-install-execution` tip. Feeds TODO-0 §2 design.

## Current interfaces (`internal/exec/adapter.go`)

- `Adapter` (42–61): `Kind() string`; `Available(ctx, rn) bool`; `Check(ctx, rn, tool, mc) bool`; `Install(ctx, rn, tool, mc) error`.
- `PlanResolver` (69–72): embeds `Adapter` + `ResolvePlan(ctx, rn, tool, mc, intent) (*plan.ResolvedInstallPlan, error)`. Read-only; after it returns, no layer below executor may consult GitHub/`{latest}`/tags/assets (63–68).
- `ResolvedInstaller` (83–86): embeds `Adapter` + `InstallResolved(ctx, rn, tool, mc, resolved) error`. Must not resolve identity: no GitHub calls, no `{latest}`, no tag/asset lookups.
- `HostCompatibilityChecker` (97–100): embeds `Adapter` + `CheckHostCompatibility(tool, mc, intent, facts, clan) error`. Read-only, deterministic; error rejects only this candidate.
- `Remover` (106–112): `Remove` + `CanRemove()`; helper `CanRemove(adapter)` (114–117).
- `AvailabilityChecker` (135–145): `CheckAvailable(...) bool`; fail-open via `checkAvailable` (159–164).
- `ElevationRequirer` (151–154): `RequiresElevation(tool, mc) bool`.

## `internal/plan` model (`plan.go`)

- `ResolvedInstallPlan` (261–278): Version, Tool, Candidate{Method, Explicit}, Identity, Artifacts, Prerequisites, Sources, Preparation, Hooks, Ensures, SourceMutations, OwnedPaths, Entrypoints, Operations, Removal{Supported, OwnedPaths, Identity}, Secrets.
- `Operation` (215–221): Kind, Description, Effect (`read_only`/`mutation`), Command, ArbitraryCode. `Validate` (227–250): any `Command` forces `ArbitraryCode=true`; argv[0] non-empty, no NULs.
- `ResolvedIdentity` (88–100) + `Validate` (106–165); `ValidateResolution(intent, resolved)` (`resolution.go:16-47`).
- `Reconcile(desired, observation) VerificationResult` (`verification.go:115`); `VerificationResult` (102–109).
- Lock: `ProjectLock` (`lock.go:349`); `LockProjection` (301–310); `RequireImmutable()` (440–448); `VerifyResolvedPlanAgainstLock` (454); `BuildLockDocument` (758); `EntryForPlan` (800); `PinnedPlanFor` (961–1012) — only identity+artifacts+sources come from lock; operations/hooks/preparation/ownership stay from current intent; `ReconcileLockedPlan` (`lifecycle.go:268`).
- Capabilities (`internal/methodkind`): `Contract` (`methodkind.go:58-74`); `Capability` bitmask (`capability.go:8-28`): ExactVersion, Channel, ImmutableIdentity, ArbitraryCode, SourceSelection, Architecture, Scope, Revision, EnvironmentTarget, VersionConstraint, MutableTag, SourceMutation, SourceTrust, Auth, Check, Remove, Upgrade, ImmutableLock, LocalArtifact. Gates: `Contract.CheckRequirements` (`capability_requirements.go:51`); `RequiredCapabilities`, `PlanCapabilities`, `MissingPlanCapabilities`, `LifecycleCapabilities`/`LockCapabilities`.
- Static intent: `planner.BuildCandidateIntent(tool, method)` (`planner/intent.go:16`) — deterministic, host-independent; resolvers may enrich only versions/revisions/digests/artifact URLs/placement.

Implementers today: `ResolvePlan`+`InstallResolved` pairs in `httpdownload/adapter.go:39,150` (http), `httpdownload/github_adapter.go:58,98`, `httpdownload/appimage_adapter.go:59,142`, `httpdownload/android_adapter.go:61,123`, `git/adapter.go:209,246`, `msi/adapter.go:34,70`. `CheckHostCompatibility` only on `httpdownload/compatibility.go:20` (http), `:28` (github).

## End-to-end resolution flow per candidate

`execute.go:192-208` phases via `attemptMethod`; `tryMethods` (157–178) iterates `config.SelectMethods` order:
1. `gateStaticIntent` (`attempt.go:66-83`): `candidatePlanIntent` → `BuildCandidateIntent` + `CheckRequirements`; then `hostResolvedPlanIntent` (`batch.go:106-119`, native-only). Mismatch → `skip_capability`; `When` mismatch → `skip_when`.
2. `gateAdapterAvailable` (87–101): nil adapter or `!Available` → `skip_unavailable`.
3. `resolveConcretePlan` (106–136) → `resolveCandidatePlan` (`resolution.go:28-54`): non-resolver → intent unchanged; else `ResolvePlan` + `ValidateResolution` (nil-plan/rewrite → `failed`). Fail-closed: resolver without installer in real mode → `failed` (119–126). Then optional `CheckHostCompatibility` → `skip_unavailable` (128–134).
4. `gateAlreadyInstalled` (140–152): `Check` true → terminal `already`.
5. `prepareCandidate` (154–169): probe → prepare (WAL keyed on static intent) → post-prepare recheck (non-dry-run) → lazy prerequisites. Reported plan = resolved + preparation projection (261–267).
6. `installCandidate` (274–330): dry-run → `would_install` (334–350); real → `planCommit()` then `InstallResolved` or legacy `Install` under `methodTimeout` (300–310); success → installed (355–396); failure → rollback/commit-unresolved handling.

## ResolvedInstaller vs PlanResolver

Total overlap by design, split on purity: `ResolvePlan` = read-only enrichment (network/tag/asset/`{latest}` allowed, probe runner only); `InstallResolved` = mutation executing exactly the resolved plan, zero re-resolution. Executor enforces pairing fail-closed. For AdapterV2: fold into one method-specific type with `Resolve` + `ExecuteResolved` sharing one resolution path; `InstallResolved` becomes the only install entry for resolving kinds, legacy `Install` retained only for non-resolving adapters.

## Notes for modeling `plan.Operation` in AdapterV2

1. Bare `Command` ops always mean `ArbitraryCode=true` (236–238). One-resolution-path-per-candidate needs a structured payload (declarative manager/argv or Kind-dispatched params) so executors run without tripping the arbitrary-code gate.
2. `ValidateResolution` (29–45) allows enrichment of only Version/Revision/Digest/Source/Artifacts; stable identity (Package, Registry, Scope, Environment, Arch, Platform) + Tool/Candidate/RequestedVersion must be preserved — op-carried identity must land in `Identity`, not inside `Operation`.
3. Ops must be fully materialized at resolve time (concrete argv/URLs/checksums): `InstallResolved` may not re-resolve, and `PinnedPlanFor` deliberately does NOT carry operations from the lock (955–961) — ops stay with current intent, identity with the lock.
4. Map op kinds onto existing buckets (execute → `Operations`, source setup → `SourceMutations`/`Preparation`, declarative state → `Ensures`); `Removal.OwnedPaths ⊆ OwnedPaths` (405–441); hooks must be `EffectMutation` + `ArbitraryCode` with non-empty `Command` (380–384, `lifecycle.go:69-80`); `EnsureAction` needs read-only `Check` + mutating `Apply` (182–193). Otherwise `Validate`/`HasMutations` (464–480) and `PlanCapabilities` misclassify.
5. Capability gating runs on the static intent before resolution (`capability.go:52-68`): resolvers cannot upgrade capability needs at resolve time, only fill concrete values for declared capabilities.

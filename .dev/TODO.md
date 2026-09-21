# depengine — schema/install DSL hardening backlog

> Derived from the adversarial schema stress-test performed on 2026-09-17/18 against the current repository state.
>
> Goal: close every gap found before treating the installation declaration format as a stable, robust, future-proof public contract.

## Status conventions

- [ ] not started
- [~] in progress
- [x] complete
- **P0** — correctness/security blocker; fix before expanding the schema
- **P1** — architectural blocker for a stable v1 schema
- **P2** — coverage/ergonomics required for broad platform support
- **P3** — hardening, documentation, and long-term maintenance

## Ground rules

1. Do **not** add generic `args = [...]` fields to package-manager methods as a shortcut. Prefer typed, auditable fields with explicit semantics.
1. A field accepted by the schema must affect either resolution, execution, or verification in a testable way. Silently ignored fields are schema bugs.
1. `validate`, `why`, `dry-run`, `install`, `status`, `upgrade`, and lock generation must derive from the same resolved semantics rather than reimplementing approximations.
1. `--dry-run` must be observational only: no package-manager sync, source mutation, hook execution, cache mutation that changes host state, package installation, filesystem ownership mutation, or arbitrary command execution.
1. Prefer declarative desired state over imperative lifecycle scripting. Keep arbitrary code as an explicit escape hatch with a security gate.
1. Do not freeze `schema_version = 1` compatibility until the v1 freeze gate at the end of this document is satisfied.

______________________________________________________________________

# P0 — correctness and security blockers

## P0.1 — Make `--dry-run` strictly side-effect-free

**Observed gap**

`pre_install` currently executes during `install --dry-run --allow-arbitrary-code`. Native package-manager sync can also run before tool execution even in dry-run mode. This contradicts the CLI promise that no changes are made.

**Work**

- [x] Centralize dry-run behavior at the execution boundary; do not rely on every adapter/hook remembering to check it.
- [x] Prevent `pre_install` from executing in dry-run mode.
- [x] Prevent `post_install` from executing in dry-run mode.
- [x] Prevent native package-manager sync (`apt-get update`, equivalent operations) in dry-run mode.
- [x] Prevent source setup/removal in dry-run mode.
- [x] Prevent prerequisite installation in dry-run mode.
- [x] Audit download/cache behavior and define whether dry-run may perform network reads. Policy: read-only network resolution/probes are allowed; downloads/cache writes and other host mutations are not.
- [x] Audit subprocess runner entrypoints so a future adapter cannot bypass the dry-run boundary (all production packages under `pkg/` are regression-tested fail-closed against direct `os/exec` imports; executable lookup and execution are routed through `pkg/run`; blocked execution policy is preserved through logging wrappers, and OS fact detection makes zero runner calls (including platform-version fallbacks) and does not materialize its embedded helper when execution is disabled).
- [x] Make output distinguish between “would resolve/check” and “would mutate”.

**Acceptance criteria**

- [x] A dry-run over a manifest containing hooks, sources, prerequisites, native sync, HTTP/GitHub artifacts, Git builds, MSI, ecosystem managers, and containers causes zero externally visible host mutations. (`TestDryRunMatrixLeavesZeroHostMutations` runs all of these through mocked adapters + fake runner and asserts zero adapter Install calls, no hook sentinels, no mutating runner argv, an untouched state dir, and all-would-install status; the same test with dry-run disabled installs everything, proving the tripwires are live.)
- [x] A regression test proves a sentinel file is not created by any hook during dry-run.
- [x] A fake runner test proves no mutating package-manager command is invoked during dry-run.
- [x] CLI wording never claims “no changes” unless this invariant is actually enforced.

**Likely areas**

`install.go`, `pkg/exec/*`, `pkg/exec/hooks.go`, `pkg/exec/sync.go`, `pkg/source/*`, adapter conformance tests.

______________________________________________________________________

## P0.2 — Close the structured `build` arbitrary-code gate bypass

**Observed gap**

Structured build declarations such as:

```toml
build = { run = ["make"] }
```

are accepted by the schema but are not always detected by the arbitrary-code gate, because the dangerous-method check handles the string form differently from the structured form.

**Work**

- [x] Replace type-specific dangerous-field detection with one canonical semantic predicate.
- [x] Mark every executable form of `build`, hooks, custom commands, installer scripts, or future command-bearing fields as arbitrary code. (`plan.Operation.Validate` also rejects any explicit argv that is not classified as arbitrary code, closing the invariant at the resolved-plan boundary.)
- [x] Make the gate operate on resolved method semantics, not raw TOML representation.
- [x] Ensure aliases/shorthands cannot bypass the gate.
- [x] Define whether shell-string commands and argv-form commands have different risk labels; both must require explicit permission when arbitrary code is executed.

**Acceptance criteria**

- [x] String-form `build` requires `--allow-arbitrary-code`.
- [x] Structured `build.run` requires `--allow-arbitrary-code`.
- [x] All hook forms require the same gate.
- [x] A table-driven test enumerates every command-bearing field and fails if any one is not gated.

**Likely areas**

`pkg/exec/hooks.go`, method contracts, semantic validation, Git adapter tests, conformance tests.

______________________________________________________________________

## P0.3 — Eliminate semantic divergence between `validate`, `why`, `dry-run`, and real installation

**Observed gaps**

Examples reproduced during the stress-test:

- `http.url` may pass validation for `.pkg`/`.dmg`/`.exe`, while real HTTP installation later rejects installer formats.
- `.pkg`/`.dmg`/`.exe` may be rejected with advice to use a dedicated method even when no such dedicated method exists.
- MSI accepts malformed URL-like values not rejected by the shared validator.
- `why` may report a native candidate as ready without running the same availability check used before installation.

**Work**

- [~] Introduce one canonical resolved plan representation; see P1.1. (`pkg/planner.BuildCandidateIntent` now creates the host-independent `ResolvedInstallPlan` used by validation, explanation, and executor capability gating; runtime resolution still needs to enrich the same object.)
- [~] Make structural/semantic validation verify every invariant needed before execution. (`ResolvedInstallPlan.Validate` now rejects malformed/NUL-bearing tool/method/source/secret/resource/lifecycle and requested-version identity, invalid operation kinds/effects, command argv with empty executables/NUL, command-bearing operations that are not arbitrary-code classified, malformed resolved artifact URLs/checksums/digests, and duplicate artifact locations including canonical URL aliases that would create contradictory payload identity; adapter-specific/runtime-only invariants remain.)
- [x] Move shared artifact validation into shared artifact contracts rather than hard-coded method-name switches.
- [x] Validate URI schemes consistently for every download-backed method.
- [x] Reject unsupported installer/container/archive formats before execution.
- [x] Make `why` use the same availability/resolution result as installation planning.
- [ ] Make dry-run render the exact executable plan, not a best-effort approximation.
- [x] Audit error messages so they never recommend a non-existent method.

**Acceptance criteria**

- [ ] Any manifest accepted by `validate` either produces a valid resolved plan or fails only because of runtime facts that cannot be known statically.
- [x] `why` and `dry-run` cannot call a candidate “ready” if the planner has already determined it is unavailable. (`ExplainTool` and `Execute` share availability/capability gating; regression tests cover `skip_unavailable`/zero `WouldInstall` and `skip_capability`.)
- [x] Unsupported `.pkg`, `.dmg`, `.exe`, MSIX/AppX, or other installer types fail with accurate, actionable diagnostics.
- [x] URL/path validation is implemented once and covered by shared contract tests. (`artifact.ValidateURL` owns credential-safe scheme/host validation for download and Git URLs; `plan.SourceReference.URL` now also requires an absolute remote URL or scp-style Git remote rather than accepting symbolic names in the URL slot; `plan.NormalizeProjectPath` owns portable project-relative local paths, with contract tests for both.)

**Likely areas**

`pkg/validate/*`, `pkg/methodkind/*`, `pkg/exec/explain.go`, `graph_why.go`, HTTP/MSI adapters.

______________________________________________________________________

## P0.4 — Add a schema-to-runtime “no ignored fields” invariant

**Observed gap**

At least one adapter accepts a field that does not affect actual installation behavior: `asdf.version` is represented but installation uses `latest` instead. This is more dangerous than a missing feature because the manifest appears precise while runtime behavior differs.

**Work**

- [x] Create a conformance mechanism that enumerates all accepted fields for each method contract (each `Field` now declares its runtime effect phase and a contract test rejects effect-less fields).
- [~] Require every field to influence the resolved plan, validation, execution, or verification. (The static planner is fail-closed for unknown fields and projects cross-cutting identity/source/artifact/command semantics; legacy candidate `sources` are now projected as typed host-configuration sources with their source kind preserved through canonicalization/locking; remaining adapter-specific fields still need plan-effect conformance coverage.)
- [x] Fix `asdf.version` handling.
- [x] Audit SDKMAN, Cargo, Go, Conda, container, Git, Snap, Flatpak, native and artifact adapters for similar discrepancies.
  - [x] SDKMAN: exact `version` now governs both install and verification.
  - [x] Cargo
  - [x] Go
  - [x] Conda
  - [x] container
  - [x] Git
  - [x] Snap
  - [x] Flatpak
  - [x] native
  - [x] artifact adapters: removed always-overridden placement fields from AppImage/Android contracts and covered MSI identity fields.
- [x] Prevent future contract additions without corresponding semantic tests. (Every public field name must have an explicit `FieldSemantic` classification; contract finalization fails closed for unclassified fields and tests reject stale classifications.)

**Acceptance criteria**

- [x] `asdf.version = "X"` installs/checks X rather than silently using latest.
- [x] Contract tests fail when a schema field is accepted but ignored. (`TestResolveEffectFieldsMoveStaticIntent` differentially probes every `EffectResolve` field across all contracts: distinct values must move `BuildCandidateIntent` or fail validation. Triage exclusions are runtime-resolved dimensions pending P1.1 static enrichment: `release`/`branch` on artifact methods, `git.url`, `native.pkg_overrides`. Execute/verify-phase fields remain covered by per-adapter behavior tests.)
- [ ] Every adapter has at least one behavior test per non-trivial declared field.

______________________________________________________________________

## P0.5 — Fix authenticated/private artifact behavior

**Observed gap**

The Go HTTP downloader can attach GitHub authentication without exposing the token on a command line, but downloader selection may prefer `curl`/`wget`, which changes authentication capability depending on host tooling.

**Work**

- [~] Make authentication requirements part of planning/capability selection. (`ResolvedInstallPlan` source requirements derive a distinct `auth` capability and the production candidate planner is now the shared pre-probe capability boundary; schema/runtime wiring for explicit secret references remains TODO.)
- [x] Do not select a downloader that cannot satisfy required auth semantics.
- [x] Ensure credentials are not passed in argv, logs, error text, lockfiles, or state files (credential-bearing URLs are rejected; query/fragment-secret and argv-secret classification is shared by validators/logging/serialization, including OAuth/AWS/GCP-style credential keys, OAuth-style `#access_token=...` fragments, and separated `--client-secret`/`--refresh-token` argv values; `PlannerError` redacts both operation and wrapped-cause display text while preserving `Unwrap`; benign URL fragments retain their exact identity spelling; duplicate external secret references are rejected in the canonical plan; state persistence rejects obvious secret material; the current lock schema stores pins/checksums only, not method config).
- [x] Define explicit secure secret references for future private registries/sources rather than literal secrets in manifests. (`pkg/plan.SecretReference` is reference-only; typed sources reject literal URL credentials and lock projection omits secret references.)
- [x] Add private GitHub release tests using fake servers/runners.

**Acceptance criteria**

- [x] Presence of `curl`/`wget` cannot cause a private GitHub install to lose authentication support.
- [x] Tokens/secrets never appear in logged commands or serialized project state.
- [x] Downloader capability mismatches fail during planning with a clear explanation. (The adapter-neutral contract boundary emits typed `auth_requirement` vs `unsupported_capability` planner errors with stable missing-capability names; the executor static-planning boundary and `validate` now consume `CheckRequirements`, so `CandidatePlanIntent`, `why`/`dry-run` skip reasons, upgrade preflight, and validation messages all carry the class. Schema/runtime wiring for explicit secret references remains TODO.)

______________________________________________________________________

# P1 — architecture required before a stable schema v1

## P1.1 — Introduce `ResolvedInstallPlan` as the canonical semantic contract

**Problem**

The current flow is too close to `manifest -> adapter Check/Install`, so multiple commands independently approximate what installation would mean.

**Target architecture**

```text
manifest intent
    -> parse + structural validation
    -> candidate selection
    -> method resolver
    -> ResolvedInstallPlan
    -> validate / why / dry-run / lock / status
    -> execute
    -> verify desired state
```

**Work**

- [x] Define a versioned internal `ResolvedInstallPlan` model.
- [x] Include tool identity, selected candidate/method, resolved version/revision/digest, source/registry, scope, environment/profile, architecture/platform, artifacts/checksums, prerequisites, source mutations, owned paths, executable entrypoints, arbitrary-code steps, and removal metadata as applicable.
- [x] Distinguish read-only resolution operations from mutating execution operations.
- [x] Make planner output serializable for tests/debugging, while avoiding secrets.
- [~] Refactor `validate` to build/check the plan where possible. (`validatePlanIntents` now builds and capability-checks the host-independent candidate plan; runtime-only resolution remains outside validation.)
- [~] Refactor `why` to explain candidate elimination and selected plan. (`ExplainTool` now uses the same candidate plan/capability boundary and `why --json` exposes `plan_intent`; runtime-resolved identity is not yet unified.)
- [~] Refactor dry-run to print the exact plan. (Execution reports now carry serialized `plan_intent`; runtime artifact/version resolution still needs to enrich that object before this can be exact.)
- [ ] Refactor install/upgrade/status/remove around the same semantic object.
- [x] Define planner error classes: invalid manifest, unsupported capability, unavailable candidate, resolution failure, auth requirement, host incompatibility.

**Acceptance criteria**

- [ ] There is exactly one method-specific resolution path for a candidate.
- [ ] `why`, `dry-run`, and `install` agree on selected candidate and all resolved identity fields.
- [x] A golden-test suite snapshots representative resolved plans.

______________________________________________________________________

## P1.2 — Define a cross-method version/revision model

**Problem**

`version` is not currently a first-class property of installation. Different adapters variously reject it, ignore it, force `latest`, partially honor it, or only check for any installed version.

**Work**

- [x] Define a shared desired-version model that can represent at least:
  - exact semantic/package version;
  - version constraint/range where supported;
  - channel/track/risk;
  - Git tag/branch/revision/commit;
  - container tag and immutable digest;
  - rolling/latest intent.
- [x] Decide which forms are portable across methods and which remain method-specific.
- [x] Define normalization rules so `latest`, channels, constraints, revisions and exact versions are unambiguous. (`VersionDigest` is validated as a concrete `algorithm:hex` pin rather than an arbitrary string, matching resolved identity/lock semantics; when a digest intent and concrete resolved digest are both present they must identify the same digest, including in persisted lock projections.)
- [~] Define method capability declarations for supported version modes. Contracts expose exact-version, channel, revision, immutable-identity and mutable-tag capabilities; `BuildCandidateIntent` maps configured version/tag/branch/channel/digest semantics into the shared model and executor/why/validate consume the resulting capability requirements. Per-manager constraint declarations remain TODO.
- [~] Define behavior when a preferred method cannot satisfy the requested version semantics: eliminate candidate rather than silently weakening intent. (The executor now rejects the candidate at the shared plan-capability boundary before probes; broader adapter/runtime resolution still needs the same resolved identity.)
- [~] Update verification to compare actual state against requested/resolved state. (The shared reconciler compares concrete resolved identity when available and now falls back to immutable `exact` version / `digest` request intent before runtime enrichment; contradictory exact requested-vs-resolved versions fail plan validation. Constraint/channel/tag semantics still require method resolvers and adapter migration.)

**Acceptance criteria**

- [ ] A user can express an exact version for every manager that natively supports exact versions.
- [ ] No adapter silently upgrades an exact request to `latest`.
- [ ] Unsupported version semantics eliminate/fail a candidate with an explanatory reason.
- [ ] Version comparison behavior is tested per ecosystem.

______________________________________________________________________

## P1.3 — Make checks reconcile desired state, not mere presence

**Problem**

Several adapters effectively answer “is something with this name installed?” rather than “does the current installation satisfy this manifest/plan?”. This breaks drift detection, upgrades, idempotency, and reproducibility.

**Work**

- [~] Replace boolean/presence-oriented checks with desired-state verification results. A shared `VerificationResult`/`Reconcile` contract now models desired-vs-observed identity; `Reconcile` is closed under `VerificationResult.Validate`, including absent/unknown/broken probe states, and non-present states never treat stale identity fields as authoritative. Fields declared authoritative are themselves structurally validated (including concrete digest syntax and credential-safe, well-formed URL-like source/registry identity), reconciliation compares source/registry URLs, scp-style Git remote hosts and digests by canonical semantic identity rather than raw spelling, and outputs deep-copy environment identity rather than aliasing probe state. Drift entries are now structurally/semantically bounded to their declared identity kind and must agree with the authoritative `ObservedIdentity`, preventing a fabricated drift payload from independently authorizing an upgrade. Contradictory unknown+drift, canonical-equivalent drift, or absent/broken+identity results are rejected. Adapter migration remains TODO. `ResolvedIdentity.Validate` is now the shared structural desired-identity contract used by both plan validation and direct reconciliation, so reconciliation fails closed on malformed desired digest/source/scope/environment or contradictory digest intent.
- [~] Report actual version/revision/source/scope/environment when discoverable. `ObservedIdentity` and authoritative `KnownFields` now carry these dimensions; adapters still need to populate them.
- [x] Distinguish satisfied, absent, drifted, unknown/unverifiable, and broken states.
- [~] Make upgrade logic consume drift information instead of independently re-resolving intent. (`TransitionForVerification` maps validated reconciliation to install/upgrade/no-op and fails closed on unknown/broken state; `ReconcileLockedPlan` now composes lock pinning + reconciliation + lifecycle selection so mutable intent cannot be re-resolved before an upgrade decision. CLI/runtime upgrade wiring remains TODO.)
- [x] Define fallback behavior when a manager cannot reliably report installed version/source: preserve presence but return `unknown` with explicit unverifiable identity dimensions rather than treating it as satisfied.

**Acceptance criteria**

- [~] Installing version A while version B is present is not reported as satisfied by the shared reconciler; adapter migration remains TODO. (The reconciler now has coverage for drift across every shared identity dimension, and `VerificationResult.Validate` rejects contradictory known/unverifiable/drift field sets.)
- [ ] SDKMAN/asdf/mise checks verify the requested candidate/version.
- [x] Container checks can distinguish mutable tag identity from pinned digest identity.
- [ ] Status output explains drift rather than collapsing it to installed/not-installed.

______________________________________________________________________

## P1.4 — Make the lockfile universal or narrow the product promise explicitly

**Problem**

The current lock resolver primarily pins release/artifact placeholders/checksums. Package-manager, language-manager, Git branch, container tag and version-manager installs can remain unpinned, while user-facing language implies “same tools, same versions”.

**Preferred direction**

Make the lockfile the immutable-resolution projection of `ResolvedInstallPlan`.

**Work**

- [~] Define per-method lock identity. The adapter-neutral `LockProjection` now captures concrete version/revision/digest, source/registry, artifact URL/path + checksum plus checksum/signature verification metadata, scope/environment and target identity; newly generated locks also retain the normalized requested version intent so a manifest branch/tag/constraint/channel change cannot be hidden by an accidentally identical concrete resolution. Adapters still need to populate the appropriate concrete fields:
  - native/ecosystem package -> resolved package version + source/registry where stable;
  - Go -> module/package version;
  - Cargo -> resolved crate version/source;
  - Git -> commit SHA;
  - container -> digest;
  - GitHub/HTTP -> resolved URL/release/asset + checksum;
  - asdf/mise/SDKMAN -> concrete tool version;
  - Snap/Flatpak -> resolved channel/branch/version when available.
- [x] Record target architecture/platform when present in resolved identity.
- [x] Define behavior for managers that cannot provide stable resolution: projection is explicitly `unavailable`, and universal document generation fails rather than emitting a partial/re-resolving pin.
- [~] Ensure lock generation does not execute installation. `ProjectLock`/`BuildLockDocument` are pure projections over already-resolved plans and cannot execute/network/mutate; property tests also prove document bytes are invariant to input-plan order and source plans are not mutated. Digest requested-intent spelling is canonicalized and compared semantically, and source trust fingerprints are canonicalized in new projections while equivalent legacy spelling remains compatible. CLI `update` still needs migration to this path.
- [~] Ensure install consumes lock identity rather than resolving “latest” again. (`LockDocument.EntryForPlan` provides a pure, fail-closed handoff from static plan intent to the matching immutable lock entry, rejecting candidate/requested-version/source/target drift and returning a deep copy; `LockDocument.PinnedPlanFor` materializes that immutable identity into a deep-isolated copy of the current plan without host I/O, preserving in-memory source secret references and operational semantics without aliasing mutable slices/maps/pointers back into the manifest intent; `ReconcileLockedPlan` derives install/upgrade/no-op only after this pinning step. Install/runtime execution still needs wiring to consume the pinned plan.)
- [~] Add lock schema/versioning and migration policy before public freeze. The new lock projection/document is versioned and rejects unknown versions; persisted `depengine.lock` migration/backward-compatibility policy remains TODO.
- [ ] If universal locking is intentionally out of scope, revise CLI/README guarantees to say exactly what is pinned.

**Acceptance criteria**

- [ ] `depengine update` produces meaningful pins for every supported mutable method class.
- [~] Reinstalling from an unchanged lock does not silently pick a newer release/version/digest. (The adapter-neutral verifier rejects identity, requested-intent and tool-set drift against an immutable `LockDocument`; persisted `LockProjection.Validate` also rejects entries claiming `immutable` without a concrete version/revision/digest or artifact checksum. Known digest algorithms require real byte lengths and syntactically valid algorithm identifiers; new lock projections canonicalize digest/checksum case, source names, trust-key URLs and scp-style remote hosts while semantic verification remains compatible with equivalent legacy spelling; sources and artifacts are canonical/unique/order-stable; remote artifact detached-signature URLs are pinned and credential-safe; persisted URLs canonicalize scheme/host/query ordering while preserving non-secret SSH usernames as identity; malformed URL-like source/registry identities and incomplete `requested_intent` records are rejected; duplicate artifact locations are rejected rather than allowing contradictory pins. Install/CLI wiring still needs to consume that verifier.)
- [~] Lockfile never stores credentials. The new projection/document strips URL userinfo and sensitive query parameters, defensively redacts manual serialization, rejects credential-bearing persisted lock identity on read, and deep-copies pointer identity so lock projection cannot mutate the source plan; the persisted legacy lock path still needs migration to this model.

______________________________________________________________________

## P1.5 — Add a first-class installation `scope`

**Problem**

User/system/global behavior is currently encoded through method-specific flags or raw paths. Artifact defaults also differ between archives and raw binaries, making the same intent platform-dependent and surprising.

**Work**

- [x] Define portable scope vocabulary, initially at least `user` and `system`; add manager-specific/global distinctions only where semantically necessary. (`pkg/plan.Scope` is canonical; adapter aliases are mapped only when semantically equivalent.)
- [x] Define method capability support for scope. (`CapabilityScope` now has per-method portable mappings and fail-closed `SupportsScope`/`AdapterScope` checks.)
- [ ] Resolve scope into platform-native paths/flags in adapters. (The adapter-neutral placement/alias policy is implemented; wiring into blocked adapters remains.)
- [ ] Remove Unix paths from the normal authoring path whenever scope is sufficient.
- [ ] Keep `extract_to`, `link_dir`, install root, etc. as advanced overrides.
- [x] Define precedence between explicit paths and scope. (Scope supplies defaults; non-empty advanced install/link path overrides win independently, but overrides must be absolute in the target OS path model.)
- [x] Define Windows user/system path behavior explicitly. (Pure planner placement uses explicit `LocalAppData`/`ProgramFiles` roots, validates Windows absolute paths host-independently including UNC roots, rejects drive-relative roots/overrides, dot/dot-dot or duplicate path components, reserved device names and trailing-dot/ADS-like components in target paths, and never borrows Unix defaults.)
- [x] Define Unix user-scope placement in terms of the XDG Base Directory model. (`XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, and `XDG_CONFIG_HOME` are explicit target inputs with spec defaults; relative XDG values are ignored, malformed or non-canonical absolute roots/overrides are rejected before placement, and executables use the conventional `~/.local/bin` because XDG defines no `XDG_BIN_HOME`.)

**Acceptance criteria**

- [ ] A user-scoped raw binary and a user-scoped archive resolve consistently without manually specifying `~/.local/bin`.
- [ ] Windows manifests do not need Unix-specific path assumptions.
- [ ] Unsupported scope on a method is detected at planning time.

______________________________________________________________________

## P1.6 — Add first-class `environment` / `profile` targeting

**Problem**

Scope does not identify virtual/project/profile targets such as Conda environments, Python environments, npm project/global contexts, Cargo install roots, Nix profiles, or version-manager profiles.

**Work**

- [x] Define an environment/profile concept separate from user/system scope. (`pkg/plan.EnvironmentTarget` is distinct from `Scope`.)
- [x] Avoid a single overloaded string if target semantics differ materially by ecosystem; use typed method fields where appropriate while preserving a common plan representation. (Shared identity distinguishes named environment, prefix, profile, and project targets.)
- [x] Make the selected environment/profile part of desired-state identity and locking where relevant. (`ResolvedIdentity`, verification, and lock projection carry the typed target; adapter wiring remains.)
- [ ] Ensure checks and removal operate in the same target environment.
- [ ] Do not implicitly depend on whatever environment happens to be active in the invoking shell unless explicitly requested.

**Acceptance criteria**

- [ ] Conda can target a named environment or prefix declaratively.
- [ ] Install/check/remove all refer to the same declared environment.
- [ ] Two identical manifests do not target different environments merely because different shells are active.

______________________________________________________________________

## P1.7 — Extend host conditions with OS/distro version facts

**Problem**

Conditions distinguish OS, distro, family, arch, libc, kernel/init, WSL/container/Android, but cannot directly express package availability tied to OS release versions.

**Work**

- [x] Add normalized OS/distro version facts from existing detection sources (`VERSION_ID`, `sw_vers`, Windows version/build information). Linux/macOS/Android/POSIX-Windows-layer facts are emitted, and native Go fallbacks query `sw_vers`/`cmd.exe ver` when the shell detector cannot run.
- [x] Define comparison semantics for versions that are not strict SemVer.
- [x] Support at least exact/min/max or a clearly constrained version expression.
- [x] Keep the condition DSL bounded; do not introduce an arbitrary expression language unnecessarily (exact/min/max fields only; no expression evaluator).
- [x] Test Ubuntu/Fedora/macOS/Windows version conditions.

**Acceptance criteria**

- [x] Manifests can distinguish e.g. Ubuntu 22.04 vs 24.04, macOS major versions, and Windows build ranges without hooks.
- [x] Version comparison rules are documented and deterministic.

______________________________________________________________________

## P1.8 — Generalize package/source/registry modeling

**Problem**

Current `sources` support is strong for the previously identified PPA/COPR/Scoop bucket/Brew tap cases, but it is not yet a general model for package sources, registries, channels and remotes.

**Work**

- [x] Define the distinction between:
  - host source configuration mutation;
  - per-install source selection;
  - registry/index/channel selection;
  - trusted signing/key material.
  (`pkg/plan.SourceReference` has explicit roles and separate trust/auth metadata.)
- [ ] Add typed source support where required for WinGet, Chocolatey, Cargo registries, Python indexes, Conda channels, Flatpak remotes, apt/dnf arbitrary repositories, etc.
- [x] Define source identity and ownership. (Typed source identity includes role/name/URL plus explicit depengine ownership for host configuration.)
- [~] Define trust/fingerprint/key verification semantics rather than accepting opaque shell snippets. (`SourceTrust` carries declarative key reference/fingerprint and validates shape; adapter-specific cryptographic verification remains TODO.)
- [x] Add secure secret references for authenticated sources. (Sources can refer to external provider/name pairs; literal URL credentials/sensitive query tokens are rejected.)
- [~] Make source selection part of desired state and lock identity when it affects resolution. (Resolved plans now carry typed sources and lock projection persists credential-free source/trust identity, including host-source kind so PPA/COPR/tap/bucket identities cannot collapse; runtime ownership and broader adapter population/verification migration remain TODO.)

**Acceptance criteria**

- [ ] A package with the same name in two registries can be deterministically pinned to the intended registry/source.
- [ ] Source changes are explainable in dry-run and reversible when depengine owns them.
- [ ] Credentials remain external to committed manifests/locks.

______________________________________________________________________

## P1.9 — Make candidate preparation transactional or explicitly owned

**Problem**

Candidate evaluation can mutate the machine by adding sources or installing prerequisites before the final candidate succeeds. A later failure/fallback does not necessarily undo those mutations.

**Work**

- [~] Separate read-only candidate probing from mutating preparation. (`pkg/plan.PreparationPlan` enforces read-only probes vs mutating prepare/commit phases; `PreparationJournal.ValidateShape` rejects malformed durable lifecycle state without needing a plan, while every transition still validates the persisted journal as an exact ordered prefix of the current plan. `pkg/state.State` now persists active preparation journals in checksum-protected state/snapshots, and `LockedState` exposes durable transition helpers that save each journal boundary while holding the exclusive state lock. `PreparationJournal.RecoveryFor` plus `LockedState.PreparationRecovery` now derive restart behavior without mutation: uncommitted confirmed preparation rolls back, `rolling_back` resumes only compensations not already durably confirmed, `committing` requires read-only desired-state reconciliation before finalization, and write-ahead `applying` / `rollback_applying` markers block replay when a crash leaves either a prepare mutation or its compensation host-side outcome ambiguous. `ResolveApplying` / `LockedState.ResolvePreparationApplying` and the symmetric rollback resolution close those WAL records only from explicit `applied` or `not_applied` evidence; unknown outcomes remain blocked. A committing journal whose normal desired-state probe is absent/drifted/unknown/broken still stays blocked, but stronger method-specific evidence that *none* of the commit operations took effect can now be persisted through `ResolveCommitNotApplied` / `ResolvePreparationCommitNotApplied`, returning the journal to `ready` so preparation can be retried or rolled back without replaying commit. The exact resolved preparation plan is now persisted beside every active WAL journal in state format v3, so restart recovery cannot reconstruct a different transaction from host state already changed by an in-flight mutation; journal/plan keys must pair 1:1 and every persisted journal is validated against that stored plan. Production executor recovery now runs before new host mutation, and source preparation uses read-only presence probes followed by durable per-source WAL; candidate-specific prerequisites remain to migrate; committing source transactions now re-identify the exact current candidate and auto-finalize only when candidate-specific presence plus every identity dimension the executor can authoritatively observe reconciles as satisfied; a reconciled commit is terminal for that run so hooks/batching/install are not replayed.)
- [~] Define prepare/commit/rollback lifecycle or another ownership/refcount model. (Shared plan model now has ordered prepare/commit phases, a persisted-data-friendly preparation journal, rollback decisions, explicit ownership, and resource refcounts; commit planning now persists an explicit `committing` state before host-side commit operations, while rollback planning persists `rolling_back` before compensation; terminal `committed`/`rolled_back` states are reached only after the corresponding host mutations complete and are rejected if left in the active-journal map. Each prepare mutation now also crosses a persisted `applying` write-ahead boundary before host mutation and is confirmed afterward, and each safe rollback compensation crosses the symmetric `rollback_applying` boundary with an ordered `rollback_applied` prefix. A crash between host execution and journal confirmation is therefore represented explicitly instead of looking like "never started", while a restart after earlier compensations resumes only the remaining reverse-order operations. Crash recovery cannot confuse a planned/in-progress transition with a completed one; a committing journal finalizes automatically only when reconciliation reports the desired identity fully satisfied, ambiguous prepare/rollback mutation outcomes remain blocked until explicit `applied`/`not_applied` evidence resolves the exact in-flight mutation, and absent/drifted/unknown/broken commit outcomes remain fail-closed unless an independent method-specific probe establishes the stronger `commit not applied` fact and explicitly returns the journal to `ready`. Plan validation also rejects duplicate mutation IDs and duplicate semantic resource identities so one source/prerequisite cannot be prepared twice under different IDs; URL-like resource/ensure identities must use the same canonical spelling used by lock/reconciliation semantics. Source preparation is now wired through this lifecycle in the production executor; committing source transactions can recover automatically when the exact current candidate is re-identified and package/version evidence fully satisfies the desired identity, while richer identity observation and prerequisite preparation remain TODO.)
- [x] Do not add sources merely to answer “could this candidate work?” unless execution has committed to that candidate. (Capability/condition/adapter gates and source-presence probes are read-only. Candidates whose sources already exist still run repository availability before any mutation; when a declared source is absent, the current repository index is not authoritative, so the selected candidate enters durable source preparation, repository availability is revalidated immediately afterward, and an unavailable target compensates the source before fallback. Lazy `method.requires` installation now happens only after that post-source availability gate.)
- [x] Track candidate-specific prerequisites/sources as owned state when created by depengine. (`ResourceIdentity`, `OwnershipKind`, and `OwnedResourceState` model source/prerequisite ownership and dependents; resolved prerequisite identities are validated and exact duplicates are rejected before lifecycle planning. `PreparationJournal.FinalizeCommit` atomically projects every prepared source into a canonical ownership snapshot and claims the committed tool without aliasing prior state. `pkg/state.State` persists canonical `OwnedResources` plus active `PreparationJournals`, validates both fail-closed on save/load, and carries them through checksum-protected snapshots. Candidate-specific `method.requires` installs are normal tracked tool transitions: failed/fallback candidates leave retained prerequisites explicit in report/state, while successful candidates claim canonical `prerequisite/tool:<name>` resources with depengine-vs-external ownership and shared owner refcounts. Static `tool.requires` edges now claim the same prerequisite identity. State v4 persists sticky `root_requested` intent so removal can distinguish a lazy helper from a helper the user also requested directly; last-reference depengine-owned lazy prerequisites are recursively removed, shared helpers wait for the final owner, and root/external prerequisites are retained.)
- [~] On candidate failure, roll back safe reversible mutations or retain them with explicit state/reporting if rollback is impossible. (`RollbackSafe` vs `RollbackRetain` and reverse-order rollback planning are implemented in the shared model; rollback accepts only the exact applied prefix representable by a valid `PreparationJournal`, consumes a complete pre-commit ownership snapshot so resources with committed dependents are retained rather than removed, and `FinalizeRollback` projects every unreverted mutation into canonical ownership state after compensation succeeds. Newly retained/orphaned resources are therefore persisted with zero committed dependents instead of being silently forgotten. Duplicate/corrupt ownership snapshots and ownership changes fail closed. Local-artifact replacement also restores the previous destination when post-commit backup cleanup fails, avoiding an ordinary failure result with a silently committed replacement. The durable state API now requires `PlanPreparationApply` before `RecordPreparationApplied`, and likewise requires `PlanPreparationRollbackApply` before each compensating mutation, so failed confirmation leaves either direction explicitly in-flight for recovery/reporting rather than silently dropping or replaying it. Explicit recovery evidence can persist either the completed mutation into the appropriate confirmed prefix or a proven `not_applied` result back to the retryable lifecycle state without guessing. Production source preparation now persists add/remove write-ahead boundaries, probes ambiguous source outcomes, resumes rollback on restart, reconciles committing transactions from exact candidate metadata plus authoritative package/version evidence when available, and otherwise remains fail-closed. Candidate-specific prerequisites still use explicit-retain semantics on a failed candidate: a successful lazy prerequisite is recorded in report/state even if that candidate fails, so it is not a silent orphan. Once a successful owner claims it, shared refcounts and root intent drive safe automatic cleanup. WAL-backed prerequisite preparation and richer identity observation remain TODO.)
- [x] Define shared-source refcount semantics so removal of one tool does not remove a source still needed by another. (`ClaimResource`/`ReleaseResource` use sorted unique dependents; `ReleaseDependentResources` applies that rule across a complete ownership snapshot, reports only last-reference depengine resources as cleanup candidates, and retains zero-ref external state. `CleanupReleasedResources` then maps only plan-owned `RollbackSafe` candidates to reverse-order cleanup operations while retaining irreversible resources explicitly; `FinalizeReleasedResourceCleanup` re-derives the full decision, including the exact ordered cleanup operations/argv, before deleting ownership state so a caller cannot silently discard `retain_report` resources or substitute/omit host cleanup.)

**Acceptance criteria**

- [x] Falling back from candidate A to B does not leave silent orphan sources/prerequisites from A. (Candidate sources are transactionally rolled back before fallback; candidate-specific prerequisites that were already installed use explicit-retain semantics and remain visible in report/state rather than disappearing. Successful owners claim shared prerequisite identities, and later removal releases those claims and recursively cleans only last-reference depengine-owned lazy helpers.)
- [x] `remove` can safely clean depengine-owned shared sources only when no dependents remain. (Removal releases the tool dependent, removes only last-reference depengine-owned sources, and persists zero-ref ownership for retry when cleanup fails.)
- [x] Dry-run shows planned prepare/commit mutations without performing them. (Runtime source probes project missing-source preparation into `plan_intent`; human output labels `prepare` and `commit`, and regression tests assert no state/WAL or mutating runner call is created.)

______________________________________________________________________

## P1.10 — Clarify hooks versus declarative ensure actions

**Problem**

Hooks are transition events, not durable desired state. A global `pre_install` may run before knowing the winning candidate and may run even when it is not semantically appropriate for a specific method. Post-install only covers some transitions.

**Work**

- [x] Specify exact hook lifecycle semantics: hooks are candidate-local transition events bound to install/upgrade/repair/remove and before/after timing; they run only for the selected plan and matching transition.
- [~] Support candidate/method-local hooks only if there is a strong use case; otherwise prefer declarative primitives. (The resolved-plan model is candidate-local; manifest/parser integration remains TODO.)
- [x] Introduce a separate concept for durable “ensure this state exists” behavior if required. (`EnsureAction` requires an explicit read-only check plus a mutating apply operation.)
- [x] Make hooks part of arbitrary-code gating and resolved-plan visibility. (Lifecycle hooks are serialized in the selected plan and always require `CapabilityArbitraryCode`.)
- [x] Define rollback/error semantics for hook failure. (Pre-hook abort prevents the transition; post-hook failure reports against an already-committed transition and never triggers implicit compensating uninstall/remove.)
- [x] Ensure hooks never masquerade as idempotent state unless an explicit check is provided. (Hooks are events only; durable ensure state is a separate checked primitive.)

**Acceptance criteria**

- [~] A hook needed only by an apt candidate cannot accidentally run when GitHub fallback wins. (The canonical plan model binds hooks to the selected candidate and exact transition; production planner/executor integration remains TODO.)
- [~] Status does not report a tool healthy merely because a one-time hook previously ran. (The plan model separates hooks from checked `EnsureAction`; status integration remains TODO.)

______________________________________________________________________

## P1.11 — Make method capability metadata first-class

**Problem**

Adding cross-cutting fields manually to every method contract will become brittle and will not explain why a candidate is incapable of honoring a request.

**Work**

- [~] Extend method contracts with capabilities such as (exact version, version constraints, channels, mutable tags, immutable identity, arbitrary-code execution, source selection/mutation/trust/auth, architecture, scope, environment/profile, revision selection, check/remove/upgrade, immutable locking, and local-artifact requirements are now represented; the adapter-neutral local-artifact resolver/materializer exists, but no production method advertises local-artifact until schema/planner wiring is complete):
  - exact version / constraints / channels / revisions;
  - source/registry selection;
  - user/system scope;
  - environment/profile targeting;
  - architecture/target selection;
  - [x] immutable resolution/locking capability metadata;
  - [x] check/remove/upgrade capability metadata;
  - arbitrary-code execution;
  - [x] offline/local artifact capability requirement (the production `local` method advertises `local-artifact`; URL-backed methods remain fail-closed for `Artifact.LocalPath`);
  - auth support.
- [x] Use capabilities during candidate filtering/planning (defensive planner boundary rejects capability mismatches before probes/execution). Lifecycle transitions and immutable-lock policy feed the same `CandidateRequirements` boundary; `Artifact.LocalPath` requires `local-artifact` and is accepted only by the production `local` method.
- [x] Expose capability mismatch reasons in `why` (`skip_capability` with named missing capabilities).
- [~] Generate relevant JSON Schema/docs from the same contract data where practical (JSON Schema now embeds capability metadata generated from method contracts; prose docs remain partly manual).

**Acceptance criteria**

- [x] Requesting a capability unsupported by one candidate eliminates it deterministically.
- [ ] There is no growing collection of method-name conditionals for cross-cutting semantics.

______________________________________________________________________

## P1.12 — Revisit implicit native fallback semantics

**Problem**

Declaring a non-native method can implicitly inject a native candidate, so “methods declared” and “methods executable” differ unless `method_only` is used.

**Work**

- [x] Decide and document one principle for implicit native fallback: only shorthand scalar/bool declarations infer native; explicit method tables mean exactly what they declare.
- [x] Strong option: allow native inference only for simple shorthand declarations; full method tables mean exactly what they declare.
- [ ] Alternative: make fallback behavior a visible defaults setting.
- [x] Ensure `why` clearly identifies inferred versus explicitly declared candidates.
- [x] Add tests for `brew`, Go/Cargo shorthand, explicit method subtables, and `method_only`.

**Acceptance criteria**

- [x] Authors can predict the candidate set from the manifest without hidden method injection rules.
- [x] Existing convenience remains available explicitly if desired.

______________________________________________________________________

## P1.13 — Clarify preference ordering versus allow-list semantics

**Problem**

`method_order`/`method_prefer` behave as preference prefixes; omitted methods remain eligible. `method_only` is the actual allow-list. The distinction is valid but easy to misread.

**Work**

- [x] Rename or document global ordering so “order” cannot reasonably be read as exhaustive.
- [x] Use `method_prefer` as the canonical term globally as well as per-tool; `defaults.method_order` remains a compatibility alias and cannot be combined with it.
- [x] Keep `method_only` as the explicit allow-list.
- [x] Make `why` show whether a method is lower-priority versus disallowed (`skip_policy` is distinct from availability/condition skips).

**Acceptance criteria**

- [x] Documentation and examples make preference vs eligibility unambiguous.
- [x] Tests cover omitted methods remaining eligible under preference-only configuration.

______________________________________________________________________

## P1.14 — Decide schema compatibility/freeze policy only after semantic stabilization

**Problem**

Current specs explicitly say only the latest schema contract is supported and schema changes may break older files. That is reasonable during DSL formation, but it is not a future-proof public compatibility promise.

**Work**

- [x] Keep current breakable policy while P0/P1 work is underway.
- [x] Define what `schema_version` will mean once v1 freezes.
- [x] Decide whether v2+ will use parser dispatch, migration tooling, deprecation windows, or only explicit manual migration.
- [x] Define backwards-compatibility policy for lock/state formats separately from manifest schema. (`pkg/state` now enforces the centralized pre-freeze state-format policy on both read and write: missing files are created at `CurrentStateVersion`, while omitted/older/future on-disk versions are rejected rather than silently reinterpreted; state format v2 made the integrity checksum mandatory, v3 persists the exact preparation plan required by every active WAL journal, and v4 adds durable `root_requested` intent required for safe prerequisite garbage collection; older formats cannot masquerade as current state and snapshot restore inherits the same reader policy.)
- [x] Do not promise compatibility before the freeze gate is met.

**Acceptance criteria**

- [x] The public documentation accurately states compatibility guarantees.
- [x] `schema_version` has a stable semantic purpose rather than being a constant-only validator.

______________________________________________________________________

# P2 — method fidelity and missing installation primitives

## P2.1 — Add first-class direct macOS installer support

**Gap**

Direct `.pkg` and `.dmg` distribution is common, while current HTTP handling rejects installer formats and only MSI has a dedicated installer method.

**Work**

- [ ] Define a typed `pkg` installer method for macOS packages.
- [ ] Define DMG semantics: mount, locate app/pkg, install/copy, detach, verify, remove ownership where possible.
- [ ] Distinguish `.app` bundle installation from `.pkg` installer execution.
- [ ] Support checksum/signature/notarization verification where practical.
- [ ] Model user/system scope where the installer permits it.
- [ ] Add deterministic discovery rules; avoid heuristic “first file in DMG” behavior unless explicitly configured.

**Acceptance criteria**

- [ ] A direct vendor `.pkg` can be represented without arbitrary shell hooks.
- [ ] A DMG containing a single declared `.app` or `.pkg` has a deterministic declarative install path.

______________________________________________________________________

## P2.2 — Add first-class Windows installer families beyond MSI

**Gap**

Direct `.exe`, MSIX/AppX and related installers are not represented cleanly.

**Work**

- [ ] Define whether EXE installers need a generic typed installer method with explicit silent-install protocol metadata or vendor-specific escape hatch.
- [ ] Add MSIX/AppX support where Windows APIs permit reliable install/check/remove.
- [ ] Model user/machine scope, architecture, product identity, silent mode, reboot requirements and uninstall identity.
- [ ] Never guess silent flags for opaque EXE installers.

**Acceptance criteria**

- [ ] Common Windows direct installers do not need to be mislabeled as raw HTTP binaries.
- [ ] Unknown/opaque EXEs fail safely unless the manifest explicitly opts into an unsafe installer protocol.

______________________________________________________________________

## P2.3 — Improve WinGet fidelity

**Work**

- [x] Add typed version selection.
- [x] Add source selection.
- [x] Add user/machine scope.
- [x] Add architecture selection where meaningful.
- [x] Represent installer type as a typed allow-list (`installer_type`); generic override/argument bags remain intentionally unsupported.
- [x] Verify installed package identity/version using WinGet `list --id --exact` data rather than executable presence alone.

______________________________________________________________________

## P2.4 — Improve Chocolatey fidelity

**Work**

- [x] Add exact/package version.
- [x] Add source selection.
- [x] Model x86/architecture (`architecture = "x86" | "x64"`; x86 maps to `--forcex86`).
- [ ] Evaluate typed package parameters and installer parameters; do not expose arbitrary argument bags by default.
- [~] Verify actual installed package version/source (exact version is verified from `choco list --limit-output`; Chocolatey does not expose durable installed-source identity through this check, so source verification remains TODO).

______________________________________________________________________

## P2.5 — Improve Scoop fidelity

**Work**

- [x] Add version support where Scoop semantics permit it.
- [x] Model bucket/source identity consistently with generalized sources.
- [x] Support user/global scope where safe.
- [x] Model architecture selection where needed.
- [x] Verify package version and bucket/source where available.

______________________________________________________________________

## P2.6 — Improve Homebrew/cask fidelity

**Work**

- [ ] Define version/pinning semantics realistically; do not promise exact historical formula versions that Homebrew cannot resolve generically.
- [ ] Model taps as sources with ownership.
- [ ] Distinguish formula vs cask semantics in plan/check/remove.
- [ ] Model architecture/prefix/scope only where portable and reliable.

______________________________________________________________________

## P2.7 — Expand Cargo installation semantics

**Work**

- [~] Add version/constraint. Exact Cargo `version` is supported; general constraints remain TODO.
- [ ] Add registry selection.
- [ ] Add `git`, branch/tag/rev semantics without conflating with generic Git build method.
- [ ] Add feature selection, `--no-default-features`, selected bins, target and install root only as typed fields with clear portability.
- [ ] Make lockfile capture concrete crate version/source/revision.
- [ ] Verify installed crate version, not just binary presence.

______________________________________________________________________

## P2.8 — Expand Go tool installation semantics

**Work**

- [x] Stop forcing `@latest` when exact version is requested.
- [x] Add explicit module/package version field.
- [ ] Define tool package path vs module identity semantics.
- [ ] Lock concrete module version.
- [ ] Verify installed version where discoverable; define limitations when binary metadata cannot prove it.

______________________________________________________________________

## P2.9 — Audit Python/Node/Ruby/PHP ecosystem adapters for version/source/scope/environment fidelity

**Targets**

pip, pipx, uv, npm, pnpm, bun, yarn, gem, composer and related adapters.

**Work**

- [~] For each adapter, document supported desired-state dimensions (coverage notes now identify version/source/scope support for pip/npm/pipx/uv/gem/composer/bun/pnpm/yarn and remaining lower-fidelity adapters).
- [~] Add exact version/constraint where native tooling supports it (pip, npm, pipx, uv, gem, composer, bun, pnpm and yarn exact versions implemented; remaining adapters TODO).
- [~] Add registry/index/source selection where needed (pip index URL, npm registry, pipx index URL, uv index, RubyGems source and Bun registry implemented; remaining adapters TODO).
- [~] Distinguish global/user/project/environment installation (pipx user/global and RubyGems default/user scope implemented; remaining adapters TODO).
- [~] Make checks verify requested package version/environment (pip, npm, pipx, uv, gem, composer, bun, pnpm and yarn verify requested versions; remaining adapters TODO).
- [ ] Add lock semantics or explicitly mark adapters non-lockable until implemented.

______________________________________________________________________

## P2.10 — Complete Conda environment semantics

**Work**

- [ ] Add named environment and prefix targeting.
- [ ] Add channels/source ordering.
- [ ] Add version/build constraints as typed package identity.
- [ ] Define solver/channel priority semantics sufficiently for reproducibility.
- [ ] Verify package state inside the declared environment.
- [ ] Lock concrete package identity where practical.

______________________________________________________________________

## P2.11 — Correct version-manager semantics (asdf/mise/SDKMAN)

**Work**

- [x] Fix asdf version installation immediately under P0.4.
- [ ] Evaluate a shared version-manager contract for asdf/mise overlap.
- [ ] Model plugin/provider installation separately from tool-version installation where needed.
- [ ] Make “current/global/local” selection explicit rather than environment-dependent.
- [x] SDKMAN checks verify the requested candidate/version, not merely any current version.
- [ ] Lock exact resolved tool versions.

______________________________________________________________________

## P2.12 — Correct Snap channel modeling

**Gap**

Snap channel semantics are richer than the four risk names; tracks and branches matter.

**Work**

- [x] Model channel as track/risk/branch matching Snap tracking semantics.
- [x] Preserve simple shorthand for `stable`, `candidate`, `beta`, `edge`.
- [x] Include confinement/classic requirements separately.
- [~] Verify installed tracking channel/revision where available (tracking channel is verified from `snap list`; revision pinning is not modeled).

______________________________________________________________________

## P2.13 — Complete Flatpak remote/branch/scope semantics

**Work**

- [x] Remove hard-coded assumption that every install comes from Flathub.
- [~] Add remote selection and ownership (selection + origin verification implemented; remote lifecycle/ownership remains TODO).
- [x] Add branch selection.
- [x] Add user/system scope.
- [x] Verify application ref + branch + origin.

______________________________________________________________________

## P2.14 — Improve Git method identity and build semantics

**Work**

- [ ] Add explicit tag/branch/rev/commit selection.
- [ ] Lock mutable refs to commits.
- [ ] Add submodule support as a typed option.
- [ ] Evaluate Git LFS support/capability.
- [ ] Add command environment/cwd semantics only as typed structured data.
- [ ] Treat any build/install command as arbitrary code.
- [ ] Define owned build outputs and verification rather than assuming repository presence means successful installation.

______________________________________________________________________

## P2.15 — Complete container image identity

**Work**

- [x] Add immutable digest representation.
- [x] Add registry/source selection.
- [x] Add platform/architecture selection.
- [ ] Add secure auth references.
- [ ] Lock tags to digests when reproducibility is requested.
- [x] Verify local image identity by digest where possible.

______________________________________________________________________

## P2.16 — Add Nix profile/flake support as a distinct semantic model

**Gap**

Nix installables/profiles/revisions do not fit a simple `{ pkg = "name" }` abstraction.

**Work**

- [ ] Define Nix installable syntax explicitly (flake ref/output or supported subset).
- [ ] Add profile targeting.
- [ ] Resolve mutable refs to immutable revisions where possible.
- [ ] Define check/remove behavior using profile state.
- [ ] Avoid pretending Nix is a conventional package manager if doing so loses reproducibility semantics.

______________________________________________________________________

## P2.17 — Support local/offline artifacts

**Gap**

Air-gapped, vendored and locally mirrored installs need a path/file source; treating all artifacts as network URLs prevents this class of deployment.

**Work**

- [x] Add explicit local-file/path artifact source rather than overloading HTTP URL validation. (`local` is a first-class method with generated JSON Schema, planner projection, capability gating, registered production adapter, and a distinct portable `Artifact.LocalPath`.)
- [x] Resolve relative paths against a documented manifest/project root. (Parsing binds the project `schema.toml` directory as runtime-only `ProjectRoot`; merged manifest methods are rebound to that project root, while plans/locks keep only canonical project-relative `/` paths. Traversal, absolute paths, Windows drive prefixes, backslashes, and symlink components are rejected.)
- [~] Support checksums/signatures for local artifacts too. (Fixed SHA-256 is declared in the method checksum contract, validated during structural validation/planning, computed/verified at resolution, and reverified immediately before materialization using the same validated open file identity through raw/archive materialization; raw staging is checksum-verified before commit and archives are extracted from a checksum-verified snapshot, closing in-place source mutation after initial resolution; archive provenance/tree markers and root/tree entries are also verified against the same inode identity before reading. Archives persist and verify a deterministic extracted-tree digest so post-install payload drift is detected, including installed root/directory permission drift; archive extraction also rejects lexical destination aliases such as empty/current-directory components while preserving the conventional root `./` directory entry; raw artifacts verify source permission bits as desired state in addition to content bytes and reject permission drift between resolve/install. `:auto` is rejected offline. Detached-signature support remains.)
- [x] Ensure lock/state can identify vendored content without embedding machine-specific absolute paths where avoidable. (Lock identity persists project-relative path + checksum and rejects absolute/non-canonical local paths. The adapter freezes an omitted checksum to the resolved SHA-256 before installation, so generic state persistence records `local_path` + content digest while `ProjectRoot` remains runtime-only.)
- [x] Test offline installation with networking disabled. (`pkg/localartifact` remains stdlib-only; the production `local` adapter invokes no downloader/subprocess and has raw/archive tests asserting zero `Runner` calls. Raw, ZIP, TAR and TAR.GZ/TGZ materialization rejects traversal/links plus host-independent Windows drive-qualified, reserved-device, ADS-like, control-character, backslash-alias and other Windows-invalid entry names; rejects parent-component aliases, case-insensitive path collisions and file/directory destination conflicts so one archive cannot materialize differently across Unix/Windows; reserves depengine metadata marker names case-insensitively; revalidates every source path component immediately before hashing/materialization to reject symlinks introduced after resolution; extracts directories with temporary writable staging permissions and applies final declared modes only after payload creation, while rejecting final file/directory modes that would make desired-state verification impossible; normalizes the installed archive root away from `MkdirTemp`'s private mode; surfaces commit/restore failures explicitly; commits transactionally; and archives carry transactional source + extracted-tree SHA-256 markers used by `Check` to detect provenance and payload/permission drift.)

______________________________________________________________________

## P2.18 — Define a safe last-resort installer-script escape hatch

**Gap**

Some vendors only publish bootstrap scripts or opaque installer commands. Forcing these into hooks makes ownership/check/remove semantics unclear.

**Work**

- [ ] Add only if real-world recipes require it after typed methods are exhausted.
- [ ] Require explicit arbitrary-code opt-in.
- [ ] Require an explicit check/verification strategy.
- [ ] Require declared ownership/removal strategy where feasible.
- [ ] Prefer argv-form execution; shell strings must be clearly marked as shell execution.
- [ ] Never allow this primitive to become the default way to express missing typed manager features.

______________________________________________________________________

## P2.19 — Clarify Android APK semantics

**Problem**

A downloaded APK handed to another installer is not equivalent to a package being installed and managed by depengine.

**Work**

- [ ] Decide whether the method means “download/prepare APK”, “invoke platform package installation”, or both as separate methods/stages.
- [ ] Name/status the state accurately.
- [ ] If actual installation is supported, model package ID, version, device/user target, install verification and removal.

______________________________________________________________________

# P2 — ergonomics and authoring model

## P2.20 — Introduce platform-neutral artifact defaults through `scope`

**Work**

- [ ] Make user-scope artifact installation the same conceptual operation for raw binaries and archives.
- [ ] Stop requiring normal manifests to know `/usr/local/bin` or `~/.local/bin` when scope expresses the intent.
- [ ] Define Windows and macOS equivalents internally.
- [ ] Keep explicit destination paths as advanced overrides.

______________________________________________________________________

## P2.21 — Simplify `schema.example.toml`

**Observed issue**

The example teaches fields such as `extract_to` and `link_dir` even when current defaults already produce the intended result, while a raw-binary example must specify a path due to inconsistent defaults.

**Work**

- [ ] Remove redundant `extract_to`/`link_dir` from simple archive examples such as `fd` once scope/defaults make them unnecessary.
- [ ] Explain `strip_components` with an archive path example.
- [ ] Explain `entrypoints` as the public executable set inside an owned payload.
- [ ] Replace raw `~/.local/bin` examples with `scope = "user"` once available.
- [ ] Separate “90% path” examples from advanced ownership/path-control examples.
- [ ] Ensure every example is compiled/validated in tests.

______________________________________________________________________

## P2.22 — Reduce schema ambiguity between method shorthand, labeled candidates and manager names

**Work**

- [ ] Document precisely when a table name is a method, a label, or a native-manager override.
- [ ] Evaluate whether syntax can make these cases structurally distinct without losing concise shorthands.
- [x] Ensure diagnostics print both label and resolved method kind. (`MethodAttempt` now carries resolved `Kind` plus `Label`; `why` renders `label (kind)` in human output and exposes both in `--json`. `ToolResult` already separated `Method`/`MethodKind`.)
- [ ] Add parser tests for ambiguous-looking declarations (`apt`, `brew`, `gh`, arbitrary labels, `kind = ...`).

______________________________________________________________________

## P2.23 — Add better schema introspection and diagnostics

**Work**

- [ ] Add a command/debug mode to show the normalized candidate list after defaults/inference.
- [ ] Show inferred candidates distinctly from explicit candidates.
- [ ] Show capability mismatches and condition failures.
- [ ] Show resolved scope/environment/source/version in `why`.
- [ ] Provide actionable diagnostics for unsupported fields/installer formats without recommending unavailable methods.

______________________________________________________________________

# P3 — validation, regression matrix, docs and freeze readiness

## P3.1 — Build an adversarial manifest fixture matrix

Create fixtures covering at least:

- [ ] Linux: Debian/Ubuntu, Fedora/RHEL family, Arch, Alpine, openSUSE, Void, Gentoo, BSD package managers where supported.
- [ ] macOS: Homebrew formula, cask, MAS, GitHub archive, direct `.pkg`, DMG app, raw binary.
- [ ] Windows: WinGet, Scoop, Chocolatey, MSI, EXE, MSIX/AppX, GitHub ZIP/raw binary.
- [ ] Language ecosystems: Go, Cargo, pip/pipx/uv, npm/pnpm/bun/yarn, gem, composer.
- [ ] Version managers: asdf/mise/SDKMAN.
- [ ] Universal managers: Conda, Nix, Snap, Flatpak.
- [ ] Git build with exact revision.
- [ ] Container tag + digest.
- [ ] Local/offline artifact.
- [ ] Private authenticated artifact/source using fake credentials/endpoints.
- [ ] Multi-architecture release assets.
- [ ] WSL/container/Android conditions.
- [ ] OS-version-specific method selection.
- [ ] Candidate fallback after unavailable package/source.
- [~] Candidate failure after preparation to test rollback/ownership. (Source preparation now has executor tests for rollback, ambiguous add outcomes, crash recovery, durable committing journals, shared ownership and post-install failure; prerequisite preparation still needs equivalent coverage.)

For each fixture, test parse -> plan -> explain -> dry-run -> lock expectations. Execution tests may use fake runners where host platform is unavailable.

______________________________________________________________________

## P3.2 — Add method contract/conformance suites

Every method should pass shared tests for the capabilities it advertises.

- [ ] structural validation
- [ ] unsupported-field rejection
- [ ] condition filtering
- [ ] exact candidate identity
- [ ] arbitrary-code classification
- [ ] dry-run purity
- [ ] version semantics
- [ ] source semantics
- [ ] scope/environment semantics
- [ ] lock resolution
- [ ] desired-state check
- [ ] install
- [ ] remove
- [ ] upgrade
- [ ] idempotency
- [ ] error classification
- [ ] secret redaction where applicable

______________________________________________________________________

## P3.3 — Add planner fuzz/property tests

Properties to enforce:

- [ ] Planning never mutates host state.
- [ ] Dry-run never invokes mutating runner operations.
- [x] Serializing a resolved plan never exposes secrets. (`ResolvedInstallPlan.MarshalJSON` deep-redacts every string field without mutating the in-memory plan; fuzz/property coverage exercises credential URLs, query secrets, map keys and argv tokens.)
- [~] An exact locked identity never resolves to a different mutable identity without an explicit update. (`VerifyResolvedPlanAgainstLock` rejects changed version/revision/digest/source/artifact identity with `ErrLockMismatch`; install/CLI consumption of this boundary remains TODO.)
- [x] Unsupported capabilities cannot silently degrade. (`PlanCapabilities`/`MissingPlanCapabilities` have direct fail-closed coverage for every adapter-neutral semantic dimension and combined requirements; partial contracts report the exact unsatisfied capability mask.)
- [ ] A schema-accepted field is represented in normalized intent/plan or rejected as irrelevant.
- [~] Candidate ordering is deterministic. (Universal lock projection/document ordering and local archive destination identity are deterministic; broader planner candidate-order fuzz/conformance remains TODO.)
- [x] Local archive path extraction never escapes the destination root for accepted names. (`safeArchiveTarget` has fuzz/property coverage for traversal, absolute/drive-qualified names, backslash aliases and arbitrary inputs.)

______________________________________________________________________

## P3.4 — Add lifecycle/state invariants

- [~] Installing an already satisfied plan is idempotent. (`localartifact.Install` now checks desired-state identity before mutation; repeated raw/archive installs preserve the existing destination inode when checksum/provenance already matches. Local source and installed-tree verification also bind metadata and bytes to the same inode to close path-swap races. Adapter/executor-wide idempotency remains TODO.)
- [~] Drifted state is reported and reconciled according to command semantics. (`VerificationResult` carries explicit per-field drift tied to the authoritative observation, and lifecycle selection consumes only a validated result; CLI status rendering/runtime adapter migration remain TODO.)
- [~] Removal only removes owned state or explicitly declared external state. (`ResolvedInstallPlan.Validate` now requires absolute canonical Unix/Windows owned paths, collapses Windows case/separator aliases to one ownership identity, rejects duplicate/invalid ownership paths, rejects removal paths not present in the declared owned set, and rejects removal identity/path metadata when removal is declared unsupported; adapter/executor-wide removal conformance remains TODO.)
- [x] Shared sources/prerequisites use refcounts or equivalent dependency tracking. (`OwnedResourceState` has sorted unique dependents with idempotent claim/release semantics; committed preparation and successful dependency edges claim resources into one canonical snapshot, and removal releases one dependent across the complete snapshot. Last-reference depengine-owned sources use plan-aware cleanup; last-reference depengine-owned lazy prerequisites are recursively uninstalled only after durable root intent proves they are not also directly requested. Shared helpers remain until the final owner is removed, explicit removal is blocked while unscheduled tracked owners still depend on a helper, external resources are never garbage-collected, and ownership records are finalized only after host cleanup succeeds. Canonical ownership/refcount snapshots and root intent are checksum-protected state. Fuzz/property tests cover claim order independence and last-dependent removal; CLI regressions cover shared, transitive, root-retained, and explicit-order prerequisite removal.)
- [~] Failed candidate fallback leaves no silent orphan mutation. (The shared preparation journal now distinguishes prepare-mutation-in-progress, commit-in-progress, and rollback-in-progress from confirmed/terminal states. `LockedState` persists `applying` before each prepare mutation and `committing`/`rolling_back` before later host mutations; rollback itself is now incremental, persisting `rollback_applying` before each compensation and `rollback_applied` after success. Automatic replay/rollback is rejected while a prepare, rollback, or commit outcome is ambiguous; restart after confirmed compensations returns only the remaining rollback operations, and either in-flight mutation can leave its WAL marker only through explicit persisted `applied`/`not_applied` evidence for the exact mutation ID. `FinalizeRollback` refuses incomplete/unconfirmed compensation and records every retained source/prerequisite in the ownership snapshot, including zero-ref orphan state that must remain visible for reporting/cleanup. The local-artifact transactional replace path also attempts to roll back a successfully renamed replacement and restore the prior destination if backup cleanup fails; if restoring the old destination itself fails after the new payload was moved back to staging, it best-effort recommits the new payload so the destination is not silently left absent. Executor-wide source/prerequisite fallback persistence/integration remains TODO.)
- [~] Upgrade does not bypass lock/version/source constraints. (`ReconcileLockedPlan` fails on lock candidate/version/source/target drift before deriving a lifecycle transition and compares observed state against the pinned immutable identity; CLI/runtime upgrade wiring remains TODO.)
- [~] Undo/state snapshots include all newly introduced mutation types. (`OwnedResources` and active `PreparationJournals` are now serialized in the checksum-protected state file and copied/restored by `LoadSnapshot`; snapshot loading verifies the original state checksum instead of bypassing integrity validation. Journal shape is validated on load, and locked transition helpers bind the journal to the current `PreparationPlan` before every persisted lifecycle change; crash recovery now has a pure/state-level decision boundary for blocked ambiguous `applying` and `rollback_applying`, explicit evidence-driven resolution of those exact in-flight mutations, `committing` reconciliation plus an explicit `not applied` escape only when stronger method-specific evidence proves no commit operation took effect, and incremental `rolling_back` resumption that omits already confirmed compensations; the executor still needs to perform the read-only probe, execute each returned rollback operation through the durable WAL boundary, and surface/resolve blocked recovery states. State/snapshot readers also reject unknown state format versions through the centralized `formatversion` policy rather than attempting implicit migration; state v4 retains the mandatory checksum and exact preparation-plan/WAL pairing while adding root intent for prerequisite cleanup, so older recovery/ownership state cannot be guessed/reconstructed. Mutation-aware undo/executor integration remains TODO.)

______________________________________________________________________

## P3.5 — Document the support boundary explicitly

The docs should state what depengine intends to model and what it intentionally does not.

- [x] Typed package managers and ecosystems are the preferred path.
- [x] Generic artifact installation is supported with explicit ownership and verification.
- [x] Opaque vendor installers/scripts are an explicit unsafe escape hatch, not equivalent to a fully modeled method.
- [x] Not every package manager can guarantee the same level of reproducibility; expose capability differences.
- [x] Explain the distinction between desired version, resolved version, and locked immutable identity.
- [x] Explain scope vs environment/profile.
- [x] Explain sources/registries vs host source mutation.

______________________________________________________________________

## P3.6 — Update product claims to match actual guarantees

Audit README/docs/CLI text for statements such as “same tools, same versions”.

- [x] Until universal locking exists, qualify claims to the methods actually pinned.
- [ ] Once universal locking exists, add tests that enforce the promise.
- [x] Ensure dry-run wording matches real side-effect guarantees.
- [ ] Ensure “installed” means desired state satisfied, not merely executable found.

______________________________________________________________________

## P3.7 — Add security/threat-model documentation

Cover:

- [x] arbitrary code and hooks/builds;
- [x] package/source trust;
- [x] checksums/signatures;
- [x] private registry/artifact credentials;
- [x] secret redaction;
- [x] downloader selection;
- [x] installer elevation;
- [x] source/key ownership;
- [x] unsafe EXE/script installers;
- [x] lockfile integrity expectations.

______________________________________________________________________

# Suggested implementation order

Do not implement the backlog strictly by adapter. The architectural work should land before broadening method coverage.

## Phase A — stop correctness/security leaks

- [~] P0.1 dry-run purity
- [x] P0.2 arbitrary-code gate
- [~] P0.3 validation/execution parity
- [~] P0.4 ignored-field invariant + asdf fix
- [~] P0.5 authenticated download capability

## Phase B — establish the semantic core

- [~] P1.1 `ResolvedInstallPlan`
- [~] P1.11 method capabilities
- [~] P1.2 version/revision model
- [~] P1.3 desired-state verification
- [~] P1.5 scope
- [~] P1.6 environment/profile
- [x] P1.7 host version conditions

## Phase C — make resolution reproducible

- [~] P1.8 generalized sources/registries
- [~] P1.9 transactional preparation/ownership
- [~] P1.4 universal lockfile
- [~] P1.10 hook semantics
- [x] P1.12 native fallback semantics
- [x] P1.13 preference vs allow-list semantics

## Phase D — expand method fidelity

- [ ] macOS PKG/DMG
- [ ] Windows EXE/MSIX/AppX
- [ ] WinGet/Chocolatey/Scoop/Homebrew fidelity
- [ ] Cargo/Go/Python/Node/Ruby/PHP ecosystems
- [ ] Conda/version managers/Snap/Flatpak
- [ ] Git/container/Nix
- [~] local/offline artifacts
- [ ] installer-script escape hatch only if still necessary

## Phase E — freeze preparation

- [ ] adversarial fixture matrix
- [~] conformance/property/lifecycle tests
- [x] documentation and security model
- [x] compatibility policy
- [ ] run the v1 freeze gate below

______________________________________________________________________

# v1 schema freeze gate

Do **not** declare the installation schema “100% ready”, stable, or future-proof until every mandatory gate below is true.

## Correctness and safety

- [ ] `--dry-run` is proven side-effect-free.
- [ ] Every arbitrary-code path is gated.
- [ ] Planner, validator, explainer, dry-run and executor share the same semantics.
- [ ] Unsupported installer formats fail before mutation.
- [ ] Secrets are never serialized/logged.

## Declarative completeness

- [ ] Exact version/revision intent is representable where underlying tooling supports it.
- [ ] Desired-state checks verify version/revision, not just presence.
- [ ] Scope is first-class.
- [ ] Environment/profile targeting is first-class where relevant.
- [ ] Sources/registries/channels are expressible without arbitrary shell hooks for supported managers.
- [ ] OS/distro version conditions are expressible.
- [ ] Local/offline artifacts are expressible.

## Reproducibility

- [ ] Mutable references can resolve to immutable lock identities across supported method classes.
- [ ] Git branches/tags lock to commits when requested.
- [ ] Container tags lock to digests when requested.
- [ ] Ecosystem/native versions are captured where the manager exposes deterministic resolution.
- [ ] Reinstall from unchanged manifest+lock cannot silently move to newer identities.

## Lifecycle

- [ ] Candidate probing is non-mutating.
- [ ] Candidate-specific preparation cannot leave silent orphan state on fallback/failure.
- [ ] Shared sources/prerequisites have ownership/refcount semantics.
- [ ] Install/check/upgrade/remove operate against the same target scope/environment/source identity.
- [ ] Hooks have precise transition semantics and never substitute for declarative state accidentally.

## Ergonomics

- [ ] Common user-scoped artifact installs need no platform-specific path knowledge.
- [ ] `schema.example.toml` demonstrates the short, recommended path first.
- [ ] Inferred candidates are obvious to users.
- [ ] Preference versus allow-list semantics are obvious.
- [ ] Advanced fields (`strip_components`, `entrypoints`, ownership paths, sources) have precise examples.

## Extensibility

- [ ] New methods advertise capabilities rather than requiring cross-cutting method-name switches.
- [ ] Method conformance tests are mandatory.
- [ ] A new schema field cannot land without semantic behavior tests.
- [ ] A new adapter can participate in plan/check/lock/dry-run/remove without bespoke command-level integration.

## Compatibility

- [x] Manifest compatibility policy for v1+ is written and intentional.
- [x] Lock/state compatibility policy is written separately.
- [x] `schema_version` has meaningful future semantics.
- [ ] All public docs and examples match the frozen contract.

______________________________________________________________________

# Definition of done for each backlog item

An item is not complete merely because parsing succeeds or an adapter accepts a new field. For every item above, completion requires, as applicable:

- [ ] schema/model change;
- [ ] strict validation;
- [ ] resolved-plan representation;
- [ ] capability declaration;
- [ ] dry-run/explain output;
- [ ] execution behavior;
- [ ] desired-state verification;
- [ ] lock behavior;
- [ ] removal/undo/state ownership behavior;
- [ ] unit tests;
- [ ] conformance/regression tests;
- [ ] example manifest update;
- [ ] JSON Schema regeneration/update;
- [ ] schema reference/CLI documentation update;
- [ ] compatibility note if public syntax changed.

______________________________________________________________________

# Explicit non-goals while closing this backlog

- [ ] Do not turn depengine into a general-purpose configuration-management language.
- [ ] Do not add arbitrary package-manager CLI flags merely to achieve feature parity quickly.
- [ ] Do not model every vendor installer before the shared plan/version/scope/environment abstractions exist.
- [ ] Do not preserve accidental pre-v1 behavior solely for backwards compatibility while the schema contract is still intentionally breakable.
- [ ] Do not claim deterministic reproducibility for managers whose resolved identity cannot yet be locked and verified.

______________________________________________________________________

# Success criterion

The schema is ready to freeze when a manifest describes **intent**, adapters resolve that intent into one inspectable plan, the lockfile captures mutable resolution into reproducible identity, execution reconciles the machine to that identity, and every supported method can clearly state which dimensions of version/source/scope/environment/platform it can and cannot honor.

At that point, adding another installer or package manager should usually be an adapter/capability implementation—not a redesign of the manifest language.

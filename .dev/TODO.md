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

- [ ] Centralize dry-run behavior at the execution boundary; do not rely on every adapter/hook remembering to check it.
- [ ] Prevent `pre_install` from executing in dry-run mode.
- [ ] Prevent `post_install` from executing in dry-run mode.
- [ ] Prevent native package-manager sync (`apt-get update`, equivalent operations) in dry-run mode.
- [ ] Prevent source setup/removal in dry-run mode.
- [ ] Prevent prerequisite installation in dry-run mode.
- [ ] Audit download/cache behavior and define whether dry-run may perform network reads. Prefer no host mutations; if cache writes are allowed, document them explicitly. Stronger target: no writes at all.
- [ ] Audit all runner entrypoints so a future adapter cannot accidentally mutate during dry-run.
- [ ] Make output distinguish between “would resolve/check” and “would mutate”.

**Acceptance criteria**

- [ ] A dry-run over a manifest containing hooks, sources, prerequisites, native sync, HTTP/GitHub artifacts, Git builds, MSI, ecosystem managers, and containers causes zero externally visible host mutations.
- [ ] A regression test proves a sentinel file is not created by any hook during dry-run.
- [ ] A fake runner test proves no mutating package-manager command is invoked during dry-run.
- [ ] CLI wording never claims “no changes” unless this invariant is actually enforced.

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

- [ ] Replace type-specific dangerous-field detection with one canonical semantic predicate.
- [ ] Mark every executable form of `build`, hooks, custom commands, installer scripts, or future command-bearing fields as arbitrary code.
- [ ] Make the gate operate on resolved method semantics, not raw TOML representation.
- [ ] Ensure aliases/shorthands cannot bypass the gate.
- [ ] Define whether shell-string commands and argv-form commands have different risk labels; both must require explicit permission when arbitrary code is executed.

**Acceptance criteria**

- [ ] String-form `build` requires `--allow-arbitrary-code`.
- [ ] Structured `build.run` requires `--allow-arbitrary-code`.
- [ ] All hook forms require the same gate.
- [ ] A table-driven test enumerates every command-bearing field and fails if any one is not gated.

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

- [ ] Introduce one canonical resolved plan representation; see P1.1.
- [ ] Make structural/semantic validation verify every invariant needed before execution.
- [x] Move shared artifact validation into shared artifact contracts rather than hard-coded method-name switches.
- [x] Validate URI schemes consistently for every download-backed method.
- [ ] Reject unsupported installer/container/archive formats before execution.
- [ ] Make `why` use the same availability/resolution result as installation planning.
- [ ] Make dry-run render the exact executable plan, not a best-effort approximation.
- [ ] Audit error messages so they never recommend a non-existent method.

**Acceptance criteria**

- [ ] Any manifest accepted by `validate` either produces a valid resolved plan or fails only because of runtime facts that cannot be known statically.
- [ ] `why` and `dry-run` cannot call a candidate “ready” if the planner has already determined it is unavailable.
- [x] Unsupported `.pkg`, `.dmg`, `.exe`, MSIX/AppX, or other installer types fail with accurate, actionable diagnostics.
- [ ] URL/path validation is implemented once and covered by shared contract tests.

**Likely areas**

`pkg/validate/*`, `pkg/methodkind/*`, `pkg/exec/explain.go`, `graph_why.go`, HTTP/MSI adapters.

______________________________________________________________________

## P0.4 — Add a schema-to-runtime “no ignored fields” invariant

**Observed gap**

At least one adapter accepts a field that does not affect actual installation behavior: `asdf.version` is represented but installation uses `latest` instead. This is more dangerous than a missing feature because the manifest appears precise while runtime behavior differs.

**Work**

- [ ] Create a conformance mechanism that enumerates all accepted fields for each method contract.
- [ ] Require every field to influence the resolved plan, validation, execution, or verification.
- [x] Fix `asdf.version` handling.
- [ ] Audit SDKMAN, Cargo, Go, Conda, container, Git, Snap, Flatpak, native and artifact adapters for similar discrepancies.
  - [x] SDKMAN: exact `version` now governs both install and verification.
  - [x] Cargo
  - [x] Go
  - [x] Conda
  - [x] container
  - [x] Git
  - [x] Snap
  - [ ] Flatpak
  - [ ] native
  - [ ] artifact adapters
- [ ] Prevent future contract additions without corresponding semantic tests.

**Acceptance criteria**

- [x] `asdf.version = "X"` installs/checks X rather than silently using latest.
- [ ] Contract tests fail when a schema field is accepted but ignored.
- [ ] Every adapter has at least one behavior test per non-trivial declared field.

______________________________________________________________________

## P0.5 — Fix authenticated/private artifact behavior

**Observed gap**

The Go HTTP downloader can attach GitHub authentication without exposing the token on a command line, but downloader selection may prefer `curl`/`wget`, which changes authentication capability depending on host tooling.

**Work**

- [ ] Make authentication requirements part of planning/capability selection.
- [x] Do not select a downloader that cannot satisfy required auth semantics.
- [ ] Ensure credentials are not passed in argv, logs, error text, lockfiles, or state files.
- [ ] Define explicit secure secret references for future private registries/sources rather than literal secrets in manifests.
- [x] Add private GitHub release tests using fake servers/runners.

**Acceptance criteria**

- [x] Presence of `curl`/`wget` cannot cause a private GitHub install to lose authentication support.
- [ ] Tokens/secrets never appear in logged commands or serialized project state.
- [ ] Downloader capability mismatches fail during planning with a clear explanation.

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

- [ ] Define a versioned internal `ResolvedInstallPlan` model.
- [ ] Include tool identity, selected candidate/method, resolved version/revision/digest, source/registry, scope, environment/profile, architecture/platform, artifacts/checksums, prerequisites, source mutations, owned paths, executable entrypoints, arbitrary-code steps, and removal metadata as applicable.
- [ ] Distinguish read-only resolution operations from mutating execution operations.
- [ ] Make planner output serializable for tests/debugging, while avoiding secrets.
- [ ] Refactor `validate` to build/check the plan where possible.
- [ ] Refactor `why` to explain candidate elimination and selected plan.
- [ ] Refactor dry-run to print the exact plan.
- [ ] Refactor install/upgrade/status/remove around the same semantic object.
- [ ] Define planner error classes: invalid manifest, unsupported capability, unavailable candidate, resolution failure, auth requirement, host incompatibility.

**Acceptance criteria**

- [ ] There is exactly one method-specific resolution path for a candidate.
- [ ] `why`, `dry-run`, and `install` agree on selected candidate and all resolved identity fields.
- [ ] A golden-test suite snapshots representative resolved plans.

______________________________________________________________________

## P1.2 — Define a cross-method version/revision model

**Problem**

`version` is not currently a first-class property of installation. Different adapters variously reject it, ignore it, force `latest`, partially honor it, or only check for any installed version.

**Work**

- [ ] Define a shared desired-version model that can represent at least:
  - exact semantic/package version;
  - version constraint/range where supported;
  - channel/track/risk;
  - Git tag/branch/revision/commit;
  - container tag and immutable digest;
  - rolling/latest intent.
- [ ] Decide which forms are portable across methods and which remain method-specific.
- [ ] Define normalization rules so `latest`, channels, constraints, revisions and exact versions are unambiguous.
- [ ] Define method capability declarations for supported version modes.
- [ ] Define behavior when a preferred method cannot satisfy the requested version semantics: eliminate candidate rather than silently weakening intent.
- [ ] Update verification to compare actual state against requested/resolved state.

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

- [ ] Replace boolean/presence-oriented checks with desired-state verification results.
- [ ] Report actual version/revision/source/scope/environment when discoverable.
- [ ] Distinguish satisfied, absent, drifted, unknown/unverifiable, and broken states.
- [ ] Make upgrade logic consume drift information instead of independently re-resolving intent.
- [ ] Define fallback behavior when a manager cannot reliably report installed version/source.

**Acceptance criteria**

- [ ] Installing version A while version B is present is not reported as satisfied.
- [ ] SDKMAN/asdf/mise checks verify the requested candidate/version.
- [ ] Container checks can distinguish mutable tag identity from pinned digest identity.
- [ ] Status output explains drift rather than collapsing it to installed/not-installed.

______________________________________________________________________

## P1.4 — Make the lockfile universal or narrow the product promise explicitly

**Problem**

The current lock resolver primarily pins release/artifact placeholders/checksums. Package-manager, language-manager, Git branch, container tag and version-manager installs can remain unpinned, while user-facing language implies “same tools, same versions”.

**Preferred direction**

Make the lockfile the immutable-resolution projection of `ResolvedInstallPlan`.

**Work**

- [ ] Define per-method lock identity:
  - native/ecosystem package -> resolved package version + source/registry where stable;
  - Go -> module/package version;
  - Cargo -> resolved crate version/source;
  - Git -> commit SHA;
  - container -> digest;
  - GitHub/HTTP -> resolved URL/release/asset + checksum;
  - asdf/mise/SDKMAN -> concrete tool version;
  - Snap/Flatpak -> resolved channel/branch/version when available.
- [ ] Record target architecture/platform if it affects resolution.
- [ ] Define behavior for managers that cannot provide stable resolution.
- [ ] Ensure lock generation does not execute installation.
- [ ] Ensure install consumes lock identity rather than resolving “latest” again.
- [ ] Add lock schema/versioning and migration policy before public freeze.
- [ ] If universal locking is intentionally out of scope, revise CLI/README guarantees to say exactly what is pinned.

**Acceptance criteria**

- [ ] `depengine update` produces meaningful pins for every supported mutable method class.
- [ ] Reinstalling from an unchanged lock does not silently pick a newer release/version/digest.
- [ ] Lockfile never stores credentials.

______________________________________________________________________

## P1.5 — Add a first-class installation `scope`

**Problem**

User/system/global behavior is currently encoded through method-specific flags or raw paths. Artifact defaults also differ between archives and raw binaries, making the same intent platform-dependent and surprising.

**Work**

- [ ] Define portable scope vocabulary, initially at least `user` and `system`; add manager-specific/global distinctions only where semantically necessary.
- [ ] Define method capability support for scope.
- [ ] Resolve scope into platform-native paths/flags in adapters.
- [ ] Remove Unix paths from the normal authoring path whenever scope is sufficient.
- [ ] Keep `extract_to`, `link_dir`, install root, etc. as advanced overrides.
- [ ] Define precedence between explicit paths and scope.
- [ ] Define Windows user/system path behavior explicitly.

**Acceptance criteria**

- [ ] A user-scoped raw binary and a user-scoped archive resolve consistently without manually specifying `~/.local/bin`.
- [ ] Windows manifests do not need Unix-specific path assumptions.
- [ ] Unsupported scope on a method is detected at planning time.

______________________________________________________________________

## P1.6 — Add first-class `environment` / `profile` targeting

**Problem**

Scope does not identify virtual/project/profile targets such as Conda environments, Python environments, npm project/global contexts, Cargo install roots, Nix profiles, or version-manager profiles.

**Work**

- [ ] Define an environment/profile concept separate from user/system scope.
- [ ] Avoid a single overloaded string if target semantics differ materially by ecosystem; use typed method fields where appropriate while preserving a common plan representation.
- [ ] Make the selected environment/profile part of desired-state identity and locking where relevant.
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

- [ ] Add normalized OS/distro version facts from existing detection sources (`VERSION_ID`, `sw_vers`, Windows version/build information).
- [ ] Define comparison semantics for versions that are not strict SemVer.
- [ ] Support at least exact/min/max or a clearly constrained version expression.
- [ ] Keep the condition DSL bounded; do not introduce an arbitrary expression language unnecessarily.
- [ ] Test Ubuntu/Fedora/macOS/Windows version conditions.

**Acceptance criteria**

- [ ] Manifests can distinguish e.g. Ubuntu 22.04 vs 24.04, macOS major versions, and Windows build ranges without hooks.
- [ ] Version comparison rules are documented and deterministic.

______________________________________________________________________

## P1.8 — Generalize package/source/registry modeling

**Problem**

Current `sources` support is strong for the previously identified PPA/COPR/Scoop bucket/Brew tap cases, but it is not yet a general model for package sources, registries, channels and remotes.

**Work**

- [ ] Define the distinction between:
  - host source configuration mutation;
  - per-install source selection;
  - registry/index/channel selection;
  - trusted signing/key material.
- [ ] Add typed source support where required for WinGet, Chocolatey, Cargo registries, Python indexes, Conda channels, Flatpak remotes, apt/dnf arbitrary repositories, etc.
- [ ] Define source identity and ownership.
- [ ] Define trust/fingerprint/key verification semantics rather than accepting opaque shell snippets.
- [ ] Add secure secret references for authenticated sources.
- [ ] Make source selection part of desired state and lock identity when it affects resolution.

**Acceptance criteria**

- [ ] A package with the same name in two registries can be deterministically pinned to the intended registry/source.
- [ ] Source changes are explainable in dry-run and reversible when depengine owns them.
- [ ] Credentials remain external to committed manifests/locks.

______________________________________________________________________

## P1.9 — Make candidate preparation transactional or explicitly owned

**Problem**

Candidate evaluation can mutate the machine by adding sources or installing prerequisites before the final candidate succeeds. A later failure/fallback does not necessarily undo those mutations.

**Work**

- [ ] Separate read-only candidate probing from mutating preparation.
- [ ] Define prepare/commit/rollback lifecycle or another ownership/refcount model.
- [ ] Do not add sources merely to answer “could this candidate work?” unless execution has committed to that candidate.
- [ ] Track candidate-specific prerequisites/sources as owned state when created by depengine.
- [ ] On candidate failure, roll back safe reversible mutations or retain them with explicit state/reporting if rollback is impossible.
- [ ] Define shared-source refcount semantics so removal of one tool does not remove a source still needed by another.

**Acceptance criteria**

- [ ] Falling back from candidate A to B does not leave silent orphan sources/prerequisites from A.
- [ ] `remove` can safely clean depengine-owned shared sources only when no dependents remain.
- [ ] Dry-run shows planned prepare/commit mutations without performing them.

______________________________________________________________________

## P1.10 — Clarify hooks versus declarative ensure actions

**Problem**

Hooks are transition events, not durable desired state. A global `pre_install` may run before knowing the winning candidate and may run even when it is not semantically appropriate for a specific method. Post-install only covers some transitions.

**Work**

- [ ] Specify exact hook lifecycle semantics: when hooks run, when they do not run, and on which state transitions.
- [ ] Support candidate/method-local hooks only if there is a strong use case; otherwise prefer declarative primitives.
- [ ] Introduce a separate concept for durable “ensure this state exists” behavior if required.
- [ ] Make hooks part of arbitrary-code gating and resolved-plan visibility.
- [ ] Define rollback/error semantics for hook failure.
- [ ] Ensure hooks never masquerade as idempotent state unless an explicit check is provided.

**Acceptance criteria**

- [ ] A hook needed only by an apt candidate cannot accidentally run when GitHub fallback wins.
- [ ] Status does not report a tool healthy merely because a one-time hook previously ran.

______________________________________________________________________

## P1.11 — Make method capability metadata first-class

**Problem**

Adding cross-cutting fields manually to every method contract will become brittle and will not explain why a candidate is incapable of honoring a request.

**Work**

- [ ] Extend method contracts with capabilities such as:
  - exact version / constraints / channels / revisions;
  - source/registry selection;
  - user/system scope;
  - environment/profile targeting;
  - architecture/target selection;
  - immutable resolution/locking;
  - check/remove/upgrade support;
  - arbitrary-code execution;
  - offline/local artifact support;
  - auth support.
- [ ] Use capabilities during candidate filtering/planning.
- [ ] Expose capability mismatch reasons in `why`.
- [ ] Generate relevant JSON Schema/docs from the same contract data where practical.

**Acceptance criteria**

- [ ] Requesting a capability unsupported by one candidate eliminates it deterministically.
- [ ] There is no growing collection of method-name conditionals for cross-cutting semantics.

______________________________________________________________________

## P1.12 — Revisit implicit native fallback semantics

**Problem**

Declaring a non-native method can implicitly inject a native candidate, so “methods declared” and “methods executable” differ unless `method_only` is used.

**Work**

- [ ] Decide and document one principle for implicit native fallback.
- [ ] Strong option: allow native inference only for simple shorthand declarations; full method tables mean exactly what they declare.
- [ ] Alternative: make fallback behavior a visible defaults setting.
- [ ] Ensure `why` clearly identifies inferred versus explicitly declared candidates.
- [ ] Add tests for `brew`, Go/Cargo shorthand, explicit method subtables, and `method_only`.

**Acceptance criteria**

- [ ] Authors can predict the candidate set from the manifest without hidden method injection rules.
- [ ] Existing convenience remains available explicitly if desired.

______________________________________________________________________

## P1.13 — Clarify preference ordering versus allow-list semantics

**Problem**

`method_order`/`method_prefer` behave as preference prefixes; omitted methods remain eligible. `method_only` is the actual allow-list. The distinction is valid but easy to misread.

**Work**

- [ ] Rename or document global ordering so “order” cannot reasonably be read as exhaustive.
- [ ] Consider `method_prefer` as the canonical term globally as well as per-tool.
- [ ] Keep `method_only` as the explicit allow-list.
- [ ] Make `why` show whether a method is lower-priority versus disallowed.

**Acceptance criteria**

- [ ] Documentation and examples make preference vs eligibility unambiguous.
- [ ] Tests cover omitted methods remaining eligible under preference-only configuration.

______________________________________________________________________

## P1.14 — Decide schema compatibility/freeze policy only after semantic stabilization

**Problem**

Current specs explicitly say only the latest schema contract is supported and schema changes may break older files. That is reasonable during DSL formation, but it is not a future-proof public compatibility promise.

**Work**

- [ ] Keep current breakable policy while P0/P1 work is underway.
- [ ] Define what `schema_version` will mean once v1 freezes.
- [ ] Decide whether v2+ will use parser dispatch, migration tooling, deprecation windows, or only explicit manual migration.
- [ ] Define backwards-compatibility policy for lock/state formats separately from manifest schema.
- [ ] Do not promise compatibility before the freeze gate is met.

**Acceptance criteria**

- [ ] The public documentation accurately states compatibility guarantees.
- [ ] `schema_version` has a stable semantic purpose rather than being a constant-only validator.

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

- [ ] Add typed version selection.
- [ ] Add source selection.
- [ ] Add user/machine scope.
- [ ] Add architecture selection where meaningful.
- [ ] Represent installer-type/override only if it can be modeled safely without generic arbitrary args.
- [ ] Verify installed package identity/version using WinGet data rather than executable presence alone.

______________________________________________________________________

## P2.4 — Improve Chocolatey fidelity

**Work**

- [ ] Add exact/package version.
- [ ] Add source selection.
- [ ] Model x86/architecture if needed.
- [ ] Evaluate typed package parameters and installer parameters; do not expose arbitrary argument bags by default.
- [ ] Verify actual installed package version/source.

______________________________________________________________________

## P2.5 — Improve Scoop fidelity

**Work**

- [ ] Add version support where Scoop semantics permit it.
- [ ] Model bucket/source identity consistently with generalized sources.
- [ ] Support user/global scope where safe.
- [ ] Model architecture selection where needed.
- [ ] Verify package version and bucket/source where available.

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

- [ ] Add version/constraint.
- [ ] Add registry selection.
- [ ] Add `git`, branch/tag/rev semantics without conflating with generic Git build method.
- [ ] Add feature selection, `--no-default-features`, selected bins, target and install root only as typed fields with clear portability.
- [ ] Make lockfile capture concrete crate version/source/revision.
- [ ] Verify installed crate version, not just binary presence.

______________________________________________________________________

## P2.8 — Expand Go tool installation semantics

**Work**

- [ ] Stop forcing `@latest` when exact version is requested.
- [ ] Add explicit module/package version field.
- [ ] Define tool package path vs module identity semantics.
- [ ] Lock concrete module version.
- [ ] Verify installed version where discoverable; define limitations when binary metadata cannot prove it.

______________________________________________________________________

## P2.9 — Audit Python/Node/Ruby/PHP ecosystem adapters for version/source/scope/environment fidelity

**Targets**

pip, pipx, uv, npm, pnpm, bun, yarn, gem, composer and related adapters.

**Work**

- [ ] For each adapter, document supported desired-state dimensions.
- [ ] Add exact version/constraint where native tooling supports it.
- [ ] Add registry/index/source selection where needed.
- [ ] Distinguish global/user/project/environment installation.
- [ ] Make checks verify requested package version/environment.
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
- [ ] SDKMAN checks must verify the requested candidate/version, not merely any current version.
- [ ] Lock exact resolved tool versions.

______________________________________________________________________

## P2.12 — Correct Snap channel modeling

**Gap**

Snap channel semantics are richer than the four risk names; tracks and branches matter.

**Work**

- [ ] Model channel as track/risk/branch or another structure matching Snap semantics.
- [ ] Preserve simple shorthand for `stable`, `candidate`, `beta`, `edge` if desired.
- [ ] Include confinement/classic requirements separately.
- [ ] Verify installed tracking channel/revision where available.

______________________________________________________________________

## P2.13 — Complete Flatpak remote/branch/scope semantics

**Work**

- [ ] Remove hard-coded assumption that every install comes from Flathub.
- [ ] Add remote selection and ownership.
- [ ] Add branch selection.
- [ ] Add user/system scope.
- [ ] Verify application ref + branch + origin.

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

- [ ] Add immutable digest representation.
- [ ] Add registry/source selection.
- [ ] Add platform/architecture selection.
- [ ] Add secure auth references.
- [ ] Lock tags to digests when reproducibility is requested.
- [ ] Verify local image identity by digest where possible.

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

- [ ] Add explicit local-file/path artifact source rather than overloading HTTP URL validation.
- [ ] Resolve relative paths against a documented manifest/project root.
- [ ] Support checksums/signatures for local artifacts too.
- [ ] Ensure lock/state can identify vendored content without embedding machine-specific absolute paths where avoidable.
- [ ] Test offline installation with networking disabled.

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
- [ ] Ensure diagnostics print both label and resolved method kind.
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
- [ ] Candidate failure after preparation to test rollback/ownership.

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
- [ ] Serializing a resolved plan never exposes secrets.
- [ ] An exact locked identity never resolves to a different mutable identity without an explicit update.
- [ ] Unsupported capabilities cannot silently degrade.
- [ ] A schema-accepted field is represented in normalized intent/plan or rejected as irrelevant.
- [ ] Candidate ordering is deterministic.

______________________________________________________________________

## P3.4 — Add lifecycle/state invariants

- [ ] Installing an already satisfied plan is idempotent.
- [ ] Drifted state is reported and reconciled according to command semantics.
- [ ] Removal only removes owned state or explicitly declared external state.
- [ ] Shared sources/prerequisites use refcounts or equivalent dependency tracking.
- [ ] Failed candidate fallback leaves no silent orphan mutation.
- [ ] Upgrade does not bypass lock/version/source constraints.
- [ ] Undo/state snapshots include all newly introduced mutation types.

______________________________________________________________________

## P3.5 — Document the support boundary explicitly

The docs should state what depengine intends to model and what it intentionally does not.

- [ ] Typed package managers and ecosystems are the preferred path.
- [ ] Generic artifact installation is supported with explicit ownership and verification.
- [ ] Opaque vendor installers/scripts are an explicit unsafe escape hatch, not equivalent to a fully modeled method.
- [ ] Not every package manager can guarantee the same level of reproducibility; expose capability differences.
- [ ] Explain the distinction between desired version, resolved version, and locked immutable identity.
- [ ] Explain scope vs environment/profile.
- [ ] Explain sources/registries vs host source mutation.

______________________________________________________________________

## P3.6 — Update product claims to match actual guarantees

Audit README/docs/CLI text for statements such as “same tools, same versions”.

- [ ] Until universal locking exists, qualify claims to the methods actually pinned.
- [ ] Once universal locking exists, add tests that enforce the promise.
- [ ] Ensure dry-run wording matches real side-effect guarantees.
- [ ] Ensure “installed” means desired state satisfied, not merely executable found.

______________________________________________________________________

## P3.7 — Add security/threat-model documentation

Cover:

- [ ] arbitrary code and hooks/builds;
- [ ] package/source trust;
- [ ] checksums/signatures;
- [ ] private registry/artifact credentials;
- [ ] secret redaction;
- [ ] downloader selection;
- [ ] installer elevation;
- [ ] source/key ownership;
- [ ] unsafe EXE/script installers;
- [ ] lockfile integrity expectations.

______________________________________________________________________

# Suggested implementation order

Do not implement the backlog strictly by adapter. The architectural work should land before broadening method coverage.

## Phase A — stop correctness/security leaks

- [ ] P0.1 dry-run purity
- [ ] P0.2 arbitrary-code gate
- [ ] P0.3 validation/execution parity
- [ ] P0.4 ignored-field invariant + asdf fix
- [ ] P0.5 authenticated download capability

## Phase B — establish the semantic core

- [ ] P1.1 `ResolvedInstallPlan`
- [ ] P1.11 method capabilities
- [ ] P1.2 version/revision model
- [ ] P1.3 desired-state verification
- [ ] P1.5 scope
- [ ] P1.6 environment/profile
- [ ] P1.7 host version conditions

## Phase C — make resolution reproducible

- [ ] P1.8 generalized sources/registries
- [ ] P1.9 transactional preparation/ownership
- [ ] P1.4 universal lockfile
- [ ] P1.10 hook semantics
- [ ] P1.12 native fallback semantics
- [ ] P1.13 preference vs allow-list semantics

## Phase D — expand method fidelity

- [ ] macOS PKG/DMG
- [ ] Windows EXE/MSIX/AppX
- [ ] WinGet/Chocolatey/Scoop/Homebrew fidelity
- [ ] Cargo/Go/Python/Node/Ruby/PHP ecosystems
- [ ] Conda/version managers/Snap/Flatpak
- [ ] Git/container/Nix
- [ ] local/offline artifacts
- [ ] installer-script escape hatch only if still necessary

## Phase E — freeze preparation

- [ ] adversarial fixture matrix
- [ ] conformance/property/lifecycle tests
- [ ] documentation and security model
- [ ] compatibility policy
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

- [ ] Manifest compatibility policy for v1+ is written and intentional.
- [ ] Lock/state compatibility policy is written separately.
- [ ] `schema_version` has meaningful future semantics.
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

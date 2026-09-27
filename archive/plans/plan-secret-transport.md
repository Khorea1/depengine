# Plan — typed secret transport for authenticated HTTP artifacts

## Execution status

Completed on 2026-09-23 in `agent/secret-http/integrate-transport`.

- T01-T25 completed.
- Final integration commit: `f61c9f8` (`fix: harden authenticated HTTP transport`).
- Repository validation passed: `go test -race ./...`, `go vet ./...`, `golangci-lint run`.
- Final review additionally found and fixed URL-only cache reuse for authenticated artifacts; typed authenticated downloads now bypass the download cache so content cannot cross credential contexts.

## Goal

Complete the next P0 slice from `docs/roadmap.md`: extend typed secret references beyond Git-backed `brew-tap`/`scoop-bucket` source preparation to authenticated HTTP artifact downloads.

The implementation must preserve the current security boundary: secret values are resolved only when the selected candidate is actually reached, remain in process memory, are sent only as an HTTP `Authorization: Bearer` header, and never appear in `ResolvedInstallPlan`, lockfiles, state, argv, reports, diagnostics, or logs.

## Scope

In scope:

- typed `secret_ref` support for the `http` artifact method;
- env-backed Bearer token resolution through the existing secret resolver;
- planner/capability representation through `ResolvedInstallPlan.Secrets`;
- authenticated downloads through the in-process Go HTTP backend;
- fallback behavior when authentication cannot be resolved;
- validation, redaction, docs, and regression coverage.

Out of scope:

- new secret providers beyond `env`;
- package-manager credential stores;
- authenticated registries;
- persistent Git credentials;
- extending auth to every artifact adapter in this slice;
- changing existing GitHub token semantics unless required to preserve the shared downloader boundary.

## Execution rules for subagents

- Work in task-specific Worktrunk worktrees; never edit the shared checkout for implementation tasks.
- Keep each task atomic. Do not expand into unrelated P1/P2 roadmap work.
- Before changing a public/schema-facing field, inspect the method contract and strict parser so unsupported methods remain fail-closed.
- A task may consume outputs from its declared dependencies, but should not duplicate their changes.
- `model: gpt 6 luna` applies to every task below.
- Use `reasoning_effort: high` only for tasks marked very complex; all others use `medium`.

## Tasks

### T01 — Map the authenticated HTTP execution path

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: none
- files to inspect: `internal/config`, `internal/planner`, `internal/methodkind`, `internal/plan`, `internal/exec`, `internal/httpdownload`
- action: trace `http` from schema parsing through static intent, capability validation, resolution, reached-candidate execution, downloader selection, retries, checksum sidecars, and reporting.
- deliverable: a short implementation map in task notes identifying the exact schema field path, plan projection point, runtime secret-resolution point, and authenticated request point.
- done when: every write path for the secret reference and every potential read path for the secret value is identified.

### T02 — Define the minimal auth contract for `http`

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T01
- files: `internal/methodkind/methodkind.go`, `internal/methodkind/field_semantics.go`, relevant contract tests
- action: define how `secret_ref` is exposed only by the intended `http` contract without accidentally enabling unsupported artifact methods that share download fields.
- constraints: reuse `config.SecretReference`; keep Bearer as the only transport in this slice; do not create a generic free-form header mechanism.
- deliverable: contract change plus focused contract tests proving `http` accepts the field and unsupported methods reject it.
- done when: field ownership is explicit in method capabilities/contracts and no method-name conditional is added where contract metadata can express the rule.

### T03 — Parse and strictly validate method-level `secret_ref`

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T02
- files: `internal/config/strict.go`, `internal/config/parse.go`, `internal/config/model.go`, `internal/config/jsonschema.go`
- action: parse the `http` method's typed secret reference and apply the existing provider/name validation semantics used by source secret references.
- negative cases: missing provider/name, empty values, surrounding whitespace, NUL, unknown fields, wrong type, unsupported method placement.
- done when: malformed or misplaced auth is rejected during configuration validation and literal secret values cannot be expressed by the new field.

### T04 — Add configuration tests for HTTP secret references

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T03
- files: config tests near `internal/config/install_capabilities_test.go` and strict-contract coverage
- action: add table-driven tests for accepted `http.secret_ref` and all rejection cases from T03.
- done when: tests prove the parser produces a typed reference and strict validation rejects unsupported placements and unknown fields.

### T05 — Project HTTP auth requirements into the resolved plan

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T03
- files: `internal/planner/artifact.go`, `internal/planner/intent.go`, `internal/plan/plan.go`
- action: project only the secret reference identity into `ResolvedInstallPlan.Secrets`; never copy the resolved secret value into any plan structure.
- constraint: preserve clone/validation behavior and avoid duplicate refs if the planner already has a shared append helper.
- done when: static planning of an authenticated HTTP candidate produces the same artifact identity plus the expected typed secret requirement.

### T06 — Add planner projection and plan-invariant tests

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T05
- files: planner tests and plan validation/clone tests
- action: test projection, clone isolation, deduplication if applicable, and absence of secret values from serialized/static plan data.
- done when: dry/static plan tests can assert the reference is present while arbitrary token material is absent.

### T07 — Generalize auth capability recognition without weakening fail-closed behavior

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T02, T05
- files: `internal/methodkind/capability_plan.go`, `internal/methodkind/capability_requirements.go`, capability tests
- action: replace the current source-only auth exception with a narrow shared-auth recognition rule that accepts exactly the transports depengine itself can securely execute: existing authenticated source setup plus the new authenticated HTTP artifact path.
- constraints: all unmatched `Secrets`/`SecretRef` combinations must continue to require `CapabilityAuth` and fail closed; do not silently clear auth requirements for generic adapters.
- done when: supported HTTP auth passes requirement checks and synthetic unsupported secret-bearing plans still produce `auth_requirement`.

### T08 — Add capability regression tests

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T07
- files: `internal/methodkind/capability_failclosed_test.go`, `capability_requirements_test.go`, related tests
- action: cover supported HTTP auth, existing source auth, orphan secret refs, unsupported source roles/kinds, and secret-bearing non-HTTP artifact plans.
- done when: tests demonstrate that the expanded exception is exact rather than capability-wide.

### T09 — Design the runtime credential handoff

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T01, T07
- files to inspect: `internal/exec/attempt.go`, `internal/exec/resolution.go`, `internal/exec/executor.go`, `internal/httpdownload/adapter.go`, adapter interfaces
- action: choose the smallest runtime boundary that lets the reached `http` candidate resolve its secret through the executor's configured `SecretResolver` without placing the value in `ResolvedInstallPlan`, `config.MethodCandidate`, command argv, or persisted state.
- preference: use an explicit runtime-only dependency or context owned by execution; avoid global mutable state.
- deliverable: concrete interface/data-flow decision documented in code comments where the boundary is introduced.
- done when: dry-run/why/status paths have no reason to resolve the value, while real reached-candidate install can obtain it.

### T10 — Implement lazy secret resolution for authenticated HTTP candidates

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T09
- files: executor/runtime boundary selected by T09 plus `internal/secret`
- action: resolve the typed ref only after the candidate is reached and immediately before the authenticated download path needs it.
- error classification: preserve existing missing/empty/unsupported-provider semantics and candidate fallback behavior; errors must identify the reference failure without echoing token material.
- done when: unreached candidates and read-only planning paths perform zero secret resolver calls.

### T11 — Add runtime resolution/fallback tests

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T10
- files: `internal/exec` tests, modeled after `source_secret_test.go`
- action: test successful resolution count, missing secret fallback, empty secret fallback, unsupported provider fallback, and unreached-candidate non-resolution.
- done when: resolver call counts prove lazy behavior and reports select the fallback candidate where expected.

### T12 — Add an explicit authenticated Go HTTP request path

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T09
- files: `internal/httpdownload/backend.go`
- action: let the in-process `GoDownloader` attach a caller-supplied Bearer credential for the specific request while preserving existing User-Agent and GitHub authentication behavior.
- constraints: do not put the token in the URL; do not expose it through `Downloader.Download` string arguments that can be logged casually; do not mutate a shared `http.Client` with persistent auth state.
- done when: the credential lives only on the request header for the authenticated transfer.

### T13 — Force secure backend selection for typed HTTP auth

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T12
- files: `internal/httpdownload/backend.go`, `internal/httpdownload/adapter.go`
- action: ensure a typed authenticated HTTP download always uses the in-process Go backend even when `curl` or `wget` exists.
- constraint: unauthenticated downloads keep the current curl -> wget -> Go preference; existing GitHub secure-backend selection must remain correct.
- done when: no typed secret can be exposed through curl/wget argv or environment and existing unauthenticated selection tests still pass.

### T14 — Wire the resolved token into artifact download execution

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T10, T13
- files: `internal/httpdownload/adapter.go` and runtime handoff boundary
- action: pass the ephemeral credential to the authenticated artifact request and keep retries using the same in-memory credential without reserializing it.
- scope boundary: authenticate the primary artifact URL only unless the schema explicitly models separate auth for checksum/signature sidecars; do not implicitly spray the credential to unrelated hosts.
- done when: the primary artifact can be downloaded from an authenticated test server and sidecar behavior remains explicit and safe.

### T15 — Define redirect credential behavior

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T12, T14
- files: `internal/httpdownload/backend.go` tests
- action: verify Go's redirect behavior and enforce a safe policy for `Authorization` across redirects, especially cross-host redirects.
- requirement: credentials must not be forwarded to an unrelated host; same-origin behavior must be intentional and tested.
- done when: redirect tests prove no credential leakage across an origin boundary.

### T16 — Add authenticated download integration tests

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T14, T15
- files: `internal/httpdownload` tests using `httptest.Server`
- action: cover correct Bearer header, absent header for normal downloads, 401/403 handling, retry behavior, secure backend choice when curl/wget are available, and successful file content.
- done when: tests observe request headers server-side without ever printing the token in assertion failure messages.

### T17 — Audit secret non-leakage surfaces

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T10, T14, T16
- files: `internal/exec/report.go`, error paths, plan serialization, lock/state projection, downloader errors, diagnostics
- action: audit every error/report/persistence path reachable from authenticated HTTP installation and add targeted tests for token absence.
- probes: plan intent, resolved plan, execution report, diagnostic output, error strings, lock projection, state persistence, downloader backend calls.
- done when: a sentinel token introduced by tests cannot be found in any user-visible or persisted output.

### T18 — Check lock/state semantics for secret references

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T05, T17
- files: `internal/lock`, `internal/state`, `docs/design/adr-001-universal-lock-projection.md`
- action: verify that secret values are never persisted and determine whether the typed reference identity itself is omitted or retained according to existing lock/state policy.
- constraint: do not broaden lockfile semantics beyond what the ADR and current plan projection require.
- done when: tests or existing invariant coverage demonstrate credential-bearing persisted identity remains rejected.

### T19 — Update schema reference documentation

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T03, T14
- files: `docs/schema-reference.md`
- action: document the exact `http` syntax, env provider, Bearer semantics, lazy resolution, fallback behavior, and limitation to the primary authenticated artifact request.
- done when: users can configure the feature without relying on implementation details.

### T20 — Update security documentation

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T15, T17
- files: `docs/security.md`
- action: document that credentials remain out of URLs/argv/persistence, authenticated downloads use the in-process backend, and redirect handling prevents cross-origin leakage.
- done when: the security doc matches tested behavior exactly.

### T21 — Reconcile README/manpage/generated docs impact

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T19, T20
- files: `README.md`, `docs/depengine.1`, documentation generation sources if applicable
- action: inspect whether user-facing auth summaries or generated schema/manpage material must change; update only source-of-truth files required by project generation rules.
- done when: no user-facing statement contradicts the new supported transport.

### T22 — Update roadmap status precisely

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T17, T19, T20
- files: `docs/roadmap.md`
- action: update the P0 typed-secret item to record authenticated HTTP artifact support while retaining `[~]` if other authenticated operations still lack explicit transport.
- done when: the roadmap states exactly what remains rather than implying all credential transport is finished.

### T23 — Run focused package validation

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T04, T06, T08, T11, T16, T17, T18
- action: run the smallest relevant package tests first for `config`, `planner`, `methodkind`, `exec`, `httpdownload`, `lock`, and `state` as touched.
- done when: all affected-package tests pass and any failure is attributed to a concrete change before proceeding.

### T24 — Run repository validation

- model: gpt 6 luna
- reasoning_effort: medium
- dependencies: T23, T21, T22
- action: run `go test -race ./...`, `go vet ./...`, and `golangci-lint run`.
- constraint: never report a check as passing unless it actually ran; separate known lint baseline findings from regressions introduced by this work.
- done when: build/test/vet pass and lint output has been triaged against the pre-existing baseline.

### T25 — Final security and scope review

- model: gpt 6 luna
- reasoning_effort: high
- complexity: very complex
- dependencies: T24
- action: review the final diff as an attacker and as a maintainer: search for token propagation, argv/env exposure, auth accidentally enabled on other methods, secret resolution in read-only paths, redirect leakage, persistence, and unrelated changes.
- checks: search for the sentinel token patterns used in tests, inspect all new `Authorization` writes, inspect all new `SecretReference` consumers, and compare behavior with `docs/roadmap.md`/`docs/security.md`.
- done when: the implementation is narrowly scoped to authenticated `http` artifacts, all security invariants are backed by tests, and no unrelated behavior changed.

## Suggested dependency waves

Wave 1: T01.

Wave 2: T02.

Wave 3: T03, T05, T07, T09, T12 where dependencies allow; keep overlapping files coordinated.

Wave 4: T04, T06, T08, T10, T13.

Wave 5: T11, T14, T15.

Wave 6: T16, T17.

Wave 7: T18, T19, T20.

Wave 8: T21, T22, T23.

Wave 9: T24.

Wave 10: T25.

## Completion criteria

The slice is complete only when an `http` candidate can declare a typed env-backed Bearer secret reference, planning carries only the reference, execution resolves the value lazily for a reached candidate, the request uses the in-process Go HTTP backend, fallback remains intact, redirects cannot leak the credential cross-origin, no token reaches persisted/user-visible surfaces, documentation matches behavior, and repository validation has run.

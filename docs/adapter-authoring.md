# Adapter authoring checklist

Use this checklist when adding or changing an installation adapter. It is a review contract, not a second schema reference: `internal/methodkind/methodkind.go` is the authoritative method/field contract, and the adapter must implement the existing uniform `exec.AdapterV2` interface. Do not add a parallel schema description or optional semantic-capability interfaces.

## Before implementation

- [ ] Confirm the method kind, fields, aliases, ordering, scopes, requirements, and removal support in `methodkind.Contracts` / `methodkind.Lookup`. Update that contract when declarative support changes; do not duplicate its field definitions here or infer them independently in the adapter.
- [ ] Trace the lifecycle: `planner.BuildCandidateIntent` creates the candidate intent; `plan.ValidateResolution` limits runtime enrichment; `Executor` selects and drives the adapter. Keep schema parsing, planning, resolution, and mutation at their existing boundaries.
- [ ] Register the implementation through the existing executor/bootstrap registry. Do not introduce a new provider framework, subprocess path, or adapter sub-interface.

## Discovery and host compatibility

- [ ] `AdapterV2.Available(ctx, runner)` is a read-only, fast check of backend availability on the host (for example, whether its binary is present). Missing candidate packages or repositories belong in `CheckAvailable`; discovery must not install prerequisites, refresh indexes, or otherwise mutate the host.
- [ ] Run external probes through `run.Runner`, never direct `exec.Command` or another subprocess API. Consider the configured runner, cancellation, output capture, and dry-run behavior.
- [ ] Keep per-tool/method feasibility in `CheckAvailable`; keep OS, architecture, and other host suitability in `CheckHostCompatibility`. Neither check is permission to mutate state.
- [ ] State any unavoidable environmental assumptions and how a failed or inconclusive probe is distinguished from a confirmed negative result.

## Observation

- [ ] `Observe` is read-only. It reports actual host state, not what the adapter expects to be true after an install.
- [ ] Use `plan.Observation.Presence` deliberately: `PresenceAbsent` means the target was successfully checked and is absent; `PresenceUnknown` means evidence is insufficient; `PresenceBroken` means the target or its state is unusable; `PresencePresent` means a target exists. Probe errors are errors, not evidence of absence.
- [ ] Populate `KnownFields` only for identity fields that the probe really observed. Leave unobserved identity dimensions unknown rather than filling them from the request or defaults.
- [ ] Keep diagnostics useful but safe to display. See `plan.Observation`, `plan.VerificationState`, and the reconciliation path in `internal/plan/verification.go`; unknown is not an alias for absent and broken state must not be silently adopted.

## Resolution

- [ ] Start from the candidate intent built by `planner.BuildCandidateIntent`; `ResolvePlan` may enrich only dimensions permitted by `plan.ValidateResolution` (for example resolved version, revision, digest, source, or artifacts).
- [ ] Do not change the requested tool, method/source choice, requested version, scope/target, declarative operations, lifecycle hooks, or ensure actions. Do not choose a different release or target as a hidden planning policy.
- [ ] Make host- or network-derived identity explicit in the returned `ResolvedInstallPlan`, and propagate resolution errors rather than manufacturing a plausible identity. A resolution probe is non-mutating.
- [ ] Preserve the validated result: the executor validates the resolver output against the original intent with `plan.ValidateResolution`.

## Execution and dry-run

- [ ] `InstallResolved` executes the supplied resolved plan. Do not redo lookup or resolution at mutation time if that could select a different artifact, version, repository, or target.
- [ ] Route every mutation through `run.Runner` and the executor's existing gates. Declare and honor arbitrary-code/build-hook requirements through the existing method contract and executor policy; adapters must not bypass those gates.
- [ ] Treat `WithDryRun` as a hard no-mutation boundary, not a hint to the adapter. Dry-run can perform the existing read-only planning/probe work and report the plan, but must not install, remove, refresh indexes, prepare host sources, run hooks/builds, write state, or begin a mutation transaction. Do not call `InstallResolved` in dry-run.
- [ ] Match the execution failure boundary in `internal/exec/attempt.go`: an ordinary install failure may permit the executor's next candidate, but an uncertain result after transactional preparation/commit is not an ordinary retry. Preserve the executor's classification and error; do not retry an ambiguously completed mutation from adapter code.

## Idempotence, preparation, and recovery

- [ ] Make observation and other checks safely repeatable and read-only. Define the target identity used to recognize an already-completed operation so repeated invocation does not install a duplicate or act on a different target.
- [ ] Keep prerequisite/source preparation within the executor's existing transactional preparation path (`internal/exec/attempt.go`, `internal/state/preparation.go`, and `internal/plan/preparation.go`). Do not implement a private journal or treat prepared state as committed installation state.
- [ ] In state-tracked execution, preserve WAL fail-closed recovery: commit intent is persisted before adapter mutation; when the outcome is uncertain, leave it unresolved for recovery rather than rolling it back, retrying it, or reporting success. Recovery observation must determine committed, not-applied, or unresolved state. Library callers with `schemaPath` empty use best-effort preparation without a durable journal or restart recovery; do not assume the tracked-state guarantee applies there.
- [ ] In state-tracked execution, a recovered commit confirmed by reconciliation is terminal for that candidate; do not execute the adapter again. Keep the stable candidate-intent transaction identity intact—do not key recovery on a mutable resolved release.

## Parsing and errors

- [ ] Prefer structured output (such as JSON) when the external tool provides it. If parsing human output is unavoidable, document the version/locale assumptions and test representative success, absence, malformed, and failure responses.
- [ ] Treat exit statuses as part of the external command contract. Distinguish expected “not installed/not found” statuses from operational errors; do not turn arbitrary non-zero exits or parse failures into `PresenceAbsent`.
- [ ] Bound and validate parsed values before they become identity or execution inputs. Return actionable errors with command context, but sanitize captured output before it reaches logs/reports.

## Security

- [ ] Keep secret values out of resolved plans, state, locks, reports, and error strings. Use existing secret references/resolution and shared redaction facilities rather than copying credentials into adapter-owned persistence.
- [ ] Minimize secrets in command arguments, URLs, and captured output. Prefer supported stdin/environment credential paths where the external tool allows them; never log a credential-bearing command or response unredacted.
- [ ] Treat package metadata, remote manifests, and command output as untrusted input. Validate the exact source, artifact, checksum/signature, and target before mutation using existing artifact/security paths.
- [ ] Hooks, builds, and other arbitrary code remain behind the existing explicit execution gate; discovery, observation, and resolution must not run them.

## Removal

- [ ] Implement `CanRemove()` truthfully and align it with `methodkind.Contract.CanRemove`. Advertise removal only when the adapter can target the same installation represented by the plan/observed identity; never claim success by deleting a similarly named but unowned resource.
- [ ] `Remove` must use the established ownership and shared-resource/refcount rules in `internal/state`; unknown or broken observation is not authorization for destructive removal. Preserve state if ownership or target identity cannot be established.
- [ ] Keep install and removal elevation independent. Implement `exec.ElevationRequirer.RequiresElevation` only for install elevation needs and `exec.RemovalElevationRequirer.RequiresRemovalElevation` only for removal; an install needing privilege does not imply that user-scoped removal does. These are session/elevation hooks, not semantic capability negotiation.
- [ ] Route removal mutations through `run.Runner` and the executor's removal path; do not perform privileged or destructive work during `CanRemove`, discovery, or observation.

## Definition of done for an adapter change

- [ ] Declarative contract and explicit bootstrap/registry wiring are updated where required; public method documentation remains aligned with that contract.
- [ ] The adapter implements the complete uniform `AdapterV2`; no new optional interface or runtime mechanism was introduced.
- [ ] Existing adapter/executor conformance coverage passes (`internal/exec/adapter_v2_executor_test.go` and `internal/exec/conformance_test.go`), with focused tests for the changed observation, resolution, install, removal, parsing, or recovery behavior as applicable.
- [ ] Dry-run coverage proves the changed adapter path has no externally visible mutation (`internal/exec/dryrun_matrix_test.go` is the broad guardrail).
- [ ] Tests exercise relevant absent/unknown/broken or error boundaries, and removal/ownership behavior when supported; they do not merely assert that the adapter method was called.
- [ ] No direct subprocess invocation, schema duplication, recovery shortcut, secret leak, or bypass of executor gates remains.

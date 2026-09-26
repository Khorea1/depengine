# Go design opportunities

Status: research note  
Last reviewed: 2026-09-26

depengine already uses contexts for cancellation, `internal/run.Runner` for
subprocesses, `slog` for logging, and `embed` for assets. Release builds use
`CGO_ENABLED=0`; CI runs the race detector, fuzzing, `govulncheck`, `go vet`,
and `golangci-lint`. The project targets Go 1.27 and has native CI coverage
across Linux, macOS, Windows, BSDs, and Android.

The following work would use Go's runtime and toolchain more consistently.
Filesystem confinement, semantic errors, and deterministic concurrency tests
have the clearest correctness benefits. The other ideas need a concrete use
case or measurement before implementation. See [Why Go](why-go.md) for the
language choice.

## Filesystem confinement

Archive extraction accepts paths from local or remote artifacts. The local
artifact installer already rejects absolute paths, `..`, Windows volume
prefixes, symlinks, hard links, duplicate destinations, case collisions, and
unsupported file kinds. It also limits entry count, path depth and length,
and total expansion size, then stages content before replacement.

Some paths are validated and later passed to a separate `os.*` call. A change
to the filesystem between those steps can invalidate the earlier check.
`os.Root`, introduced in Go 1.24, scopes filesystem operations to a directory
tree and rejects traversal or symlink escapes. `internal/container` already
uses `os.OpenRoot`.

Local and HTTP archive extraction now use rooted filesystem operations for
materialization. The local artifact installer retains its portable naming,
collision, resource-limit, and ownership checks. HTTP ZIP, TAR, gzip, and
bzip2 content is decoded in process; XZ and Zstd use external decompression
that streams into the rooted TAR extractor, without giving the subprocess an
extraction destination. Project-relative vendored reads and other
materialization paths may still benefit from a separate audit. Rooted
operations provide confinement; archive policy still belongs to depengine.

## Semantic download errors

Retry policy previously looked for `checksum` in `err.Error()`. A message edit
could therefore change whether a download was retried. As of this review,
`VerifyChecksum` returns `*ChecksumMismatchError` (which unwraps to
`ErrChecksumMismatch`), `GoDownloader` returns `*HTTPStatusError`, and the
retry policy uses `errors.Is` and `errors.As`.

Extend typed classification where the program has structured information.
HTTP 408, 429, and 5xx responses can be retried; a resolved artifact's 404 and
a checksum mismatch should stop attempts. Cancellation should stop them too.
Deadline and temporary transport errors need classification according to the
operation's context. External downloaders may expose only stderr, so their
classification remains best effort.

Human-readable errors can change wording without changing these decisions.

## Deterministic concurrency tests

`testing/synctest` is generally available since Go 1.25. It can run timer and
goroutine interactions under controlled time. Retry tests already use it for
exponential backoff, capped delays, cancellation, and final-error propagation
without wall-clock sleeps.

The next target is executor timeout and cancellation behavior. Tests should
cover workers that are already running when the parent context is cancelled,
dependents that must stay blocked, and worker shutdown. Method and tool
timeouts nest under the run context; tests should check which scope cancels
hooks, preparation, subprocesses, and downloads. Go 1.27's synctest HTTP test
server may help exercise retry and timeout behavior without relying on network
scheduling.

These tests should assert the transition and cleanup that matter, rather than
use a long watchdog as evidence that the program probably finished.

## Goroutine lifecycle

The race detector finds data races, but blocked goroutines can survive a
completed command. Likely points of failure include worker pools, timers,
retries, HTTP bodies, and subprocess waits.

Go 1.27 makes the `goroutineleak` runtime/pprof profile generally available.
Use targeted lifecycle tests for `internal/exec`, cancellation during
preparation or installation, interrupted downloads, and delayed fake runners.
Check that depengine-owned goroutines exit after each operation. These tests
are particularly useful when one test process runs the executor repeatedly.

## Runtime diagnostics

`DEPENGINE_TRACE_ID` and structured logs already support correlation. Add
`runtime/pprof` labels around work such as a tool, method, and phase so CPU,
goroutine, and blocking profiles can identify the operation involved. Labels
must omit credentials and sensitive URLs.

Go 1.25's `runtime/trace.FlightRecorder` can retain recent trace events for an
intermittent failure or timeout. Prototype an optional debug flag or
environment setting that saves a trace to a selected path; inspect the result
with `go tool trace`. Avoid routine trace files on successful installs.

Go 1.27 also adds a standard-library `uuid` package. If a trace ID is absent,
depengine could generate one for logs, contexts, child-process environment,
and diagnostic reports. Treat it as a run identifier, with no persistence
contract.

## Adapter capabilities and generated metadata

The roadmap calls for metadata derived from adapter capabilities. The project
already has `cmd/schema-gen`. A typed descriptor could record whether a
method supports exact versions, locks, removal, sources, credentials, and
network access. Its fields should follow the existing adapter model.

Generate or verify schema metadata, documentation tables, CLI help, and
contract tests from that descriptor. Start with one projection and check for
generated-file drift in CI. For deterministic repository-owned generators,
`//go:generate` provides a standard entry point. A single capability source
would remove the independent method lists that can diverge.

## Small concurrency primitives

Go 1.25 added `sync.WaitGroup.Go`. Review simple launch patterns in
`internal/exec/execute.go` and use it where a goroutine only needs to be
counted and joined.

The executor also aggregates per-tool results, blocks dependents, preserves
candidate fallback, and continues independent work after an adapter error.
Those rules justify its own worker orchestration. A generic group that cancels
on the first error would change installation behavior.

## Benchmarks and PGO

`internal/config/benchmark_test.go` uses the traditional `b.N` loop. Review
whether Go 1.24's `testing.B.Loop` simplifies those benchmarks, with setup
kept outside the measured body.

Add representative benchmarks for graph construction, candidate selection,
plan construction, lock projection, observation and reconciliation, and state
serialization. A large schema should include many tools, candidates,
conditions, and dependency edges. Keep the workload stable so refactors can
be compared against it.

Go's profile-guided optimization can use a `default.pgo` CPU profile during
builds. Profile local CPU work such as parse, validate, graph, plan, lock, and
status before trying it. Network waits and external package managers dominate
many installs; a profile should be committed only after a repeatable gain on
representative work.

## Narrow uses of generics

Go 1.27 adds generic methods. A `Set[T]` or typed cache may reduce repeated
code if several packages share the same operations. Existing maps such as
`map[string]struct{}` and `map[plan.ResourceIdentity]struct{}` are possible
places to inspect. Repetition alone is insufficient if a helper would hide
domain meaning or add more code than it removes.

Planner, executor, and adapter boundaries have distinct semantics. Keep their
types specific. Generic interfaces would need an actual shared operation and
call sites before they earn a place in the design.

## Safe logging types

depengine already has typed secret references and centralized redaction.
Values that cross many logging boundaries could also implement
`slog.LogValuer` so their logged representation is safe by construction.
Candidates include secret values, authenticated URLs, source identities, and
resolved artifact references. Check each type's other formatting paths as
well; `slog.LogValuer` protects only logging through `slog`.

## Toolchain maintenance and test modes

Review `go fix ./...` periodically for mechanical updates. Test its proposed
changes across OS-specific build contexts before making a clean-diff check a
required gate.

Use `go test -shuffle=on ./...` selectively to find test-order dependence in
process-wide defaults such as the adapter registry, logger, elevation
configuration, and GitHub resolver. For concurrency-heavy packages, combine
race detection with repeated runs and multiple `-cpu` settings in a scheduled
or manual stress job. These checks need not run on every pull request.

## Ideas that need evidence first

- Go 1.27's experimental `runtime/secret` support may be worth studying later.
  Current credential controls are typed references, redaction, scoped
  transports, environment omission, and short credential lifetimes.
- Experimental SIMD APIs have no identified project-owned hot loop. Continue
  using mature hashing implementations unless profiling finds one.
- Consider `encoding/json/v2` for state or SBOM serialization only after
  checking compatibility and a concrete benefit from its semantics or API.
- Set `GOGC`, `GOMEMLIMIT`, or other runtime defaults only for a measured memory
  or latency problem.

## Suggested order

1. Audit archive and materialization paths for `os.Root` and finish semantic
   classification where downloads provide structured errors.
2. Extend synctest coverage to executor cancellation and timeouts, then add
   targeted goroutine lifecycle tests.
3. Add safe pprof labels and prototype optional trace capture for failures.
4. Define adapter capabilities and generate one useful metadata projection.
5. Broaden benchmarks before considering PGO or other performance tuning.

Small uses of `WaitGroup.Go`, `B.Loop`, generics, and `go fix` can be reviewed
as the relevant code changes. The core design work remains in depengine's
schema, graph, resolution, ownership, lock, and installation semantics.

## References

- Go 1.27 release notes: <https://go.dev/doc/go1.27>
- `os.Root`: <https://pkg.go.dev/os#Root>
- `testing/synctest`: <https://pkg.go.dev/testing/synctest>
- Go 1.25 release notes: <https://go.dev/doc/go1.25>
- Flight Recorder: <https://go.dev/blog/flight-recorder>
- `runtime/trace`: <https://pkg.go.dev/runtime/trace>
- `runtime/pprof`: <https://pkg.go.dev/runtime/pprof>
- `testing.B.Loop`: <https://go.dev/blog/testing-b-loop>
- Profile-guided optimization: <https://go.dev/doc/pgo>
- `go fix`: <https://go.dev/blog/gofix>

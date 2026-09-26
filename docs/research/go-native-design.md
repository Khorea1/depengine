# Go-native design opportunities

Status: research note  
Last reviewed: 2026-09-25

This note explores how depengine can make fuller use of Go as a platform, not
just as an implementation language.

[Why Go](why-go.md) answers a different question: why Go remains a good
whole-project fit compared with Rust and other languages. This note assumes Go
and asks what the project should do to extract more value from the language,
standard library, runtime, testing stack, compiler, and toolchain.

The goal is not to adopt every new Go feature. The goal is to replace
project-local complexity with mature Go primitives where that improves
correctness, security, diagnosability, portability, or maintenance.

## Current baseline

depengine already uses Go well in several important ways:

- the project targets Go 1.27 and ships standalone binaries;
- release builds use `CGO_ENABLED=0`;
- cancellation propagates through `context.Context`;
- subprocesses are centralized behind `internal/run.Runner` and
  `exec.CommandContext`;
- structured logging uses `log/slog`;
- embedded assets use `embed`;
- newer standard-library helpers such as `errors.Join`, `slices`, and
  `maps` are already present;
- CI runs the race detector;
- fuzzing is a first-class CI gate;
- `govulncheck`, `go vet`, and `golangci-lint` are part of validation;
- native CI covers Linux, macOS, Windows, BSDs, and Android;
- `os.OpenRoot` is already used in at least one filesystem-sensitive path.

This means the next improvements are not primarily syntactic modernization.
They are about making the runtime and toolchain part of the architecture.

## Recommendation summary

The highest-value opportunities are:

1. make rooted filesystem access a standard security primitive;
2. remove control flow based on error-message strings;
3. use `testing/synctest` for deterministic concurrency and timeout tests;
4. add runtime diagnostics for goroutine lifecycle and execution tracing;
5. generate adapter-derived metadata from typed Go capabilities;
6. modernize benchmarks and measure planner/executor costs before optimizing;
7. adopt small Go 1.25-1.27 improvements where they reduce boilerplate;
8. use generics only for narrow reusable data structures;
9. evaluate PGO only after representative benchmarks justify it.

The first three are correctness work. The others improve observability,
maintainability, or performance discipline.

---

## 1. Treat `os.Root` as a filesystem security boundary

Implementation status (2026-09-26): the offline/local archive installer now
opens its staging directory with `os.OpenRoot` and performs archive directory
creation, file creation, final directory chmods, and metadata-marker writes
through that rooted capability. The existing portable-name, collision, type,
and expansion-budget checks remain in front of the filesystem boundary. HTTP
archive staging already uses rooted copies for stripped payloads; subprocess
`tar`/`unzip` extraction and other materialization paths remain to be audited.

### Why it fits depengine

depengine consumes paths and archives that may originate from project
configuration or remote artifacts. That makes path confinement one of the
project's highest-value security properties.

`internal/localartifact/install.go` already performs substantial defensive
validation:

- absolute path rejection;
- `..` rejection;
- Windows volume-prefix rejection;
- portable path-component checks;
- symlink and hard-link rejection;
- case-collision detection;
- duplicate destination detection;
- aggregate expansion limits;
- entry-count limits;
- path-depth and path-length limits;
- transactional staging before final replacement.

Those checks are still valuable, but many eventually resolve a path and then
perform a separate `os.*` operation on that path. A filesystem may change
between validation and use.

Go's `os.Root`, introduced in Go 1.24, provides filesystem operations scoped
to one directory tree. Root operations reject attempts to escape the root,
including through path traversal and escaping symbolic links.

depengine already uses `os.OpenRoot` in `internal/container`. The stronger
architectural move is to make rooted filesystem access the default capability
for code that materializes untrusted trees.

### Desired direction

Prefer:

```text
archive entry
    |
    v
portable identity validation
    |
    v
collision / policy validation
    |
    v
os.Root rooted at staging directory
    |
    v
root-scoped mkdir/open/stat operations
```

over:

```text
archive entry
    |
    v
validate string
    |
    v
filepath.Join(staging, entry)
    |
    v
ordinary os.OpenFile/os.MkdirAll
```

### Important limitation

`os.Root` does not replace the current archive-policy checks.

depengine still needs its own rules for:

- portable cross-platform path identity;
- case-insensitive collisions;
- reserved metadata names;
- archive expansion budgets;
- unsupported file kinds;
- deterministic ownership/removal semantics.

`os.Root` should be the final filesystem confinement layer underneath those
semantic rules.

### Candidate scope

Good initial targets:

- local archive extraction;
- HTTP archive extraction;
- project-relative vendored artifact reads where confinement matters;
- staging directories populated from untrusted archive entries;
- any future installer that expands an archive into a controlled root.

A useful project rule would be:

> Code that materializes an untrusted directory tree should use a rooted
> filesystem capability unless there is a documented reason not to.

This would convert a collection of local path-safety decisions into a durable
architectural invariant.

---

## 2. Make errors semantic, not textual

Implementation status (2026-09-25): checksum mismatch and Go HTTP status
classification are now semantic. `VerifyChecksum` returns
`*ChecksumMismatchError`, which unwraps to `ErrChecksumMismatch`;
`GoDownloader` returns `*HTTPStatusError`; and retry policy uses
`errors.Is` / `errors.As`. External downloader/transport classification remains
best-effort where only subprocess stderr is available.

Previously, `internal/httpdownload/retry.go` decided that checksum failures were
permanent by inspecting the text of the error:

```go
if strings.Contains(strings.ToLower(err.Error()), "checksum") {
    return err
}
```

That was fragile because human-facing error text became part of runtime control
flow. A wording change could silently alter retry behavior.

### Desired direction

Use Go's error chain as the machine-readable contract:

```go
var ErrChecksumMismatch = errors.New("checksum mismatch")
```

or a typed error:

```go
type ChecksumError struct {
    Expected string
    Actual   string
}
```

and classify with `errors.Is` / `errors.As`.

The same principle can be extended to HTTP and transport failures.

For example, a typed HTTP status error allows retry policy to distinguish:

```text
408                  retryable
429                  retryable, possibly honoring Retry-After
5xx                  retryable
404                  permanent for the resolved artifact
checksum mismatch    permanent
context cancellation permanent
deadline exceeded    context-dependent
temporary network    retryable
```

without parsing strings.

### Project rule

A useful rule for depengine is:

> Strings are for diagnostics; control flow uses typed values, sentinel errors,
> enums, or explicit result fields.

This complements the existing `errorlint` work and the broader movement toward
typed planning and observation state.

---

## 3. Use `testing/synctest` for executor and retry semantics

Implementation status (2026-09-25): retry backoff and cancellation now have
deterministic `testing/synctest` coverage. Tests assert exponential delay, delay
capping, cancellation during backoff, and final-error propagation without
wall-clock sleeps. Executor timeout/cancellation coverage remains Phase B work.

Go 1.25 made `testing/synctest` generally available. Go 1.27 adds further
integration, including a `Sleep` helper and an in-memory HTTP test server
designed for synctest use.

This is unusually well matched to depengine because the codebase contains many
behaviors driven by:

- goroutines;
- channels;
- timers;
- retry backoff;
- cancellation;
- nested contexts;
- worker pools;
- HTTP;
- subprocess lifecycle.

Today some tests use real `time.After` watchdogs, and runtime code such as
`internal/httpdownload/retry.go` uses real backoff timers.

### High-value test targets

#### Retry/backoff

A test should be able to verify:

```text
attempt 1 -> fail
wait 1s
attempt 2 -> fail
wait 2s
attempt 3 -> fail
wait 4s
attempt 4 -> success
```

without spending seven wall-clock seconds.

#### Executor cancellation

Test deterministic sequences such as:

```text
tool A starts
tool B starts
A finishes
parent context is cancelled
B observes cancellation
dependent C never starts
workers exit
```

#### Method and tool timeouts

The executor has nested timeout scopes:

```text
run context
  -> tool timeout
      -> method timeout
          -> hook / preparation / subprocess / download
```

These are exactly the kinds of interactions where virtualized time and
deterministic blocking are preferable to sleeps and large watchdogs.

#### HTTP retry behavior

The Go 1.27 `httptest` additions should make it possible to test HTTP retry,
cancellation, and timeout behavior inside a synctest environment without
depending on real network scheduling.

### Expected benefit

The objective is not merely faster tests.

The important gain is that concurrency semantics become reproducible rather
than probabilistic. This is particularly valuable as depengine increases
parallel installation work.

---

## 4. Treat goroutine lifecycle as a testable resource

The CI already runs `go test -race`, which catches data races. That is
necessary but not sufficient for concurrent orchestration.

A CLI can be race-free while still leaking goroutines that remain permanently
blocked on:

- channels;
- timers;
- subprocess completion;
- retry loops;
- HTTP bodies;
- synchronization primitives.

Go 1.27 makes the `goroutineleak` runtime/pprof profile generally available.

### Candidate use

Add targeted lifecycle tests around:

- `internal/exec` worker pools;
- cancellation while a tool is installing;
- cancellation during preparation;
- retries interrupted by context cancellation;
- fake runners with delays;
- HTTP requests interrupted during body transfer.

The acceptance property should be stronger than "the command returned":

> the command returned and no depengine-owned goroutine remained permanently
> blocked.

This is especially important for tests that repeatedly exercise the executor in
one process, where leaked goroutines can otherwise remain invisible.

---

## 5. Add semantic runtime labels and optional execution tracing

depengine already has structured logs and `DEPENGINE_TRACE_ID`.

The next useful step is to make Go runtime diagnostics carry the same semantic
context.

### pprof labels

Use `runtime/pprof` labels around meaningful units of work such as:

```text
tool=zsh
method=apt
phase=install
```

or:

```text
tool=neovim
method=github
phase=resolve
```

This makes CPU, goroutine, and blocking diagnostics more useful than raw stack
traces alone.

Do not include secrets, raw credentials, or sensitive URLs in runtime labels.

### Flight Recorder

Go 1.25 added `runtime/trace.FlightRecorder`, which retains a moving window of
recent execution-trace data.

This is a good fit for intermittent executor problems:

```text
depengine install
      |
      v
rolling in-memory execution trace
      |
      +---- normal success -> discard
      |
      +---- internal failure / timeout -> optional trace snapshot
```

It should remain opt-in diagnostics rather than normal runtime behavior.

Possible interfaces include a development environment variable or explicit
debug flag that selects a trace output path.

A saved trace can then be inspected with `go tool trace`.

### Trace identifiers

Go 1.27 adds a standard-library `uuid` package.

If `DEPENGINE_TRACE_ID` is absent, depengine could generate a process/run ID
in-process and propagate it through:

- `slog`;
- contexts;
- child-process environment;
- diagnostic reports.

A time-sortable UUID variant may be useful for log ordering, but this should be
treated as an observability convenience rather than a persisted identity
contract.

---

## 6. Make typed adapter capabilities the source of generated metadata

The roadmap already calls for generating schema/docs metadata from adapter
capabilities rather than scattering method-name conditionals.

This is one of the strongest opportunities to use the Go toolchain as an
architectural component.

The project already has `cmd/schema-gen`. The longer-term direction should be
that typed Go descriptors are the canonical source for adapter capabilities,
and generated artifacts are projections.

For example:

```go
type CapabilitySet struct {
    SupportsVersion      bool
    SupportsExactVersion bool
    SupportsLock         bool
    SupportsRemove       bool
    SupportsSource       bool
    SupportsCredentials  bool
    RequiresNetwork      bool
}
```

The exact shape should follow the existing adapter model rather than this
illustrative struct.

From one canonical capability description, generation could produce or verify:

```text
typed adapter descriptors
        |
        +--> JSON schema metadata
        +--> schema documentation tables
        +--> CLI/help capability tables
        +--> validation metadata
        +--> contract-test matrices
```

### `go generate`

Where generation is deterministic and repository-owned, use standard
`//go:generate` entry points and verify generated output in CI.

A typical gate is:

```sh
go generate ./...
git diff --exit-code
```

The value is not the directive itself. The value is eliminating independent
lists of method capabilities that can drift apart.

---

## 7. Modernize concurrency primitives selectively

Go 1.25 added `sync.WaitGroup.Go`.

Where the code is exactly the standard pattern:

```go
wg.Add(1)
go func() {
    defer wg.Done()
    work()
}()
```

prefer the newer primitive when it improves readability.

`internal/exec/execute.go` is an obvious place to review.

This does not imply replacing the executor with `errgroup`.

depengine often needs domain-specific semantics:

- continue independent work;
- aggregate results;
- record per-tool failures;
- block dependents;
- preserve candidate fallback;
- avoid cancelling unrelated tools on the first adapter error.

A custom worker pool can therefore remain more appropriate than a generic
"cancel on first error" abstraction.

The rule should be:

> Use small standard-library concurrency primitives where they match the
> semantics exactly; keep custom orchestration where the domain semantics are
> genuinely different.

---

## 8. Modernize benchmarks and benchmark the semantic pipeline

`internal/config/benchmark_test.go` still uses the traditional `b.N` loop.

Go 1.24 introduced `testing.B.Loop`, which is now the preferred benchmark
form. It handles setup/cleanup timing more predictably and reduces several
common benchmark mistakes.

Existing benchmarks should migrate to:

```go
for b.Loop() {
    // measured operation
}
```

where appropriate.

### More important: broaden what is benchmarked

TOML parsing is useful, but it is not the only CPU-local workload worth
measuring.

Representative benchmark scenarios should cover:

- schema parse and normalization;
- placeholder expansion;
- validation;
- dependency-graph construction;
- topological level construction;
- candidate selection;
- resolved-plan construction;
- lock projection and hashing;
- observation/reconciliation logic;
- state serialization;
- SBOM generation.

A large synthetic scenario could approximate:

```text
500 tools
x 5 candidates/tool
x dependency edges
x conditions
x lock entries
x resolved-plan projection
```

The objective is to create a stable performance budget for refactors.

Avoid turning benchmark numbers into release promises unless the benchmark
environment and acceptance criterion are intentionally fixed.

---

## 9. Evaluate PGO only after representative measurement

Go supports profile-guided optimization using CPU profiles. A
`default.pgo` file in a main-package directory can be picked up automatically
by the Go toolchain.

This capability is attractive because depengine already has a controlled
release build.

It should still not be enabled merely because the feature exists.

Most installation wall time is likely dominated by:

- network latency;
- package managers;
- subprocesses;
- decompression;
- filesystem operations.

PGO cannot materially optimize those external costs.

### Evaluation procedure

Use a measured sequence:

```text
representative benchmark/workload
        |
        v
collect CPU profile
        |
        v
build with PGO
        |
        v
compare with non-PGO build
        |
        v
keep only if improvement is repeatable and meaningful
```

Useful workloads are likely the local phases:

```text
parse -> validate -> graph -> plan -> lock -> status
```

If there is no meaningful gain, do not add a committed profile and its
maintenance burden.

---

## 10. Use generics for containers, not architecture

Go 1.27 adds generic methods, expanding where generics can be expressed.

That does not make broad generic abstraction desirable for depengine.

Good candidates are narrow, mechanically reusable structures such as:

```go
Set[T comparable]
Cache[K comparable, V any]
Index[K comparable, V any]
```

but only when repeated code already demonstrates the abstraction.

For example, the codebase contains multiple set-like maps such as:

```go
map[string]struct{}
map[plan.ResourceIdentity]struct{}
```

A small internal `Set[T]` may be worthwhile if it removes repeated,
error-prone boilerplate across several packages.

Likewise, typed cache helpers may be useful where `sync.Map` currently
requires assertions and the access pattern is actually shared.

Avoid turning semantic boundaries into generic frameworks:

```text
Adapter[T]
Planner[T]
Executor[T]
Repository[T]
```

unless a concrete repeated pattern demands it.

depengine benefits more from distinct domain types than from parameterizing
everything.

---

## 11. Use types to make logging and secret handling safer by construction

The project already centralizes redaction and typed secret references.

Go's type system and `slog.LogValuer` can push more of that safety into the
types themselves.

For example, a secret-bearing type can guarantee that logging produces only a
redacted representation.

Similarly, URL or source-reference wrappers can expose a safe log
representation while retaining the full value internally.

Conceptually:

```text
raw string + remember to redact
              |
              v
typed value with safe logging semantics
```

This should be applied selectively to values that cross many logging
boundaries:

- secret values;
- secret references;
- authenticated URLs;
- source identities;
- resolved artifact references.

This is one of the most practical ways to import some of Rust's
"invalid states are harder to express" discipline into Go without rewriting the
project.

---

## 12. Use `go fix` as a modernization aid, not a style exercise

Modern Go releases have substantially expanded `go fix` and its modernizers.

Because depengine intentionally targets a recent Go version, it does not need
to preserve source compatibility with old language/toolchain releases.

That makes periodic `go fix` review useful.

A possible CI or local-maintenance check is:

```sh
go fix ./...
git diff --exit-code
```

The exact gate should be tested before making it mandatory, especially across
OS-specific build contexts.

The objective is to let the Go toolchain identify mechanical modernizations
instead of maintaining a project-specific checklist of outdated idioms.

---

## 13. Expand native test modes before adding test frameworks

The existing test stack is already strong and should remain mostly standard
library plus Go tooling.

Useful additions include selective runs with:

```sh
go test -shuffle=on ./...
```

to expose test-order dependence and leaked process-global state.

This is especially relevant because the architecture intentionally retains a
small number of process-wide defaults such as:

- adapter registry;
- logger;
- elevation configuration;
- default GitHub resolver.

For concurrency-heavy packages, periodic stress runs can combine:

- `-race`;
- repeated counts;
- multiple `-cpu` settings;
- shuffled order;
- goroutine-leak inspection.

These do not all need to run on every pull request. Some are better suited to a
scheduled or manually triggered stress job.

---

## Features that should remain conditional or out of scope

"Use more Go" should not become "use every Go feature."

### `runtime/secret`

Go 1.27 includes ongoing work around experimental secret-memory support.

depengine's credential security contract should not depend on an experimental
runtime feature.

It may be worth a future hardening experiment, but typed secret references,
redaction, scoped transports, environment omission, and minimal credential
lifetime remain the primary production controls.

### SIMD

The experimental SIMD APIs do not currently address a meaningful depengine
bottleneck.

Archive hashing and checksums should continue to rely on mature crypto/hash
implementations unless profiling proves a project-owned hot loop.

### `encoding/json/v2`

Go 1.27 includes the newer JSON implementation and related APIs.

Migration should be semantic, not fashionable. SBOM or state serialization
should move only if the stricter semantics or new API materially improve a
contract and compatibility has been reviewed.

### GC tuning

Do not add `GOGC`, `GOMEMLIMIT`, or runtime tuning defaults without a
measured memory/latency problem.

depengine is a short-lived CLI and usually waits on external systems. Runtime
tuning is unlikely to be a first-order optimization.

### PGO

Do not commit a PGO profile until benchmarks demonstrate a repeatable benefit.

### Generics

Do not genericize core architecture merely because Go 1.27 supports generic
methods.

---

## Proposed implementation order

### Phase A: correctness and security

1. Continue the archive/materialization audit for `os.Root`: local/offline
   archive extraction is rooted; subprocess HTTP extraction and other paths
   remain.
2. Finish semantic download error coverage (checksum mismatch and Go HTTP
   status are implemented; external downloader/transport classification remains).
3. Keep retry/error policy free of `err.Error()` text classification.
4. Extend synctest coverage beyond retry into executor timeout/cancellation
   behavior (retry backoff/cancellation coverage is implemented).

These changes have direct correctness value and fit current P0/P1 work.

### Phase B: concurrency confidence

1. Add deterministic executor timeout/cancellation tests with
   `testing/synctest`.
2. Add goroutine-lifecycle tests around worker pools and cancellation.
3. Review simple `WaitGroup` launch patterns for `WaitGroup.Go`.
4. Add selective shuffled/stress test jobs.

### Phase C: observability

1. Generate a run trace ID when none is supplied.
2. Add safe pprof labels for tool/method/phase.
3. Prototype opt-in `runtime/trace.FlightRecorder` capture.
4. Document how to inspect traces with standard Go tooling.

### Phase D: capability generation

1. Define the canonical typed capability representation.
2. Migrate one small generated projection to it.
3. Expand generation to schema/docs/contract metadata.
4. Add a generation-drift CI check.

This work should align with the existing roadmap item to remove
method-name-specific conditionals.

### Phase E: performance discipline

1. Migrate benchmarks to `B.Loop`.
2. Add graph/planner/lock/reconciliation benchmarks.
3. Establish representative large-schema workloads.
4. Evaluate PGO only after a stable benchmark baseline exists.

---

## Architectural principle

The broader design principle is:

> Prefer standard Go capabilities at trust, concurrency, diagnostic, and
> tooling boundaries; keep depengine-specific code focused on dependency
> semantics.

That means using Go itself for:

```text
filesystem confinement
concurrency + cancellation
race detection
fuzzing
virtual-time concurrency tests
goroutine leak detection
structured logging
profiling
execution tracing
code generation
benchmarking
PGO
cross-compilation
vulnerability analysis
```

while depengine remains responsible for:

```text
schema semantics
adapter capabilities
dependency graph semantics
resolved install identity
source ownership
reconciliation
lock semantics
security policy
installation lifecycle
```

This division is valuable because the first category is infrastructure that the
Go toolchain can test and maintain more broadly than depengine can. The second
category is where project-specific complexity is unavoidable and deserves the
project's engineering attention.

## Expected effect on the Go-versus-Rust trade-off

The strongest argument for Rust remains semantic modeling: enums, exhaustive
matching, ownership, and richer compile-time invariants.

A Go implementation should respond by strengthening its domain types and phase
boundaries, not by recreating Rust abstractions mechanically.

At the same time, deeper use of Go's standard runtime/toolchain increases the
cost of a language rewrite because depengine can obtain more infrastructure
without adding dependencies or a separate runtime ecosystem.

The result is not that Go becomes a stronger type system than Rust.

It is that Go becomes a more complete platform for depengine's actual workload:

```text
networked cross-platform installer
+ concurrent subprocess orchestrator
+ archive/filesystem manipulator
+ short-lived CLI
+ small bootstrap surface
```

That reinforces the conclusion in [Why Go](why-go.md): the main advantage is
whole-system simplicity, not raw runtime performance.

## References

Primary Go references:

- Go 1.27 release notes: <https://go.dev/doc/go1.27>
- `os.Root`: <https://pkg.go.dev/os#Root>
- `testing/synctest`: <https://pkg.go.dev/testing/synctest>
- Go 1.25 release notes: <https://go.dev/doc/go1.25>
- Flight Recorder: <https://go.dev/blog/flight-recorder>
- `runtime/trace`: <https://pkg.go.dev/runtime/trace>
- `runtime/pprof`: <https://pkg.go.dev/runtime/pprof>
- `testing.B.Loop`: <https://go.dev/blog/testing-b-loop>
- Profile-guided optimization: <https://go.dev/doc/pgo>
- modern `go fix`: <https://go.dev/blog/gofix>

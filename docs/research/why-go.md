# Why Go

Status: research note  
Last reviewed: 2026-09-25

This note records why Go is a good fit for depengine, what Rust would improve,
and which parts of the decision are historical versus still architecturally
relevant.

It is not a claim that Go is universally better than Rust. The question is
narrower: given depengine's workload, distribution model, portability goals,
and maintenance constraints, which language better fits the project as a
whole?

## Summary

Go remains the better overall fit for depengine.

Rust would provide a stronger type system for the semantic core, especially for
representing planner states, resolved identities, observations, reconciliation,
and capability-rich adapter contracts. If the project were primarily a
dependency solver or compiler-like planner, Rust would have a stronger case.

depengine is broader than that. It is also a cross-platform systems CLI that
spends much of its time coordinating subprocesses, package managers,
filesystems, HTTP APIs, downloads, archives, credentials, locks, and
platform-specific behavior. It must be easy to build and distribute as a
standalone executable across several operating systems.

For that complete workload, Go's standard library, concurrency model, build
model, and lower implementation complexity outweigh Rust's semantic-modeling
advantage.

The current conclusion is therefore not a strict Go/Rust tie. Rust remains the
strongest alternative, but Go has the edge for the project as it actually
exists.

## Historical context

Go was originally chosen over Rust for three practical reasons:

1. depengine was expected to perform substantial network and web integration;
2. Go appeared simpler to implement and maintain than Rust;
3. depengine was the author's first Git project, so reducing language and
   tooling complexity mattered.

Those reasons should be separated into historical and durable arguments.

"First Git project" is historical context, not a permanent architectural
requirement. It should not by itself justify keeping Go indefinitely.

Simplicity, however, remains relevant. A project that integrates many package
managers, protocols, operating systems, and external processes already has a
large accidental-complexity budget. Language complexity competes for the same
maintenance budget.

The networking argument also remains relevant, but needs a more precise
formulation. Rust is not inherently bad or heavy at networking. Its async
ecosystem is capable of very high concurrency. The advantage of Go is instead
that much of the networking stack depengine needs is available through a
cohesive standard library and a comparatively simple concurrency model.

## What depengine actually optimizes for

depengine is not primarily CPU-bound software.

Its dominant classes of work are closer to:

- parsing and validating configuration;
- constructing dependency graphs and resolved install plans;
- invoking package managers and other subprocesses;
- querying registries and release APIs;
- downloading artifacts;
- verifying checksums and signatures;
- extracting archives;
- manipulating files and state;
- coordinating installation work across independent tools;
- adapting behavior to Linux, macOS, and Windows.

Most of these operations are I/O-bound. Runtime performance differences between
Go and Rust are therefore rarely the central constraint. Network latency,
package-manager execution, decompression, disk access, and external installers
usually dominate.

The more important comparison is implementation complexity, correctness,
portability, and distribution.

## Networking and protocol breadth

The original networking motivation for Go was directionally correct, but not
because Rust would struggle to issue many requests.

Go's standard library provides mature HTTP, TLS, URL, networking, compression,
and archive primitives. In particular, `net/http.Transport` supports connection
reuse and concurrent use, and the standard HTTP stack supports HTTP/2. The
standard library also includes `crypto/tls`, `archive/tar`, and
`archive/zip`.

This matters for depengine because its most important remote integrations are
HTTP-shaped:

- GitHub APIs and release assets;
- direct HTTP/HTTPS artifacts;
- language-registry APIs;
- checksum and signature documents;
- metadata endpoints;
- mirrors and content-addressed downloads;
- services such as archive.org that expose HTTP-based access.

A large fraction of the transport layer can therefore be implemented without
introducing a separate async runtime or a large protocol stack.

Go also has well-established extension packages such as
`golang.org/x/crypto/ssh` when SSH-family transports are needed.

That does not mean every possible depengine transport is part of the Go
standard library. SFTP, FTP, and WebDAV client support may require external
packages. Rust likewise has mature crates for these use cases. Protocol count
alone is therefore not a decisive Go advantage.

The durable advantage is the shape of the common path: HTTP(S), TLS, streams,
archives, files, and concurrency are unusually well integrated in Go's default
toolchain.

## Many concurrent installations

A future depengine may perform multiple independent network-bound operations at
once: registry lookups, GitHub release resolution, artifact downloads,
checksum retrieval, or source preparation.

Both Go and Rust can handle this efficiently.

Go's model is straightforward:

```go
go resolve(tool)
```

with channels, contexts, semaphores, and ordinary blocking-looking APIs.

Rust can achieve equal or better resource efficiency using async/await with a
runtime such as Tokio. That is not a runtime-performance weakness. Tokio is
specifically designed for large numbers of concurrent I/O operations.

The difference is architectural cost.

An async Rust implementation typically introduces runtime selection, async
traits or adapter boundaries, executor-aware APIs, feature selection, and
additional interactions between synchronous subprocess/filesystem operations
and asynchronous network operations.

That complexity can be justified in a high-throughput server. depengine is a
short-lived CLI orchestrator whose concurrency is bounded by the number of
install operations it can safely perform at once. Go's goroutine model is a
particularly good fit for that scale.

The correct conclusion is therefore:

> Many network requests do not make Rust too heavy at runtime. They make Go
> attractive because Go reaches the required concurrency with less application
> architecture.

## Dependency and build surface

As of this review, depengine's `go.mod` has only a small set of direct
dependencies: TOML parsing and the Cobra/pflag CLI stack. Much of the rest of
the implementation relies on Go's standard library.

That is useful for an installer.

depengine itself sits at the beginning of a dependency bootstrap chain. A
simple release artifact is therefore a product feature, not merely a
development convenience.

The project currently builds release binaries with `CGO_ENABLED=0`. This
keeps the ordinary release path close to:

```text
source
  -> Go compiler
  -> platform binary
```

Go also exposes target OS and architecture directly through `GOOS` and
`GOARCH`, making cross-compilation a normal part of the toolchain.

Rust also produces native standalone binaries and supports many compilation
targets. It can be made similarly self-contained, especially with carefully
selected TLS and other dependencies. However, the dependency graph and target
configuration tend to require more deliberate management. For example, an HTTP
client may involve selecting TLS backends and Cargo features, while async
networking usually introduces an executor such as Tokio.

None of those are defects in Rust. They are additional choices depengine would
have to own.

For a package installer, fewer bootstrap and target-specific choices are
valuable.

## Where Rust is better

Rust's strongest argument is the semantic core.

depengine increasingly behaves like a small declarative engine:

```text
schema
  -> parsed configuration
  -> validated intent
  -> dependency graph
  -> resolved install plan
  -> observation
  -> reconciliation
  -> execution / state / lock / SBOM
```

Several important concepts naturally resemble algebraic data types:

- requested identity;
- resolved identity;
- locked identity;
- method kind;
- capability sets;
- secret references;
- observation state;
- verification result;
- reconciliation result;
- graph node and edge kinds.

Rust can encode many of these more precisely with enums, newtypes, exhaustive
pattern matching, and ownership.

For example, a state model such as:

```text
Absent
Satisfied(identity)
Drifted(desired, actual)
Unknown(reason)
Broken(reason)
```

is more directly expressed and exhaustively checked in Rust than in idiomatic
Go.

This could reduce classes of invalid state and make large refactors safer.

That advantage is real and should influence Go design even without a language
rewrite.

## Import the Rust/ML design, not necessarily the language

The most useful response to Go's weaker sum-type support is to make phase
boundaries explicit.

Prefer a flow such as:

```text
RawConfig
  -> ValidatedConfig
  -> RequestedInstall
  -> ResolvedInstallPlan
  -> Observation
  -> Reconciliation
  -> ExecutableTransition
```

instead of passing one broad mutable structure through many phases.

Similarly:

- use distinct types for requested, resolved, and installed identity;
- keep adapter capabilities typed and explicit;
- avoid generic argument bags;
- make unknown, broken, absent, drifted, and satisfied states distinct;
- centralize transitions rather than re-deriving them in CLI commands;
- prefer immutable plan-like objects after resolution;
- make invalid combinations fail during validation or construction.

These ideas capture part of the value Rust would provide while preserving Go's
operational advantages.

## Other languages

### Zig

Zig has an attractive systems model and strong cross-compilation story. Tagged
unions and compile-time facilities could model adapter state well.

Its ecosystem is less compelling for depengine's combination of HTTP/TLS,
archives, package-manager integration, cross-platform filesystem behavior, and
high-level application tooling. The project would spend more effort building
infrastructure that Go already provides.

### OCaml and other ML-family languages

OCaml would be excellent for the planner and graph model. Algebraic data types
and pattern matching map naturally to depengine's semantic domain.

The trade-off appears in the outer system: subprocesses, platform integration,
installer formats, release distribution, and the breadth of system-facing
libraries are less convenient than Go for this particular project.

### Python, TypeScript, Ruby, JVM, and .NET languages

These languages can offer excellent development ecosystems, but they weaken
depengine's bootstrap property by adding runtime or larger self-contained
distribution requirements.

That is a poor default for a tool whose purpose is itself to install project
dependencies.

### C and C++

They provide control depengine does not need while increasing memory-safety,
build-system, and portability costs.

## Revised Go versus Rust assessment

If the only criterion were domain modeling, Rust would be preferable.

If the only criterion were raw asynchronous-network performance, there would be
no strong reason to prefer Go; Rust is fully competitive.

For the whole project, the important criteria are different:

| Criterion | Go | Rust |
|---|---|---|
| CLI implementation | excellent | excellent |
| HTTP/TLS common path | excellent, stdlib-heavy | excellent, crate-heavy |
| bounded I/O concurrency | excellent and simple | excellent and more explicit |
| subprocess orchestration | excellent | excellent |
| cross-platform filesystem/process work | excellent | excellent |
| standalone distribution | excellent | excellent |
| cross-compilation workflow | very simple | capable, more configuration-sensitive |
| domain-state modeling | good | excellent |
| exhaustive state handling | limited | excellent |
| compile-time invariant enforcement | good | excellent |
| build iteration | fast/simple | heavier |
| language learning/maintenance burden | lower | higher |
| dependency/bootstrap surface | usually smaller for this workload | usually larger for this workload |

This is not a universal scorecard. It reflects depengine's workload.

With the project's actual priorities, Go should be considered the preferred
language rather than merely tied with Rust.

A reasonable qualitative framing is:

```text
whole-project fit:

Go      clear edge
Rust    strong second choice
Zig     specialized alternative
OCaml   attractive semantic-core alternative
others  materially weaker fit
```

The gap would narrow substantially if depengine became mostly a resolver,
solver, or long-running high-concurrency service.

The gap widens if it continues to prioritize easy cross-platform distribution,
many OS/package-manager adapters, a small bootstrap surface, and maintainability
by a small contributor base.

## What could change the decision

The language decision should be revisited if one or more of these become true:

1. invalid semantic states remain a dominant source of bugs despite stronger Go
   type boundaries;
2. the planner becomes sufficiently sophisticated that algebraic modeling and
   exhaustive matching dominate the codebase;
3. memory usage or GC behavior becomes a measured operational problem;
4. depengine evolves into a long-running high-concurrency service rather than a
   short-lived CLI;
5. a substantial Rust subsystem is introduced for another reason and proves
   operationally simpler than expected;
6. Go's build/distribution advantages stop being important to the supported
   platform matrix.

A rewrite should be driven by measured failure of the current language against
those requirements, not by the abstract superiority of another type system.

## Conclusion

The original choice of Go was sound, although the networking rationale is best
stated as simplicity rather than performance.

Rust would not make multiple network installations intrinsically heavy. It can
handle them extremely well. The difference is that Go provides the required
HTTP/TLS, I/O, concurrency, archive, process, and cross-compilation primitives
with less architectural and toolchain overhead.

That matters disproportionately for depengine because the project already has a
large integration surface.

Rust remains stronger for the typed semantic core. The appropriate response is
to borrow its modeling discipline where possible while retaining Go for the
whole system.

Given depengine's current purpose, Go is not merely an acceptable incumbent. It
is still the better overall language choice.

## References

- Go `net/http` package:
  <https://pkg.go.dev/net/http>
- Go `crypto/tls` package:
  <https://pkg.go.dev/crypto/tls>
- Go `archive/tar` package:
  <https://pkg.go.dev/archive/tar>
- Go `archive/zip` package:
  <https://pkg.go.dev/archive/zip>
- Go SSH package:
  <https://pkg.go.dev/golang.org/x/crypto/ssh>
- Go source/build documentation, including `GOOS` and `GOARCH`:
  <https://go.dev/doc/install/source>
- Tokio tutorial:
  <https://tokio.rs/tokio/tutorial>
- Reqwest TLS configuration:
  <https://docs.rs/reqwest/latest/reqwest/tls/>
- Cargo build target documentation:
  <https://doc.rust-lang.org/cargo/commands/cargo-build.html>

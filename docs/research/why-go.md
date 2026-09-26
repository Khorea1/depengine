# Why Go

Status: research note  
Last reviewed: 2026-09-26

Go suits depengine's work as an installer: it handles HTTP, files, archives,
subprocesses, and concurrent operations with a small dependency set and a
straightforward release build. Rust offers stronger types for planning and
state transitions, but would add more tooling and integration choices around
the rest of the program. Go remains the better fit for the project today.

## How the choice was made

The original decision favored Go for its networking support and simpler
tooling. Reducing complexity also mattered because depengine was the author's
first Git project. That last reason explains the initial choice; the current
case for Go rests on the work depengine performs and how it is distributed.

The program parses configuration, builds dependency graphs, resolves install
plans, calls package managers, queries registries, downloads and verifies
artifacts, extracts archives, and records state. It runs on several operating
systems. Most install time is spent waiting for networks, disks, or external
programs, so language runtime speed has little effect on a typical run.

The main costs to compare are implementation effort, correctness, portability,
and the work required to build and release the CLI.

## Networking and concurrency

Go's standard library covers the common transport path: HTTP, TLS, URLs,
streams, compression, and tar and zip archives. `net/http.Transport` supports
connection reuse and concurrent requests. GitHub APIs, release assets, direct
downloads, registry metadata, and checksum files all use that path. SSH support
is available through `golang.org/x/crypto/ssh`; SFTP, FTP, and WebDAV clients
may need other packages.

Rust has capable networking libraries and can handle many concurrent requests.
With Tokio and async/await, it could meet depengine's performance needs. The
extra work lies in choosing and integrating a runtime, HTTP and TLS features,
and interfaces between asynchronous network calls and synchronous process or
filesystem operations.

Go's goroutines, contexts, channels, and semaphores are enough for bounded
parallel work such as release lookups and downloads. Installation still needs
limits because package managers and tools can contend for shared state.

## Building and distributing the CLI

At this review, `go.mod` has few direct dependencies beyond TOML parsing and
the Cobra/pflag CLI stack. Release builds use `CGO_ENABLED=0`, and the Go
toolchain selects targets through `GOOS` and `GOARCH`. This keeps the bootstrap
path short for a tool that installs other dependencies.

Rust can also produce standalone binaries for many targets. Its build would
need explicit choices for the HTTP client, TLS backend, async runtime, Cargo
features, and some target configurations. Those choices are manageable, but
they would become part of depengine's maintenance work.

## Where Rust would help

The planner has concepts that benefit from stricter type boundaries: requested,
resolved, and locked identities; method capabilities; secret references;
observations; and reconciliation results. Rust enums and exhaustive matching
could make invalid combinations harder to construct and state handling easier
to check during a refactor.

For example, an observation can be absent, satisfied with an identity, drifted
from a desired identity, unknown for a stated reason, or broken. Rust can
express those cases as one enum. In Go, distinct types and constructors can
still make the transitions clear, though the compiler checks fewer cases.

The useful design response in Go is to keep each phase's data explicit:

```text
RawConfig -> ValidatedConfig -> RequestedInstall -> ResolvedInstallPlan
          -> Observation -> Reconciliation -> ExecutableTransition
```

Requested, resolved, and installed identities should have distinct types.
Adapters should expose typed capabilities, and validation should reject
invalid combinations before execution. Plans should become stable once
resolved. These boundaries address concrete sources of ambiguity without a
language rewrite.

## Other language options

Zig offers tagged unions and cross-compilation, but depengine would need more
project-owned infrastructure for HTTP/TLS, archives, and platform integration.
OCaml's algebraic data types suit the planner and graph; its system integration
and release path are less convenient for this CLI.

Python, TypeScript, Ruby, JVM, and .NET ecosystems offer capable application
tooling, with additional runtime or distribution requirements for this use
case. C and C++ provide low-level control that the installer rarely needs and
increase memory-safety and build maintenance costs.

## When to reconsider

The decision merits review if semantic-state bugs persist despite stronger Go
types, the planner grows into the dominant part of the codebase, or profiling
shows a material memory or GC problem. A shift to a long-running service, a
successful Rust subsystem, or a change in release requirements would also
alter the balance.

For the current CLI, Go covers the integration work with less build and
toolchain overhead. Rust's clearest advantage remains the typed semantic core;
that advantage should continue to inform the Go design.

## References

- Go `net/http`: <https://pkg.go.dev/net/http>
- Go `crypto/tls`: <https://pkg.go.dev/crypto/tls>
- Go `archive/tar`: <https://pkg.go.dev/archive/tar>
- Go `archive/zip`: <https://pkg.go.dev/archive/zip>
- Go SSH package: <https://pkg.go.dev/golang.org/x/crypto/ssh>
- Go source and build documentation: <https://go.dev/doc/install/source>
- Tokio tutorial: <https://tokio.rs/tokio/tutorial>
- Reqwest TLS configuration: <https://docs.rs/reqwest/latest/reqwest/tls/>
- Cargo build targets: <https://doc.rust-lang.org/cargo/commands/cargo-build.html>

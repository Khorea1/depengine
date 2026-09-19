# Support boundary

depengine models installation intent; it does not make every installer or
package manager equally reproducible. Typed methods describe only the identity,
source, target, and verification semantics supported by the underlying tool.

## Preferred models

Typed package-manager and ecosystem adapters are the default. Fields such as
`version`, `source`, `registry`, `channel`, `scope`, `environment`, `prefix`,
`architecture`, revision and digest are accepted only where the method contract
can carry them through resolution, execution and/or verification. A configured
capability that a candidate cannot honor is rejected before execution.

Generic artifact methods (`http`, `github`, `appimage`, `android`, `msi`) are
supported when ownership and verification can be made explicit. Use checksums,
signatures, managed paths, and method-specific identity instead of checks that
only test whether a binary name exists.

Opaque vendor scripts, EXE installers and arbitrary build/hooks are escape
hatches. They may be necessary, but they are not equivalent to a fully modeled
package-manager transaction. They require the arbitrary-code permission gate
where applicable and may provide weaker ownership, rollback and desired-state
verification.

## Reproducibility levels

A declaration can contain several different identities:

- **Desired identity** is what `schema.toml` asks for, for example version
  `1.2.3`, channel `stable`, Git tag `v1.2.3`, or container tag `latest`.
- **Resolved identity** is the concrete result selected during resolution, when
  a method supports resolution. A mutable release selector may resolve to a
  concrete release or artifact.
- **Locked immutable identity** is the identity persisted in `depengine.lock`
  when that method is supported by the lock model, for example a resolved
  artifact plus checksum. Lock coverage is not yet universal across package
  managers and ecosystems.

A method that supports exact versions is stronger than one that only tests
package presence. A digest or commit is stronger than a mutable tag or branch.
Method capability metadata and `depengine why` report these differences.

## Scope, environments and profiles

`scope` selects an installation ownership domain exposed by a package manager,
such as user/system or user/global. An `environment`, `prefix`, install root or
profile selects a concrete target namespace. They are separate concepts: a
user-scoped package can still live in one of several environments, while a
named environment does not by itself imply OS-level user/system scope.

Adapters must use the same target semantics for check, install, remove and
installed-version reporting. If an underlying tool cannot identify a target
reliably, depengine should not expose a field that suggests it can.

## Sources and host mutation

A package `source`, registry, remote, bucket or channel selects where a
candidate/package is resolved. That is distinct from mutating host-wide source
configuration such as adding an APT repository, COPR, Homebrew tap or Scoop
bucket. Host source mutation is candidate-scoped, should be idempotent, and has
separate ownership/cleanup concerns.

For container methods, `source` is the complete repository identity including
an optional registry host/port (for example
`registry.example:5000/team/tool`); tags and digests are separate fields.

## Non-goals

depengine does not promise that every external package manager offers immutable
resolution, transactional rollback, uniform version syntax, or equivalent
security metadata. It also does not reinterpret arbitrary argument bags as a
portable semantic model. New options should be represented as typed fields with
explicit validation and runtime behavior.

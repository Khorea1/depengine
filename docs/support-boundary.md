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

### Adapter tiers

The tiers below describe reproducibility evidence, not whether a method is
usable. **Tier 1** requires both complete immutable lock coverage for the
adapter and CI tests that exercise its behavior against the real platform or
package manager. **Tier 2** means best effort: a declaration may preserve
requested package/version fields and the adapter may have unit tests, but
depengine does not guarantee that a reinstall selects the same package build.

Every adapter registered by `internal/ecosystem` is currently **Tier 2**:

| Adapter(s) | Tier | Boundary |
|---|---|---|
| `cargo`, `go` | 2 | Exact version, source or revision fields can guide installation where supported, but ecosystem package versions are not fully projected into `depengine.lock`. CI exercises adapter logic with fake runners, not real Cargo/Go registries and installs. |
| `pip`, `pipx`, `uv` | 2 | Requested versions and indexes are passed through where supported; resolved Python distributions and their dependency/build artifacts are not completely locked. Adapter tests use fake runners. |
| `npm`, `pnpm`, `bun`, `yarn`, `yarn-berry` | 2 | Requested versions and registry settings are best effort; the lock does not capture a complete immutable package tree for these global installs. Adapter tests use fake runners. |
| `gem`, `composer`, `apm` | 2 | Package identity/version is not completely captured as an immutable lock entry; CI does not install and verify packages against the real registries. |
| `flatpak`, `snap` | 2 | Remote, channel, branch or revision inputs do not amount to complete adapter lock coverage, and distro CI does not exercise these adapters against their real services. |
| `vscode`, `vscodium`, `cask`, `mas`, `appman` | 2 | Extension, cask, store or AppMan package resolution is not completely pinned in the lock. CI tests adapter behavior with fakes rather than performing real service installs. |
| `sdkman`, `steamcmd`, `pacstall`, `aur` (including `paru`/`yay` aliases), `conda`, `asdf` | 2 | Adapter-specific selectors may be passed to the underlying tool, but immutable lock coverage is incomplete and CI does not verify installs against each real ecosystem. |

This classification follows the current lock projection: it pins supported
resolved GitHub/URL artifact identities and checksums or local artifact
digests, but does not pin ecosystem package-manager versions and full package
dependency resolutions. Ecosystem adapter tests use fake-runner unit tests.
The macOS and Windows jobs run the Go test suite on those operating systems;
they do not make ecosystem registry installs real-platform integration tests.
Linux integration jobs require Docker and cover the listed distro/native
package-manager scenarios, not the ecosystem registries above. A future move
to Tier 1 requires both complete lock coverage and relevant real CI behavior
tests.

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

<h1 align="center">depengine</h1>

<p align="center">
  <b>Install the tools a project needs, without tying the project to one distro.</b>
</p>

<p align="center">
  <a href="https://github.com/Khorea1/depengine/actions"><img src="https://github.com/Khorea1/depengine/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-GPL--3.0--or--later-blue" alt="License"></a>
  <img src="https://img.shields.io/badge/go-1.27-blue" alt="Go 1.27">
</p>

depengine reads a `schema.toml`, chooses an install method available on the
current machine, and tries the configured fallbacks until one works. It can use
native package managers, language package managers, GitHub releases, direct
downloads, Git builds, Flatpak, and other adapters.

It ships as a single Go executable with no separately installed depengine
runtime. Release builds use `CGO_ENABLED=0`, so Linux artifacts do not acquire
libc dependencies through cgo; platform binaries still use the operating
system's normal kernel/system interfaces. Host detection is native Go on every
supported platform: depengine reads standard OS metadata directly and, when
useful, runs narrowly scoped read-only probes such as `uname`, `sw_vers`,
`ldd`/`getconf`, or `cmd.exe` through the shared runner boundary. A POSIX shell
is not required for host detection.

```sh
depengine init --add "zsh,bat,nvim,ruff"
depengine validate
depengine install
depengine status
```

Linux has the broadest integration coverage. macOS and Windows run the Go test
suite on native CI runners. Windows supports winget, Scoop, Chocolatey, file
locking, and state handling, but its package-manager lifecycle coverage is
still lighter than Linux.

## Installing depengine

Use the artifacts published on the project's [GitHub Releases
page](https://github.com/Khorea1/depengine/releases). Release tags produce
platform archives plus supported native package artifacts, a SHA-256 checksum
file, and keyless cosign material. Install the matching artifact for your
platform and place `depengine` on `PATH`. Building from a source checkout with
the pinned Go toolchain is also supported.

For an initial **source** installation on Linux or macOS (without an existing
`depengine` executable), download and inspect the repository's standalone
bootstrap script, then run it with Git and a compatible Go toolchain installed:

```sh
curl -fsSLo ./install-source.sh \
  https://raw.githubusercontent.com/Khorea1/depengine/master/scripts/install-source.sh
sh ./install-source.sh --bin-dir "$HOME/.local/bin"
# Optionally choose a specific tag or commit with --ref.
```

The script clones a temporary checkout, builds using the checkout's `go.mod`,
checks that the result starts, and stages a new executable in the destination
directory before an atomic rename. Build/clone failures leave the existing
executable in place. It does **not** register the bootstrap in depengine's
managed state or provide cross-platform transactional self-upgrade. Put the
chosen destination on `PATH`; avoid running unreviewed remote scripts as root.

Version-suffixed module installation such as
`go install github.com/Khorea1/depengine@vX.Y.Z` is **not** a supported
distribution path. The main module intentionally contains `replace` directives
for transitive `gopkg.in` sources, and the Go tool rejects `go install
pkg@version` when the providing module contains `replace` directives. Use a
published release artifact or build from a checkout instead.

After bootstrapping, a depengine source checkout can be declared as an **ordinary
Git method**, without a special `self` command:

```toml
schema_version = 1

[tools.depengine]
method_only = ["git"]

[tools.depengine.git]
url = "https://github.com/Khorea1/depengine.git"
branch = "master"
build = { run = ["go", "build", "-buildvcs=true", "-o", "depengine", "."] }
artifact = "depengine"
extract_to = "~/.local/bin"
binary = "depengine"
```

The Git adapter now verifies the installed revision from the clean Go build's
VCS stamp. **Do not use this recipe to upgrade the running depengine binary yet**:
the generic upgrade replacement still removes the previous installation before
building the replacement. The standalone bootstrap above stages the replacement
safely, but transactional managed self-upgrade remains separate follow-up work.

## Quick start

Create a project schema:

```toml
schema_version = 1

[tools]
simple = ["zsh", "bat", "kitty"]

# Native package names may vary by package manager.
fd = { apt = "fd-find" }

# Ecosystem buckets expand to several compatible methods.
ruff = { python = true }
```

Then validate and install it:

```sh
depengine validate
depengine install
depengine status
```

Commit `schema.toml` with the project. Other contributors can run
`depengine install` after cloning the repository and get the same declared tool
set, using the methods available on their machine.

For conditions, hooks, dependencies, archives, GitHub assets, checksums, and
every supported method, see the [`schema.toml` reference](docs/schema-reference.md).

## Schema, manifest, and catalog

depengine can merge three layers:

| Layer | Purpose | Commit it? |
|---|---|---|
| `schema.toml` | Project dependencies and project-owned install rules | Yes |
| `~/.config/depengine/manifest.toml` | Personal recipes and machine defaults | No |
| `<config dir>/depengine/catalog/<name>.toml` | Reusable user-owned recipes, looked up by name | No |

Project values win when layers set the same field; the personal manifest
overrides catalog recipes. By default, tools that exist only in the personal
manifest are ignored. Set `[manifest] allow_new_tools = true` in the manifest
if you want them added to a project run.

Project schemas use `[tools]`; personal manifests and catalog recipes use
`[packages]` (a catalog recipe declares exactly one package and uses
`recipe_version = 1`). Catalog recipes are consulted only for tools the project
schema declares and for their transitive `requires`.

The catalog defaults to `$XDG_CONFIG_HOME/depengine/catalog` on Linux,
`~/Library/Application Support/depengine/catalog` on macOS, and
`%AppData%\depengine\catalog` on Windows. Use `--catalog` (repeatable) or
`DEPENGINE_CATALOG` to replace the roots, and `--no-catalog` to disable the
layer. Lock v2 pins each catalog recipe's content digest, so
`--frozen-lockfile` fails when a recipe changes; catalog-sourced tools require
a v2 lock, written by a whole-schema `depengine update`.

`depengine import <recipe.toml>` validates a recipe before writing it into the
first catalog root. It rejects hooks, command-typed fields and plans that
require arbitrary code, requires a checksum for downloaded artifacts, and flags
`sha256:auto` as trust-on-first-use. The preview shows the recipe digest that a
v2 lock will pin. Import does not verify the recipe's origin.

See [manifest merge rules](docs/schema-reference.md#manifest-merge-rules) and
[catalog recipes](docs/schema-reference.md#catalog-recipes) for the field-level
behavior.

## Common commands

| Command | Purpose |
|---|---|
| `depengine init` | Create `schema.toml` |
| `depengine import <recipe.toml>` | Validate and add a recipe to the catalog |
| `depengine validate` | Validate configuration without installing |
| `depengine install` | Install declared tools |
| `depengine check <tool>` | Check one tool |
| `depengine status` | Show recorded install state |
| `depengine doctor` | Diagnose the host, layers, managers, elevation, and lock |
| `depengine why <tool>` | Show which method would be chosen and why |
| `depengine explain <term>` | Look up a schema field, method, or error code offline |
| `depengine remove <tool>` | Remove a tool |
| `depengine forget <tool>` | Remove a tool from state without uninstalling it |
| `depengine update` | Resolve lockable mutable references and write `depengine.lock` |
| `depengine upgrade` | Upgrade using the lockfile where supported |
| `depengine graph` | Show tool dependencies |
| `depengine undo` | Restore a previous install snapshot |
| `depengine sbom` | Export CycloneDX or SPDX SBOM data |

The complete command and flag reference is in
[`docs/cli-reference.md`](docs/cli-reference.md).

## Install methods

depengine currently supports:

| Group | Methods |
|---|---|
| Native | `native`, plus package-manager aliases such as `apt`, `pacman`, `dnf`, `brew`, `port`, `winget`, `scoop`, `choco` |
| Languages | `cargo`, `go`, `pip`, `pipx`, `uv`, `npm`, `pnpm`, `bun`, `gem`, `yarn`, `yarn-berry`, `composer`, `apm` |
| Desktop | `flatpak`, `snap`, `vscode`, `vscodium`, `cask`, `mas`, `appman` |
| Specialized | `sdkman`, `pacstall`, `aur`, `conda`, `asdf`, `nix`, `container`, `appimage`, `android`, `deb`, `msi`, `exe`, `msix`, `appx`, `macpkg`, `dmg` |
| Artifacts/builds | `git`, `local`, `github`, `http` |
`steamcmd` remains accepted by schema validation, but ordinary `depengine install` currently fails closed before installation because its observer cannot verify an installed server identity.

`native` detects the host package manager. A tool can also name a uniquely
identifying or explicitly disambiguated manager directly when package names
differ by distro. The shared pkg executable is not a global method alias;
use it only as a clan-scoped pkg_overrides key. On macOS, `native` defaults
to Homebrew; MacPorts is an explicit alternative with examples in the
[schema reference](docs/schema-reference.md).

`deb` installs a downloaded package through dpkg, records its package/version/architecture identity, and removes only that recorded package after confirming the identity still matches. It requires an explicit package-compatible distro target outside the Debian family.

## Lockfile and dry run

Lock paths are **per schema**, even when several schemas share a directory.
`schema.toml` continues to use `depengine.lock`; an alternate name such as
`desktop.toml` uses `desktop.toml.lock`. The mapping depends only on the schema
filename, not how many schemas happen to be present. `update`, `install`,
`upgrade`, `status`, `audit`, `doctor` and `sbom` use the same default rule; pass
`--lock path/to/custom.lock` to select an explicit path on those commands.
The SBOM uses the schema recorded in state when no `--lock` is supplied.

**Migration:** If you previously used `depengine.toml` or `depends.toml` with
`depengine.lock`, check which schema generated that lock. Move it to the
corresponding `<filename>.lock` only when its provenance is known; otherwise
regenerate with `depengine update --schema <filename>`. Frozen installs **do not
fall back** to a neighboring legacy lock. When renaming a schema, move its lock
explicitly or regenerate it. Symlink aliases are treated as separately named
schema paths unless the same explicit `--lock` is selected. Lockfile isolation
does **not** arbitrate conflicting global installs. Concurrent `update` and
non-dry-run `install` operations using the **same lock path** are serialized for
the full read/resolve/write operation via an OS file lock. A persistent hidden
sidecar `.<lockfile-name>.depengine-txn` is created alongside that lock; leave it
in place (and add `.*.depengine-txn` to your Git ignore rules if desired).
Crashes release the OS lock, while a second invocation may wait for an active
long-running installation. Separate lock paths are independent, and this does
not protect global package state across different projects.

`depengine.lock` v2 stores the immutable projection of each selected resolved
install plan. `depengine update` fails instead of writing a partial lock when a
non-native manager cannot expose a concrete version, revision, digest,
checksummed artifact, or required Git-backed source revision. A native package
name and presence-only check are not a version pin, so when native methods are
the only unpinnable tools the update keeps or refreshes the legacy v1 lock and
warns which tools blocked v2; it never downgrades an existing v2 lock.
`install --frozen-lockfile` and
`upgrade` replay that pinned plan and reject schema intent or resolved identity
drift; they do not re-resolve mutable selectors to fill gaps. For tracked tools
whose versions are outdated, `upgrade` performs the replacement even when an
adapter can only observe that the target is present, not verify its version.

Legacy v1 lockfiles remain readable with their method-specific guarantees
(resolved checksums/releases, Git commits, container digests, and supported
npm/pnpm/Yarn Classic package versions). A successful whole-schema update
rewrites them as v2; `depengine update --profile` only refreshes that profile's
v1 pins, because a partial resolution cannot prove a projection for the tools it
left out. On an existing v2 lock, a profiled update carries forward omitted
entries only when the resulting projection still exactly covers every
non-virtual tool in the current whole-schema install closure; otherwise it
fails and requires a whole-schema
update. Exact package versions still do not lock transitive dependency graphs.

```sh
depengine update
depengine install --frozen-lockfile
```

`depengine install --dry-run` plans the operation without running install
adapters or hooks and without writing state, lock data, package-source changes,
or download-cache entries. Planning may still perform read-only probes and
network resolution.

See [support boundaries](docs/support-boundary.md) for the current reproducibility limits.

## Offline artifact bundles

An offline bundle carries the payloads for eligible artifacts already identified
by a v2 `depengine.lock`. First create or refresh the lock, then export its
locked artifacts to a new directory:

```sh
depengine update
depengine bundle export --lock depengine.lock --output ./depengine-bundle
```

Export requires a readable v2 lock and a destination directory that does not
already exist. It fetches each eligible locked artifact, verifies its literal
SHA-256 checksum, and writes the payloads and bundle manifest. Only HTTP/GitHub
locked artifacts with a fixed `sha256:<hex>` checksum are eligible. Missing
checksums and signature-backed artifacts are rejected; signature material is
not included or verified by this bundle format.

The bundle is bounded to these artifact bytes. It does not make dependency
resolution or external package managers offline: package-manager metadata,
transitive package graphs, repositories, and other install inputs may still
need network access. A frozen lock constrains only identities represented by
the lock; it is not a universal offline or reproducibility guarantee.

See [support boundaries](docs/support-boundary.md#offline-artifact-bundles) for
the exact limits.

On the target machine, make the project's schema, v2 lockfile, and exported
directory available, then consume matching locked artifact payloads with:

```sh
depengine install --frozen-lockfile --bundle ./depengine-bundle
```

Bundle use requires a current frozen v2 lock. Installation checks that the
bundle artifact identities and checksums match the locked artifacts it will
use; invalid, missing, or mismatched entries fail before execution. The bundle
does not replace the schema or lockfile.

## Downloads and trust

`github` is the preferred method for GitHub release assets:

```toml
[tools.yq.github]
repo  = "mikefarah/yq"
asset = "yq_{os_any}_{arch_any}"
```

Use `local` for project-vendored files and archives, and `http` for direct
downloads. Artifact methods can verify fixed checksums; some methods also
support release-asset checksum discovery or signatures.

Credentials must not be embedded in HTTP(S) URLs. Private GitHub methods,
private HTTPS `git` methods, Git-backed Cargo sources, container image pulls,
and HTTP-backed artifact methods (`http`, `appimage`, `android`, `msi`, `exe`,
`msix`, `appx`, `macpkg`, and `dmg`) can
declare typed env-backed secret references so the credential is part of project
intent without storing its value. HTTP-backed artifact credentials are scoped to the
specific primary/checksum/signature request and require HTTPS for remote URLs
(plain HTTP is allowed only on loopback). Git uses its value as a scoped
Bearer token for clone, fetch, and same-origin recursive submodules. Cargo Git
sources are prefetched with the same scoped transport, then installed from the
local checkout so Cargo and crate build scripts do not receive the credential.
Without a typed GitHub reference, `GITHUB_TOKEN`, `GH_TOKEN`, or existing `gh`
authentication remain available for compatibility.

See [security](docs/security.md) before using hooks, build commands, mutable
downloads, or custom package sources.

## Editor support

[`schema/depengine.schema.json`](schema/depengine.schema.json) provides editor
completion and validation for `schema.toml`. For Taplo in VS Code:

```json
{
  "taplo.schema.enabled": true,
  "taplo.schema.url": "https://raw.githubusercontent.com/Khorea1/depengine/master/schema/depengine.schema.json"
}
```

## Environment variables

| Variable | Purpose |
|---|---|
| `DEPENGINE_LANG` | Opt-in CLI language for command summaries (`en` default, `pt`). Locale variables are ignored; flags, errors and logs stay English. |
| `DEPENGINE_DETECT_SCRIPT` | Deprecated explicit legacy host-detector override; normal detection is native Go |
| `DEPENGINE_MANIFEST` | Override the personal manifest path |
| `DEPENGINE_CATALOG` | Override catalog roots (path list; `--catalog` wins) |
| `DEPENGINE_CACHE_MAX_BYTES` | Download-cache size limit; `0` disables eviction |
| `DEPENGINE_DOWNLOAD_MAX_BYTES` | Maximum bytes accepted per HTTP artifact download (default 4 GiB; positive values override) |
| `XDG_CONFIG_HOME` | Base directory for the personal manifest and catalog |
| `XDG_CACHE_HOME` | Base directory for downloads |
| `XDG_STATE_HOME` | Base directory for state |
| `XDG_DATA_HOME` | Unix base directory for user-scope tool placement |
| `GITHUB_TOKEN` / `GH_TOKEN` | GitHub API and private release authentication |
| `NO_COLOR` / `FORCE_COLOR` | Control ANSI color output |
| `DEPENGINE_TRACE_ID` | Trace ID passed to subprocesses |
| `DEPENGINE_LOG_JSON` | Set to `1` for JSON logs |

When the home directory is unavailable and the XDG state or cache base is not
set, depengine uses a per-user private fallback: a checked directory below the
system temporary directory on Unix, or the OS-provided per-user cache directory
on Windows and other platforms. If that private location cannot be resolved or
validated, state and cache operations fail with a filesystem error; depengine
does not fall back to shared temporary paths or the current working directory.
## Documentation

| Document | Use it for |
|---|---|
| [`docs/schema-reference.md`](docs/schema-reference.md) | Schema syntax and install methods |
| [`docs/cli-reference.md`](docs/cli-reference.md) | Commands and flags |
| [`docs/cheatsheet.md`](docs/cheatsheet.md) | Copyable examples |
| [`docs/troubleshooting.md`](docs/troubleshooting.md) | Exit codes and fixes for common messages |
| [`docs/support-boundary.md`](docs/support-boundary.md) | What depengine can and cannot guarantee |
| [`docs/security.md`](docs/security.md) | Trust, arbitrary code, checksums, credentials |
| [`docs/compatibility.md`](docs/compatibility.md) | File-format version policy |
| [`docs/architecture.md`](docs/architecture.md) | Internal packages and execution flow |
| [`docs/development.md`](docs/development.md) | Where project notes and design records belong |
| [`docs/testing.md`](docs/testing.md) | Property tests, fuzzing, security checks and reproduction |
| [`docs/alternatives.md`](docs/alternatives.md) | Where depengine fits against mise, aqua, Nix, and Brewfile |
| [`docs/roadmap.md`](docs/roadmap.md) | Unfinished project work |

## Development

Run `scripts/setup-hooks.sh` once in each worktree to enable local commit and
push checks. The commit hook builds and vets the staged files; the push hook
builds, vets, race-tests, and lints each commit tip being pushed. Both require
Go; pushing also requires `golangci-lint`.

```sh
go test ./...
go vet ./...
go build -o depengine .
```

The slower distro integration suite lives under `tests/integration` and needs
Docker or Podman plus network access.

## License

[GNU General Public License v3 or later](LICENSE)

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

Version-suffixed module installation such as
`go install github.com/Khorea1/depengine@vX.Y.Z` is **not** a supported
distribution path. The main module intentionally contains `replace` directives
for transitive `gopkg.in` sources, and the Go tool rejects `go install
pkg@version` when the providing module contains `replace` directives. Use a
published release artifact or build from a checkout instead.

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

## Schema and personal manifest

depengine can merge two files:

| File | Purpose | Commit it? |
|---|---|---|
| `schema.toml` | Project dependencies and project-owned install rules | Yes |
| `~/.config/depengine/manifest.toml` | Personal recipes and machine defaults | No |

Project values win when both files set the same field. By default, tools that
exist only in the personal manifest are ignored. Set
`[manifest] allow_new_tools = true` in the manifest if you want them added to a
project run.

Project schemas use `[tools]`; personal manifests use `[packages]`.

See [manifest merge rules](docs/schema-reference.md#manifest-merge-rules) for the
field-level behavior.

## Common commands

| Command | Purpose |
|---|---|
| `depengine init` | Create `schema.toml` |
| `depengine validate` | Validate configuration without installing |
| `depengine install` | Install declared tools |
| `depengine check <tool>` | Check one tool |
| `depengine status` | Show recorded install state |
| `depengine why <tool>` | Show which method would be chosen and why |
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
| Native | `native`, plus package-manager aliases such as `apt`, `pacman`, `dnf`, `brew`, `winget`, `scoop`, `choco` |
| Languages | `cargo`, `go`, `pip`, `pipx`, `uv`, `npm`, `pnpm`, `bun`, `gem`, `yarn`, `yarn-berry`, `composer`, `apm` |
| Desktop | `flatpak`, `snap`, `vscode`, `vscodium`, `cask`, `mas`, `appman` |
| Specialized | `sdkman`, `steamcmd`, `pacstall`, `aur`, `conda`, `asdf`, `nix`, `container`, `appimage`, `android`, `msi`, `exe`, `msix`, `appx` |
| Specialized | `sdkman`, `steamcmd`, `pacstall`, `aur`, `conda`, `asdf`, `nix`, `container`, `appimage`, `android`, `msi`, `macpkg`, `dmg` |
| Artifacts/builds | `git`, `local`, `github`, `http` |

`native` detects the host package manager. A tool can also name a manager
directly when package names differ by distro.

## Lockfile and dry run

`depengine.lock` v2 stores the immutable projection of each selected resolved
install plan. `depengine update` fails instead of writing a partial lock when a
manager cannot expose a concrete version, revision, digest, checksummed artifact,
or required Git-backed source revision. `install --frozen-lockfile` and
`upgrade` replay that pinned plan and reject schema intent or resolved identity
drift; they do not re-resolve mutable selectors to fill gaps.

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

See [support boundaries](docs/support-boundary.md) for the current
reproducibility limits.

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
and HTTP-backed artifact methods (`http`, `appimage`, `android`, `msi`, `macpkg`, and `dmg`) can
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
| `DEPENGINE_DETECT_SCRIPT` | Deprecated explicit legacy host-detector override; normal detection is native Go |
| `DEPENGINE_MANIFEST` | Override the personal manifest path |
| `DEPENGINE_CACHE_MAX_BYTES` | Download-cache size limit; `0` disables eviction |
| `XDG_CONFIG_HOME` | Base directory for the personal manifest |
| `XDG_CACHE_HOME` | Base directory for downloads |
| `XDG_STATE_HOME` | Base directory for state |
| `XDG_DATA_HOME` | Unix base directory for user-scope tool placement |
| `GITHUB_TOKEN` / `GH_TOKEN` | GitHub API and private release authentication |
| `NO_COLOR` / `FORCE_COLOR` | Control ANSI color output |
| `DEPENGINE_TRACE_ID` | Trace ID passed to subprocesses |
| `DEPENGINE_LOG_JSON` | Set to `1` for JSON logs |

## Documentation

| Document | Use it for |
|---|---|
| [`docs/schema-reference.md`](docs/schema-reference.md) | Schema syntax and install methods |
| [`docs/cli-reference.md`](docs/cli-reference.md) | Commands and flags |
| [`docs/cheatsheet.md`](docs/cheatsheet.md) | Copyable examples |
| [`docs/support-boundary.md`](docs/support-boundary.md) | What depengine can and cannot guarantee |
| [`docs/security.md`](docs/security.md) | Trust, arbitrary code, checksums, credentials |
| [`docs/compatibility.md`](docs/compatibility.md) | File-format version policy |
| [`docs/architecture.md`](docs/architecture.md) | Internal packages and execution flow |
| [`docs/development.md`](docs/development.md) | Where project notes and design records belong |
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

# Support boundaries

depengine can describe many ways to install a tool, but those methods do not
all provide the same reproducibility or lifecycle guarantees.

## What depengine checks

A method only accepts fields it knows how to carry through planning and
execution. For example, a `version`, `scope`, `registry`, `channel`, revision,
or digest is rejected when the adapter cannot honor it.

Package-manager and ecosystem adapters use their native concepts. File and
archive adapters track concrete paths, checksums, and entrypoints so depengine
can tell what it owns and remove it safely.

Hooks, build commands, opaque vendor installers, and similar escape hatches are
less observable. depengine cannot infer complete ownership, rollback behavior,
or desired state from an arbitrary command.

## Reproducibility

Current `depengine.lock` files use format v2. `depengine update` resolves the
selected install candidate, projects its credential-free immutable identity into
an adapter-neutral `LockDocument`, and writes that projection alongside the
method-specific pins retained for migration. `install` and `upgrade` replay the
pinned plan and reject requested-intent or resolved-identity drift.

Generation is all-or-nothing. If a selected manager cannot expose a stable
version, revision, digest, checksummed artifact, or required Git-backed source
revision, `update` fails with `immutable lock identity unavailable`; it never
writes a partial v2 projection. This closes silent mutable re-resolution without
pretending unsupported managers are reproducible. Lock v1 remains readable and
keeps the method-specific behavior documented below; the next successful
whole-schema update migrates it to v2.

An exact package version constrains one package. A fully locked package graph
also needs immutable identities for its dependencies.

Current ecosystem adapters should therefore be treated as best-effort for
reproducibility. Their behavior is covered by unit tests, often through fake
runners, while real registry and package-manager integration coverage varies by
platform.

The useful identity terms are:

| Term | Meaning |
|---|---|
| requested identity | What the schema asks for, such as `1.2.3`, `stable`, a branch, or `latest` |
| resolved identity | The concrete version, release, commit, or artifact selected during resolution |
| locked identity | Immutable information persisted in `depengine.lock` for methods the lock model supports |

Digests and commits are stronger replay inputs than mutable tags, branches,
channels, or URLs.

Legacy lock v1 covers each selector class as follows:

| Selector requested in schema.toml | Pinned in depengine.lock? | Note |
|---|---|---|
| `{latest}` inside a literal `url` | Yes | The resolved release tag is stored as a pin and substituted into the URL before adapters see it. |
| Latest release of a repo-backed method: `repo` present, `release` empty or `"latest"`, and no `branch` (the `repo` + `asset` forms of `github`, `http`, `appimage`, `android`, and `msi` qualify) | Yes | The latest release tag at resolution time is stored as a pin; installation resolves the asset only within that pinned release. |
| Literal `checksum = "<algo>:<hex>"` | Yes | Recorded alongside the method. The schema's literal value still governs, because the lock only substitutes a checksum into `*:auto` and checksum-less local declarations. |
| `checksum = "<algo>:auto"` | Yes, once materialized | The digest the adapter resolves from the declared checksum source is recorded by a normal non-frozen install. Frozen validation requires that pin and never computes one, and `depengine update` does not download payloads. |
| `local_path` artifact | Yes | The content digest is computed during lock resolution even when the schema omits a checksum. |
| Exact ecosystem version (`version = "1.2.3"` on npm, pip, cargo, go, ...) | No | `version` is never written to the lock, and the dependency graph behind it is not locked either. |
| Plain npm, pnpm, or Yarn Classic registry package without `version` | Yes, package version only | The selected manager resolves the registry's `latest` dist-tag to a concrete version. The lock also hashes the requested package and declared registry; frozen installs reject drift and install `pkg@version` without re-querying `latest`. Dependencies and an undeclared registry selected by local manager configuration are not pinned. An explicit `version = "latest"`, package alias, URL, path, or already-versioned package spec is outside this coverage. |
| Native package install (apt, dnf, pacman, brew, ...) | No | Native methods carry no version field for the lock to record. |
| Git `branch` or `tag` | Yes | Lock v1 stores the requested selector plus its concrete 40/64-hex commit. Annotated tags use the peeled commit; install reuses the locked commit without `ls-remote`, and frozen mode rejects a missing pin or selector drift. |
| `cargo` git source (`branch` or `tag`) | Yes | The requested branch/tag remains in schema intent, while lock v1 stores the concrete commit resolved through Git. Install uses `cargo install --git ... --rev <commit>`; frozen mode rejects a missing pin or selector drift. |
| `cargo` git source (`rev`) | No extra pin | `rev` is already an explicit immutable selector in schema; lock v1 adds no second copy. |
| Container `tag` (including implicit `latest`) | Yes | Lock v1 stores the requested tag plus the immutable OCI manifest digest resolved through the registry API. Install, status probes, and removal reuse the digest; frozen mode rejects a missing pin or tag drift. |
| Snap channel or track | No | The lock records no resolved snap revision. |
| Flatpak `branch` | No | The lock records no resolved commit on that branch. |
| `sdkman` or `asdf` version | No | The lock records no resolved version. |
| Selectorless kinds (`cask`, `mas`, `aur`, `apm`, `vscode`, ...) | No | There is no selector for the lock to pin. |
| Artifact method with no `checksum` | No | Downloaded content is unpinned. |
| Explicit literal selector (`release`, `branch`, `version`, `rev`, or `digest`) | No — fixed by the schema | The same literal value is requested on every run, so the lock adds no constraint of its own. It also records nothing about where a mutable request such as a git `branch` or an exact package `version` resolved. |
| Candidate host package sources (`sources = [...]`) | Declaration identity plus Git-backed source revision preflight | Lock v1 hashes source `kind`/`name`/credential-free `url` and an optional Brew/Scoop `revision`; frozen validation rejects declaration drift. A declared revision is compared with the local source HEAD at preflight. Homebrew may auto-update a tap during install; PPA/COPR publication state and signing-key trust remain unpinned. |

For the legacy lock v1 subset, `depengine install --frozen-lockfile` fails
closed when the lockfile is missing, unreadable, or written for an unsupported
lock version; when a tool's method identity is absent from the lock — the tool
was added to the schema after the lock was written; when the stored method
kind/label ordering no longer matches the schema; or when a required supported
pin is missing. Required pins currently include repo-backed latest GitHub
releases, `{latest}` URL templates, implicit local-artifact digests, resolved
`*:auto` checksums, concrete commits for direct-Git or Cargo-Git branch/tag
selectors, immutable digests for mutable container tags, and concrete versions
for plain unversioned npm, pnpm, and Yarn Classic packages. Candidate host
sources additionally require their stored source-identity hash to be present and to match the schema. Frozen mode
does not perform checksum TOFU to
create a missing auto-checksum pin. Remote `*:auto` checksums are materialized
by a normal non-frozen install; `depengine update` alone does not download the
remote payload needed to compute that digest.

Frozen validation follows the effective install closure after
`--only`/`--skip`/`--profile` filtering. Dependencies pulled into that
closure are still validated, while deliberately omitted tools do not make a
partial/profile install fail frozen validation.

`depengine update` is the operation that accepts changed method or package-source
identity. It
re-resolves the lockable selectors for every tool in scope — the whole schema,
or only tools matching `--profile` — and refreshes those pins and their stored
identity hashes. Pins the fresh resolution cannot recompute, such as an
already-materialized `*:auto` checksum, are carried over from the existing
lock instead of being dropped, as are the pins and identities of tools
excluded by `--profile`, provided the existing lock is readable. A `--profile`
update over a lock that is not yet v2 refreshes only that profile's v1 pins and
writes no projection: a partial resolution cannot prove identity for the tools
it left out, so the next whole-schema update performs the v2 migration. On an
existing v2 lock, omitted entries are retained only for installable tools still
in the current whole-schema install closure, and the final projection must cover
every non-virtual tool in that closure exactly. If the schema added an
out-of-profile tool that the previous
projection cannot supply, the profiled update fails without rewriting the lock;
a whole-schema update is required. If the existing lock is unreadable or has an
unsupported version, `update` warns and
regenerates from the fresh resolution; it cannot preserve data it cannot
parse. A plain `depengine install` never changes stored method or package-source identity:
frozen installs fail validation against a changed identity, and non-frozen
installs keep the stored hash and only warn. Non-frozen installs complete any
supported pins missing from a readable older lock before planning, then persist
that same resolved value after execution.

This check is intentionally narrower than universal immutable resolution.
Package-manager constraints, channels, and other selectors not represented by
legacy lock v1 are not made immutable by `--frozen-lockfile`. Direct Git and
Cargo Git branch/tag selectors persist concrete commits, container tags
persist concrete OCI manifest digests, and plain unversioned npm, pnpm, and
Yarn Classic packages persist a concrete package version. These pins do not fix their dependency graphs. The v1 method identity hash also covers method kind,
label, and ordering rather than every requested field inside a candidate;
candidate host-source declarations are covered separately by source hashes.
After changing resolver details that keep the same kind/label, run
`depengine update`; the universal lock projection
([ADR-001](design/adr-001-universal-lock-projection.md)) closes this
requested-identity gap: `update` resolves the selected install candidate,
projects its immutable identity into a v2 `LockDocument`, and persists it
alongside the legacy pins, while `install` and `upgrade` replay the pinned
plan and reject requested-intent or resolved-identity drift.

## Scope and environments

`scope` describes who owns an install, usually user versus system/global.
`environment`, `prefix`, install roots, and profiles select a target namespace.
These are separate settings.

An adapter should use the same target for install, check, version reporting,
and remove. If the underlying tool cannot identify that target reliably,
depengine should not expose a field that implies it can.

Desired-state checks reconcile the resolved plan with the adapter's reported
identity. `unknown` means depengine lacks authoritative observations for one or
more desired fields; it does not mean the target is absent. `broken` means the
observation is invalid or verification failed. Destructive upgrade and remove
operations stop for either state.

## Package sources

A package source, registry, remote, bucket, or channel selects where a package
comes from. Adding a host-wide repository, PPA, COPR, Brew tap, or Scoop bucket
is a separate machine mutation.

Source setup happens only when the relevant candidate is reached. Cleanup needs
ownership evidence so depengine does not remove a source shared with other
tools or created outside depengine.

For Git-backed Brew taps and Scoop buckets with an explicit URL, presence is
not established by name alone: depengine verifies the configured repository
origin. A same-name source with a different origin blocks that candidate rather
than being adopted as external/shared state. Source preparation persists the
credential-free URL alongside the transactional journal so recovery uses the
same verification rule.

For containers, `source` is the repository name, including an optional registry
host. Tags and digests are separate fields.

## What depengine cannot promise

depengine cannot make an external package manager transactional, give it
immutable resolution if the manager does not provide it, normalize every
version syntax, or prove that an upstream package is trustworthy.

New method options should be represented by validated fields with defined
runtime behavior. Generic argument bags are avoided because they hide details
depengine needs for checks, removal, locks, and dry runs.

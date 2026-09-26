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

Lock coverage is method-specific today.

`depengine.lock` can pin supported resolved artifact identities and checksums,
including GitHub or URL-based artifacts and local artifact digests. It does not
yet capture complete immutable resolution for every native or ecosystem package
manager.

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

For the legacy lock v1 subset, `depengine install --frozen-lockfile` fails
closed when the lockfile is missing or unreadable, when the stored method
kind/label ordering no longer matches, or when a required supported pin is
missing. Required pins currently include repo-backed latest GitHub releases,
`{latest}` URL templates, implicit local-artifact digests, and resolved
`*:auto` checksums. Frozen mode does not perform checksum TOFU to create a
missing auto-checksum pin. Remote `*:auto` checksums are materialized by a
normal non-frozen install; `depengine update` alone does not download the
remote payload needed to compute that digest.

Frozen validation follows the effective install closure after
`--only`/`--skip`/`--profile` filtering. Dependencies pulled into that
closure are still validated, while deliberately omitted tools do not make a
partial/profile install fail frozen validation.

This check is intentionally narrower than universal immutable resolution.
Container tags, Git branches/tags, package-manager constraints, channels, and
other selectors not represented by legacy lock v1 are not made immutable by
`--frozen-lockfile`. The v1 method identity hash also covers method kind,
label, and ordering rather than every requested field inside a candidate.
After changing resolver details that keep the same kind/label, run
`depengine update`; the planned universal lock projection is the path that
will close this requested-identity gap.

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

For containers, `source` is the repository name, including an optional registry
host. Tags and digests are separate fields.

## What depengine cannot promise

depengine cannot make an external package manager transactional, give it
immutable resolution if the manager does not provide it, normalize every
version syntax, or prove that an upstream package is trustworthy.

New method options should be represented by validated fields with defined
runtime behavior. Generic argument bags are avoided because they hide details
depengine needs for checks, removal, locks, and dry runs.

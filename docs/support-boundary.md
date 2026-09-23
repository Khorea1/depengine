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

An exact package version is still useful, but it is not automatically the same
as a fully locked package graph.

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

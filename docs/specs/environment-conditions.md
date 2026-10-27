# Environment conditions: semantic contract

Status: contract for implementation (dep-ixd.5, accepted 2026-10-25).
`schema_version` stays `1`; no parser alias or migration is introduced.

This document defines how a `when` condition is evaluated against host facts.
It is authoritative for `internal/config` matching, `depengine validate`,
`depengine why`, and candidate selection.

## Terms

- **Fact**: one observed property of the host, represented by
  `platform.Facts`.
- **Condition**: one `when` table. It is a conjunction of dimensions.
- **Known / unknown trait**: a boolean fact whose probe either produced a
  conclusive answer or failed to produce one. `is_wsl` and `is_container`
  are three-state: true, false, or unknown.

## Matching model

- Fields are ANDed. Every non-empty field must match.
- List values within one field are ORed.
- Empty fields are ignored. A condition with only `arch` does not require
  `libc`, `kernel`, `init_system`, or either environment trait.
- A nil or absent `when` always matches.
- A non-empty condition never matches nil facts.
- A missing or unobservable fact never satisfies a dimension that requires it.
  This is fail-closed: an absent value is not treated as a wildcard.

## Dimensions

| Field | Type | Contract |
|---|---|---|
| `distro_family` | string[] | Exact, case-insensitive match against the resolved clan (`platform.ResolveFamily`). Groups related distributions: `debian` covers Debian, Ubuntu, Mint, and derivatives whose `ID_LIKE` resolves to the clan. |
| `target_family` | string[] | Exact, case-insensitive. `unix` or `windows`. |
| `distro_id` | string[] | Exact, case-insensitive match against the raw `ID` (for example `ubuntu`, not its family). |
| `distro_version` | string[] | Exact normalized comparison using `platform.CompareVersion`. Not SemVer. |
| `distro_version_min` | string | Inclusive minimum via `CompareVersion`. A host with no detected version does not match. |
| `distro_version_max` | string | Inclusive maximum via `CompareVersion`. A host with no detected version does not match. |
| `arch` | string[] | Exact, case-insensitive match against `uname -m` (normalized). |
| `os` | string[] | Exact, case-insensitive match against the runtime kernel name (`linux`, `darwin`, `windows`, `freebsd`, `openbsd`, `netbsd`). |
| `kernel` | string[] | Exact, case-insensitive match against `uname -r`. Exact equality is not a range: `6.7` does not match `6.7.0-arch1-1`. |
| `kernel_min` | string | Inclusive minimum via `CompareVersion`. A host with no detected kernel does not match. |
| `kernel_max` | string | Inclusive maximum via `CompareVersion`. A host with no detected kernel does not match. |
| `libc` | string[] | Family-token prefix match, trimmed and case-insensitive. Detection emits `glibc`, `musl`, `uClibc`, or `freebsd-libc` without a version. `glibc` matches `glibc` and a versioned fact such as `glibc 2.35`, but `glibc 2.35` does not match the detected `glibc`. |
| `init_system` | string[] | Exact, case-insensitive. |
| `is_wsl` | bool | Three-state. `true` requires a positive detection. `false` requires a conclusive negative. An inconclusive probe matches neither. |
| `is_container` | bool | Three-state, same rule as `is_wsl`. |
| `is_android` | bool | Boolean fact detected from Termux environment markers or Android build properties. `os` reports `linux` on Termux, so use this field instead of `os = ["android"]`. |

### Kernel equality versus range

`kernel` is an exact match. Use `kernel_min`/`kernel_max` for a range; both
bounds are inclusive and compared with the same numeric/text-run comparison used
for distribution versions. Do not write `kernel = ["6.7.*"]`: the value is
literal and will never match. `validate` reports `W_AMBIGUOUS_CONDITION` for
wildcard or comparison-operator syntax in `kernel` and `distro_version`.

### libc normalization

Both sides are trimmed and lowercased. The rule is prefix matching because a
future detector may append a version to the family token. Current detection
does not emit a version, so a condition that includes one (for example
`glibc 2.35`) can never match; `validate` reports
`W_AMBIGUOUS_CONDITION` for such values. An empty or `unknown` libc fact never
matches.

### Three-state environment traits

Detection separates a conclusive negative from an inconclusive probe:

| Trait | Positive | Conclusive negative | Inconclusive |
|---|---|---|---|
| `is_wsl` | `microsoft` in `uname -r` or `/proc/version`, or `WSL_DISTRO_NAME`/`WSL_INTEROP` set | Non-Linux host, or `/proc/version` readable and no positive signal | Linux host, `/proc/version` unreadable, and no positive signal |
| `is_container` | `/.dockerenv`, `/run/.containerenv`, `container` env var, or a cgroup entry naming docker/containerd/lxc | Non-Linux host, or `/proc/1/cgroup` or `/proc/self/cgroup` readable and no positive signal | Linux host, both cgroup files unreadable, and no positive signal |

Matching rules:

- `is_wsl = true` / `is_container = true`: match only on a positive detection.
- `is_wsl = false` / `is_container = false`: match only on a conclusive
  negative. Unknown does not match.
- Omitting the field imposes no requirement.

The `{is_wsl}` and `{is_container}` placeholders expand to `true`, `false`, or
`unknown`. A template that cannot tolerate `unknown` must guard the candidate
with an explicit `when` condition.

## Diagnostics

- `depengine why` reports `skip_when` with the first failing dimension, for
  example `is_wsl is false but detection is inconclusive`.
- `depengine validate` reports `W_AMBIGUOUS_CONDITION` for exact-match
  fields written as ranges, libc values carrying a version, and inverted
  min/max pairs. Warnings are soft; `--strict` turns them into a non-zero exit.
- A condition with no recognized keys remains `W_UNKNOWN_WHEN_KEY`.

## Test matrix

The contract is exercised with representative facts rather than the author's
host:

- Debian/Ubuntu (glibc, systemd, kernel range), Alpine (musl, openrc),
  Arch (rolling kernel), macOS, Windows, Termux/Android.
- Kernel equality versus `kernel_min`/`kernel_max` boundaries.
- libc family, prefix, case, and versioned-counterexample values.
- WSL and container: positive, conclusive negative, and inconclusive.
- A dimension-independent condition (arch only) on a host with every other
  fact missing.
- `distro_family` grouping versus exact `distro_id`.
- Detection failure paths: unreadable `/proc/version` and cgroup files.

## Compatibility

`schema_version` remains `1`. Existing conditions keep their meaning, with one
intentional fail-closed change: on a Linux host where WSL or container detection
is inconclusive, `is_wsl = false` or `is_container = false` no longer matches.
Recipes that relied on "not detected means false" must either accept the
inconclusive case explicitly (omit the field) or keep the condition and rely on
the fact that an inconclusive host is not silently treated as a conclusive
negative.

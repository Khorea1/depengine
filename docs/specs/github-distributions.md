# GitHub distributions: semantic contract

Status: contract for implementation (dep-1n0, proposal E approved 2026-10-11).
Replaces the `cases` static-expansion hypothesis (spike `spike/cases-expansion`,
archived). `schema_version` stays `1` per the pre-freeze policy
([schema-compatibility](schema-compatibility.md)): no legacy parser, no aliases,
no migration.

## Terms

- **Method**: one installation alternative with its own lifecycle and fallback
  (`github`, `aur`, `native`, ...). Methods are selected by priority, eligibility
  (`when`), and fallback, unchanged.
- **Distribution**: one packaging of the same software inside a method that needs
  it (`distributions`). It selects the host-compatible payload. A distribution
  never creates another method and never changes method, origin, or install cycle.
- **Asset selector**: identifies one file inside a release: the existing template
  string (`asset = "..."`) or an explicit regex (`asset = { regex = "..." }`).

## Grammar

A method without `distributions` works exactly as today. A method with
`distributions` carries common fields plus an exhaustive list:

```toml
[packages.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

[[packages.br.github.distributions]]
when = { os = ["darwin"] }

[[packages.br.github.distributions]]
when = { os = ["linux"], libc = ["musl"], is_android = false }
asset = "br-{version}-{os_any}_musl_{arch_any}.tar.gz"
```

Distribution fields (v1, closed list):

| Field | Required | Rule |
|---|---|---|
| `when` | yes | Existing `Condition` semantics; all distributions are validated on every host |
| `asset` | no | Template string or `{ regex = "..." }`; absent inherits base |
| `checksum`, `checksum_url`, `checksum_file_format`, `signature_url` | no | Absent inherits base; must match the resolved release, not another tag |
| `binary`, `strip_components`, `extract_to` | no | Absent inherits base; scalar replaces |
| `entrypoints` | no | Absent inherits base; declared replaces the whole map (no deep merge) |

Forbidden per distribution in v1: `repo`, `release`, `branch`, `scope`,
`post_install`, `pre_install`, `requires`, `kind`. The specializable-field set is
owned once per method contract (`internal/methodkind`), not duplicated across
parser, validator, and JSON Schema.

## Semantics

1. The `distributions` list is exhaustive: with `distributions` present there is
   no implicit base distribution.
2. The method-level `when`, when present, is an additional constraint over each
   distribution's `when` (AND, never OR).
3. Selection against host facts: zero match → method ineligible (next method may
   be tried); exactly one → inherit base fields, apply overrides, continue as a
   conventional candidate; more than one → manifest error before any GitHub call.
4. Specialization is pure (no network, no host mutation) and runs after layer
   merge, before the first effective plan (`BuildCandidateIntent`). Downstream
   (planner, adapters, `why`, state, lock) only ever sees the specialized
   conventional candidate.
5. Effective checksum/sidecar combinations are validated across every merged
   distribution before command workflows proceed, including variants that do not
   match the current host. Elevation preflight likewise specializes first, then
   considers only the selected candidate and its effective destinations.
6. Layer merge: `distributions` is atomic — a layer redefining it replaces the
   whole list (no index merge, no `when` matching). Common fields keep normal
   merge rules; distribution overrides apply only after the merge.
7. Failure classes: ineligibility (no distribution, unsupported host) allows
   fallback; ambiguity, invalid regex, or resolution mismatch (zero/multiple
   assets, forbidden format, bad checksum) is an explicit error, never silent
   fallback to another method.

## Asset matching

Literal templates keep current behavior. Regex form (`asset = { regex = "..." }`):

- RE2 (`regexp`), case-insensitive, anchored `^...$` by depengine;
  `{arch_any}`/`{os_any}` expand to escaped host-alias groups, `{version}` to the
  escaped release tag; anything else is literal.
- Invalid regex → validation error. Zero matches → resolution error. Multiple
  matches → ambiguity error. One match → format/integrity checks as usual.
- Regex selects names only; `when` plus the method contract still gate platform
  and lifecycle. Regex applies to release assets, never to AUR/native/ecosystem
  package names.

## Non-goals

- Portable `.exe` direct download stays with `dep-laz` (parallel work).
- Implicit native fallback inference stays a separate task.
- No global platform matrix, no per-distribution priority/fallback, no map
  patching, no `extends`/`inherit` flags.

## Acceptance cases (implemented from 8.2 on)

1. Real fixtures `br`/`omp`/`wt` parse under the new grammar with identical
   effective behavior per host (Linux glibc/musl, macOS, Windows, Android/Termux).
2. Ambiguous distributions on real facts → error before GitHub access.
3. Invalid regex, forbidden per-distribution field, empty list → validation error.
4. Cross-layer redefinition replaces the whole list; base-table override does not
   leak into sibling distributions.
5. Determinism: same facts + same release catalog → same selection or error.
6. Old locks/state incompatible with the new representation fail explicitly with
   no identity guessing and no silent ownership loss.

# archmap — architecture working model

Machine-checked architecture notes optimized for selective reading by humans and
LLMs. The codebase remains authoritative. The YAML model is the source for
**derived architecture views only** (DOT/SVG today), not a replacement for code,
ADRs or durable product documentation.

Schema: [`archmap.schema.json`](archmap.schema.json).

## Why this exists

Depengine is large enough that reconstructing its architecture from packages on
every session is wasteful. archmap stores the small amount of semantic structure
that is expensive to infer repeatedly: ownership, important runtime/data relations,
invariants and end-to-end flows. It deliberately does **not** mirror every Go
import, symbol or package. Mechanically recoverable facts should be derived by
tools instead of copied into YAML and then maintained twice, because humans
already invented enough synchronization bugs.

## Layout

```
index.yaml            entry point: project, externals, summaries/catalogues, glossary
units/<id>.yaml       architectural subsystems or actual runtime/deployable units
flows/<id>.yaml       ordered end-to-end scenarios
views/<id>.yaml       named projections: what a diagram should contain, never coordinates
notes/<id>.md         prose the graph cannot carry: intent, trade-offs, pitfalls
tools/validate.py     schema + semantic + code-revision validation
tools/render.py       view -> Graphviz DOT/SVG
```

A `unit` is intentionally broader than a deployable. In a monolithic CLI such as
Depengine, useful units are usually subsystems such as planning, reconciliation,
adapters or persistence. If the project later contains actual services/processes,
those may also be units. Do not lie about deployment topology merely to obtain
smaller files.

## Using archmap from code worktrees

The `dev-notes` branch is commonly checked out as the primary repository's
nested `.dev/` worktree. Sibling code worktrees created by helpers such as
`wt switch` therefore should **not** assume that `.dev/architecture/` exists
under their own root.

Resolve the actual `dev-notes` checkout with `git worktree list --porcelain`
as documented in `../INDEX.md`, then use
`<dev-notes-root>/architecture/index.yaml`. If `dev-notes` is not checked out,
`git show dev-notes:architecture/index.yaml` is sufficient for selective reads;
`git archive dev-notes architecture` can materialize a temporary tree when the
validator or renderer needs normal files.

This is deliberately a lookup rule, not a synchronization scheme. Do not copy
the model into every code worktree and manufacture another stale cache.

## Reading protocol

1. Read `index.yaml` only. Its one-line summaries decide what matters. Respect
   `scope`: when `exhaustive: false`, absence from the model is not evidence of
   absence from the codebase; use the declared fallback before concluding that a
   capability does not exist.
1. Open the relevant `units/<id>.yaml`, never all units by default.
1. For behaviour crossing units, open the matching `flows/<id>.yaml`.
1. When a relevant claim/step exposes `symbols`, use those as narrow symbol-aware
   starting points (Serena when available). Expand references/callers/implementations
   only as the task requires; do not dump a whole package symbol graph into context.
1. Open `views/<id>.yaml` when deciding which architectural projection to render.
1. Open `notes/<id>.md` for rationale and pitfalls.
1. `confidence: confirmed` means the claim was checked at that **document's**
   `verified.revision`; `evidence` names the supporting path/line at that revision.
   `inferred` is a useful hypothesis, not a fact.

## Symbol navigation

archmap and Serena solve different problems and should remain separate layers:

- **archmap stores meaning:** boundaries, invariants, important semantic relations,
  flow order, and a few deliberately chosen symbol starting points;
- **Serena derives structure:** declarations, references, callers/callees,
  implementations and package/file outlines from the current checkout;
- **text search is fallback:** use it when neither the semantic map nor symbol
  navigation gives a useful entry point.

`symbols` entries are therefore anchors, not an inventory and not evidence:

```yaml
symbols:
  - name: ResolveAll
    path: internal/lock/lock.go
    role: "Start here when changing which mutable selectors become lock pins."
```

Prefer a path-constrained symbol query first, fetch a body only when needed, and
expand references from a specific declaration rather than issuing broad symbol
queries. Serena's index can be exhaustive; the prompt context should not be.
If an anchor is missing, search symbols by the task's concrete nouns before
falling back to repository-wide text search.

Do **not** grow archmap toward symbol-level completeness. A useful inclusion test
is: would recovering this fact from code require architectural reasoning, or is it
mechanically derivable from the language/tooling? Only the former belongs here.

## Model rules

- **Ids are persistent.** They are join keys across files and generated views.
- **Catalogue summaries live only in `index.yaml`.** Detail files do not duplicate
  them. The index is deliberately cheap to read.
- **Scope is explicit.** A selective model must say so in the index and tell readers
  how to fall back to code search when no unit or flow matches the task.
- **References are dotted:** `unit` or `unit.component`. Within a unit file,
  `.component` is shorthand for that unit's own component. Flows/views use full
  references only.
- **A relation lives with its source.** External-source relations live in the
  index; unit/component-source relations live in the owning unit file.
- **Model semantic architecture, not imports.** `imports` is intentionally not a
  relation kind. Import/symbol/call graphs can be derived from Go when needed.
- **Symbol anchors are selective.** Add only declarations that materially shorten
  the jump from a semantic claim to code. Never enumerate every symbol in a unit.
- **Confirmed claims require evidence.** This applies to relations, flow steps and
  invariants.
- **Use `paths: []`, not a synthetic common parent.** A subsystem may legitimately
  span several package roots.
- **Freshness is per document.** Re-verify and bump only the document that was
  actually checked. With `--repo`, commit distance alone is not treated as stale:
  warnings appear when owned paths, evidence files or symbol-anchor paths changed
  since that document's recorded revision.
- **Views choose content, renderers choose layout.** Never put coordinates, colors,
  ranks or Graphviz/Mermaid-specific styling in a view file.
- **Planned units/components may reference paths that do not exist yet.** Active
  ones may not.
- Keep prose rationale in `notes/`; do not turn YAML into an ADR format.

## Evidence and revisions

Each document has:

```yaml
verified:
  revision: "0123abc"
  verified_on: "2026-09-28"
```

With `--repo`, evidence, owned paths and symbol-anchor paths are checked against
that Git revision using the repository object database, **not against the current
checkout**. This matters: a line that exists at HEAD may not have existed when the
architectural claim was verified. Freshness warnings additionally compare only
the document's relevant code paths against HEAD, avoiding useless "N commits
behind" noise after unrelated changes.

The index may additionally record the expected branch/ref:

```yaml
project:
  source:
    branch: master
```

That field is advisory metadata. Freshness comes from each document's recorded
revision.

## Views

A structural view is only a selection:

```yaml
archmap: 2
doc: view
id: core-pipeline
verified:
  revision: "0123abc"
  verified_on: "2026-09-28"
kind: structure
elements:
  - cli
  - configuration
  - planning
  - reconciliation
relation_kinds: [calls, reads, writes]
```

A flow view points at an existing flow:

```yaml
kind: flow
flow: install
```

The renderer decides positions and visual style. A future renderer can produce a
different presentation without mutating the architecture model.

## Commands

Python dependencies are sufficient for validation and DOT rendering. SVG
rendering additionally requires the Graphviz `dot` executable on `PATH`;
Graphviz is a system dependency and is therefore not part of
`tools/requirements.txt`.

```sh
pip install -r architecture/tools/requirements.txt

python architecture/tools/validate.py
python architecture/tools/validate.py --repo /path/to/depengine
python architecture/tools/validate.py --strict --repo /path/to/depengine
python architecture/tools/validate.py --json

python architecture/tools/render.py core-pipeline
python architecture/tools/render.py core-pipeline --format svg -o /tmp/core.svg

python -m pytest architecture/tools/tests
```

`--repo` matters because the dev-notes branch contains the architecture model but
not the code checkout. Without it, schema and graph semantics are still checked,
but code paths, evidence and Git freshness are not.

`--strict` turns warnings into failures. That is useful once placeholders are gone
and for deliberate local audits; do not wire it into unrelated CI merely to make
an orphan notes branch everybody else's problem.

## Current Depengine pilot

The checked-in model is intentionally small and project-specific. It currently
models six subsystem units, five high-value flows, and five views:

```
units/
  cli.yaml
  configuration.yaml
  planning.yaml
  reconciliation.yaml
  adapters.yaml
  state-and-lock.yaml

flows/
  install.yaml
  status.yaml
  update.yaml
  upgrade.yaml
  desired-state-observation.yaml

views/
  system-context.yaml
  core-pipeline.yaml
  execution.yaml
  reproducibility.yaml
  install-flow.yaml
```

This is a pilot, not an attempt to mirror every package. Add components, units,
flows or symbol anchors when they reduce the cost of understanding/change; do not
expand merely to improve coverage statistics or make the graph resemble the
directory tree. The generic placeholder model used by the tool tests lives under
`tools/tests/fixtures/model/`, so validator tests do not force the real Depengine
model to remain a template.

## Scope and lifecycle

This implementation is intentionally project-local while its shape is still
being proven against Depengine. There is no compatibility promise between
projects and no requirement to keep a hypothetical organization-wide schema in
sync. Generalize only after repeated real use demonstrates a stable abstraction.

Because `.dev/` is a separate working-context branch, archmap is not authoritative
for the code itself. If it becomes release-critical or CI-enforced, promote the
mature model/tooling into the normal code branch so architecture changes and code
changes can be reviewed atomically.

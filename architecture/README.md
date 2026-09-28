# archmap — architecture model

Machine-checked architecture map, written for LLMs first and humans second.
The YAML files are the source of truth; diagrams and prose are derived from them.
Schema: [`archmap.schema.json`](archmap.schema.json) (v1). It is project-agnostic and
identical across repositories.

## Layout

```
index.yaml          entry point: project, externals, catalogue of containers/flows, glossary
containers/<id>.yaml  one per deployable/major unit: components, invariants, relations
flows/<id>.yaml       one per end-to-end scenario: ordered steps, failure modes
notes/<id>.md         prose the graph cannot carry: intent, trade-offs, pitfalls
tools/                validate.py + tests
```

## Reading protocol (for LLMs and newcomers)

1. Read `index.yaml` only. Summaries are one line each and decide what to open next.
2. Open the `containers/<id>.yaml` you need, never all of them.
3. For behaviour across units, open the matching `flows/<id>.yaml`.
4. Open `notes/<id>.md` for the *why*.
5. Trust `confidence: confirmed` at the recorded `project.source.revision`; treat
   `inferred` as a lead to verify in the code. `evidence` gives the file and line.

## Writing rules

- **Ids are permanent.** kebab-case, unique per namespace. Renaming breaks references and diffs.
- **References are dotted:** `container` or `container.component`. In a container file,
  `.component` is shorthand for its own components. Flows use full references only.
- **A relation lives in the file that owns its source.** Externals are owned by the index.
  Never duplicate a relation on both ends.
- **`confirmed` requires `evidence`** (`path`, `path:LINE`, `path:START-END`). If you did not
  check the code, write `inferred`.
- **Summaries are one line** (8–200 chars). The index summary and the file's own summary
  must be identical; the validator enforces it.
- **Quote strings that look like other types** (dates, versions). YAML is parsed as 1.2,
  so `no`/`on` stay strings, but `1.10` and `2026-01-01` do not.
- Add org-specific fields only under an `x-` prefix.
- After re-verifying against the code, bump `project.source.revision` and `verified_on`.

## Commands

```sh
pip install -r architecture/tools/requirements.txt

python architecture/tools/validate.py                      # schema + semantics
python architecture/tools/validate.py --repo /path/to/code # + paths, evidence, revision drift
python architecture/tools/validate.py --strict --repo ...  # warnings are errors (use in CI once filled)
python architecture/tools/validate.py --json               # machine-readable, for LLM loops

python -m pytest architecture/tools/tests
```

`--repo` matters: this branch does not contain the code, so without it paths and evidence
are not checked.

## Filling the template

The shipped files are a valid, minimal example. Everything to replace contains `TODO`, an id
starting with `placeholder-`, or the zeroed revision; the validator counts these per file and
`--strict` fails until none remain. Rename `placeholder-*` files together with their `id`,
delete what does not apply, and add one container/flow file per real unit.

Start small: one container, verified with `--repo`, before scaling out.

## Using it in another project

Copy `archmap.schema.json` and `tools/` unchanged, then create that project's own
`index.yaml`. Do not fork the schema per project: extend through `x-` keys, and change the
shared schema only with a version bump (`archmap: 2`). Once several projects use it, move
the schema and tools into a single org-owned repository and vendor from there.

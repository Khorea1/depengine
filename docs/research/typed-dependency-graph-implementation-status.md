# Typed dependency graph implementation status

Status: P1 semantic IR integration complete

The research proposal in `docs/research/typed-dependency-graph.md` identified the need for one typed intermediate representation between schema loading, scheduling, and rendering.

## Implemented

- typed graph IR (`Graph`, `Node`, `Edge`);
- semantic edge kinds (`ToolRequire`, `MethodRequire`);
- edge roles (`Scheduling`, `Activation`);
- semantic multiedge preservation;
- lossless retention of declared `requires_when` and method `when` conditions as opaque guard values;
- deterministic, human-readable condition labels for graph presentation;
- candidate-level method dependency walking, so same-kind candidates with distinct guards are not collapsed;
- deterministic canonicalization over complete edge semantics;
- defensive copying of node slice metadata;
- declared-graph construction through small graph-facing config interfaces;
- dependency-only node metadata;
- scheduling projection;
- Kahn topological sorting and cycle detection over scheduling edges only;
- DOT and Mermaid rendering from the typed IR;
- level-oriented text rendering from the typed IR;
- `constraint=false` for activation-only method edges in DOT;
- `depengine graph` builds one declared IR and reuses it for sorting and rendering;
- context-driven effective projection that evaluates guards and omits inactive edges;
- context-driven resolved projection that additionally filters method edges to the selected method;
- compatibility wrappers for existing graph APIs;
- unit coverage for builders, guards, candidate multiplicity, multiedges, canonicalization, projections, scheduling roles, and renderers.

## Guard boundary

`internal/graph` does not import `internal/config`.

Instead, config exposes graph-facing walkers using `fmt.Stringer`. The graph stores the concrete condition behind an opaque `Guard` interface. This keeps every condition field available for a future evaluator while using `Guard.String()` only for deterministic ordering and presentation.

This avoids the two failure modes the research warned about:

1. reconstructing incomplete conditions as ad-hoc strings;
2. coupling graph analysis directly to config internals.

## Compatibility behavior retained

The compatibility entry points `Sort(tools)`, `RenderMermaid(tools)`, `RenderDOT(tools)`, and `RenderText(levels, tools)` remain available. They translate existing graph-facing tool interfaces into the typed IR and delegate to graph-native functions.

The default graph remains a declared-schema view. No host facts are gathered by `depengine graph`.

## Intentionally deferred

- the config/platform adapter that evaluates graph guards against host facts;
- active/inactive edge retention and `--show-inactive` diagnostics;
- the install-plan adapter that supplies selected methods to the resolved projection;
- CLI `--view declared|effective|resolved`;
- terminal layered layout, routing, width fallback, component grouping, and isolated-node compaction.

`EffectiveView` and `ResolvedView` are graph-generic and require callbacks only when the input needs them. Missing required guard/method context returns `ErrProjectionUnavailable`; evaluator errors are wrapped with edge context.

## Design constraints validated

- `internal/graph` does not import `internal/config`;
- config exposes only small graph-facing primitives and walkers;
- one declared IR feeds sorting and rendering in the CLI path;
- activation edges are visible but never affect scheduling or cycle detection;
- method-edge rendering is explicitly non-constraining in DOT;
- semantic multiedges and candidate-specific guards survive graph construction;
- projections consume caller-supplied context without importing config/platform/plan types;
- missing projection context fails closed;
- the existing default CLI view stays host-independent and deterministic.

## Next implementation slice

1. wire a config/platform guard evaluator into the CLI for `effective`;
2. decide whether diagnostics should retain inactive edges or only omit them;
3. wire selected install-plan/candidate data into `resolved`;
4. expose `--view declared|effective|resolved` after those adapters are stable;
5. then implement terminal graph analysis/layout as another consumer of the same IR.

Terminal visualization remains a presentation follow-up; it no longer needs to invent dependency semantics.

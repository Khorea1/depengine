# Typed dependency graph implementation status

Status: typed IR, host-aware CLI projections, and the first terminal renderer complete

The research proposal in `docs/research/typed-dependency-graph.md` identified the need for one typed intermediate representation between schema loading, scheduling, and rendering.

## Implemented

- typed graph IR (`Graph`, `Node`, `Edge`);
- semantic edge kinds (`ToolRequire`, `MethodRequire`);
- edge roles (`Scheduling`, `Activation`);
- semantic multiedge preservation;
- lossless retention of declared `requires_when` and method `when` conditions as opaque guard values;
- deterministic, human-readable condition labels for graph presentation;
- candidate-level method dependency walking with retained candidate ordinals, so same-kind unlabeled candidates remain distinguishable through resolved projection;
- deterministic canonicalization over complete edge semantics;
- defensive copying of node slice metadata;
- declared-graph construction through small graph-facing config interfaces;
- dependency-only node metadata;
- scheduling projection;
- Kahn topological sorting and cycle detection over scheduling edges only;
- DOT and Mermaid rendering from the typed IR;
- level-oriented text rendering from the typed IR;
- `constraint=false` for activation-only method edges in DOT;
- `depengine graph` builds one declared IR and reuses it for projection, sorting, and rendering;
- context-driven effective projection that evaluates guards and omits inactive edges;
- context-driven resolved projection that filters method edges to the exact selected candidate before evaluating candidate guards;
- CLI config/platform guard adapter backed by the canonical `Condition.Match` semantics;
- exact candidate identity retained in explain attempts instead of reconstructing identity from kind/label;
- read-only resolved-candidate selection reused from `ExplainTool`, including correct fail-closed behavior when a synthesized winning candidate has no declared ordinal;
- `depengine graph --view declared|effective|resolved`, with `declared` preserving the existing host-independent default;
- weakly connected component analysis, isolated-node compaction, and per-component edge sets for layout;
- scheduling ranks exposed from levels for layered layout;
- `depengine graph --format graph`, an orthogonal terminal renderer with explicit `--width` input (0 detects terminal width through `internal/term`), per-component compact dependency-edge fallback when the layout exceeds the width, line-style edge semantics with an annotations section, and a wrapped isolated-node list;
- compatibility wrappers for existing graph APIs;
- unit coverage for builders, guards, candidate multiplicity, multiedges, canonicalization, projections, scheduling roles, renderers, CLI view parsing, guard adaptation, and exact candidate selection.

## Guard boundary

`internal/graph` does not import `internal/config`.

Config exposes graph-facing walkers using `fmt.Stringer`. The graph stores the concrete condition behind an opaque `Guard` interface. For evaluated CLI views, the app-layer adapter recognizes `*config.Condition` and delegates to `Condition.Match`; graph analysis itself still has no config/platform dependency.

This avoids the two failure modes the research warned about:

1. reconstructing incomplete conditions as ad-hoc strings;
2. coupling graph analysis directly to config internals.

An unknown concrete guard type fails closed instead of being treated as active.

## Compatibility behavior retained

The compatibility entry points `Sort(tools)`, `RenderMermaid(tools)`, `RenderDOT(tools)`, and `RenderText(levels, tools)` remain available. They translate existing graph-facing tool interfaces into the typed IR and delegate to graph-native functions.

The default `depengine graph` view remains `declared`: it gathers no host facts and preserves the previous deterministic output semantics. Host facts and candidate probes are used only when the caller explicitly requests `effective` or `resolved`.

## Projection policy

`effective` and `resolved` currently omit inactive edges. They do not retain a second inactive-edge set in the IR.

For `resolved`, candidate selection follows executor ordering and the read-only planning gates exposed by `ExplainTool`. The first `already_installed` or `would_install` attempt is the selected candidate. If that winner is synthesized and therefore has no exact declared ordinal, no later declared candidate is substituted; declared method activation edges for that tool are omitted.

`EffectiveView` and `ResolvedView` remain graph-generic and require callbacks only when the input needs them. Missing required guard/candidate context or exact candidate identity returns `ErrProjectionUnavailable`; guards on already-unselected candidates do not require evaluation, and evaluator errors are wrapped with edge context.

## Intentionally deferred

- active/inactive edge retention and a possible `--show-inactive` diagnostic mode;
- crossing reduction and layout-only edge bundling;
- Windows terminal-width detection (callers fall back to the default width).

## Design constraints validated

- `internal/graph` does not import `internal/config`;
- config exposes only small graph-facing primitives and walkers;
- one declared IR feeds every CLI projection, sorter, and renderer;
- activation edges are visible but never affect scheduling or cycle detection;
- method-edge rendering is explicitly non-constraining in DOT;
- semantic multiedges and candidate-specific guards survive graph construction;
- projections consume caller-supplied context without importing config/platform/plan types;
- missing projection context fails closed;
- exact candidate identity is carried from the merged method list rather than inferred from display metadata;
- the existing default CLI view stays host-independent and deterministic.

## Next implementation slice

1. improve crossing reduction and route bundling only as needed by real-schema snapshots;
2. retain inactive edges behind a possible `--show-inactive` diagnostic mode.

Terminal visualization can now consume declared, effective, or resolved graphs without inventing dependency semantics.

# Typed dependency graph and terminal visualization research

- Status: research / design proposal
- Scope: `depengine graph` representation, analysis, and future terminal rendering
- Implementation status: not started
- Naming used in this document: **DPG** = **DePenGine Graph** (project shorthand, not "Program Dependence Graph")

## Motivation

The current human-facing graph output is primarily a topological-level view:

```text
level 0: aichat, bat, ..., zathura, zsh
level 1: DepartureMono, marina, zathura-pdf-mupdf
```

That is useful for scheduling, but it is not a literal dependency-graph view. It
does not immediately show which prerequisites feed which dependent tools, fan-in
patterns, connected components, or the semantic difference between kinds of
dependencies.

The existing implementation already has most of the raw ingredients:

- `internal/graph.Sort` performs deterministic Kahn topological sorting and
  cycle detection;
- `internal/graph.RenderDOT` emits directed Graphviz edges;
- `internal/graph.RenderMermaid` emits Mermaid flowchart edges;
- `internal/graph.RenderText` emits topological levels;
- `collectEdges` reconstructs tool and method-scoped edges for renderers.

The proposed direction is therefore **not** to replace the graph subsystem with
a visualization library. It is to introduce a typed graph intermediate
representation that can be analyzed and rendered consistently.

## Current semantic gaps

The current interfaces collapse distinct schema semantics.

`Tool.GraphDependencies()` returns `Tool.Requires`. The graph sorter therefore
uses tool-level prerequisites as scheduling constraints.

`Tool.GraphConditionalDependencies()` currently returns only
`MethodCandidate.Requires`, grouped by method label/kind. DOT and Mermaid render
those as dashed method-labelled edges.

However, `Tool.RequiresWhen` is not represented as edge metadata by those graph
interfaces. A dependency that is declared as:

```toml
requires = ["unzip"]
requires_when = { unzip = { target_family = ["unix"] } }
```

is currently exposed to the graph renderer as an ordinary `requires` edge.
This loses the fact that the edge is guarded by host facts.

There is also a deliberate semantic difference between:

- tool-level `requires`, which participates in topological scheduling; and
- method-level `method.requires`, which is a lazy candidate prerequisite and
  does not currently participate in `Sort`.

The future model should make both distinctions explicit rather than relying on
renderer-specific conventions.

## Terminology and compiler-graph inspiration

Program Dependence Graphs (PDGs) are useful as an architectural inspiration
because they make multiple dependence relations explicit in one intermediate
representation. The original PDG work combines data dependence and control
dependence.

Those compiler terms should **not** be copied literally into Depengine:

- a Depengine `requires` edge is not compiler data dependence;
- `requires_when` is not formal control dependence;
- method candidate selection is not a control-flow graph.

Compiler terminology is useful here as a reminder that one graph may contain
multiple typed relations, not as a domain model.

Depengine should keep domain-specific names such as `ToolRequire`,
`MethodRequire`, guards, method selectors, and scheduling roles.

## Proposed graph IR

The core proposal is a graph IR built once from the merged schema and consumed
by sorting, analysis, and renderers.

```text
STRUCT Graph:
    nodes: Map<ToolID, Node>
    edges: List<Edge>

STRUCT Node:
    id: ToolID
    tags: List<String>
    dependencyOnly: Bool

STRUCT Edge:
    from: ToolID
    to: ToolID
    kind: EdgeKind
    guard: Optional<Condition>
    method: Optional<MethodRef>
    role: EdgeRole

ENUM EdgeKind:
    ToolRequire
    MethodRequire

ENUM EdgeRole:
    Scheduling
    Activation
```

Direction remains:

```text
dependency -> dependent
```

This matches the existing DOT/Mermaid direction and naturally communicates
installation precedence.

### Why separate `kind` from `guard`

A guarded tool dependency is still a tool dependency. Encoding every combination
as a separate enum would scale poorly:

```text
ToolRequire
ConditionalToolRequire
MethodRequire
ConditionalMethodRequire
...
```

Instead:

```text
kind = ToolRequire
guard = target_family == unix
```

preserves two orthogonal facts.

### Why separate `role`

Not every visible edge must constrain scheduling.

A method prerequisite can be worth rendering even if it is not part of the
global topological ordering. The model should say that explicitly:

```text
ToolRequire:
    role = Scheduling

MethodRequire:
    role = Activation
```

This mirrors a useful Graphviz concept: an edge may be rendered while
`constraint=false` keeps it out of rank assignment.

## Building the declared graph

Pseudocode:

```text
FUNCTION BuildDeclaredGraph(tools):

    graph = Graph()

    FOR toolName IN sorted(tool names):
        tool = tools[toolName]

        graph.AddNode(
            id             = toolName,
            tags           = tool.Tags,
            dependencyOnly = tool.DependencyOnly
        )

    FOR toolName IN sorted(tool names):
        tool = tools[toolName]

        FOR dependency IN tool.Requires:
            graph.AddEdge(
                from   = dependency,
                to     = toolName,
                kind   = ToolRequire,
                guard  = tool.RequiresWhen[dependency] OR nil,
                method = nil,
                role   = Scheduling
            )

        FOR method IN tool.Methods:
            selector =
                method.Label IF method.Label != ""
                ELSE method.Kind

            FOR dependency IN method.Requires:
                graph.AddEdge(
                    from   = dependency,
                    to     = toolName,
                    kind   = MethodRequire,
                    guard  = method.When,
                    method = selector,
                    role   = Activation
                )

    RETURN Canonicalize(graph)
```

The IR should allow more than one semantic edge between the same pair of tools.
For example, a pair may have a general tool prerequisite and a distinct
candidate-specific prerequisite. Renderers may choose to merge or bundle those
edges visually, but the model should not discard them.

## Declared, effective, and resolved projections

There are three different questions a graph command may eventually answer:

1. What dependencies are declared by the merged schema?
2. Which dependencies apply on this host?
3. Which dependencies participate in the selected installation plan?

These should be modeled as projections rather than separate graph types.

```text
DeclaredGraph  = all declared semantic edges
EffectiveGraph = guards evaluated against host facts
ResolvedGraph  = guards plus selected candidate/method resolution
```

Pseudocode:

```text
FUNCTION Project(graph, view, context):

    result = graph with all nodes

    FOR edge IN graph.edges:

        IF view == DECLARED:
            result.Add(edge WITH status = Declared)
            CONTINUE

        IF edge.guard exists AND edge.guard does not match context.facts:
            IF view wants inactive edges:
                result.Add(edge WITH status = Inactive)
            CONTINUE

        IF view == EFFECTIVE:
            result.Add(edge WITH status = ActiveOrUnresolved)
            CONTINUE

        IF view == RESOLVED:
            IF edge.method exists
               AND edge.method != context.selectedMethod(edge.to):
                CONTINUE

            result.Add(edge WITH status = Active)

    RETURN result
```

The first implementation does not need to expose all three CLI views. The
important design constraint is that the IR must not make those future views
impossible.

## Scheduling projection

Topological ordering should operate only on edges that constrain installation
order.

```text
FUNCTION SchedulingProjection(graph):
    RETURN graph containing:
        all nodes
        edges WHERE edge.role == Scheduling
        and edge.status != Inactive
```

The existing Kahn implementation can then remain the basis of level computation:

```text
FUNCTION ComputeLevels(graph):

    scheduling = SchedulingProjection(graph)

    indegree = zero for every node
    children = adjacency map

    FOR edge IN scheduling.edges:
        indegree[edge.to] += 1
        children[edge.from].append(edge.to)

    remaining = all nodes
    levels = []

    WHILE remaining is not empty:

        level =
            sorted nodes in remaining
            WHERE indegree[node] == 0

        IF level is empty:
            RAISE CycleError(ExtractCycle(...))

        levels.append(level)

        FOR node IN level:
            remove node from remaining

            FOR child IN sorted(children[node]):
                indegree[child] -= 1

    RETURN levels
```

This reframes the existing `level 0 / level 1 / ...` output as one analysis of
the graph rather than as the graph itself.

## Terminal renderer: layered layout

For directed dependency graphs, a layered / hierarchical layout is a natural
fit. Graphviz `dot` uses ranked directed layouts and explicitly separates rank
assignment, crossing reduction, positioning, and edge routing.

The Depengine terminal renderer does not need a complete Sugiyama
implementation. A deterministic "Sugiyama-lite" pipeline should be sufficient:

```text
DPG
 |
 v
project visible edges
 |
 v
weakly connected components
 |
 v
assign ranks
 |
 v
order nodes within ranks
 |
 v
assign terminal coordinates
 |
 v
route edges
 |
 v
paint Unicode grid
```

### Connected components and isolated nodes

Large schemas may contain many independent tools. Drawing every isolated tool
as a full node would produce a large but uninformative diagram.

The renderer should identify weakly connected components by temporarily ignoring
edge direction.

```text
FUNCTION WeakComponents(graph):

    undirected = empty adjacency map

    FOR edge IN graph.edges:
        undirected[edge.from].add(edge.to)
        undirected[edge.to].add(edge.from)

    visited = Set()
    components = []

    FOR node IN sorted(graph.nodes):

        IF node IN visited:
            CONTINUE

        component = BFS(node, undirected)

        components.append(component)
        visited.add_all(component)

    RETURN components
```

Rendering policy:

- components with at least one edge are drawn;
- isolated nodes are condensed into a sorted textual list;
- each connected component is laid out independently.

For the example schema, the intended human-facing shape is closer to:

```text
fontconfig ───────┐
                  ├──▶ DepartureMono
unzip ────────────┘

docker-compose ───┐
golang ───────────┼──▶ marina
podman ───────────┘

zathura ─────────────▶ zathura-pdf-mupdf

isolated:
aichat, bat, bspwm, ctpv, ...
```

rather than a wall of dozens of boxes.

## Rank assignment

For scheduling edges, existing topological levels can provide initial ranks:

```text
rank[node] = topological level
```

Activation-only edges remain visible but do not alter rank.

This makes the scheduling/view distinction structural rather than a rendering
special case.

## Crossing reduction

Do not attempt globally optimal crossing minimization.

A deterministic barycenter heuristic is sufficient for an initial terminal
renderer:

```text
FUNCTION OrderRanks(ranks, edges):

    sort rank[0] alphabetically

    REPEAT a small fixed number of sweeps:

        FOR rank FROM left TO right:
            FOR node IN rank:
                parents = visible predecessors
                barycenter[node] = average(position(parents))
            stable-sort rank by barycenter, then node id

        FOR rank FROM right TO left:
            FOR node IN rank:
                children = visible successors
                barycenter[node] = average(position(children))
            stable-sort rank by barycenter, then node id
```

A fixed sweep count keeps output deterministic and runtime bounded.

## Edge routing

The first renderer should prefer orthogonal box-drawing routes rather than
diagonals:

```text
A ─────┐
       ├──▶ X
B ─────┘
```

Pseudocode:

```text
FUNCTION RouteEdges(layout):

    FOR each adjacent rank boundary:

        edges = visible edges crossing boundary

        assign deterministic routing lanes

        FOR edge IN edges:

            start = output port of edge.from
            end   = input port of edge.to

            route =
                horizontal(start -> lane)
                vertical(lane)
                horizontal(lane -> end)

            layout.Store(route)
```

For edges spanning multiple ranks, the layout layer may introduce virtual
routing points. These are layout-only objects, never semantic graph nodes.

## Visual edge semantics

The renderer should expose only a small number of stable visual distinctions:

```text
tool require:
────────────▶

guarded tool require:
- - [unix] -▶

method require:
┄┄ [http] ┄▶
```

Exact glyphs may degrade based on terminal capability, but semantic style should
be represented independently:

```text
STRUCT EdgeStyle:
    line: Solid | Dashed | Dotted
    label: Optional<String>
    state: Declared | Active | Inactive | Unresolved
```

Do not make ANSI color the sole carrier of meaning.

## CLI evolution

The current command supports:

```text
--format text
--format dot
--format mermaid
```

A compatibility-preserving direction is:

```text
--format text      # existing topological-level output
--format graph     # new terminal dependency diagram
--format dot
--format mermaid
```

A later cleanup may introduce `levels` as a clearer name while keeping
`text` as an alias:

```text
levels   -> scheduling/topological view
graph    -> terminal dependency view
dot      -> Graphviz interchange/rendering
mermaid  -> documentation/interchange
```

No default-output change is required for the first implementation.

## `--only` and future graph slicing

A typed IR makes `--only` useful as graph traversal rather than only input
filtering.

Possible later model:

```text
FUNCTION SubgraphAround(graph, target, direction, depth):

    IF direction == dependencies:
        traverse predecessors

    IF direction == dependents:
        traverse successors

    IF direction == both:
        union both traversals

    stop at requested depth if bounded
```

Potential future flags:

```text
--direction deps
--direction dependents
--direction both
--depth N
```

These are deliberately outside the first implementation scope.

## Evaluation of `hmdsefi/gograph`

`gograph` is a useful reference implementation and API benchmark. It provides
directed/acyclic graph types, BFS/DFS/topological traversal, connectivity, and
path algorithms.

It is **not recommended as a production dependency for this feature initially**.

Reasons:

1. Depengine already has the graph operation most critical to its execution
   semantics: deterministic Kahn topological sorting with cycle reporting.
2. Weak components and predecessor/successor traversal are small `O(V + E)`
   algorithms that can be implemented against the domain IR directly.
3. The current `gograph.Graph.AddEdge` contract rejects an edge that already
   exists between the same endpoints, and its directed `GetAllEdges` contract
   returns a single edge. The proposed Depengine IR should be able to retain
   multiple semantic relations between the same tool pair.
4. Depengine edges require domain metadata such as dependency kind, guard,
   method selector, scheduling role, and projection state. A specialized IR
   keeps those invariants explicit.
5. Adding a graph library solely to replace the existing sorter would increase
   dependency surface without materially reducing domain code.

Re-evaluate a library if future requirements expand to substantial general
graph analysis such as SCC algorithms, several shortest-path variants,
centrality, partitioning, or other algorithms where a maintained implementation
would meaningfully reduce complexity.

## Suggested internal structure

Initial structure:

```text
internal/graph/
    model.go
    build.go
    sort.go
    render.go
    render_terminal.go
```

If the terminal layout grows enough to justify separation:

```text
internal/graph/
    model.go
    build.go
    project.go
    analyze.go
    sort.go

    render_text.go
    render_terminal.go
    render_dot.go
    render_mermaid.go

    layout/
        rank.go
        order.go
        route.go
        grid.go
```

Avoid creating the larger package structure until responsibilities actually
separate.

## End-to-end pseudocode

```text
FUNCTION GraphCommand(schema, manifest, filters, format, view):

    schema = ParseSchema(schema)
    schema = MergeManifest(schema, manifest)
    schema = ApplyToolFilters(schema, filters)

    graph = BuildDeclaredGraph(schema.Tools)

    context = GatherContextIfNeeded(view)

    visibleGraph = Project(graph, view, context)

    ValidateGraph(visibleGraph)

    SWITCH format:

        CASE "text":
            levels = ComputeLevels(visibleGraph)
            RETURN RenderLevels(levels, visibleGraph)

        CASE "graph":
            analysis = AnalyzeForLayout(visibleGraph)
            layout = Layout(analysis)
            RETURN RenderTerminal(layout)

        CASE "dot":
            RETURN RenderDOT(visibleGraph)

        CASE "mermaid":
            RETURN RenderMermaid(visibleGraph)
```

```text
FUNCTION AnalyzeForLayout(graph):

    scheduling = SchedulingProjection(graph)

    levels = ComputeLevels(scheduling)
    components = WeakComponents(graph)

    isolated = components WHERE edge_count == 0
    connected = components WHERE edge_count > 0

    RETURN Analysis {
        levels,
        connected,
        isolated
    }
```

## Implementation sequence

The implementation should be incremental and preserve existing behavior before
adding a new renderer.

1. Introduce `Graph`, `Node`, and typed `Edge` without changing CLI output.
2. Build the IR once from merged `config.Tool` values, preserving
   `requires_when` and method-scoped metadata.
3. Migrate DOT and Mermaid to consume the IR; keep output compatibility where
   the current output is semantically complete.
4. Make topological sorting operate on the scheduling projection.
5. Migrate the existing text/level renderer to the same IR.
6. Add `--format graph` with weak-component grouping and isolated-node
   compaction.
7. Add simple ranked layout and orthogonal routing.
8. Improve crossing reduction and edge bundling only after testing real schemas.

The first several steps should be refactoring/semantic preservation rather than
a visible feature change.

## Non-goals for the first implementation

- interactive TUI graph browser;
- full Sugiyama or Graphviz-compatible layout engine;
- changing installation semantics;
- replacing Graphviz or Mermaid output;
- introducing compiler concepts such as CFG nodes, data dependence, or control
  dependence into the public schema vocabulary;
- adding a general-purpose graph dependency without demonstrated need;
- changing the default `depengine graph` output immediately.

## Open design questions

Before implementation, resolve these points:

1. Should the initial `graph` renderer show inactive guarded edges, or only
   declared/active edges?
2. Should `depengine graph` remain a declared-schema view by default, with a
   later explicit effective/resolved flag?
3. How should multiple semantic edges between the same pair be bundled in DOT,
   Mermaid, and terminal output?
4. Should method guards and method dependency labels be rendered separately or
   combined into one edge label?
5. Should `--only` preserve the current filtering behavior initially and gain
   traversal semantics only under a new flag?
6. What terminal width threshold should switch a component from diagram form to
   a compact textual fallback?

## References

Project implementation:

- [`internal/app/graph_why.go`](../../internal/app/graph_why.go)
- [`internal/graph/render.go`](../../internal/graph/render.go)
- [`internal/graph/sort.go`](../../internal/graph/sort.go)
- [`internal/config/model.go`](../../internal/config/model.go)

Dependence-graph and compiler background:

- Ferrante, Ottenstein, Warren, *The Program Dependence Graph and Its Use in
  Optimization*:
  https://research.ibm.com/publications/the-program-dependence-graph-and-its-use-in-optimization
- Program dependence graph:
  https://en.wikipedia.org/wiki/Program_dependence_graph
- Data dependency:
  https://en.wikipedia.org/wiki/Data_dependency
- Control dependency:
  https://en.wikipedia.org/wiki/Control_dependency
- Loop dependence analysis / control dependence:
  https://en.wikipedia.org/wiki/Loop_dependence_analysis#Control_dependence
- Control-flow graph:
  https://en.wikipedia.org/wiki/Control-flow_graph
- Directed graph:
  https://en.wikipedia.org/wiki/Directed_graph

Layout references:

- Graphviz `dot`:
  https://graphviz.org/docs/layouts/dot/
- Graphviz `constraint` edge attribute:
  https://graphviz.org/docs/attrs/constraint/
- Graphviz library guide (`dot` ranking, crossing minimization, positioning,
  and routing):
  https://graphviz.org/pdf/libguide.pdf
- Layered / Sugiyama-style graph drawing:
  https://en.wikipedia.org/wiki/Layered_graph_drawing

Go graph-library reference:

- `hmdsefi/gograph`:
  https://github.com/hmdsefi/gograph
- Graph interface / edge behavior:
  https://github.com/hmdsefi/gograph/blob/master/graph.go

## Provisional conclusion

Proceed with a **typed Depengine graph IR first**, then add a terminal renderer
as another consumer of that IR.

The design should preserve three separations:

1. graph semantics vs rendering;
2. declared/effective/resolved projection;
3. visible edges vs scheduling constraints.

Those boundaries provide most of the architectural value. The literal terminal
graph then becomes a contained presentation feature rather than another source
of dependency semantics.

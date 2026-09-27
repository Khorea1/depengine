# Plan: `depengine graph --format=graph` (terminal renderer)

Source of truth: `docs/research/typed-dependency-graph.md` +
`docs/research/typed-dependency-graph-implementation-status.md`
("Next implementation slice", items 1-4).

## Scope

1. `internal/graph/analyze.go` — layout-facing analysis:
   - `Component` (weakly connected nodes + their semantic edges);
   - `Analysis{Levels, Ranks, Connected, Isolated}`;
   - `Analyze(Graph)` using scheduling levels for ranks and *all* visible
     edges for weak connectivity.
2. `internal/graph/render_terminal.go` — `RenderTerminalGraph(g, width)`:
   - per-component decision: `layout.width <= width` -> diagram, else compact
     dependency-edge list (`from -> to [annotation]`);
   - rank columns left-to-right, rows = index within rank (centered), one
     routing corridor row per non-adjacent route (shared when spans do not
     overlap);
   - lanes per gap: exit region (source side) and entry region (target side),
     shared by row so routes to the same node merge into one entry bar;
   - line style carries semantics: solid = tool require, dashed = guarded,
     dotted = method require; junctions use shared box-drawing glyphs;
   - `annotations:` block for diagram-rendered components (line style alone
     cannot say *which* guard/method an edge carries);
   - `isolated:` block, wrapped to the available width.
3. `internal/term` — terminal width discovery (`Width(*os.File)`,
   `OutputWidth()` with 80-column fallback); `internal/exec` delegates to it
   instead of keeping its own ioctl copy.
4. CLI: `--format graph` plus `--width` (0 = detect). Renderer stays
   environment-free; the app layer supplies the width.
5. Docs: regenerate `docs/cli-reference.md` + `docs/depengine.1`, update the
   research status/sequence, roadmap, architecture package table.

## Non-goals (still deferred)

Crossing reduction, layout-only edge bundling, interactive TUI, ANSI color as
the only meaning carrier, changing the default `text` output.

## Checks

`go build`, `go test -race ./...`, `go vet ./...`, `golangci-lint run`,
`go generate ./internal/app` for the docs golden.

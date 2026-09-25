package graph

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// referenceGraph is the mixed sample used to exercise merges, backward
// activation edges, guarded annotations, isolated nodes, and per-component
// width fallback. "DepartureMono" sorts byte-wise before every lowercase ID,
// which also documents component ordering by lowest node ID.
func referenceGraph() Graph {
	g := NewGraph()
	for _, id := range []string{"aichat", "bat", "bspwm", "ctpv", "docker-compose", "fontconfig", "golang", "marina", "podman", "unzip", "zathura", "zathura-pdf-mupdf", "DepartureMono"} {
		g.AddNode(Node{ID: id})
	}
	add := func(from, to string, kind EdgeKind, role EdgeRole, guard Guard, method string) {
		g.AddEdge(Edge{From: from, To: to, Kind: kind, Role: role, Guard: guard, Method: method})
	}
	add("fontconfig", "DepartureMono", ToolRequire, Scheduling, nil, "")
	add("unzip", "DepartureMono", ToolRequire, Scheduling, testGuard(`target_family in ["unix"]`), "")
	add("docker-compose", "marina", ToolRequire, Scheduling, nil, "")
	add("golang", "marina", ToolRequire, Scheduling, nil, "")
	add("podman", "marina", ToolRequire, Scheduling, nil, "")
	add("zathura", "zathura-pdf-mupdf", ToolRequire, Scheduling, nil, "")
	add("golang", "marina", MethodRequire, Activation, nil, "git")
	add("bat", "ctpv", ToolRequire, Scheduling, nil, "")
	add("ctpv", "bat", MethodRequire, Activation, nil, "http")
	return g
}

func TestRenderTerminalGraphRejectsNonPositiveWidth(t *testing.T) {
	for _, width := range []int{0, -1} {
		out, err := RenderTerminalGraph(referenceGraph(), width)
		if err == nil {
			t.Fatalf("width %d: expected an error", width)
		}
		if !strings.Contains(err.Error(), "must be positive") {
			t.Errorf("width %d: unexpected error: %v", width, err)
		}
		if out != "" {
			t.Errorf("width %d: expected empty output, got %q", width, out)
		}
	}
}

func TestRenderTerminalGraphEmpty(t *testing.T) {
	out, err := RenderTerminalGraph(NewGraph(), 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for an empty graph, got %q", out)
	}
}

func TestRenderTerminalGraphIsolatesNodesWithoutEdges(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "beta"})
	g.AddNode(Node{ID: "alpha"})

	out, err := RenderTerminalGraph(g, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	if want := "isolated:\nalpha, beta\n"; out != want {
		t.Errorf("got %q, want %q", out, want)
	}
}

func TestRenderTerminalGraphDeterministic(t *testing.T) {
	first := referenceGraph()

	reversed := NewGraph()
	for id := range first.Nodes {
		reversed.AddNode(Node{ID: id})
	}
	for i := len(first.Edges) - 1; i >= 0; i-- {
		reversed.AddEdge(first.Edges[i])
	}

	fromFirst, err := RenderTerminalGraph(first, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	fromReversed, err := RenderTerminalGraph(reversed, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph(reversed): %v", err)
	}
	if fromReversed != fromFirst {
		t.Errorf("insertion order changed the rendering:\nfirst:\n%s\nreversed:\n%s", fromFirst, fromReversed)
	}

	again, err := RenderTerminalGraph(first, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph(repeat): %v", err)
	}
	if again != fromFirst {
		t.Errorf("repeated rendering differs:\nfirst:\n%s\nagain:\n%s", fromFirst, again)
	}
}

func TestLayoutTerminalComponentReducesSimpleCrossing(t *testing.T) {
	component := Component{
		Nodes: []string{"a", "b", "c", "d"},
		Edges: []Edge{
			{From: "a", To: "d", Kind: ToolRequire, Role: Scheduling},
			{From: "b", To: "c", Kind: ToolRequire, Role: Scheduling},
		},
	}
	ranks := map[string]int{"a": 0, "b": 0, "c": 1, "d": 1}

	layout, err := layoutTerminalComponent(component, ranks)
	if err != nil {
		t.Fatalf("layoutTerminalComponent: %v", err)
	}

	if got := layout.columns[1].nodes[0].id; got != "d" {
		t.Errorf("first node in target rank = %q, want d", got)
	}
	if got := layout.columns[1].nodes[1].id; got != "c" {
		t.Errorf("second node in target rank = %q, want c", got)
	}
	if out := renderTerminalDiagram(layout); strings.Contains(out, "┼") {
		t.Errorf("simple crossing was not reduced:\n%s", out)
	}
}

func TestLayoutTerminalComponentBundlesSemanticMultiedges(t *testing.T) {
	t.Run("long route shares one corridor", func(t *testing.T) {
		g := NewGraph()
		for _, id := range []string{"a", "middle", "z"} {
			g.AddNode(Node{ID: id})
		}
		g.AddEdge(Edge{From: "a", To: "middle", Kind: ToolRequire, Role: Scheduling})
		g.AddEdge(Edge{From: "middle", To: "z", Kind: ToolRequire, Role: Scheduling})
		g.AddEdge(Edge{From: "a", To: "z", Kind: MethodRequire, Role: Activation, Method: "git"})
		g.AddEdge(Edge{From: "a", To: "z", Kind: MethodRequire, Role: Activation, Method: "http"})

		analysis, err := Analyze(g)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		layout, err := layoutTerminalComponent(analysis.Connected[0], analysis.Ranks)
		if err != nil {
			t.Fatalf("layoutTerminalComponent: %v", err)
		}

		if got := len(layout.routes); got != 3 {
			t.Errorf("physical routes = %d, want 3 for 4 semantic edges", got)
		}
		if got := len(layout.corridors); got != 1 {
			t.Errorf("corridors = %d, want 1 shared long-edge corridor", got)
		}

		annotations := strings.Join(terminalAnnotations(layout), "\n")
		for _, want := range []string{"a -> z [git]", "a -> z [http]"} {
			if !strings.Contains(annotations, want) {
				t.Errorf("semantic annotation %q missing:\n%s", want, annotations)
			}
		}
	})

	t.Run("strongest relation determines bundle style", func(t *testing.T) {
		g := NewGraph()
		for _, id := range []string{"a", "b"} {
			g.AddNode(Node{ID: id})
		}
		g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
		g.AddEdge(Edge{From: "a", To: "b", Kind: MethodRequire, Role: Activation, Method: "git"})

		analysis, err := Analyze(g)
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		layout, err := layoutTerminalComponent(analysis.Connected[0], analysis.Ranks)
		if err != nil {
			t.Fatalf("layoutTerminalComponent: %v", err)
		}
		if got := len(layout.routes); got != 1 {
			t.Fatalf("physical routes = %d, want 1", got)
		}
		if got := layout.routes[0].style; got != terminalStyleSolid {
			t.Errorf("bundle style = %v, want solid", got)
		}
	})
}

func TestRenderTerminalGraphSnapshotWidth80(t *testing.T) {
	const want = `fontconfig ┬─▶ DepartureMono
unzip      ┘

 ┌┄▶ bat ──▶ ctpv ┐
 └┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┘

docker-compose ┐
golang         ┼─▶ marina
podman         ┘

zathura ──▶ zathura-pdf-mupdf

annotations:
  unzip -> DepartureMono [target_family in ["unix"]]
  ctpv -> bat [http]
  golang -> marina [git]

isolated:
aichat, bspwm
`

	out, err := RenderTerminalGraph(referenceGraph(), 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	if out != want {
		t.Errorf("rendering mismatch\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// TestRenderTerminalGraphFitsWidth checks the wrapped contract: diagram lines
// and the isolated list never exceed the requested width. Compact headers and
// the two-space-indented compact/annotation entries are documented as
// unwrapped, so they are exempt.
func TestRenderTerminalGraphFitsWidth(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		out, err := RenderTerminalGraph(referenceGraph(), width)
		if err != nil {
			t.Fatalf("width %d: %v", width, err)
		}
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if line == "" || strings.HasPrefix(line, "component needs ") || strings.HasPrefix(line, "  ") {
				continue
			}
			if n := len([]rune(line)); n > width {
				t.Errorf("width %d: line spans %d columns: %q", width, n, line)
			}
		}
	}
}

// TestRenderTerminalGraphWidthOnlyAffectsFallback pins that widths above the
// widest component produce byte-identical output: width changes fallback
// decisions, not geometry.
func TestRenderTerminalGraphWidthOnlyAffectsFallback(t *testing.T) {
	base, err := RenderTerminalGraph(referenceGraph(), 40)
	if err != nil {
		t.Fatalf("width 40: %v", err)
	}
	for _, width := range []int{80, 120} {
		out, err := RenderTerminalGraph(referenceGraph(), width)
		if err != nil {
			t.Fatalf("width %d: %v", width, err)
		}
		if out != base {
			t.Errorf("width %d differs from width 40:\n%d:\n%s\n40:\n%s", width, width, out, base)
		}
	}
}

func TestRenderTerminalGraphFallsBackPerComponent(t *testing.T) {
	// At 24 columns the fontconfig, docker-compose, and zathura components are
	// too wide for their diagrams, while the 19-column bat component still is.
	out, err := RenderTerminalGraph(referenceGraph(), 24)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}

	if !strings.Contains(out, "component needs 28 columns (24 available):") {
		t.Errorf("expected the fontconfig component to fall back:\n%s", out)
	}
	if !strings.Contains(out, " ┌┄▶ bat ──▶ ctpv ┐") {
		t.Errorf("expected the bat component to stay a diagram:\n%s", out)
	}

	// Compact components carry their annotations inline instead of listing
	// them again in the annotations section.
	if got := strings.Count(out, "unzip -> DepartureMono"); got != 1 {
		t.Errorf("expected unzip's annotation exactly once (inline), got %d:\n%s", got, out)
	}
	idx := strings.Index(out, "\nannotations:\n")
	if idx < 0 {
		t.Fatalf("expected an annotations section:\n%s", out)
	}
	if tail := out[idx:]; !strings.Contains(tail, "ctpv -> bat [http]") {
		t.Errorf("expected the diagram-rendered annotation in the section:\n%s", tail)
	}
}

func TestRenderTerminalGraphEdgeLineStyles(t *testing.T) {
	// Solid runs carry plain scheduling edges, dashed runs guarded ones.
	long := NewGraph()
	for _, id := range []string{"a", "b", "c"} {
		long.AddNode(Node{ID: id})
	}
	long.AddEdge(Edge{From: "c", To: "b", Kind: ToolRequire, Role: Scheduling})
	long.AddEdge(Edge{From: "b", To: "a", Kind: ToolRequire, Role: Scheduling})
	long.AddEdge(Edge{From: "c", To: "a", Kind: ToolRequire, Role: Scheduling, Guard: testGuard("os = linux")})

	out, err := RenderTerminalGraph(long, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	if !strings.Contains(out, "└╌") {
		t.Errorf("expected a dashed run for the guarded edge:\n%s", out)
	}
	if !strings.Contains(out, "──▶") {
		t.Errorf("expected a solid run for the plain scheduling edge:\n%s", out)
	}
	if !strings.Contains(out, "  c -> a [os = linux]") {
		t.Errorf("expected the guard annotation:\n%s", out)
	}

	// Dotted runs carry method requirements.
	out, err = RenderTerminalGraph(referenceGraph(), 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	if !strings.Contains(out, "┌┄▶") {
		t.Errorf("expected a dotted run for the method edge:\n%s", out)
	}
}

func TestRenderTerminalGraphWrapsIsolatedList(t *testing.T) {
	g := NewGraph()
	ids := []string{"consul", "helm", "kubectl", "nomad", "packer", "terraform", "vault", "vagrant"}
	for _, id := range ids {
		g.AddNode(Node{ID: id})
	}

	const width = 20
	out, err := RenderTerminalGraph(g, width)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "isolated:" {
		t.Fatalf("expected an isolated section, got %q", lines[0])
	}
	for _, line := range lines[1:] {
		if n := len([]rune(line)); n > width {
			t.Errorf("wrapped line spans %d columns: %q", n, line)
		}
	}
	for _, id := range ids {
		if !strings.Contains(out, id) {
			t.Errorf("isolated node %q missing from output:\n%s", id, out)
		}
	}
}

func TestRenderTerminalGraphEscapesVariableWidthNodeIDs(t *testing.T) {
	g := NewGraph()
	for _, id := range []string{"工具", "e\u0301"} {
		g.AddNode(Node{ID: id})
	}

	out, err := RenderTerminalGraph(g, 80)
	if err != nil {
		t.Fatalf("RenderTerminalGraph: %v", err)
	}
	for _, id := range []string{"工具", "e\u0301"} {
		label := strconv.QuoteToASCII(id)
		if !strings.Contains(out, label) {
			t.Fatalf("rendering missing escaped label %q:\n%s", label, out)
		}
		if strings.Contains(out, id) {
			t.Fatalf("rendering leaked variable-width label %q instead of ASCII escape:\n%s", id, out)
		}
	}
}

func TestRenderTerminalGraphRejectsCycle(t *testing.T) {
	g := NewGraph()
	g.AddNode(Node{ID: "a"})
	g.AddNode(Node{ID: "b"})
	g.AddEdge(Edge{From: "a", To: "b", Kind: ToolRequire, Role: Scheduling})
	g.AddEdge(Edge{From: "b", To: "a", Kind: ToolRequire, Role: Scheduling})

	_, err := RenderTerminalGraph(g, 80)
	if err == nil {
		t.Fatal("expected a cycle error")
	}
	var cycle *CycleError
	if !errors.As(err, &cycle) {
		t.Errorf("expected a CycleError, got %v", err)
	}
}

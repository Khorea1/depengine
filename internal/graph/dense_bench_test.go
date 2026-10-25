package graph

// Dense-graph benchmarks for dep-bench-round2-update-why-graph-h8c.
//
// They build a 5,000-node acyclic graph where every node depends on the next
// fanIn nodes, then measure the typed-IR build, scheduling sort, analysis, and
// renderers. The app-level BenchmarkGraphCommand measures the same graph
// through the CLI, including schema parsing and projection.

import (
	"fmt"
	"testing"
)

type denseTool struct {
	deps []string
}

func (t denseTool) GraphDependencies() []string { return t.deps }
func (t denseTool) GraphTags() []string         { return nil }
func (t denseTool) GraphConditionalDependencies() map[string][]string {
	return nil
}

func denseTools(n, fanIn int) map[string]denseTool {
	tools := make(map[string]denseTool, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		deps := make([]string, 0, fanIn)
		for j := 1; j <= fanIn && i+j < n; j++ {
			deps = append(deps, fmt.Sprintf("tool%05d", i+j))
		}
		tools[name] = denseTool{deps: deps}
	}
	return tools
}

func BenchmarkDenseGraphBuild(b *testing.B) {
	for _, fanIn := range []int{5, 20} {
		b.Run(fmt.Sprintf("nodes=5000/fanin=%d", fanIn), func(b *testing.B) {
			tools := denseTools(5000, fanIn)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				g := BuildDeclaredGraph(tools)
				if len(g.Nodes) != 5000 {
					b.Fatalf("nodes = %d, want 5000", len(g.Nodes))
				}
			}
		})
	}
}

func BenchmarkDenseGraphSort(b *testing.B) {
	for _, fanIn := range []int{5, 20} {
		b.Run(fmt.Sprintf("nodes=5000/fanin=%d", fanIn), func(b *testing.B) {
			g := BuildDeclaredGraph(denseTools(5000, fanIn))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				levels, err := SortGraph(g)
				if err != nil {
					b.Fatal(err)
				}
				if len(levels) == 0 {
					b.Fatal("empty levels")
				}
			}
		})
	}
}

func BenchmarkDenseGraphAnalyze(b *testing.B) {
	for _, fanIn := range []int{5, 20} {
		b.Run(fmt.Sprintf("nodes=5000/fanin=%d", fanIn), func(b *testing.B) {
			g := BuildDeclaredGraph(denseTools(5000, fanIn))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := Analyze(g); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDenseGraphRenderDOT(b *testing.B) {
	for _, fanIn := range []int{5, 20} {
		b.Run(fmt.Sprintf("nodes=5000/fanin=%d", fanIn), func(b *testing.B) {
			g := BuildDeclaredGraph(denseTools(5000, fanIn))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if out := RenderDOTGraph(g); out == "" {
					b.Fatal("empty DOT output")
				}
			}
		})
	}
}

func BenchmarkDenseGraphRenderText(b *testing.B) {
	for _, fanIn := range []int{5, 20} {
		b.Run(fmt.Sprintf("nodes=5000/fanin=%d", fanIn), func(b *testing.B) {
			g := BuildDeclaredGraph(denseTools(5000, fanIn))
			levels, err := SortGraph(g)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if out := RenderTextGraph(levels, g); out == "" {
					b.Fatal("empty text output")
				}
			}
		})
	}
}

package exec

// Explain/why core-cost benchmark for dep-bench-round2-update-why-graph-h8c.
//
// It measures Executor.ExplainTool across every tool in a synthetic schema with
// two candidates per tool. The mock adapter and fake runner keep the cost in
// engine code, so the result is comparable with the planning benchmarks in
// planning_bench_test.go.

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func BenchmarkExplainAllTools(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			ex := newBenchExecutor(b, &run.FakeRunner{}, mockHTTPAdapter())
			s := candidateSchema(n, 2)
			names := make([]string, 0, len(s.Tools))
			for name := range s.Tools {
				names = append(names, name)
			}
			sort.Strings(names)
			tools := make([]*config.Tool, 0, len(names))
			for _, name := range names {
				tools = append(tools, s.Tools[name])
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, tool := range tools {
					if attempts := ex.ExplainTool(context.Background(), tool, "debian"); len(attempts) == 0 {
						b.Fatalf("ExplainTool(%q) returned no attempts", tool.Name)
					}
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

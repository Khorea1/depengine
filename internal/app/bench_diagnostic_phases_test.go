package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/validate"
)

func diagnosticPhaseInputs(tb testing.TB, n int, withDependencies bool) (string, map[string]string, *config.Schema, *platform.Facts) {
	tb.Helper()
	_, path := writeBenchSchema(tb, n, withDependencies)
	facts := &platform.Facts{
		TargetArch: "x86_64", DistroID: "debian", DistroName: "Debian",
		DistroVersion: "12", DistroIDLike: "debian", TargetFamily: "unix",
		DetectionMethod: "os-release", Confidence: "high",
	}
	clan := platform.ResolveFamily(facts)
	factMap := config.BuildMap(facts, clan)
	schema, err := config.ParseProjectSchema(path, factMap)
	if err != nil {
		tb.Fatal(err)
	}
	return path, factMap, schema, facts
}

func resolveDiagnosticLayers(tb testing.TB, schema *config.Schema, facts map[string]string) *config.Schema {
	tb.Helper()
	layers, err := config.ResolveLayers(schema, nil, nil, facts, false)
	if err != nil {
		tb.Fatal(err)
	}
	return layers.Schema
}

func validateDiagnosticSchema(tb testing.TB, schema *config.Schema) {
	tb.Helper()
	if err := config.ValidateEffectiveIntegrity(schema); err != nil {
		tb.Fatal(err)
	}
	result := validate.ValidateSchema(schema, exec.RegisteredKinds())
	if result.HasErrors() {
		tb.Fatalf("generated schema has validation errors: %v", result.Errors)
	}
}

func BenchmarkDiagnosticPhases(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("why/tools=%d", n), func(b *testing.B) {
			path, factsMap, parsed, facts := diagnosticPhaseInputs(b, n, false)
			merged := resolveDiagnosticLayers(b, parsed, factsMap)
			validateDiagnosticSchema(b, merged)
			clan := platform.ResolveFamily(facts)
			executor := newProjectExecutor(merged, clan, facts, run.OSExecRunner{})
			tool := merged.Tools["tool00000"]
			attempts := executor.ExplainTool(context.Background(), tool, clan)
			report := buildWhyReport("tool00000", tool, merged.Provenance["tool00000"], attempts)

			b.Run("parse", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := config.ParseProjectSchema(path, factsMap); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("layers", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					resolveDiagnosticLayers(b, parsed, factsMap)
				}
			})
			b.Run("validate", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					validateDiagnosticSchema(b, merged)
				}
			})
			b.Run("select-one-tool", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					attempts = executor.ExplainTool(context.Background(), tool, clan)
				}
			})
			b.Run("project-why-report", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					report = buildWhyReport("tool00000", tool, merged.Provenance["tool00000"], attempts)
				}
			})
			b.Run("output-why-json", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var out bytes.Buffer
					enc := json.NewEncoder(&out)
					enc.SetIndent("", "  ")
					if err := enc.Encode(report); err != nil {
						b.Fatal(err)
					}
				}
			})
		})

		b.Run(fmt.Sprintf("graph/tools=%d", n), func(b *testing.B) {
			path, factsMap, parsed, facts := diagnosticPhaseInputs(b, n, true)
			merged := resolveDiagnosticLayers(b, parsed, factsMap)
			validateDiagnosticSchema(b, merged)
			declared := graph.BuildDeclaredGraph(merged.Tools)
			ctx := context.Background()
			projected, err := projectGraphView(ctx, declared, merged, facts, graph.DeclaredView, false)
			if err != nil {
				b.Fatal(err)
			}
			selectedTools := map[string]struct{}{"tool00000": {}}
			// A final unreferenced tool tagged for audit provides a one-node
			// --profile subset while preserving the graph benchmark's chain shape.
			merged.Tools[fmt.Sprintf("tool%05d", n-1)].Tags = []string{"audit"}
			var rendered string

			b.Run("parse", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := config.ParseProjectSchema(path, factsMap); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("layers", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					resolveDiagnosticLayers(b, parsed, factsMap)
				}
			})
			b.Run("validate", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					validateDiagnosticSchema(b, merged)
				}
			})
			b.Run("build-declared", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					declared = graph.BuildDeclaredGraph(merged.Tools)
				}
			})
			b.Run("select-one-tool", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_, err := resolvedGraphCandidates(ctx, merged, facts, selectedTools)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("profile-filter-build-one-tool", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					filtered := filterTools(merged.Tools, "", "", "audit")
					if len(filtered) != 1 {
						b.Fatalf("profile filter selected %d tools, want one", len(filtered))
					}
					_ = graph.BuildDeclaredGraph(filtered)
				}
			})
			b.Run("project-declared", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					var err error
					projected, err = projectGraphView(ctx, declared, merged, facts, graph.DeclaredView, false)
					if err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("output-dot", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					rendered = graph.RenderDOTGraph(projected)
					if _, err := io.WriteString(io.Discard, rendered); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

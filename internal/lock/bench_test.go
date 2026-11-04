package lock

// Update/lock core-cost benchmarks for dep-bench-round2-update-why-graph-h8c.
//
// They isolate the lock-layer work that whole-schema update performs with
// thousands of tools: intent hashing, legacy v1 resolution, v2 projection
// construction, and persistence. The app-level BenchmarkUpdateCommand measures
// the same path through the CLI, including schema parsing and validation.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

const benchChecksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func benchSchema(n int) *config.Schema {
	s := &config.Schema{
		Version:  1,
		Defaults: config.Defaults{MethodOrder: []string{"http"}},
		Tools:    make(map[string]*config.Tool, n),
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		s.Tools[name] = &config.Tool{
			Name:       name,
			MethodOnly: []string{"http"},
			Methods: []*config.MethodCandidate{{
				Kind: "http",
				Config: map[string]any{
					"url":      "https://example.test/" + name + ".tar.gz",
					"checksum": benchChecksum,
				},
			}},
		}
	}
	return s
}

func benchPlans(n int) []plan.ResolvedInstallPlan {
	plans := make([]plan.ResolvedInstallPlan, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		p := plan.New(name, "http", true)
		p.Artifacts = []plan.Artifact{{
			Kind:     plan.ArtifactArchive,
			URL:      "https://example.test/" + name + ".tar.gz",
			Checksum: benchChecksum,
		}}
		plans[i] = p
	}
	return plans
}

func BenchmarkSnapshotIntentMetadata(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			s := benchSchema(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := SnapshotIntentMetadata(s); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

func BenchmarkResolveLegacyV1(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			s := benchSchema(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := ResolveLegacyV1(context.Background(), s, &run.FakeRunner{})
				if err != nil {
					b.Fatal(err)
				}
				if len(got.Tools) != n {
					b.Fatalf("pins = %d, want %d", len(got.Tools), n)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

func BenchmarkBuildLockDocument(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			plans := benchPlans(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				doc, err := plan.BuildLockDocument(plans)
				if err != nil {
					b.Fatal(err)
				}
				if len(doc.Entries) != n {
					b.Fatalf("entries = %d, want %d", len(doc.Entries), n)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

func BenchmarkNewUniversal(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			s := benchSchema(n)
			doc, err := plan.BuildLockDocument(benchPlans(n))
			if err != nil {
				b.Fatal(err)
			}
			methodsHash, sourceHash, err := SnapshotIntentMetadata(s)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := NewUniversal(doc, methodsHash, sourceHash); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

func BenchmarkSaveLoadUniversal(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			s := benchSchema(n)
			doc, err := plan.BuildLockDocument(benchPlans(n))
			if err != nil {
				b.Fatal(err)
			}
			methodsHash, sourceHash, err := SnapshotIntentMetadata(s)
			if err != nil {
				b.Fatal(err)
			}
			lk, err := NewUniversal(doc, methodsHash, sourceHash)
			if err != nil {
				b.Fatal(err)
			}
			path := filepath.Join(b.TempDir(), "depengine.lock")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := Save(path, lk); err != nil {
					b.Fatal(err)
				}
				if _, err := Load(path); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

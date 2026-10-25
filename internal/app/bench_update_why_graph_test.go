package app

// Second-round scalability benchmarks (dep-bench-round2-update-why-graph-h8c).
//
// These benchmarks exercise the CLI-level paths that the first planning round
// left unmeasured: whole-schema update/lock, why with a large schema, and a
// dense graph with 5,000 tools. They use generated schemas, a deterministic
// legacy host-detection script, and discarded output, so no network or host
// package manager is involved. They are benchmarks, not production code.
//
// Run:
//
//	go test ./internal/app -run '^$' -bench 'UpdateCommand|WhyCommand|GraphCommand' -benchmem -benchtime=3x
//
// The generated schema uses literal HTTP URLs and checksums so update resolves
// without network access. Graph benchmarks use declared requires edges; the
// update/why schemas deliberately have no dependencies.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/lock"
	deplog "github.com/Khorea1/depengine/internal/log"
)

const benchChecksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func writeBenchSchema(tb testing.TB, n int, withDependencies bool) (dir, schemaPath string) {
	tb.Helper()
	dir = tb.TempDir()
	schemaPath = filepath.Join(dir, "schema.toml")

	var sb strings.Builder
	sb.Grow(n * 220)
	sb.WriteString("schema_version = 1\n\n")
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		fmt.Fprintf(&sb, "[tools.%s]\nmethod_only = [\"http\"]\n", name)
		if withDependencies {
			deps := make([]string, 0, 5)
			for j := 1; j <= 5 && i+j < n; j++ {
				deps = append(deps, fmt.Sprintf("\"tool%05d\"", i+j))
			}
			if len(deps) > 0 {
				fmt.Fprintf(&sb, "requires = [%s]\n", strings.Join(deps, ", "))
			}
		}
		fmt.Fprintf(&sb, "\n[tools.%s.http]\nurl = \"https://example.test/%s.tar.gz\"\nchecksum = %q\n\n", name, name, benchChecksum)
	}
	if err := os.WriteFile(schemaPath, []byte(sb.String()), 0o600); err != nil {
		tb.Fatal(err)
	}

	detector := filepath.Join(dir, "detect.sh")
	writeExecutableFixture(tb, detector, "#!/bin/sh\ncat <<'EOF'\n{\"target_arch\":\"x86_64\",\"distro_id\":\"debian\",\"distro_name\":\"Debian\",\"distro_version\":\"12\",\"distro_id_like\":\"debian\",\"target_family\":\"unix\",\"detection_method\":\"os-release\",\"confidence\":\"high\",\"is_wsl\":false,\"is_container\":false,\"is_android\":false}\nEOF\n")
	tb.Setenv("DEPENGINE_DETECT_SCRIPT", detector)
	tb.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	tb.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	return dir, schemaPath
}

func silenceBenchOutput(tb testing.TB) func() {
	tb.Helper()
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		tb.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	oldLog := deplog.Default
	os.Stdout, os.Stderr = devnull, devnull
	deplog.Default = deplog.New(io.Discard, deplog.LevelError)
	return func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		deplog.Default = oldLog
		_ = devnull.Close()
	}
}

func BenchmarkUpdateCommand(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d/cold", n), func(b *testing.B) {
			_, schemaPath := writeBenchSchema(b, n, false)
			restore := silenceBenchOutput(b)
			defer restore()

			manifest, lockFlag, profile := "", "", ""
			noManifest, frozen, dryRun, verbose := true, false, false, false
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = os.Remove(lock.DefaultPath(schemaPath))
				path := schemaPath
				if err := runUpdate(context.Background(), &path, &manifest, &noManifest, &lockFlag, &profile, &frozen, &dryRun, &verbose); err != nil {
					b.Fatalf("runUpdate: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})

		b.Run(fmt.Sprintf("tools=%d/warm", n), func(b *testing.B) {
			_, schemaPath := writeBenchSchema(b, n, false)
			restore := silenceBenchOutput(b)
			defer restore()

			manifest, lockFlag, profile := "", "", ""
			noManifest, frozen, dryRun, verbose := true, false, false, false
			path := schemaPath
			if err := runUpdate(context.Background(), &path, &manifest, &noManifest, &lockFlag, &profile, &frozen, &dryRun, &verbose); err != nil {
				b.Fatalf("warm-up runUpdate: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				path := schemaPath
				if err := runUpdate(context.Background(), &path, &manifest, &noManifest, &lockFlag, &profile, &frozen, &dryRun, &verbose); err != nil {
					b.Fatalf("runUpdate: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

func BenchmarkWhyCommand(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			_, schemaPath := writeBenchSchema(b, n, false)
			restore := silenceBenchOutput(b)
			defer restore()

			manifest := ""
			noManifest, asJSON, fields := true, false, false
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				path := schemaPath
				if err := runWhy(context.Background(), "tool00000", &path, &manifest, &noManifest, &asJSON, &fields); err != nil {
					b.Fatalf("runWhy: %v", err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N), "ns/why")
		})
	}
}

func BenchmarkGraphCommand(b *testing.B) {
	for _, format := range []string{"dot", "text"} {
		for _, n := range []int{1000, 5000} {
			b.Run(fmt.Sprintf("%s/tools=%d", format, n), func(b *testing.B) {
				_, schemaPath := writeBenchSchema(b, n, true)
				restore := silenceBenchOutput(b)
				defer restore()

				manifest := ""
				noManifest, showInactive := true, false
				width := 0
				view, direction := "declared", "deps"
				profile, only, skip := "", "", ""
				depth := -1
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					path := schemaPath
					if err := runGraphView(context.Background(), &path, &manifest, &noManifest, &format, &width, &view, &showInactive, &profile, &only, &skip, &direction, &depth); err != nil {
						b.Fatalf("runGraphView: %v", err)
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
			})
		}
	}
}

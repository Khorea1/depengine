package exec

// Planning-cost benchmarks.
//
// Question answered: when the executor plans a large schema, does engine CPU
// (candidate selection, dependency ordering, reporting) matter, or does
// subprocess/network I/O dominate?
//
// Method: every benchmark plans with WithDryRun (no install, no state write)
// and a fake runner, so what is measured is pure engine cost. Subprocess
// *counts* are recorded from the fake runner and compared with the measured
// cost of one real process spawn (BenchmarkSubprocessSpawn). Together they
// give a per-tool CPU-versus-I/O estimate:
//
//	cpu_share = ns/tool / (ns/tool + calls/tool * spawn_ns)
//
// Run:
//
//	go test ./internal/exec -run '^$' -bench 'Planning|SubprocessSpawn' -benchmem
//
// Arbitrary-code gate protocol (dep-gate-lean-operations-bit-7j4): compare
// BenchmarkPlanningCandidates (gate on) with BenchmarkPlanningCandidatesGateOff
// (WithAllowArbitraryCode) at the same candidate counts. The gate's marginal
// cost is on minus off; a gate change is judged by the share of that marginal
// cost it removes. Use -count=10 and benchstat; single-run noise reached ~25%.
//
// Limits: the fake runner has zero latency, so wall-clock I/O is modeled, not
// measured end to end. Network resolution (GitHub releases, registries) is not
// covered; it only adds to the I/O side.

import (
	"context"
	"fmt"
	"io"
	osexec "os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

// debianSimRunner models the subprocess surface of a healthy Debian host for
// the native adapter's dry-run path: dpkg-query reports packages as absent
// (its documented exit status 1) except for the "dpkg" sentinel, and
// apt-cache confirms every package exists. It counts every call.
type debianSimRunner struct {
	calls atomic.Int64
}

func (r *debianSimRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls.Add(1)
	switch name {
	case "dpkg-query":
		if len(args) > 0 && args[len(args)-1] == "dpkg" {
			return run.Result{Stdout: []byte("dpkg\tinstall ok installed\n")}
		}
		return run.Result{ExitCode: 1}
	case "apt-cache":
		return run.Result{Stdout: []byte("Package: x\n")}
	}
	return run.Result{}
}

func benchFacts() *platform.Facts {
	return &platform.Facts{
		TargetArch:   "x86_64",
		DistroID:     "debian",
		DistroIDLike: "debian",
		Libc:         "glibc 2.36",
		OS:           "linux",
		TargetFamily: "unix",
	}
}

func newBenchExecutor(b *testing.B, rn run.Runner, adapters ...AdapterV2) *Executor {
	b.Helper()
	b.Setenv("XDG_STATE_HOME", b.TempDir())
	ex := New()
	WithRunner(rn)(ex)
	WithAdapters(adapters...)(ex)
	WithFacts(benchFacts())(ex)
	WithDryRun()(ex)
	WithQuiet()(ex)
	WithOutput(io.Discard)(ex)
	return ex
}

// nativeSchema builds n native tools that all require one shared helper, so
// dependency ordering is exercised with n edges.
func nativeSchema(n int) *config.Schema {
	s := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"native"}},
		Tools:    make(map[string]*config.Tool, n+1),
	}
	s.Tools["helper"] = &config.Tool{Name: "helper", Methods: []*config.MethodCandidate{
		{Kind: "native", Config: map[string]any{"pkg": "helper"}},
	}}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		s.Tools[name] = &config.Tool{
			Name:     name,
			Requires: []string{"helper"},
			Methods: []*config.MethodCandidate{
				{Kind: "native", Config: map[string]any{"pkg": name}},
			},
		}
	}
	return s
}

// candidateSchema builds n tools with c candidates each. Only the last
// candidate matches the host, which is the worst case for a linear scan.
func candidateSchema(n, c int) *config.Schema {
	s := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"http"}},
		Tools:    make(map[string]*config.Tool, n),
	}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%05d", i)
		methods := make([]*config.MethodCandidate, c)
		for j := 0; j < c; j++ {
			arch := "riscv64"
			if j == c-1 {
				arch = "x86_64"
			}
			methods[j] = &config.MethodCandidate{
				Kind:   "http",
				Label:  fmt.Sprintf("c%d", j),
				Config: map[string]any{"url": fmt.Sprintf("https://example.test/%s/%d.tar.gz", name, j)},
				When:   &config.Condition{Arch: []string{arch}, Libc: []string{"glibc"}},
			}
		}
		s.Tools[name] = &config.Tool{Name: name, Methods: methods}
	}
	return s
}

func mockHTTPAdapter() *testMockAdapter {
	return &testMockAdapter{
		kindValue:     "http",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
	}
}

func mustPlan(b *testing.B, ex *Executor, s *config.Schema, wantTools int) {
	b.Helper()
	rep, err := ex.Execute(context.Background(), s, "debian")
	if err != nil {
		b.Fatalf("Execute: %v", err)
	}
	if rep.Failed != 0 || len(rep.Tools) < wantTools {
		b.Fatalf("unexpected report: failed=%d tools=%d want>=%d", rep.Failed, len(rep.Tools), wantTools)
	}
}

// BenchmarkPlanningNative plans n native tools through the real native
// adapter against the simulated Debian runner. calls/tool is the number of
// subprocesses the planning path spawns per tool.
func BenchmarkPlanningNative(b *testing.B) {
	for _, n := range []int{100, 1000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			rn := &debianSimRunner{}
			ex := newBenchExecutor(b, rn, NewNativeAdapter("debian"))
			s := nativeSchema(n)
			mustPlan(b, ex, s, n) // warm-up and sanity check
			rn.calls.Store(0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mustPlan(b, ex, s, n)
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
			b.ReportMetric(float64(rn.calls.Load())/float64(b.N)/float64(n), "calls/tool")
		})
	}
}

// BenchmarkPlanningTools measures how pure engine cost grows with the number
// of tools (2 candidates each, mock adapter: no subprocesses at all).
func BenchmarkPlanningTools(b *testing.B) {
	for _, n := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprintf("tools=%d", n), func(b *testing.B) {
			ex := newBenchExecutor(b, &run.FakeRunner{}, mockHTTPAdapter())
			s := candidateSchema(n, 2)
			mustPlan(b, ex, s, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mustPlan(b, ex, s, n)
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/tool")
		})
	}
}

// BenchmarkPlanningCandidates measures how engine cost grows with the number
// of candidates per tool (100 tools; worst case: the match is last).
func BenchmarkPlanningCandidates(b *testing.B) {
	const tools = 100
	for _, c := range []int{1, 16, 64, 480} {
		b.Run(fmt.Sprintf("cands=%d", c), func(b *testing.B) {
			ex := newBenchExecutor(b, &run.FakeRunner{}, mockHTTPAdapter())
			s := candidateSchema(tools, c)
			mustPlan(b, ex, s, tools)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mustPlan(b, ex, s, tools)
			}
			b.StopTimer()
			perTool := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / float64(tools)
			b.ReportMetric(perTool, "ns/tool")
			b.ReportMetric(perTool/float64(c), "ns/cand")
		})
	}
}

// BenchmarkSubprocessSpawn is the I/O-side yardstick: the cost of one real
// process spawn through the production runner. "true" is the cheapest
// possible child; dpkg-query (when present) is a realistic observation call.
func BenchmarkSubprocessSpawn(b *testing.B) {
	if runtime.GOOS == "windows" {
		b.Skip("uses POSIX commands")
	}
	cases := []struct {
		name string
		cmd  string
		args []string
	}{
		{"true", "true", nil},
		{"dpkg-query", "dpkg-query", []string{"-W", "-f=${binary:Package}\t${Status}\n", "dpkg"}},
	}
	for _, tc := range cases {
		if _, err := osexec.LookPath(tc.cmd); err != nil {
			continue
		}
		b.Run(strings.ReplaceAll(tc.name, " ", "_"), func(b *testing.B) {
			rn := run.OSExecRunner{}
			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if res := rn.Run(ctx, tc.cmd, tc.args...); res.Err != nil {
					b.Fatalf("%s: %v", tc.cmd, res.Err)
				}
			}
		})
	}
}

// BenchmarkPlanningCandidatesGateOff is the "gate off" arm of the gate
// protocol: identical to BenchmarkPlanningCandidates but with
// WithAllowArbitraryCode, which skips the per-candidate arbitrary-code scan.
func BenchmarkPlanningCandidatesGateOff(b *testing.B) {
	const tools = 100
	for _, c := range []int{1, 16, 64, 480} {
		b.Run(fmt.Sprintf("cands=%d", c), func(b *testing.B) {
			ex := newBenchExecutor(b, &run.FakeRunner{}, mockHTTPAdapter())
			WithAllowArbitraryCode()(ex)
			s := candidateSchema(tools, c)
			mustPlan(b, ex, s, tools)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				mustPlan(b, ex, s, tools)
			}
			b.StopTimer()
			perTool := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / float64(tools)
			b.ReportMetric(perTool, "ns/tool")
			b.ReportMetric(perTool/float64(c), "ns/cand")
		})
	}
}

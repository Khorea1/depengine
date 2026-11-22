package exec

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
)

// TestCandidateConditionsAcrossRepresentativeHosts evaluates the same recipe on
// hosts with different families, libc, kernel ranges, and WSL/container
// detection states. It asserts the condition gate only, so adapters are not
// required: a matching candidate may fail later for an unrelated reason.
func TestCandidateConditionsAcrossRepresentativeHosts(t *testing.T) {
	trait := func(value bool) *bool { return &value }
	tool := &config.Tool{
		Name: "demo",
		Methods: []*config.MethodCandidate{
			{
				Kind:   "native",
				Config: map[string]any{"pkg": "demo"},
				When:   &config.Condition{DistroFamily: []string{"debian"}, KernelMin: "6.6"},
			},
			{
				Kind:   "http",
				Config: map[string]any{"url": "https://example.test/demo-musl"},
				When:   &config.Condition{Libc: []string{"musl"}},
			},
			{
				Kind:   "http",
				Config: map[string]any{"url": "https://example.test/demo"},
				When:   &config.Condition{IsWSL: trait(false)},
			},
		},
	}

	cases := []struct {
		name       string
		facts      *platform.Facts
		wantSkip   []bool
		wantReason string
	}{
		{
			name:     "debian glibc",
			facts:    &platform.Facts{OS: "linux", DistroID: "ubuntu", DistroIDLike: "debian", TargetFamily: "unix", Kernel: "6.12.0-generic", Libc: "glibc"},
			wantSkip: []bool{false, true, false},
		},
		{
			name:     "alpine musl",
			facts:    &platform.Facts{OS: "linux", DistroID: "alpine", TargetFamily: "unix", Kernel: "6.12.0-lts", Libc: "musl"},
			wantSkip: []bool{true, false, false},
		},
		{
			name:     "kernel below minimum",
			facts:    &platform.Facts{OS: "linux", DistroID: "ubuntu", DistroIDLike: "debian", TargetFamily: "unix", Kernel: "6.5.0-generic", Libc: "glibc"},
			wantSkip: []bool{true, true, false},
		},
		{
			name:       "wsl detection inconclusive",
			facts:      &platform.Facts{OS: "linux", DistroID: "ubuntu", DistroIDLike: "debian", TargetFamily: "unix", Kernel: "6.12.0-generic", Libc: "glibc", IsWSLUnknown: true},
			wantSkip:   []bool{false, true, true},
			wantReason: "inconclusive",
		},
		{
			name:     "wsl detected",
			facts:    &platform.Facts{OS: "linux", DistroID: "ubuntu", DistroIDLike: "debian", TargetFamily: "unix", Kernel: "6.12.0-microsoft-standard-WSL2", Libc: "glibc", IsWSL: true},
			wantSkip: []bool{false, true, true},
		},
		{
			name:     "macos",
			facts:    &platform.Facts{OS: "darwin", TargetFamily: "unix", TargetArch: "arm64", Kernel: "24.0.0"},
			wantSkip: []bool{true, true, false},
		},
		{
			name:     "windows",
			facts:    &platform.Facts{OS: "windows", TargetFamily: "windows", TargetArch: "amd64", Kernel: "10.0.22631"},
			wantSkip: []bool{true, true, false},
		},
		{
			// Termux reports os = linux without a distro family or libc.
			name:     "termux android",
			facts:    &platform.Facts{OS: "linux", TargetFamily: "unix", TargetArch: "aarch64", Kernel: "6.6.30", IsAndroid: true},
			wantSkip: []bool{true, true, false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executor := New()
			executor.facts = tc.facts
			for i, method := range tool.Methods {
				selection := executor.resolveCandidateForSelection(context.Background(), tool, method, "debian")
				skipped := selection.status == "skip_when"
				if skipped != tc.wantSkip[i] {
					t.Fatalf("method %d status = %q, want skip_when = %v (facts %+v)", i, selection.status, tc.wantSkip[i], tc.facts)
				}
				if skipped && i == 2 && tc.wantReason != "" && (selection.err == nil || !strings.Contains(selection.err.Error(), tc.wantReason)) {
					t.Fatalf("method %d reason = %v, want %q", i, selection.err, tc.wantReason)
				}
			}
		})
	}
}

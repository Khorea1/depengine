package contracttest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestSpecializedVerificationFieldProbes(t *testing.T) {
	t.Run("go package and exact version", func(t *testing.T) {
		adapter := ecosystem.NewGoAdapter()
		t.Run("pkg", func(t *testing.T) {
			runner := &specializedRunner{paths: map[string]bool{"go": true, "tool-a": true}}
			methods := [2]*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "example.org/cmd/tool-a"}}, {Kind: "go", Config: map[string]any{"pkg": "example.org/cmd/tool-b"}}}
			requireProbeStates(t, adapter, runner, [2]plan.ResolvedIdentity{{Package: "example.org/cmd/tool-a"}, {Package: "example.org/cmd/tool-b"}}, methods)
		})
		t.Run("version", func(t *testing.T) {
			runner := &specializedRunner{paths: map[string]bool{"go": true, "tool-a": true}, responses: map[string]string{"tool-a --version": "tool-a version v1.2.3\n"}}
			methods := [2]*config.MethodCandidate{{Kind: "go", Config: map[string]any{"pkg": "example.org/cmd/tool-a", "version": "v1.2.3"}}, {Kind: "go", Config: map[string]any{"pkg": "example.org/cmd/tool-a", "version": "v2.0.0"}}}
			requireProbeStates(t, adapter, runner, [2]plan.ResolvedIdentity{{Package: "example.org/cmd/tool-a", Version: "v1.2.3"}, {Package: "example.org/cmd/tool-a", Version: "v2.0.0"}}, methods)
		})
	})

	t.Run("sdkman package and exact version", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		for _, path := range []string{
			filepath.Join(home, ".sdkman", "candidates", "gradle", "current"),
			filepath.Join(home, ".sdkman", "candidates", "gradle", "8.7"),
		} {
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		adapter := ecosystem.NewSDKManAdapter()
		for _, tc := range []struct {
			name         string
			a, b         map[string]any
			wantA, wantB plan.ResolvedIdentity
		}{
			{"pkg", map[string]any{"pkg": "gradle"}, map[string]any{"pkg": "maven"}, plan.ResolvedIdentity{Package: "gradle"}, plan.ResolvedIdentity{Package: "maven"}},
			{"version", map[string]any{"pkg": "gradle", "version": "8.7"}, map[string]any{"pkg": "gradle", "version": "8.8"}, plan.ResolvedIdentity{Package: "gradle", Version: "8.7"}, plan.ResolvedIdentity{Package: "gradle", Version: "8.8"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				methods := [2]*config.MethodCandidate{{Kind: "sdkman", Config: tc.a}, {Kind: "sdkman", Config: tc.b}}
				requireProbeStates(t, adapter, &verifyStateRunner{}, [2]plan.ResolvedIdentity{tc.wantA, tc.wantB}, methods)
			})
		}
	})

	t.Run("asdf package and exact version", func(t *testing.T) {
		adapter := ecosystem.NewAsdfAdapter()
		for _, tc := range []struct {
			name         string
			a, b         map[string]any
			wantA, wantB plan.ResolvedIdentity
			responses    map[string]string
		}{
			{"pkg", map[string]any{"pkg": "nodejs"}, map[string]any{"pkg": "ruby"}, plan.ResolvedIdentity{Package: "nodejs"}, plan.ResolvedIdentity{Package: "ruby"}, map[string]string{"asdf list nodejs": "20.1.0\n", "asdf list ruby": ""}},
			{"version", map[string]any{"pkg": "nodejs", "version": "20.1.0"}, map[string]any{"pkg": "nodejs", "version": "22.0.0"}, plan.ResolvedIdentity{Package: "nodejs", Version: "20.1.0"}, plan.ResolvedIdentity{Package: "nodejs", Version: "22.0.0"}, map[string]string{"asdf list nodejs": "20.1.0\n"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				runner := &verifyStateRunner{responses: tc.responses}
				methods := [2]*config.MethodCandidate{{Kind: "asdf", Config: tc.a}, {Kind: "asdf", Config: tc.b}}
				requireProbeStates(t, adapter, runner, [2]plan.ResolvedIdentity{tc.wantA, tc.wantB}, methods)
			})
		}
	})

	t.Run("aur package", func(t *testing.T) {
		runner := &verifyStateRunner{exitCodes: map[string]int{"paru -Qi demo-b": 1}}
		methods := [2]*config.MethodCandidate{{Kind: "aur", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "aur", Config: map[string]any{"pkg": "demo-b"}}}
		requireProbeStates(t, ecosystem.NewAURAdapter("paru"), runner, [2]plan.ResolvedIdentity{{Package: "demo-a"}, {Package: "demo-b"}}, methods)
	})

	t.Run("pacstall package", func(t *testing.T) {
		runner := &verifyStateRunner{exitCodes: map[string]int{"pacstall -Ci demo-b": 1}}
		methods := [2]*config.MethodCandidate{{Kind: "pacstall", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "pacstall", Config: map[string]any{"pkg": "demo-b"}}}
		requireProbeStates(t, ecosystem.NewPacstallAdapter(), runner, [2]plan.ResolvedIdentity{{Package: "demo-a"}, {Package: "demo-b"}}, methods)
	})

	t.Run("yarn berry package", func(t *testing.T) {
		const check = "yarn node -e try{process.exit(require.resolve(process.argv[1])?0:1)}catch(e){process.exit(1)}"
		runner := &verifyStateRunner{exitCodes: map[string]int{check + " demo-b": 1}}
		methods := [2]*config.MethodCandidate{{Kind: "yarn-berry", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "yarn-berry", Config: map[string]any{"pkg": "demo-b"}}}
		requireProbeStates(t, ecosystem.NewYarnBerryAdapter(), runner, [2]plan.ResolvedIdentity{{Package: "demo-a"}, {Package: "demo-b"}}, methods)
	})

	t.Run("snap package", func(t *testing.T) {
		runner := &verifyStateRunner{responses: map[string]string{"snap list demo-a": "Name Version Rev Tracking Publisher Notes\ndemo-a 1 1 latest/stable store -\n"}, exitCodes: map[string]int{"snap list demo-b": 1}}
		adapter := ecosystem.NewBaseAdapter(ecosystem.Configs["snap"])
		methods := [2]*config.MethodCandidate{{Kind: "snap", Config: map[string]any{"pkg": "demo-a"}}, {Kind: "snap", Config: map[string]any{"pkg": "demo-b"}}}
		requireProbeStates(t, adapter, runner, [2]plan.ResolvedIdentity{{Package: "demo-a"}, {Package: "demo-b"}}, methods)
	})

	t.Run("steamcmd package remains unverifiable", func(t *testing.T) {
		adapter := ecosystem.NewSteamCMDAdapter()
		for _, pkg := range []string{"100", "200"} {
			method := &config.MethodCandidate{Kind: "steamcmd", Config: map[string]any{"pkg": pkg}}
			got := probeObservation(t, adapter, run.Runner(&verifyStateRunner{}), method)
			if got.Presence != plan.PresenceUnknown {
				t.Fatalf("Observe(%s) presence = %s, want unknown", pkg, got.Presence)
			}
		}
	})
}

type specializedRunner struct {
	paths     map[string]bool
	responses map[string]string
}

func (r *specializedRunner) LookPath(_ context.Context, name string) bool { return r.paths[name] }

func (r *specializedRunner) Run(_ context.Context, name string, args ...string) run.Result {
	key := strings.Join(append([]string{name}, args...), " ")
	result := run.Result{}
	if output, ok := r.responses[key]; ok {
		result.Stdout = []byte(output)
		return result
	}
	if !r.paths[name] {
		result.ExitCode = 1
	}
	return result
}

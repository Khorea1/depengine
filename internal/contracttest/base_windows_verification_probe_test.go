package contracttest

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

type differentialVerifyRunner struct {
	responses     map[string]run.Result
	paths         map[string]bool
	defaultResult run.Result
}

func (r *differentialVerifyRunner) Run(_ context.Context, name string, args ...string) run.Result {
	key := strings.Join(append([]string{name}, args...), " ")
	if result, ok := r.responses[key]; ok {
		return result
	}
	return r.defaultResult
}

func (r *differentialVerifyRunner) LookPath(_ context.Context, name string) bool {
	if ok, exists := r.paths[name]; exists {
		return ok
	}
	return true
}

func (r *differentialVerifyRunner) RunWithEnv(ctx context.Context, _ map[string]string, _ []string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func (r *differentialVerifyRunner) RunInDir(ctx context.Context, _ string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func TestBaseAndWindowsVerificationFieldProbes(t *testing.T) {
	t.Run("Windows managers", func(t *testing.T) {
		adapters := make(map[string]exec.AdapterV2)
		for _, adapter := range exec.WindowsAdapters() {
			adapters[adapter.Kind()] = adapter
		}
		for _, tc := range []struct {
			kind, field      string
			configA, configB map[string]any
			responses        map[string]run.Result
		}{
			{kind: "scoop", field: "pkg", configA: map[string]any{"pkg": "alpha"}, configB: map[string]any{"pkg": "beta"}, responses: map[string]run.Result{"scoop list alpha": {Stdout: []byte("alpha 1.2 main\n")}, "scoop list beta": {Stdout: []byte("alpha 1.2 main\n")}}},
			{kind: "scoop", field: "version", configA: map[string]any{"pkg": "alpha", "version": "1.2"}, configB: map[string]any{"pkg": "alpha", "version": "2.0"}, responses: map[string]run.Result{"scoop list alpha": {Stdout: []byte("alpha 1.2 main\n")}}},
			{kind: "scoop", field: "bucket", configA: map[string]any{"pkg": "alpha", "bucket": "main"}, configB: map[string]any{"pkg": "alpha", "bucket": "extras"}, responses: map[string]run.Result{"scoop list alpha": {Stdout: []byte("alpha 1.2 main\n")}}},
			{kind: "scoop", field: "scope", configA: map[string]any{"pkg": "alpha", "scope": "user"}, configB: map[string]any{"pkg": "alpha", "scope": "global"}, responses: map[string]run.Result{"scoop list alpha": {Stdout: []byte("alpha 1.2 main\n")}, "scoop list alpha --global": {Stdout: []byte("\n")}}},
			{kind: "choco", field: "pkg", configA: map[string]any{"pkg": "alpha"}, configB: map[string]any{"pkg": "beta"}, responses: map[string]run.Result{"choco list --local-only --exact --limit-output alpha": {Stdout: []byte("alpha|1.2\n")}, "choco list --local-only --exact --limit-output beta": {Stdout: []byte("alpha|1.2\n")}}},
			{kind: "choco", field: "version", configA: map[string]any{"pkg": "alpha", "version": "1.2"}, configB: map[string]any{"pkg": "alpha", "version": "2.0"}, responses: map[string]run.Result{"choco list --local-only --exact --limit-output alpha": {Stdout: []byte("alpha|1.2\n")}}},
		} {
			t.Run(tc.kind+"/"+tc.field, func(t *testing.T) {
				probeAB(t, adapters[tc.kind], tc.kind, tc.configA, tc.configB, &differentialVerifyRunner{responses: tc.responses})
			})
		}
	})

	t.Run("BaseAdapter kinds", func(t *testing.T) {
		for _, tc := range []struct {
			kind, field      string
			configA, configB map[string]any
			responses        map[string]run.Result
			paths            map[string]bool
		}{
			baseCase("bun", "pkg", "bun pm ls -g", "├── alpha@1.2.0\n", "pkg"),
			baseCase("bun", "version", "bun pm ls -g", "├── alpha@1.2.0\n", "version"),
			baseCase("cask", "pkg", "brew list --cask alpha", "", "pkg"),
			baseCase("gem", "pkg", "gem list --local --exact alpha --all", "alpha (1.2.0, 1.1.0)\n", "pkg"),
			baseCase("gem", "version", "gem list --local --exact alpha --all", "alpha (1.2.0, 1.1.0)\n", "version"),
			baseCase("pipx", "pkg", "pipx list --output json alpha", `{"pipx_spec_version":"0.1","venvs":{"alpha":{"main_package":{"package":"alpha","package_version":"1.2.0"}}}}`, "pkg"),
			baseCase("pipx", "version", "pipx list --output json alpha", `{"pipx_spec_version":"0.1","venvs":{"alpha":{"main_package":{"package":"alpha","package_version":"1.2.0"}}}}`, "version"),
			baseCase("npm", "pkg", "npm ls -g --depth=0 --json alpha", `{"dependencies":{"alpha":{"version":"1.2.0"}}}`, "pkg"),
			baseCase("npm", "version", "npm ls -g --depth=0 --json alpha", `{"dependencies":{"alpha":{"version":"1.2.0"}}}`, "version"),
			baseCase("apm", "pkg", "apm list --installed --bare", "alpha@1.2.0\n", "pkg"),
			baseCase("mas", "pkg", "mas list", "123456789 Alpha App (1.2)\n", "123456789"),
			baseCase("uv", "pkg", "uv tool list", "alpha v1.2.0\n- alpha\n", "pkg"),
			baseCase("uv", "version", "uv tool list", "alpha v1.2.0\n- alpha\n", "version"),
			baseCase("vscode", "pkg", "code --list-extensions", "publisher.alpha\n", "publisher.alpha", "publisher.beta"),
			baseCase("vscodium", "pkg", "codium --list-extensions", "publisher.alpha\n", "publisher.alpha", "publisher.beta"),
			baseCase("composer", "pkg", "composer global show --locked vendor/alpha", "name     : vendor/alpha\nversions : * 1.2.0\n", "vendor/alpha"),
			baseCase("composer", "version", "composer global show --locked vendor/alpha", "name     : vendor/alpha\nversions : * 1.2.0\n", "version"),
			baseCase("appman", "pkg", "", "", "pkg"),
			baseCase("yarn", "pkg", "yarn global list --depth=0", "info \"alpha@1.2.0\" has binaries:\n", "pkg"),
			baseCase("yarn", "version", "yarn global list --depth=0", "info \"alpha@1.2.0\" has binaries:\n", "version"),
			baseCase("pnpm", "pkg", "pnpm list -g --depth=0 --json", `[{"dependencies":{"alpha":{"version":"1.2.0"}}}]`, "pkg"),
			baseCase("pnpm", "version", "pnpm list -g --depth=0 --json", `[{"dependencies":{"alpha":{"version":"1.2.0"}}}]`, "version"),
		} {
			t.Run(tc.kind+"/"+tc.field, func(t *testing.T) {
				runner := &differentialVerifyRunner{responses: tc.responses, paths: tc.paths}
				probeAB(t, ecosystem.NewBaseAdapter(ecosystem.Configs[tc.kind]), tc.kind, tc.configA, tc.configB, runner)
			})
		}
	})
}

func baseCase(kind, field, command, output, desiredA string, desiredB ...string) struct {
	kind, field      string
	configA, configB map[string]any
	responses        map[string]run.Result
	paths            map[string]bool
} {
	valueB := "beta"
	if len(desiredB) > 0 {
		valueB = desiredB[0]
	}
	configA := map[string]any{"pkg": "alpha"}
	configB := map[string]any{"pkg": "beta"}
	if field == "version" {
		configA["version"], configB["version"] = "1.2.0", "2.0.0"
		if kind == "composer" {
			configA["pkg"], configB["pkg"] = "vendor/alpha", "vendor/alpha"
		}
	} else {
		if desiredA != "pkg" {
			configA["pkg"] = desiredA
		}
		configB["pkg"] = valueB
	}
	responses := map[string]run.Result{}
	paths := map[string]bool{}
	switch kind {
	case "appman":
		paths["alpha"], paths["beta"] = true, false
	case "cask":
		responses[command] = run.Result{}
		responses["brew list --cask beta"] = run.Result{ExitCode: 1}
	default:
		responses[command] = run.Result{Stdout: []byte(output)}
	}
	return struct {
		kind, field      string
		configA, configB map[string]any
		responses        map[string]run.Result
		paths            map[string]bool
	}{kind, field, configA, configB, responses, paths}
}

func probeAB(t *testing.T, adapter interface {
	Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error)
}, kind string, configA, configB map[string]any, runner run.Runner) {
	t.Helper()
	methods := [2]*config.MethodCandidate{{Kind: kind, Config: configA}, {Kind: kind, Config: configB}}
	states := [2]plan.VerificationState{}
	for i, method := range methods {
		observation, err := adapter.Observe(context.Background(), runner, &config.Tool{Name: "probe"}, method)
		if err != nil {
			t.Fatalf("Observe(%s) failed: %v", kind, err)
		}
		states[i] = plan.Reconcile(plan.ResolvedIdentity{Package: method.Config["pkg"].(string)}, observation).State
	}
	if states[0] != plan.StateSatisfied || states[1] == plan.StateSatisfied {
		t.Fatalf("A/B verification states = %v; want satisfied then absent/drifted", states)
	}
}

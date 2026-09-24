package contracttest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

// Every owned EffectExecute field is varied independently through planning,
// adapter resolution, and InstallResolved. FakeRunner captures the actual
// process boundary without a host package manager or network access.
func TestExecuteCoverageRegisteredAtInit(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		if !executionProbeKinds[contract.Kind] {
			continue
		}
		for field, spec := range contract.Fields {
			if spec.Effects&methodkind.EffectExecute == 0 {
				continue
			}
			if contract.Kind == "cargo" && field == "secret_ref" {
				continue // runtime secret resolution is covered at the executor boundary
			}
			key := contract.Kind + "." + field
			if _, ok := CoverageFor(PhaseExecute, key); !ok {
				t.Errorf("execution evidence for %s is not registered during package initialization", key)
			}
		}
	}
}

func TestExecuteEffectFieldProbes(t *testing.T) {
	for _, contract := range methodkind.Contracts {
		if !executionProbeKinds[contract.Kind] {
			continue
		}
		for field, spec := range contract.Fields {
			if spec.Effects&methodkind.EffectExecute == 0 {
				continue
			}
			if contract.Kind == "cargo" && field == "secret_ref" {
				continue // runtime secret resolution is covered at the executor boundary
			}
			key := contract.Kind + "." + field
			t.Run(key, func(t *testing.T) {
				values, ok := executionValues(field, spec)
				if !ok {
					t.Fatalf("no differential fixture for %s", key)
				}
				before := executionCalls(t, contract.Kind, field, values[0])
				after := executionCalls(t, contract.Kind, field, values[1])
				if reflect.DeepEqual(before, after) {
					t.Fatalf("changing %s did not change execution calls: %#v", key, before)
				}
				coverage, ok := CoverageFor(PhaseExecute, key)
				if !ok || coverage.Consumer != "TestExecuteEffectFieldProbes" {
					t.Fatalf("missing static execution evidence for %s", key)
				}
			})
		}
	}
}

func executionCalls(t *testing.T, kind, field string, value any) []run.FakeCall {
	t.Helper()
	if kind == "sdkman" {
		root := t.TempDir()
		initScript := filepath.Join(root, "bin", "sdkman-init.sh")
		if err := os.MkdirAll(filepath.Dir(initScript), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(initScript, []byte("# test SDKMAN init\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("SDKMAN_DIR", root)
	}
	adapter := executionAdapter(kind)
	if adapter == nil {
		t.Fatalf("no execution adapter for %q", kind)
	}
	cfg := map[string]any{}
	switch {
	case kind == "cargo" && (field == "branch" || field == "tag" || field == "rev"):
		cfg["git"] = "https://example.test/demo.git"
	case kind == "conda" && field == "build":
		cfg["pkg"] = "demo"
		cfg["version"] = "1.2.3"
	default:
		cfg["pkg"] = "demo"
	}
	cfg[field] = value
	method := &config.MethodCandidate{Kind: kind, Config: cfg}
	tool := &config.Tool{Name: "demo"}
	intent, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("build %s intent: %v", kind, err)
	}
	paths := map[string]bool{}
	for _, binary := range []string{"scoop", "choco", "cargo", "go", "pipx", "uv", "pip", "npm", "pnpm", "bun", "gem", "yarn", "composer", "apm", "code", "codium", "flatpak", "snap", "brew", "appman", "mas", "sdk", "steamcmd", "pacstall", "yay", "conda", "asdf"} {
		paths[binary] = true
	}
	runner := &run.FakeRunner{LookPaths: paths}
	resolved, err := adapter.ResolvePlan(context.Background(), runner, tool, method, &intent)
	if err != nil {
		t.Fatalf("resolve %s plan: %v", kind, err)
	}
	if err := adapter.InstallResolved(context.Background(), runner, tool, method, resolved); err != nil {
		t.Fatalf("execute %s: %v", kind, err)
	}
	return runner.Calls
}

func executionAdapter(kind string) exec.AdapterV2 {
	switch kind {
	case "scoop", "choco":
		for _, adapter := range exec.WindowsAdapters() {
			if adapter.Kind() == kind {
				return adapter
			}
		}
	case "cargo":
		return ecosystem.NewCargoAdapter()
	case "go":
		return ecosystem.NewGoAdapter()
	case "conda":
		return ecosystem.NewCondaAdapter()
	case "sdkman":
		return ecosystem.NewSDKManAdapter()
	case "steamcmd":
		return ecosystem.NewSteamCMDAdapter()
	case "pacstall":
		return ecosystem.NewPacstallAdapter()
	case "aur":
		return ecosystem.NewAURAdapter("yay")
	case "asdf":
		return ecosystem.NewAsdfAdapter()
	case "yarn-berry":
		return ecosystem.NewYarnBerryAdapter()
	default:
		if cfg, ok := ecosystem.Configs[kind]; ok {
			return ecosystem.NewBaseAdapter(cfg)
		}
	}
	return nil
}

func executionValues(field string, spec methodkind.Field) ([2]any, bool) {
	if len(spec.Enum) > 1 {
		return [2]any{spec.Enum[0], spec.Enum[1]}, true
	}
	switch spec.Type {
	case methodkind.Boolean:
		return [2]any{false, true}, true
	case methodkind.StringList:
		return [2]any{[]any{"first-value"}, []any{"second-value"}}, true
	case methodkind.StringStringMap:
		return [2]any{map[string]string{"launcher": "first"}, map[string]string{"launcher": "second"}}, true
	case methodkind.Integer:
		return [2]any{1, 2}, true
	case methodkind.IntegerOrString:
		return [2]any{1, 2}, true
	case methodkind.String:
		if field == "pkg" {
			return [2]any{"demo-first", "demo-second"}, true
		}
		if field == "git" {
			return [2]any{"https://example.test/first.git", "https://example.test/second.git"}, true
		}
		return [2]any{fmt.Sprintf("first-%s", field), fmt.Sprintf("second-%s", field)}, true
	default:
		return [2]any{}, false
	}
}

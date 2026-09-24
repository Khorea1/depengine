package exec

import (
	"context"
	"os"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestTypedSecretOmittedFromHookChild(t *testing.T) {
	if os.Getenv("DEPENGINE_SECRET_ENV_CHILD") != "1" {
		return
	}
	if _, found := os.LookupEnv("DEPENGINE_SECRET_ENV_TOKEN"); found {
		t.Fatal("typed secret source reached hook child")
	}
	if os.Getenv("DEPENGINE_SECRET_ENV_UNRELATED") != "preserved" {
		t.Fatal("unrelated environment was removed from hook child")
	}
}

func TestTypedSecretVisibleToUnrelatedHookChild(t *testing.T) {
	if os.Getenv("DEPENGINE_SECRET_ENV_CHILD") != "1" {
		return
	}
	if got := os.Getenv("DEPENGINE_SECRET_ENV_TOKEN"); got != "runtime-secret-sentinel" {
		t.Fatalf("unrelated hook lost ambient variable: %q", got)
	}
}

func TestExecutorOmitsTypedSecretSourceFromPreAndPostHooks(t *testing.T) {
	t.Setenv("DEPENGINE_SECRET_ENV_TOKEN", "runtime-secret-sentinel")
	t.Setenv("DEPENGINE_SECRET_ENV_UNRELATED", "preserved")
	t.Setenv("DEPENGINE_SECRET_ENV_CHILD", "1")
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hook := config.Hook{Run: []string{exe, "-test.run=^TestTypedSecretOmittedFromHookChild$"}}
	unrelatedHook := config.Hook{Run: []string{exe, "-test.run=^TestTypedSecretVisibleToUnrelatedHookChild$"}}
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"http"}},
		Tools: map[string]*config.Tool{"demo": {
			Name: "demo", PreInstall: []config.Hook{hook}, PostInstall: []config.Hook{hook},
			Methods: []*config.MethodCandidate{{
				Kind: "http", SecretRef: &config.SecretReference{Provider: "env", Name: "DEPENGINE_SECRET_ENV_TOKEN"},
				Config: map[string]any{"url": "https://example.test/demo.tar.gz"},
			}},
		},
			"unrelated": {
				Name: "unrelated", PreInstall: []config.Hook{unrelatedHook},
				Methods: []*config.MethodCandidate{{Kind: "http", Config: map[string]any{"url": "https://example.test/unrelated.tar.gz"}}},
			},
		},
	}
	ex := New()
	WithAllowArbitraryCode()(ex)
	WithRunner(run.OSExecRunner{})(ex)
	WithAdapters(&testMockAdapter{kindValue: "http"})(ex)
	report, err := ex.Execute(context.Background(), schema, "")
	if err != nil {
		t.Fatal(err)
	}
	if report.Success != 2 {
		t.Fatalf("typed secret hook execution = %+v", report.Tools)
	}
	for _, result := range report.Tools {
		if result.Tool == "demo" && (!result.PreinstallDone || !result.PostinstallDone) {
			t.Fatalf("typed secret tool hooks did not run: %+v", result)
		}
	}
	if os.Getenv("DEPENGINE_SECRET_ENV_TOKEN") != "runtime-secret-sentinel" {
		t.Fatal("parent secret environment was changed")
	}
}

type secretEnvironmentProbeAdapter struct {
	*testMockAdapter
	executable string
}

func (a *secretEnvironmentProbeAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate, _ *plan.ResolvedInstallPlan) error {
	return run.CheckResult(rn.Run(ctx, a.executable, "-test.run=^TestTypedSecretOmittedFromHookChild$"), "secret environment probe")
}

func TestDirectResolvedInstallOmitsTypedSecretSourceFromAdapterChild(t *testing.T) {
	t.Setenv("DEPENGINE_SECRET_ENV_TOKEN", "runtime-secret-sentinel")
	t.Setenv("DEPENGINE_SECRET_ENV_UNRELATED", "preserved")
	t.Setenv("DEPENGINE_SECRET_ENV_CHILD", "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	method := &config.MethodCandidate{
		Kind: "http", SecretRef: &config.SecretReference{Provider: "env", Name: "DEPENGINE_SECRET_ENV_TOKEN"},
		Config: map[string]any{"url": "https://example.test/demo.tar.gz"},
	}
	ex := New()
	WithAdapters(&secretEnvironmentProbeAdapter{testMockAdapter: &testMockAdapter{kindValue: "http"}, executable: exe})(ex)
	resolved := plan.New("demo", "http", true)
	if err := ex.InstallResolvedCandidate(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "demo"}, method, &resolved); err != nil {
		t.Fatal(err)
	}
}

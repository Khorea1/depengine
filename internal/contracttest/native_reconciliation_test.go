package contracttest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	executor "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type nativeLifecycleRunner struct {
	installed bool
	installs  int
	calls     []run.FakeCall
}

func (r *nativeLifecycleRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: append([]string(nil), args...)})
	matches := func(command []string) bool {
		return len(command) > 0 && name == command[0] && reflect.DeepEqual(args, command[1:])
	}
	switch {
	case matches(native.BuildDpkgObserveCmd("demo")):
		if r.installed {
			return run.Result{Stdout: []byte("demo\tinstall ok installed\n")}
		}
		return run.Result{Stdout: []byte("demo\tdeinstall ok not-installed\n")}
	case matches(native.BuildDpkgSentinelCmd()):
		return run.Result{Stdout: []byte("dpkg\tinstall ok installed\n")}
	case name == "apt-get" && len(args) > 0 && args[0] == "install" ||
		name == "sudo" && len(args) > 1 && args[0] == "apt-get" && args[1] == "install":
		r.installs++
		r.installed = true
	}
	return run.Result{}
}

func (*nativeLifecycleRunner) LookPath(context.Context, string) bool { return true }

func (r *nativeLifecycleRunner) RunWithEnv(ctx context.Context, _ map[string]string, _ []string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func (r *nativeLifecycleRunner) RunInDir(ctx context.Context, _ string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func TestNativeExecutorInstallIsObservedTrackedAndIdempotent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &nativeLifecycleRunner{}
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{"native"},
		Methods:    []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": "demo"}}},
	}
	schema := &config.Schema{
		Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"native"}},
		Tools:    map[string]*config.Tool{"demo": tool},
	}
	ex := executor.New()
	executor.WithRunner(runner)(ex)
	executor.WithAdapters(executor.NewNativeAdapter("debian"))(ex)
	executor.WithSchemaInfo("/test/native-schema.toml", time.Now())(ex)

	first, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if len(first.Tools) != 1 || first.Tools[0].Status != executor.StatusInstalled || !first.Tools[0].InstallCommitted {
		t.Fatalf("first Execute() = %+v, want committed native install", first.Tools)
	}
	if runner.installs != 1 || !runner.installed {
		t.Fatalf("native install state: calls=%d installed=%v", runner.installs, runner.installed)
	}
	tracked, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if tool, ok := tracked.Tools["demo"]; !ok || tool.MethodKind != "native" {
		t.Fatalf("tracked state = %+v (present=%v), want verified native install", tool, ok)
	}

	second, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("repeat Execute() error = %v", err)
	}
	if len(second.Tools) != 1 || second.Tools[0].Status != executor.StatusAlready {
		t.Fatalf("repeat Execute() = %+v, want already", second.Tools)
	}
	if runner.installs != 1 {
		t.Fatalf("repeat Execute() added native install calls: %d", runner.installs)
	}
}

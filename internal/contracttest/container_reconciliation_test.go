package contracttest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	containeradapter "github.com/Khorea1/depengine/internal/container"
	executor "github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
)

type containerLifecycleRunner struct {
	images       map[string]string
	calls        []run.FakeCall
	probeFailure bool
}

func (r *containerLifecycleRunner) Run(_ context.Context, name string, args ...string) run.Result {
	call := run.FakeCall{Name: name, Args: append([]string(nil), args...)}
	r.calls = append(r.calls, call)
	if len(args) >= 3 && args[0] == "images" && args[1] == "-q" {
		if r.probeFailure {
			return run.Result{Err: errors.New("container engine unavailable")}
		}
		if id := r.images[args[2]]; id != "" {
			return run.Result{Stdout: []byte(id + "\n")}
		}
		return run.Result{}
	}
	if len(args) == 2 && args[0] == "pull" {
		if r.images == nil {
			r.images = make(map[string]string)
		}
		r.images[args[1]] = "sha256:controlled-image-id"
	}
	return run.Result{}
}

func (*containerLifecycleRunner) LookPath(context.Context, string) bool { return true }

func (r *containerLifecycleRunner) RunWithEnv(ctx context.Context, _ map[string]string, _ []string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func (r *containerLifecycleRunner) RunInDir(ctx context.Context, _ string, name string, args ...string) run.Result {
	return r.Run(ctx, name, args...)
}

func TestContainerExecutorInstallIsObservedTrackedAndIdempotent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &containerLifecycleRunner{images: make(map[string]string)}
	schema := containerLifecycleSchema()
	ex := executor.New()
	executor.WithRunner(runner)(ex)
	executor.WithAdapters(containeradapter.NewContainerAdapter())(ex)
	executor.WithSchemaInfo("/test/container-schema.toml", time.Now())(ex)

	first, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if len(first.Tools) != 1 || first.Tools[0].Status != executor.StatusInstalled || !first.Tools[0].InstallCommitted {
		t.Fatalf("first Execute() = %+v, want committed install", first.Tools)
	}
	if got := containerPulls(runner.calls); got != 1 {
		t.Fatalf("pull calls after first Execute() = %d, want 1", got)
	}
	tracked, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if tool, ok := tracked.Tools["demo"]; !ok || tool.MethodKind != "container" {
		t.Fatalf("tracked state = %+v (present=%v), want verified container install", tool, ok)
	}

	second, err := ex.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("repeat Execute() error = %v", err)
	}
	if len(second.Tools) != 1 || second.Tools[0].Status != executor.StatusAlready {
		t.Fatalf("repeat Execute() = %+v, want already", second.Tools)
	}
	if got := containerPulls(runner.calls); got != 1 {
		t.Fatalf("repeat Execute() added a pull: %d total", got)
	}
}

func TestContainerExecutorUnknownInventoryDoesNotInstallOrTrack(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	runner := &containerLifecycleRunner{probeFailure: true}
	ex := executor.New()
	executor.WithRunner(runner)(ex)
	executor.WithAdapters(containeradapter.NewContainerAdapter())(ex)
	executor.WithSchemaInfo("/test/container-schema.toml", time.Now())(ex)

	report, err := ex.Execute(context.Background(), containerLifecycleSchema(), "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != executor.StatusFailed || report.Tools[0].InstallCommitted {
		t.Fatalf("Execute() = %+v, want failed/uncommitted on unknown inventory", report.Tools)
	}
	if got := containerPulls(runner.calls); got != 0 {
		t.Fatalf("unknown inventory authorized %d pulls", got)
	}
	tracked, err := state.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked.Tools) != 0 {
		t.Fatalf("unknown inventory created tracked tools: %+v", tracked.Tools)
	}
}

func containerLifecycleSchema() *config.Schema {
	tool := &config.Tool{
		Name:       "demo",
		MethodOnly: []string{"container"},
		Methods: []*config.MethodCandidate{{Kind: "container", Config: map[string]any{
			"manager": "podman",
			"source":  "registry.example.test/team/demo",
			"tag":     "1.2.3",
		}}},
	}
	return &config.Schema{
		Defaults: config.Defaults{Manager: "container", MethodOrder: []string{"container"}},
		Tools:    map[string]*config.Tool{"demo": tool},
	}
}

func containerPulls(calls []run.FakeCall) int {
	count := 0
	for _, call := range calls {
		if len(call.Args) > 0 && call.Args[0] == "pull" {
			count++
		}
	}
	return count
}

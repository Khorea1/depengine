package exec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	deplog "github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	depstate "github.com/Khorea1/depengine/internal/state"
)

type failingVersionProbeAdapter struct {
	*testMockAdapter
}

func (a *failingVersionProbeAdapter) InstalledVersion(ctx context.Context, rn run.Runner, _ *config.Tool, _ *config.MethodCandidate) (string, error) {
	result := rn.Run(ctx, "version-probe")
	if result.ExitCode == 0 && result.Err == nil {
		return "1.0.0", nil
	}
	return "", errors.New("version probe failed")
}

func TestInstalledVersionFailureUsesProbeLoggingLevel(t *testing.T) {
	capture := deplog.NewTestLogger(t)
	inner := &run.FakeRunner{ExitCode: 1, Stderr: "not installed"}
	runner := run.NewLoggingRunner(inner, capture.Logger)
	adapter := &failingVersionProbeAdapter{testMockAdapter: &testMockAdapter{kindValue: "probe-kind"}}

	ex := New()
	WithRunner(runner)(ex)
	WithAdapters(adapter)(ex)
	got := ex.installedVersion(context.Background(), &config.Tool{Name: "demo"}, ToolResult{MethodKind: "probe-kind"})
	if got != "" {
		t.Fatalf("installedVersion = %q, want empty on failed probe", got)
	}
	logOutput := capture.String()
	if !strings.Contains(logOutput, "run exited non-zero") {
		t.Fatalf("missing failed probe log: %s", logOutput)
	}
	if strings.Contains(logOutput, "WARN") {
		t.Fatalf("version probe failure logged as warning: %s", logOutput)
	}
}
func TestToolStateVersionPrecedence(t *testing.T) {
	planVersion := &plan.ResolvedInstallPlan{Identity: plan.ResolvedIdentity{Version: "2.0.0"}}
	existing := depstate.ToolState{Version: "1.5.0"}

	t.Run("observed version wins over resolved version", func(t *testing.T) {
		adapter := &failingVersionProbeAdapter{testMockAdapter: &testMockAdapter{kindValue: "probe-kind"}}
		ex := New()
		WithRunner(&run.FakeRunner{})(ex)
		WithAdapters(adapter)(ex)
		got := ex.toolStateForResult(context.Background(), &config.Tool{Name: "demo"}, ToolResult{
			Status: StatusInstalled, MethodKind: "probe-kind", PlanIntent: planVersion,
		}, existing, true, true)
		if got.Version != "1.0.0" {
			t.Fatalf("Version = %q, want observed 1.0.0", got.Version)
		}
	})

	t.Run("resolved version wins when observation is unavailable", func(t *testing.T) {
		ex := New()
		WithAdapters(&testMockAdapter{kindValue: "probe-kind"})(ex)
		got := ex.toolStateForResult(context.Background(), &config.Tool{Name: "demo"}, ToolResult{
			Status: StatusInstalled, MethodKind: "probe-kind", PlanIntent: planVersion,
		}, existing, true, true)
		if got.Version != "2.0.0" {
			t.Fatalf("Version = %q, want resolved 2.0.0", got.Version)
		}
	})

	t.Run("nil plan preserves historical version", func(t *testing.T) {
		ex := New()
		WithAdapters(&testMockAdapter{kindValue: "probe-kind"})(ex)
		got := ex.toolStateForResult(context.Background(), &config.Tool{Name: "demo"}, ToolResult{
			Status: StatusInstalled, MethodKind: "probe-kind",
		}, existing, true, true)
		if got.Version != existing.Version {
			t.Fatalf("Version = %q, want historical %q", got.Version, existing.Version)
		}
	})

	t.Run("already does not invent resolved version", func(t *testing.T) {
		ex := New()
		WithAdapters(&testMockAdapter{kindValue: "probe-kind"})(ex)
		got := ex.toolStateForResult(context.Background(), &config.Tool{Name: "demo"}, ToolResult{
			Status: StatusAlready, MethodKind: "probe-kind", PlanIntent: planVersion,
		}, existing, true, true)
		if got.Version != existing.Version {
			t.Fatalf("Version = %q, want historical satisfied identity %q", got.Version, existing.Version)
		}
	})
}

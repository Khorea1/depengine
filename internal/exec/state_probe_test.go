package exec

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	deplog "github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/run"
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

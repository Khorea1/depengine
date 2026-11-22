package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

type tutorialInvocation struct {
	env  map[string]string
	args []string
}

func TestTutorialRunsReadOnlySteps(t *testing.T) {
	var invocations []tutorialInvocation
	out := captureStdout(t, func() {
		err := runTutorial(context.Background(), tutorialOptions{
			JSON: true,
			Run: func(_ context.Context, env map[string]string, args []string) run.Result {
				invocations = append(invocations, tutorialInvocation{env: env, args: args})
				return run.Result{Stdout: []byte("ok\n")}
			},
		})
		if err != nil {
			t.Fatalf("runTutorial: %v", err)
		}
	})

	envelope := decodeEnvelope(t, out)
	if envelope.Command != "tutorial" {
		t.Fatalf("envelope command = %q, want tutorial", envelope.Command)
	}
	var report tutorialReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatalf("decode tutorial report: %v", err)
	}
	if report.Directory != "<tutorial>" || report.Schema != "<tutorial>/schema.toml" {
		t.Errorf("report paths = %q / %q, want deterministic placeholders", report.Directory, report.Schema)
	}
	if len(report.Steps) != 5 {
		t.Fatalf("steps = %d, want 5", len(report.Steps))
	}
	for _, step := range report.Steps {
		if step.Status != "ok" {
			t.Errorf("step %s status = %q, want ok", step.Name, step.Status)
		}
	}

	wantCommands := []string{"validate", "why", "update", "install"}
	if len(invocations) != len(wantCommands) {
		t.Fatalf("ran %d commands, want %d: %+v", len(invocations), len(wantCommands), invocations)
	}
	for i, want := range wantCommands {
		if invocations[i].args[0] != want {
			t.Errorf("command %d = %q, want %q", i, invocations[i].args[0], want)
		}
		if invocations[i].env["XDG_STATE_HOME"] == "" {
			t.Errorf("%s did not isolate XDG_STATE_HOME", want)
		}
		if strings.Join(invocations[i].args, " ") == "" {
			t.Errorf("%s has empty argv", want)
		}
	}
	for _, arg := range invocations[3].args {
		if arg == "--dry-run" {
			return
		}
	}
	t.Error("install step is missing --dry-run")
}

func TestTutorialStopsOnFailedStepAndCleansUp(t *testing.T) {
	ran := 0
	err := runTutorial(context.Background(), tutorialOptions{
		JSON: true,
		Run: func(_ context.Context, _ map[string]string, args []string) run.Result {
			ran++
			if args[0] == "why" {
				return run.Result{Stderr: []byte("boom\n"), ExitCode: 1}
			}
			return run.Result{}
		},
	})
	if code := requireExitCode(t, err); code != 1 {
		t.Fatalf("failed step exit = %d, want 1", code)
	}
	if ran != 2 {
		t.Fatalf("ran %d commands, want to stop after validate+why", ran)
	}
}

func TestTutorialKeepLeavesDirectory(t *testing.T) {
	var report tutorialReport
	out := captureStdout(t, func() {
		err := runTutorial(context.Background(), tutorialOptions{
			JSON: true,
			Keep: true,
			Run: func(context.Context, map[string]string, []string) run.Result {
				return run.Result{}
			},
		})
		if err != nil {
			t.Fatalf("runTutorial: %v", err)
		}
	})
	envelope := decodeEnvelope(t, out)
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatalf("decode tutorial report: %v", err)
	}
	defer func() { _ = os.RemoveAll(report.Directory) }()
	if _, err := os.Stat(report.Directory); err != nil {
		t.Fatalf("--keep directory missing: %v", err)
	}
}

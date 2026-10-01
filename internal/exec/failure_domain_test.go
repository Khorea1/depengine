package exec

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/run"
)

func TestExecutorFailureDomainPreservesIndependentSibling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		maxJobs int
	}{
		{name: "jobs1", maxJobs: 1},
		{name: "jobs2", maxJobs: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())

			var mu sync.Mutex
			installCalls := make(map[string]int)
			adapter := &testMockAdapter{
				kindValue: "failure-domain",
				checkFunc: func(string) bool { return false },
				installFunc: func(name string) error {
					mu.Lock()
					installCalls[name]++
					mu.Unlock()
					if name == "root-fail" {
						return errors.New("controlled root failure")
					}
					return nil
				},
			}

			ex := New()
			WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
			WithAdapters(adapter)(ex)
			WithMaxJobs(tc.maxJobs)(ex)

			schema := &config.Schema{
				Defaults: config.Defaults{MethodOrder: []string{"failure-domain"}},
				Tools: map[string]*config.Tool{
					"root-fail": {
						Name:    "root-fail",
						Methods: []*config.MethodCandidate{{Kind: "failure-domain"}},
					},
					"child": {
						Name:     "child",
						Requires: []string{"root-fail"},
						Methods:  []*config.MethodCandidate{{Kind: "failure-domain"}},
					},
					"sibling-ok": {
						Name:    "sibling-ok",
						Methods: []*config.MethodCandidate{{Kind: "failure-domain"}},
					},
				},
			}

			report, err := ex.Execute(context.Background(), schema, "")
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			results := make(map[string]ToolResult, len(report.Tools))
			for _, result := range report.Tools {
				results[result.Tool] = result
			}
			if len(results) != 3 {
				t.Fatalf("tool results = %#v, want root-fail, child, and sibling-ok", report.Tools)
			}
			if got := results["root-fail"]; got.Status != StatusFailed || !strings.Contains(got.Error, "controlled root failure") {
				t.Fatalf("root-fail result = %+v, want controlled failure", got)
			}
			if got := results["child"]; got.Status != StatusFailed || !strings.Contains(got.Error, "requires failed dependency: root-fail") {
				t.Fatalf("child result = %+v, want failed dependency", got)
			}
			if got := results["sibling-ok"]; got.Status != StatusInstalled {
				t.Fatalf("sibling-ok result = %+v, want installed", got)
			}
			if report.Failed != 2 {
				t.Fatalf("report failed = %d, want root-fail and child only", report.Failed)
			}

			mu.Lock()
			defer mu.Unlock()
			if installCalls["root-fail"] != 1 || installCalls["sibling-ok"] != 1 {
				t.Fatalf("install calls = %#v, want one root-fail and sibling-ok install", installCalls)
			}
			if installCalls["child"] != 0 {
				t.Fatalf("child install calls = %d, want none", installCalls["child"])
			}
		})
	}
}

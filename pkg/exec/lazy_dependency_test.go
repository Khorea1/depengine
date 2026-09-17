package exec

import (
	"context"
	"sync"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/run"
)

type countingAdapter struct {
	mu     sync.Mutex
	checks map[string]int
}

func (a *countingAdapter) Kind() string                               { return "counting" }
func (a *countingAdapter) Available(context.Context, run.Runner) bool { return true }
func (a *countingAdapter) Check(_ context.Context, _ run.Runner, tool *config.Tool, _ *config.MethodCandidate) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.checks[tool.Name]++
	return false
}
func (a *countingAdapter) Install(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}

func TestLazyDependencyRunsOnceUnderConcurrency(t *testing.T) {
	adapter := &countingAdapter{checks: map[string]int{}}
	method := func(requires ...string) []*config.MethodCandidate {
		return []*config.MethodCandidate{{Kind: "counting", Config: map[string]any{"pkg": "x"}, Requires: requires}}
	}
	schema := &config.Schema{Defaults: config.Defaults{MethodOrder: []string{"counting"}}, Tools: map[string]*config.Tool{
		"helper": {Name: "helper", DependencyOnly: true, Methods: method()},
		"a":      {Name: "a", Methods: method("helper")},
		"b":      {Name: "b", Methods: method("helper")},
	}}
	executor := New()
	WithAdapters(adapter)(executor)
	WithDryRun()(executor)
	WithMaxJobs(2)(executor)
	if _, err := executor.Execute(context.Background(), schema, ""); err != nil {
		t.Fatal(err)
	}
	if got := adapter.checks["helper"]; got != 1 {
		t.Fatalf("helper checks=%d want=1", got)
	}
}

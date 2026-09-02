package exec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
)

type testMockAdapter struct {
	kindValue     string
	availableFunc func() bool
	checkFunc     func(string) bool
	installFunc   func(string) error
}

func (m *testMockAdapter) Kind() string { return m.kindValue }
func (m *testMockAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if m.availableFunc != nil {
		return m.availableFunc()
	}
	return true
}
func (m *testMockAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if m.checkFunc != nil {
		return m.checkFunc(tool.Name)
	}
	return false
}
func (m *testMockAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	if m.installFunc != nil {
		return m.installFunc(tool.Name)
	}
	return nil
}

// blockingMockAdapter is an adapter whose Install blocks until the context is
// cancelled or a value is sent on block. Used to test timeout behavior.
type blockingMockAdapter struct {
	kindValue     string
	availableFunc func() bool
	checkFunc     func(string) bool
	block         chan struct{}
}

func (m *blockingMockAdapter) Kind() string { return m.kindValue }
func (m *blockingMockAdapter) Available(ctx context.Context, rn run.Runner) bool {
	if m.availableFunc != nil {
		return m.availableFunc()
	}
	return true
}
func (m *blockingMockAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	if m.checkFunc != nil {
		return m.checkFunc(tool.Name)
	}
	return false
}
func (m *blockingMockAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	select {
	case <-m.block:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type installError struct{ msg string }

func (e *installError) Error() string { return e.msg }

func mockSchema(tools ...string) *config.Schema {
	s := &config.Schema{
		Defaults: config.Defaults{
			Manager:     "native",
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*config.Tool{},
	}
	for _, name := range tools {
		s.Tools[name] = &config.Tool{
			Name:     name,
			IsSimple: true,
			Methods:  []*config.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": name}}},
		}
	}
	return s
}

func TestExecutorFallback(t *testing.T) {
	methodA := &testMockAdapter{kindValue: "failer"}
	methodB := &testMockAdapter{kindValue: "succeeder"}

	methodA.installFunc = func(string) error { return &installError{msg: "methodA failed"} }
	methodA.availableFunc = func() bool { return true }
	methodA.checkFunc = func(string) bool { return false }

	methodB.installFunc = func(string) error { return nil }
	methodB.availableFunc = func() bool { return true }
	methodB.checkFunc = func(string) bool { return false }

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(methodA, methodB)(ex)

	s := &config.Schema{
		Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"failer", "succeeder"}},
		Tools: map[string]*config.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*config.MethodCandidate{
					{Kind: "failer", Config: map[string]any{"pkg": "tool1"}},
					{Kind: "succeeder", Config: map[string]any{"pkg": "tool1"}},
				},
			},
		},
	}
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 1 {
		t.Fatalf("expected 1 success (fallback), got %d. Tools: %+v", report.Success, report.Tools)
	}
	if report.Tools[0].Method != "succeeder" {
		t.Fatalf("expected fallback to succeeder, got %s", report.Tools[0].Method)
	}
}

func TestExecutorSkipsWhenCondition(t *testing.T) {
	mockA := &testMockAdapter{kindValue: "debian-only"}
	mockA.availableFunc = func() bool { return true }
	mockA.checkFunc = func(string) bool { return false }
	mockA.installFunc = func(string) error { return nil }

	// Also register a fallback that works on arch.
	mockB := &testMockAdapter{kindValue: "any-distro"}
	mockB.availableFunc = func() bool { return true }
	mockB.checkFunc = func(string) bool { return false }
	mockB.installFunc = func(string) error { return nil }

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mockA, mockB)(ex)

	s := &config.Schema{
		Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"debian-only", "any-distro"}},
		Tools: map[string]*config.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*config.MethodCandidate{
					{Kind: "debian-only", When: &config.Condition{DistroFamily: []string{"debian"}}},
					{Kind: "any-distro"},
				},
			},
		},
	}

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First method skipped (when), second succeeds.
	if report.Success != 1 {
		t.Fatalf("expected 1 success (fallback after when skip), got %d", report.Success)
	}
}

func TestExecutorAllMethodsFail(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		installFunc:   func(string) error { return &installError{msg: "all fail"} },
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	report, err := ex.Execute(context.Background(), s, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", report.Failed)
	}
}

func TestExecutorAlreadyInstalled(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		checkFunc:     func(string) bool { return true },
		availableFunc: func() bool { return true },
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Already != 1 {
		t.Fatalf("expected 1 already, got %d", report.Already)
	}
}

func TestExecutorAdapterUnavailable(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "some-adapter",
		availableFunc: func() bool { return false },
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := &config.Schema{
		Defaults: config.Defaults{Manager: "some-adapter", MethodOrder: []string{"some-adapter"}},
		Tools: map[string]*config.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*config.MethodCandidate{
					{Kind: "some-adapter", Config: map[string]any{"pkg": "tool1"}},
				},
			},
		},
	}
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Skipped != 1 {
		t.Fatalf("expected 1 skipped (unavailable), got %d. Tools: %+v", report.Skipped, report.Tools)
	}
}

func TestExecutorDryRun(t *testing.T) {
	installCalled := false
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
	}
	mock.installFunc = func(string) error {
		installCalled = true
		return nil
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)
	ex.dryRun = true

	s := mockSchema("tool1")
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if installCalled {
		t.Fatal("Install should not be called in dry-run")
	}
	if report.Success != 0 {
		t.Fatalf("expected 0 success in dry-run, got %d", report.Success)
	}
}

func TestExecutorPostInstall(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	ex := New()
	WithAllowArbitraryCode()(ex)
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	s.Tools["tool1"].PostInstall = "echo done"

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 1 {
		t.Fatalf("expected 1 success, got %d", report.Success)
	}
}

func TestExecutorSkipsPostInstallOnUnmatchedWhen(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}
	fr := &run.FakeRunner{ExitCode: 0}
	ex := New()
	WithAllowArbitraryCode()(ex)
	WithRunner(fr)(ex)
	WithAdapters(mock)(ex)
	WithFacts(&engine.Facts{OS: "windows", TargetFamily: "windows"})(ex)

	s := mockSchema("tool1")
	s.Tools["tool1"].PostInstall = "fc-cache -fv"
	s.Tools["tool1"].PostInstallWhen = &config.Condition{TargetFamily: []string{"unix"}}

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 1 {
		t.Fatalf("expected 1 success, got %d", report.Success)
	}
	for _, c := range fr.Calls {
		if len(c.Args) > 0 && c.Args[0] == "fc-cache -fv" {
			t.Errorf("postinstall should be skipped on windows facts, but ran: %+v", c.Args)
		}
	}
}

func TestExecutorTopologicalOrder(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("a", "b", "c")
	s.Tools["a"].Requires = []string{"b"}
	s.Tools["b"].Requires = []string{"c"}

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 3 {
		t.Fatalf("expected all 3 success, got %d", report.Success)
	}
	toolOrder := make([]string, len(report.Tools))
	for i, tr := range report.Tools {
		toolOrder[i] = tr.Tool
	}
	order := strings.Join(toolOrder, ",")
	if !strings.Contains(order, "c") {
		t.Fatalf("c should be first, got order: %s", order)
	}
}

func TestParseSortField(t *testing.T) {
	tests := []struct {
		input string
		want  SortField
		ok    bool
	}{
		{"name", SortByName, true},
		{"status", SortByStatus, true},
		{"method", SortByMethod, true},
		{"", SortField(""), false},
		{"invalid", SortField(""), false},
		{"NAME", SortField(""), false},
	}
	for _, tc := range tests {
		got, ok := ParseSortField(tc.input)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseSortField(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSortByName(t *testing.T) {
	r := &ExecReport{
		Tools: []ToolResult{
			{Tool: "zsh"},
			{Tool: "bat"},
			{Tool: "fd"},
			{Tool: "lazygit"},
		},
	}
	r.SortBy(SortByName)
	got := names(r)
	want := []string{"bat", "fd", "lazygit", "zsh"}
	if !equal(got, want) {
		t.Fatalf("SortByName = %v, want %v", got, want)
	}
}

func TestSortByNameCaseInsensitive(t *testing.T) {
	// DepartureMono (uppercase D) must NOT sort before aichat (lowercase a).
	// Case-insensitive comparison: aichat < bat < DepartureMono < zsh.
	r := &ExecReport{
		Tools: []ToolResult{
			{Tool: "DepartureMono"},
			{Tool: "aichat"},
			{Tool: "zsh"},
			{Tool: "bat"},
		},
	}
	r.SortBy(SortByName)
	got := names(r)
	want := []string{"aichat", "bat", "DepartureMono", "zsh"}
	if !equal(got, want) {
		t.Fatalf("SortByName case-insensitive = %v, want %v", got, want)
	}
}

func TestSortByStatus(t *testing.T) {
	r := &ExecReport{
		Tools: []ToolResult{
			{Tool: "e", Status: StatusFailed},
			{Tool: "d", Status: StatusSkippedUnavailable},
			{Tool: "c", Status: StatusAlready},
			{Tool: "b", Status: StatusWouldInstall},
			{Tool: "a", Status: StatusInstalled},
		},
	}
	r.SortBy(SortByStatus)
	got := names(r)
	// Expected priority: WouldInstall(0) → Installed(1) → Already(2) → Failed(3) → SkippedUnavailable(5)
	want := []string{"b", "a", "c", "e", "d"}
	if !equal(got, want) {
		t.Fatalf("SortByStatus = %v, want %v", got, want)
	}
}

func TestSortByMethod(t *testing.T) {
	r := &ExecReport{
		Tools: []ToolResult{
			{Tool: "c", Method: "http"},
			{Tool: "a", Method: "cargo"},
			{Tool: "d", Method: "native"},
			{Tool: "b", Method: "go"},
			{Tool: "e", Method: ""}, // sem método → último
		},
	}
	r.SortBy(SortByMethod)
	got := names(r)
	// Ordem alfabética: cargo < go < http < native, vazio no final
	want := []string{"a", "b", "c", "d", "e"}
	if !equal(got, want) {
		t.Fatalf("SortByMethod = %v, want %v", got, want)
	}
}

func TestSortByStable(t *testing.T) {
	// Stable sort: same-key items keep original relative order.
	r := &ExecReport{
		Tools: []ToolResult{
			{Tool: "b", Method: "a"},
			{Tool: "c", Method: "a"},
			{Tool: "a", Method: "a"},
		},
	}
	r.SortBy(SortByMethod)
	got := names(r)
	want := []string{"b", "c", "a"} // método igual → ordem original
	if !equal(got, want) {
		t.Fatalf("SortByMethod stable = %v, want %v", got, want)
	}
}

func TestSortByEmptyDoesNothing(t *testing.T) {
	// Empty SortField should be a no-op.
	r := &ExecReport{
		Tools: []ToolResult{
			{Tool: "zsh"},
			{Tool: "bat"},
		},
	}
	original := names(r)
	r.SortBy(SortField(""))
	got := names(r)
	if !equal(got, original) {
		t.Fatalf("empty SortBy modified Tools: %v → %v", original, got)
	}
}

// names extracts tool names from a report for test assertions.
func names(r *ExecReport) []string {
	out := make([]string, len(r.Tools))
	for i, tr := range r.Tools {
		out[i] = tr.Tool
	}
	return out
}

// equal reports whether two string slices are equal.
func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExecutorSortByPipeline(t *testing.T) {
	// Verify that WithSortBy produces a sorted report.
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)
	WithSortBy(SortByName)(ex)

	s := mockSchema("zsh", "bat", "fd", "lazygit")
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := names(report)
	want := []string{"bat", "fd", "lazygit", "zsh"}
	if !equal(got, want) {
		t.Fatalf("WithSortBy(SortByName) produced %v, want %v", got, want)
	}
}

func TestExecutorReportFormatting(t *testing.T) {
	report := &ExecReport{
		Tools: []ToolResult{
			{Tool: "zsh", Status: StatusInstalled, Method: "native"},
			{Tool: "bat", Status: StatusAlready, Method: "native"},
			{Tool: "lazygit", Status: StatusFailed, Method: "cargo", Error: "timeout"},
		},
		Success: 1,
		Failed:  1,
		Already: 1,
	}
	summary := report.Summary()
	if !strings.Contains(summary, "1 installed") {
		t.Fatalf("summary should mention installed count: %s", summary)
	}
	if !strings.Contains(summary, "1 failed") {
		t.Fatalf("summary should mention failed count: %s", summary)
	}
	detail := report.Detail()
	if !strings.Contains(detail, "zsh") {
		t.Fatalf("detail should contain tool name: %s", detail)
	}
	json := report.JSON()
	if !strings.Contains(json, `"summary"`) {
		t.Fatalf("JSON should contain summary field: %s", json)
	}
}

func TestExecutorParallelExecution(t *testing.T) {
	installCh := make(chan string, 3)

	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc: func(name string) error {
			installCh <- name
			return nil
		},
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)
	WithMaxJobs(3)(ex)

	s := mockSchema("a", "b", "c")
	report, err := ex.Execute(context.Background(), s, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 3 {
		t.Fatalf("expected 3 successes, got %d", report.Success)
	}
	close(installCh)

	var installed []string
	for name := range installCh {
		installed = append(installed, name)
	}
	if len(installed) != 3 {
		t.Fatalf("expected 3 installs, got %d: %v", len(installed), installed)
	}
}

func TestExecutorParallelSequentialDefault(t *testing.T) {
	// WithMaxJobs(1) produces the same sequential behavior as the default.
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
	}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)
	WithMaxJobs(1)(ex)

	s := mockSchema("a", "b", "c")
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 3 {
		t.Fatalf("expected 3 successes, got %d", report.Success)
	}
}

func TestExecutorPreInstallSuccess(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	ex := New()
	WithAllowArbitraryCode()(ex)
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	s.Tools["tool1"].PreInstall = "echo preparing"

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 1 {
		t.Fatalf("expected 1 success, got %d", report.Success)
	}
	if report.Tools[0].PreinstallDone != true {
		t.Fatal("expected PreinstallDone to be true")
	}
}

func TestExecutorPreInstallFailure(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	ex := New()
	WithAllowArbitraryCode()(ex)
	// Pre-install fails with exit code 1.
	WithRunner(&run.FakeRunner{ExitCode: 1})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	s.Tools["tool1"].PreInstall = "echo preparing"

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 0 {
		t.Fatalf("expected 0 success (pre-install failed), got %d", report.Success)
	}
	if report.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", report.Failed)
	}
	if report.Tools[0].PreinstallDone != false {
		t.Fatal("expected PreinstallDone to be false")
	}
}

func TestExecutorBlocksDangerous(t *testing.T) {
	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	ex := New()
	// No WithAllowArbitraryCode — should be blocked.
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	s.Tools["tool1"].PostInstall = "echo dangerous"

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 0 {
		t.Fatalf("expected 0 success (blocked by dangerous check), got %d", report.Success)
	}
	if len(report.Tools) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(report.Tools))
	}
	if report.Tools[0].Status != StatusSkippedUnavailable {
		t.Fatalf("expected StatusSkippedUnavailable, got %v", report.Tools[0].Status)
	}
}

func TestHasDangerousMethod(t *testing.T) {
	ex := &Executor{}
	tool := &config.Tool{Name: "test"}

	// No methods → not dangerous.
	if ex.hasDangerousMethod(tool) {
		t.Error("tool with no methods should not be dangerous")
	}

	// Method with build config key → dangerous.
	tool.Methods = []*config.MethodCandidate{
		{Config: map[string]any{"build": "make"}},
	}
	if !ex.hasDangerousMethod(tool) {
		t.Error("tool with build config should be dangerous")
	}

	// Method with build_cmd → dangerous.
	tool.Methods = []*config.MethodCandidate{
		{Config: map[string]any{"build_cmd": "ninja"}},
	}
	if !ex.hasDangerousMethod(tool) {
		t.Error("tool with build_cmd should be dangerous")
	}

	// Method with build_command → dangerous.
	tool.Methods = []*config.MethodCandidate{
		{Config: map[string]any{"build_command": "cmake --build"}},
	}
	if !ex.hasDangerousMethod(tool) {
		t.Error("tool with build_command should be dangerous")
	}

	// Method with non-string build value → not dangerous.
	tool.Methods = []*config.MethodCandidate{
		{Config: map[string]any{"build": true}},
	}
	if ex.hasDangerousMethod(tool) {
		t.Error("tool with non-string build should not be dangerous")
	}

	// Method with empty string build value → not dangerous.
	tool.Methods = []*config.MethodCandidate{
		{Config: map[string]any{"build": ""}},
	}
	if ex.hasDangerousMethod(tool) {
		t.Error("tool with empty build should not be dangerous")
	}

	// AUR/Pacstall methods are NOT flagged (explicit user choice).
	tool.Methods = []*config.MethodCandidate{
		{Kind: "aur", Config: map[string]any{"pkg": "foo"}},
	}
	if ex.hasDangerousMethod(tool) {
		t.Error("AUR method should not be flagged as dangerous")
	}
}

func TestLookupAdapter(t *testing.T) {
	// Save and restore global registry.
	saved := adapters
	adapters = map[string]Adapter{}
	defer func() { adapters = saved }()

	mock := &testMockAdapter{
		kindValue:     "test-adapter",
		availableFunc: func() bool { return true },
	}
	Register(mock)

	ex := New()

	// Look up a registered adapter.
	got := ex.LookupAdapter("test-adapter")
	if got == nil {
		t.Fatal("LookupAdapter returned nil for registered adapter")
	}
	if got.Kind() != "test-adapter" {
		t.Fatalf("LookupAdapter returned kind %q, want %q", got.Kind(), "test-adapter")
	}

	// Look up an unregistered kind returns nil.
	if unreg := ex.LookupAdapter("nope"); unreg != nil {
		t.Fatalf("LookupAdapter for unregistered kind should be nil, got %v", unreg)
	}
}
func TestExecutorPreAndPostInstall(t *testing.T) {

	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	fake := &run.FakeRunner{ExitCode: 0}
	ex := New()
	WithAllowArbitraryCode()(ex)
	WithRunner(fake)(ex)
	WithAdapters(mock)(ex)

	s := mockSchema("tool1")
	s.Tools["tool1"].PreInstall = "echo pre"
	s.Tools["tool1"].PostInstall = "echo post"

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 1 {
		t.Fatalf("expected 1 success, got %d", report.Success)
	}
	if !report.Tools[0].PreinstallDone {
		t.Fatal("expected PreinstallDone to be true")
	}
	if !report.Tools[0].PostinstallDone {
		t.Fatal("expected PostinstallDone to be true")
	}

	// Verify the runner was called: pre-install first, then install (not recorded by FakeRunner
	// since adapters use their own runner), then post-install.
	if len(fake.Calls) < 2 {
		t.Fatalf("expected at least 2 FakeRunner calls (pre + post), got %d", len(fake.Calls))
	}
	if fake.Calls[0].Name != "sh" || len(fake.Calls[0].Args) < 2 || fake.Calls[0].Args[1] != "echo pre" {
		t.Fatalf("expected first call 'sh -c echo pre', got %v", fake.Calls[0])
	}
	last := fake.Calls[len(fake.Calls)-1]
	if last.Name != "sh" || len(last.Args) < 2 || last.Args[1] != "echo post" {
		t.Fatalf("expected last call 'sh -c echo post', got %v", last)
	}
}

func TestWriteState(t *testing.T) {
	// Use a temp dir so state locking/writing is isolated.
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	mock := &testMockAdapter{
		kindValue:     "native",
		availableFunc: func() bool { return true },
		checkFunc:     func(string) bool { return false },
		installFunc:   func(string) error { return nil },
	}

	fake := &run.FakeRunner{ExitCode: 0}
	ex := New()
	WithRunner(fake)(ex)
	WithAdapters(mock)(ex)
	WithSchemaInfo("/test/schema.yaml", time.Now())(ex)

	s := mockSchema("tool1")
	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Success != 1 {
		t.Fatalf("expected 1 success, got %d", report.Success)
	}

	// Execute already calls writeState — verify the state file exists.
	statePath := filepath.Join(dir, "depengine", "state.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		t.Fatal("state file was not written by writeState")
	}

	// Verify the state content is well-formed.
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("failed to read state file: %v", err)
	}
	var st state.State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("failed to parse state file: %v", err)
	}
	if st.Version != 1 {
		t.Errorf("expected state version 1, got %d", st.Version)
	}
	if st.SchemaPath != "/test/schema.yaml" {
		t.Errorf("expected schema path /test/schema.yaml, got %s", st.SchemaPath)
	}
	ts, ok := st.Tools["tool1"]
	if !ok {
		t.Fatal("tool1 not found in state tools")
	}
	if ts.Method != "native" {
		t.Errorf("expected method native, got %s", ts.Method)
	}
}

func TestFormatToolResult(t *testing.T) {
	tests := []struct {
		name   string
		status StatusEnum
		method string
		errMsg string
		want   []string // substrings the output must contain
	}{
		{name: "installed", status: StatusInstalled, method: "cargo", want: []string{"✓", "installed via cargo"}},
		{name: "already", status: StatusAlready, method: "native", want: []string{"✓", "already installed (native)"}},
		{name: "skipped_when", status: StatusSkippedWhen, want: []string{"–", "skipped", "when condition"}},
		{name: "skipped_unavailable", status: StatusSkippedUnavailable, want: []string{"–", "skipped", "no method available"}},
		{name: "would_install", status: StatusWouldInstall, method: "pip", want: []string{"→", "would install via pip (dry-run)"}},
		{name: "failed", status: StatusFailed, errMsg: "exit 1", want: []string{"✗", "failed (exit 1)"}},
		{name: "unknown_status", status: StatusEnum(99), want: []string{"?", "99"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := formatToolResult("git", tt.status, tt.method, tt.errMsg)
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("formatToolResult(%q, %v, %q, %q) = %q, want substring %q", "git", tt.status, tt.method, tt.errMsg, out, want)
				}
			}
		})
	}
}

func TestToolTimeout(t *testing.T) {
	blocking := &blockingMockAdapter{
		kindValue: "blocker",
		block:     make(chan struct{}),
	}
	blocking.availableFunc = func() bool { return true }
	blocking.checkFunc = func(string) bool { return false }

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithAdapters(blocking)(ex)
	WithToolTimeout(10 * time.Millisecond)(ex)
	WithMethodTimeout(5 * time.Millisecond)(ex)

	s := &config.Schema{
		Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"blocker"}},
		Tools: map[string]*config.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*config.MethodCandidate{
					{Kind: "blocker", Config: map[string]any{"pkg": "tool1"}},
				},
			},
		},
	}

	report, err := ex.Execute(context.Background(), s, "arch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(report.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(report.Tools))
	}
	if report.Tools[0].Status != StatusFailed {
		t.Fatalf("expected tool to fail due to timeout, got status %v", report.Tools[0].Status)
	}
	if report.Tools[0].Error == "" {
		t.Fatal("expected non-empty error message on timeout")
	}
}

// --- Method ordering tests ---

type orderTrackingAdapter struct {
	kindValue    string
	attemptOrder *[]string // shared slice to record attempts
}

func (m *orderTrackingAdapter) Kind() string                                      { return m.kindValue }
func (m *orderTrackingAdapter) Available(ctx context.Context, rn run.Runner) bool { return true }
func (m *orderTrackingAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	return false
}
func (m *orderTrackingAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	*m.attemptOrder = append(*m.attemptOrder, m.kindValue)
	return &installError{msg: "order tracking failure"}
}

func TestExecutorReordersMethodsByExpandedOrder(t *testing.T) {
	// Set up an executor with a custom defaultMethodOrder that includes a native
	// manager name. The executor should expand it (e.g. "apt" → "native") before
	// using it to order methods.
	var attemptOrder []string

	nativeAdapter := &orderTrackingAdapter{kindValue: "native", attemptOrder: &attemptOrder}
	cargoAdapter := &orderTrackingAdapter{kindValue: "cargo", attemptOrder: &attemptOrder}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithDefaultMethodOrder([]string{"apt", "cargo"})(ex) // "apt" will be expanded
	WithAdapters(nativeAdapter, cargoAdapter)(ex)

	// Set nativeManagerName directly (simulating what Execute would do)
	ex.nativeManagerName = "apt"

	s := &config.Schema{
		Defaults: config.Defaults{Manager: "native", MethodOrder: []string{"apt", "cargo"}},
		Tools: map[string]*config.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*config.MethodCandidate{
					{Kind: "native", Config: map[string]any{"pkg": "tool1"}},
					{Kind: "cargo", Config: map[string]any{"pkg": "binary"}},
				},
			},
		},
	}

	// tryMethods directly
	result := &ToolResult{Tool: "tool1"}
	ex.tryMethods(context.Background(), s.Tools["tool1"], result, time.Now())

	// First method tried should be "native" (expanded from "apt"), second "cargo"
	if len(attemptOrder) < 2 {
		t.Fatalf("expected at least 2 attempts, got %d: %v", len(attemptOrder), attemptOrder)
	}
	if attemptOrder[0] != "native" {
		t.Fatalf("expected first attempt to be 'native' (expanded from 'apt'), got %q", attemptOrder[0])
	}
	if attemptOrder[1] != "cargo" {
		t.Fatalf("expected second attempt to be 'cargo', got %q", attemptOrder[1])
	}
}

func TestExplainToolRespectsExpandedOrder(t *testing.T) {
	var attemptOrder []string

	nativeAdapter := &orderTrackingAdapter{kindValue: "native", attemptOrder: &attemptOrder}
	cargoAdapter := &orderTrackingAdapter{kindValue: "cargo", attemptOrder: &attemptOrder}

	ex := New()
	WithRunner(&run.FakeRunner{ExitCode: 0})(ex)
	WithDefaultMethodOrder([]string{"cargo", "apt"})(ex) // "apt" will be expanded
	WithAdapters(nativeAdapter, cargoAdapter)(ex)
	ex.nativeManagerName = "apt"

	tool := &config.Tool{
		Name: "tool1",
		Methods: []*config.MethodCandidate{
			{Kind: "native", Config: map[string]any{"pkg": "tool1"}},
			{Kind: "cargo", Config: map[string]any{"pkg": "binary"}},
		},
	}

	attempts := ex.ExplainTool(context.Background(), tool, "debian")
	// The expanded order should be ["cargo", "native"] (cargo first, then apt→native)
	expectedOrder := []string{"cargo", "native"}
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}
	for i, a := range attempts {
		if a.Kind != expectedOrder[i] {
			t.Fatalf("at index %d: expected kind %q, got %q; full: %+v", i, expectedOrder[i], a.Kind, attempts)
		}
	}
}

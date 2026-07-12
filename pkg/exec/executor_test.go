package exec

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"depengine/pkg/run"
	"depengine/pkg/schema"
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
func (m *testMockAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	if m.checkFunc != nil {
		return m.checkFunc(tool.Name)
	}
	return false
}
func (m *testMockAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	if m.installFunc != nil {
		return m.installFunc(tool.Name)
	}
	return nil
}

type installError struct{ msg string }

func (e *installError) Error() string { return e.msg }

func mockSchema(tools ...string) *schema.Schema {
	s := &schema.Schema{
		Defaults: schema.Defaults{
			Manager:     "native",
			MethodOrder: []string{"native"},
		},
		Tools: map[string]*schema.Tool{},
	}
	for _, name := range tools {
		s.Tools[name] = &schema.Tool{
			Name:     name,
			IsSimple: true,
			Methods:  []*schema.MethodCandidate{{Kind: "native", Config: map[string]any{"pkg": name}}},
		}
	}
	return s
}

func captureLogs() (Option, *bytes.Buffer) {
	var buf bytes.Buffer
	return WithOutput(&buf), &buf
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

	s := &schema.Schema{
		Defaults: schema.Defaults{Manager: "native", MethodOrder: []string{"failer", "succeeder"}},
		Tools: map[string]*schema.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*schema.MethodCandidate{
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

	s := &schema.Schema{
		Defaults: schema.Defaults{Manager: "native", MethodOrder: []string{"debian-only", "any-distro"}},
		Tools: map[string]*schema.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*schema.MethodCandidate{
					{Kind: "debian-only", When: &schema.Condition{DistroFamily: []string{"debian"}}},
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
	report, err := ex.Execute(context.Background(), s, "arch")
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

	s := &schema.Schema{
		Defaults: schema.Defaults{Manager: "some-adapter", MethodOrder: []string{"some-adapter"}},
		Tools: map[string]*schema.Tool{
			"tool1": {
				Name: "tool1",
				Methods: []*schema.MethodCandidate{
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
		kindValue: "native",
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
	report, err := ex.Execute(context.Background(), s, "arch")
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
		kindValue: "native",
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

func TestExecutorPreAndPostInstall(t *testing.T) {
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
	if fake.Calls[0].Name != "echo" || fake.Calls[0].Args[0] != "pre" {
		t.Fatalf("expected first call 'echo pre', got %v", fake.Calls[0])
	}
	last := fake.Calls[len(fake.Calls)-1]
	if last.Name != "echo" || last.Args[0] != "post" {
		t.Fatalf("expected last call 'echo post', got %v", last)
	}
}

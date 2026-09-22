package main

import (
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/state"
)

func TestNormalizeStatusFormat_JSONShorthand(t *testing.T) {
	format := "text"
	isJSON := true
	normalizeStatusFormat(&format, &isJSON)
	if format != "json" {
		t.Fatalf("format = %q, want %q", format, "json")
	}
}

func TestNormalizeStatusFormat_ExplicitFormatKept(t *testing.T) {
	format := "json"
	isJSON := true
	normalizeStatusFormat(&format, &isJSON)
	if format != "json" {
		t.Fatalf("format = %q, want %q", format, "json")
	}
}

func TestNormalizeStatusFormat_NoJSONFlag(t *testing.T) {
	format := "text"
	isJSON := false
	normalizeStatusFormat(&format, &isJSON)
	if format != "text" {
		t.Fatalf("format = %q, want %q", format, "text")
	}
}

func TestResolveStatusSchemaPath_FlagOverride(t *testing.T) {
	st := &state.State{SchemaPath: "state.toml"}
	flag := "flag.toml"
	path, stop := resolveStatusSchemaPath(st, &flag)
	if stop || path != "flag.toml" {
		t.Fatalf("path = %q stop = %v, want %q false", path, stop, "flag.toml")
	}
}

func TestResolveStatusSchemaPath_StatePath(t *testing.T) {
	st := &state.State{SchemaPath: "state.toml"}
	flag := ""
	path, stop := resolveStatusSchemaPath(st, &flag)
	if stop || path != "state.toml" {
		t.Fatalf("path = %q stop = %v, want %q false", path, stop, "state.toml")
	}
}

func TestResolveStatusSchemaPath_EmptyStops(t *testing.T) {
	st := &state.State{}
	flag := ""
	path, stop := resolveStatusSchemaPath(st, &flag)
	if !stop || path != "" {
		t.Fatalf("path = %q stop = %v, want %q true", path, stop, "")
	}
}

func TestResolveStatusSchemaPath_EmptyPathWithToolsContinues(t *testing.T) {
	st := &state.State{Tools: map[string]state.ToolState{"a": {}}}
	flag := ""
	path, stop := resolveStatusSchemaPath(st, &flag)
	if stop || path != "" {
		t.Fatalf("path = %q stop = %v, want %q false", path, stop, "")
	}
}

func TestStatusToolOutdated_DefinitionDrift(t *testing.T) {
	tool := &config.Tool{Name: "foo"}
	ts := state.ToolState{DefinitionHash: "stale-hash"}
	if !statusToolOutdated(ts, tool, nil, "foo") {
		t.Fatal("statusToolOutdated = false, want true for definition drift")
	}
}

func TestStatusToolOutdated_Fresh(t *testing.T) {
	tool := &config.Tool{Name: "foo"}
	ts := state.ToolState{DefinitionHash: state.DefinitionHash(tool)}
	if statusToolOutdated(ts, tool, nil, "foo") {
		t.Fatal("statusToolOutdated = true, want false for fresh install")
	}
}

func TestStatusToolOutdated_VersionDrift(t *testing.T) {
	candidate := &config.MethodCandidate{Kind: "go"}
	tool := &config.Tool{Name: "foo", Methods: []*config.MethodCandidate{candidate}}
	ts := state.ToolState{
		Method:         "go",
		Version:        "1.0.0",
		DefinitionHash: state.DefinitionHash(tool),
	}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{"foo/go/0": {Latest: "2.0.0"}}}
	if !statusToolOutdated(ts, tool, lk, "foo") {
		t.Fatal("statusToolOutdated = false, want true for version drift")
	}
}

func TestStatusToolOutdated_VersionMatchesPin(t *testing.T) {
	candidate := &config.MethodCandidate{Kind: "go"}
	tool := &config.Tool{Name: "foo", Methods: []*config.MethodCandidate{candidate}}
	ts := state.ToolState{
		Method:         "go",
		Version:        "2.0.0",
		DefinitionHash: state.DefinitionHash(tool),
	}
	lk := &lock.Lock{Tools: map[string]lock.ToolPin{"foo/go/0": {Latest: "2.0.0"}}}
	if statusToolOutdated(ts, tool, lk, "foo") {
		t.Fatal("statusToolOutdated = true, want false when version matches pin")
	}
}

func statusTestSchema(tools ...string) *config.Schema {
	s := &config.Schema{Tools: map[string]*config.Tool{}}
	for _, name := range tools {
		s.Tools[name] = &config.Tool{Name: name}
	}
	return s
}

func statusOf(tools []toolStatus, name string) (toolStatus, bool) {
	for _, ts := range tools {
		if ts.Name == name {
			return ts, true
		}
	}
	return toolStatus{}, false
}

func TestClassifyStatusTools_AllStatuses(t *testing.T) {
	a := &config.Tool{Name: "a"}
	s := statusTestSchema("a", "c")
	installed := map[string]state.ToolState{
		"a": {Method: "native", DefinitionHash: state.DefinitionHash(a)},
		"b": {Method: "cargo"},
	}
	got := classifyStatusTools(installed, s, nil, false)
	if ts, ok := statusOf(got, "a"); !ok || ts.Status != "installed" {
		t.Errorf("a = %+v, want installed", ts)
	}
	if ts, ok := statusOf(got, "b"); !ok || ts.Status != "orphaned" {
		t.Errorf("b = %+v, want orphaned", ts)
	}
	if ts, ok := statusOf(got, "c"); !ok || ts.Status != "missing" {
		t.Errorf("c = %+v, want missing", ts)
	}
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestClassifyStatusTools_OrphansOnly(t *testing.T) {
	s := statusTestSchema("a")
	installed := map[string]state.ToolState{
		"a": {Method: "native"},
		"b": {Method: "cargo"},
	}
	got := classifyStatusTools(installed, s, nil, true)
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "b" || got[0].Status != "orphaned" {
		t.Fatalf("got = %+v, want orphaned b", got[0])
	}
}

func TestClassifyStatusTools_NilSchema(t *testing.T) {
	installed := map[string]state.ToolState{
		"a": {Method: "native"},
	}
	got := classifyStatusTools(installed, nil, nil, false)
	if len(got) != 1 || got[0].Status != "installed" {
		t.Fatalf("got = %+v, want single installed tool", got)
	}
}

func TestClassifyStatusTools_Outdated(t *testing.T) {
	s := statusTestSchema("a")
	installed := map[string]state.ToolState{
		"a": {Method: "native", DefinitionHash: "stale"},
	}
	got := classifyStatusTools(installed, s, nil, false)
	ts, ok := statusOf(got, "a")
	if !ok || ts.Status != "outdated" {
		t.Fatalf("a = %+v, want outdated", ts)
	}
}

func TestStatusRank_Order(t *testing.T) {
	order := []string{"outdated", "missing", "orphaned", "installed"}
	for i := 1; i < len(order); i++ {
		if statusRank(order[i-1]) >= statusRank(order[i]) {
			t.Fatalf("rank(%q) = %d, rank(%q) = %d: want strictly increasing",
				order[i-1], statusRank(order[i-1]), order[i], statusRank(order[i]))
		}
	}
	if statusRank("bogus") != statusRank("installed") {
		t.Fatalf("rank(bogus) = %d, want %d", statusRank("bogus"), statusRank("installed"))
	}
}

func TestSortStatusTools_RankThenName(t *testing.T) {
	tools := []toolStatus{
		{Name: "b", Status: "installed"},
		{Name: "a", Status: "missing"},
		{Name: "c", Status: "installed"},
		{Name: "d", Status: "outdated"},
	}
	sortStatusTools(tools)
	want := []string{"d", "a", "b", "c"}
	for i, name := range want {
		if tools[i].Name != name {
			t.Fatalf("position %d = %q, want %q (full: %+v)", i, tools[i].Name, name, tools)
		}
	}
}

func TestRenderStatusTable_Empty(t *testing.T) {
	if err := renderStatusTable(nil, false); err != nil {
		t.Fatalf("renderStatusTable(nil, false) = %v, want nil", err)
	}
	if err := renderStatusTable(nil, true); err != nil {
		t.Fatalf("renderStatusTable(nil, true) = %v, want nil", err)
	}
}

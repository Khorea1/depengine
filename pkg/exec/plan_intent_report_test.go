package exec

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestReportJSONIncludesSelectedPlanIntent(t *testing.T) {
	p := plan.New("demo", "native", true)
	r := &ExecReport{Tools: []ToolResult{{Tool: "demo", Status: StatusWouldInstall, Method: "native", PlanIntent: &p}}}
	if got := r.JSON(); !strings.Contains(got, `"plan_intent"`) || !strings.Contains(got, `"plan_version": 1`) {
		t.Fatalf("JSON() = %s", got)
	}
}

func TestReportDetailShowsConcreteProviderAndResolvedPlanDetails(t *testing.T) {
	p := plan.New("fd", "native", true)
	p.Identity.Package = "fd-find"
	p.Identity.Version = "10.2.0"
	p.Artifacts = []plan.Artifact{{URL: "https://example.test/fd-10.2.0.tar.gz"}}
	r := &ExecReport{Tools: []ToolResult{{
		Tool: "fd", Status: StatusWouldInstall, Method: "native", Provider: "apt", PlanIntent: &p,
	}}}

	got := r.Detail()
	for _, want := range []string{"Via", "apt", "package: fd-find", "version: 10.2.0", "url: https://example.test/fd-10.2.0.tar.gz"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Detail() missing %q:\n%s", want, got)
		}
	}
}

func TestReportJSONIncludesConcreteProvider(t *testing.T) {
	r := &ExecReport{Tools: []ToolResult{{Tool: "fd", Status: StatusWouldInstall, Method: "native", Provider: "xbps"}}}
	if got := r.JSON(); !strings.Contains(got, `"provider": "xbps"`) {
		t.Fatalf("JSON() = %s", got)
	}
}

func TestReportDetailShowsGitBackedCargoSourceAndRef(t *testing.T) {
	p := plan.New("demo", "cargo", true)
	p.Identity.Package = "demo-cli"
	p.Identity.Source = "https://github.com/example/demo.git"
	p.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionGitTag, Value: "v1.2.3"}
	r := &ExecReport{Tools: []ToolResult{{
		Tool: "demo", Status: StatusWouldInstall, Method: "cargo", Provider: "cargo", PlanIntent: &p,
	}}}
	got := r.Detail()
	for _, want := range []string{"package: demo-cli", "tag: v1.2.3", "source: https://github.com/example/demo.git"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Detail() missing %q:\n%s", want, got)
		}
	}
}

func TestProviderForMethodKindNormalizesVariantLabelsToAdapterKind(t *testing.T) {
	ex := &Executor{nativeManagerName: "xbps"}
	for kind, want := range map[string]string{"native": "xbps", "http": "http", "cargo": "cargo", "git": "git"} {
		if got := ex.providerForMethodKind(kind); got != want {
			t.Errorf("providerForMethodKind(%q) = %q, want %q", kind, got, want)
		}
	}
}

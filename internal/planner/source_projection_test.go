package planner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/planner"
)

func TestBuildCandidateIntentProjectsHostSources(t *testing.T) {
	tool := &config.Tool{Name: "demo"}
	method := &config.MethodCandidate{
		Kind:   "native",
		Config: map[string]any{"pkg": "demo"},
		Sources: []config.Source{
			{Kind: "apt-ppa", Name: "ppa:vendor/stable"},
			{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"},
		},
	}

	got, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("sources = %+v, want 2 host sources", got.Sources)
	}
	for i, source := range got.Sources {
		if source.Role != plan.SourceHostConfiguration {
			t.Fatalf("source %d role = %q, want %q", i, source.Role, plan.SourceHostConfiguration)
		}
	}
	if got.Sources[0].Kind != "apt-ppa" || got.Sources[1].Kind != "brew-tap" {
		t.Fatalf("source kinds not preserved: %+v", got.Sources)
	}
	contract, ok := methodkind.Lookup("native")
	if !ok {
		t.Fatal("native contract missing")
	}
	missing, err := contract.MissingPlanCapabilities(got)
	if err != nil {
		t.Fatalf("MissingPlanCapabilities() error: %v", err)
	}
	if missing != 0 {
		t.Fatalf("host-source plan unexpectedly rejected by method capability boundary: %v", methodkind.CapabilityNames(missing))
	}
}

func TestSourceSecretReferenceFlowsFromSchemaToPlanAndRequiresAuth(t *testing.T) {
	t.Setenv("CORP_TOKEN", "private-value-must-not-enter-plan")
	for _, withRef := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "schema.toml")
		ref := ""
		if withRef {
			ref = `, secret_ref = { provider = "env", name = "CORP_TOKEN" }`
		}
		data := "schema_version = 1\n[tools.demo.native]\npkg = \"demo\"\nsources = [{ kind = \"apt-ppa\", name = \"ppa:vendor/stable\"" + ref + " }]\n"
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		schema, err := config.ParseProjectSchema(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		tool := schema.Tools["demo"]
		intent, err := planner.BuildCandidateIntent(tool, tool.Methods[0])
		if err != nil {
			t.Fatal(err)
		}
		contract, _ := methodkind.Lookup("native")
		missing, err := contract.MissingPlanCapabilities(intent)
		if err != nil {
			t.Fatal(err)
		}
		if withRef {
			if len(intent.Sources) != 1 || intent.Sources[0].SecretRef == nil || *intent.Sources[0].SecretRef != (plan.SecretReference{Provider: "env", Name: "CORP_TOKEN"}) {
				t.Fatalf("plan source = %+v", intent.Sources)
			}
			if missing&methodkind.CapabilityAuth == 0 {
				t.Fatalf("missing capabilities = %v, want auth", methodkind.CapabilityNames(missing))
			}
			if _, err := planner.BuildValidatedCandidateIntent(tool, tool.Methods[0], methodkind.CandidateRequirements{}); err == nil || !plan.IsClass(err, plan.ErrorAuthRequirement) {
				t.Fatalf("validated intent error = %v, want auth requirement", err)
			}
		} else if missing&methodkind.CapabilityAuth != 0 {
			t.Fatalf("missing capabilities = %v, unexpected auth", methodkind.CapabilityNames(missing))
		}
		encoded, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "private-value-must-not-enter-plan") {
			t.Fatal("plan serialized environment secret material")
		}
	}
}

func TestBuildCandidateIntentProjectsGitCloneURLAsSourceIdentity(t *testing.T) {
	tool, method := candidate("demo", "git", map[string]any{
		"url": "https://github.com/example/demo.git",
		"tag": "v1.2.3",
	})
	got, err := planner.BuildCandidateIntent(tool, method)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity.Source != "https://github.com/example/demo.git" {
		t.Fatalf("source = %q", got.Identity.Source)
	}
	if got.Identity.RequestedVersion == nil || got.Identity.RequestedVersion.Mode != plan.VersionGitTag || got.Identity.RequestedVersion.Value != "v1.2.3" {
		t.Fatalf("requested version = %+v", got.Identity.RequestedVersion)
	}
}

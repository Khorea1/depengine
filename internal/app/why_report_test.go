package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// captureStdout runs fn with os.Stdout redirected and returns what it wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = writer
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		done <- string(data)
	}()
	fn()
	_ = writer.Close()
	os.Stdout = old
	return <-done
}

func findFieldProvenance(report *whyReport, field string) *whyFieldProv {
	for i := range report.FieldProvenance {
		if report.FieldProvenance[i].Field == field {
			return &report.FieldProvenance[i]
		}
	}
	return nil
}

// TestBuildWhyReportSelectsFirstRunnable covers the pure audit assembly: the
// selected candidate is the first one the executor would use, and every other
// candidate keeps its structured reason code.
func TestBuildWhyReportSelectsFirstRunnable(t *testing.T) {
	tool := &config.Tool{
		Name: "demo",
		Recipe: &config.RecipeOrigin{
			Name:      "demo",
			Root:      "/catalog",
			RootIndex: 1,
			Path:      "/catalog/demo.toml",
			Digest:    "sha256:" + strings.Repeat("a", 64),
		},
	}
	provenance := []config.FieldSource{
		{Field: "MethodPrefer", Source: config.LayerCatalog, Merged: []string{"native"}, Values: map[string]any{config.LayerCatalog: []string{"native"}}},
		{Field: "MethodPrefer", Source: config.LayerSchema, Merged: []string{"native"}, Values: map[string]any{
			config.LayerCatalog: []string{"native"},
			config.LayerSchema:  []string{"native"},
		}, Overridden: []string{config.LayerCatalog}},
		{Field: "Tags", Source: config.CombinedSource, Merged: []string{"editor"}, Values: map[string]any{
			config.LayerCatalog: []string{"editor"},
			config.LayerSchema:  []string{"editor"},
		}},
		{Field: "SecretRef", Source: config.LayerSchema, Merged: "env:DEMO_TOKEN", Values: map[string]any{config.LayerSchema: "env:DEMO_TOKEN"}},
	}
	attempts := []exec.MethodAttempt{
		{Kind: "native", Candidate: 0, CandidateKnown: true, Status: "skip_when", ReasonCode: exec.ReasonWhenNotMatched, Error: "when condition not met: os is darwin"},
		{Kind: "http", Label: "mirror", Candidate: 1, CandidateKnown: true, Status: "skip_unavailable", ReasonCode: exec.ReasonAdapterUnavailable, Error: "adapter not available"},
		{Kind: "http", Candidate: 2, CandidateKnown: true, Status: "would_install"},
	}

	report := buildWhyReport("demo", tool, provenance, attempts)
	if report.Recipe == nil || report.Recipe.RootIndex != 1 || report.Recipe.Layer != config.LayerCatalog {
		t.Fatalf("recipe = %+v, want catalog root #1", report.Recipe)
	}
	if report.Selected == nil || report.Selected.Kind != "http" || report.Selected.Candidate != 2 {
		t.Fatalf("selected = %+v, want http #2", report.Selected)
	}
	if len(report.Candidates) != 3 || report.Candidates[0].ReasonCode != exec.ReasonWhenNotMatched {
		t.Fatalf("candidates = %+v, want structured reason codes preserved", report.Candidates)
	}

	methodPrefer := findFieldProvenance(report, "MethodPrefer")
	if methodPrefer == nil || methodPrefer.Origin != config.LayerSchema {
		t.Fatalf("MethodPrefer = %+v, want schema origin", methodPrefer)
	}
	if len(methodPrefer.Overridden) != 1 || methodPrefer.Overridden[0] != config.LayerCatalog {
		t.Fatalf("MethodPrefer overridden = %v, want [catalog]", methodPrefer.Overridden)
	}
	if report.CatalogUpdate == nil || !report.CatalogUpdate.AffectsTool {
		t.Fatalf("catalog update = %+v, want the catalog tag change to affect the tool", report.CatalogUpdate)
	}
	if !containsLayerName(report.CatalogUpdate.Overridden, "MethodPrefer") {
		t.Fatalf("catalog update overridden = %v, want MethodPrefer shielded", report.CatalogUpdate.Overridden)
	}
	if !containsLayerName(report.CatalogUpdate.Fields, "Tags") {
		t.Fatalf("catalog update fields = %v, want Tags affected", report.CatalogUpdate.Fields)
	}

	secret := findFieldProvenance(report, "SecretRef")
	if secret == nil || secret.Effective != "<secret reference>" {
		t.Fatalf("SecretRef provenance = %+v, want a redacted reference", secret)
	}
	if strings.Contains(secret.Effective, "DEMO_TOKEN") {
		t.Fatal("provenance leaked a secret reference value")
	}
}

// whyAuditFixture writes a catalog recipe, a personal manifest and a project
// schema that all declare method_prefer with different values, so an override
// conflict is guaranteed. The only candidate is skipped by a host condition, so
// the run stays deterministic without probing package managers.
func whyAuditFixture(t *testing.T) (schemaPath, manifestPath, catalogRoot, recipePath string) {
	t.Helper()
	dir := t.TempDir()
	catalogRoot = filepath.Join(dir, "catalog")
	if err := os.Mkdir(catalogRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	writeCatalogRecipe(t, catalogRoot, "nvim", `
[packages.nvim]
method_prefer = ["native"]
tags = ["editor"]

[packages.nvim.native]
pkg = "neovim"
when = { os = ["plan9"] }
`)
	recipePath = filepath.Join(catalogRoot, "nvim.toml")

	schemaPath = filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(schemaPath, []byte(`schema_version = 1

[tools.nvim]
method_prefer = ["native"]
tags = ["editor", "project"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath = filepath.Join(dir, "manifest.toml")
	if err := os.WriteFile(manifestPath, []byte(`schema_version = 1

[packages.nvim]
method_prefer = ["cargo"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	return schemaPath, manifestPath, catalogRoot, recipePath
}

func TestWhyJSONAuditReportsRecipeProvenanceAndOverrides(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schemaPath, manifestPath, catalogRoot, recipePath := whyAuditFixture(t)

	whyJSON, whyFields, noManifest := true, false, false
	catalog := config.NewCatalog([]string{catalogRoot})
	out := captureStdout(t, func() {
		if err := runWhy(context.Background(), "nvim", &schemaPath, &manifestPath, &noManifest, &whyJSON, &whyFields, catalog); err != nil {
			t.Fatalf("runWhy: %v", err)
		}
	})

	var report whyReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("why --json is not the documented object: %v\n%s", err, out)
	}
	if report.Tool != "nvim" {
		t.Fatalf("tool = %q, want nvim", report.Tool)
	}
	if report.Recipe == nil {
		t.Fatal("expected recipe identity in the JSON report")
	}
	if report.Recipe.Layer != config.LayerCatalog || report.Recipe.RootIndex != 0 ||
		report.Recipe.Root != catalogRoot || report.Recipe.Path != recipePath {
		t.Fatalf("recipe = %+v, want catalog root #0 at %s", report.Recipe, recipePath)
	}
	if !strings.HasPrefix(report.Recipe.Digest, "sha256:") {
		t.Fatalf("recipe digest = %q, want sha256 digest", report.Recipe.Digest)
	}

	if len(report.Candidates) != 1 {
		t.Fatalf("candidates = %+v, want one skipped candidate", report.Candidates)
	}
	if got := report.Candidates[0]; got.Kind != "native" || got.Status != "skip_when" || got.ReasonCode != exec.ReasonWhenNotMatched {
		t.Fatalf("candidate = %+v, want native skip_when/when_not_matched", got)
	}
	if report.Selected != nil {
		t.Fatalf("selected = %+v, want none for an all-skipped tool", report.Selected)
	}

	methodPrefer := findFieldProvenance(&report, "MethodPrefer")
	if methodPrefer == nil {
		t.Fatal("expected MethodPrefer provenance")
	}
	if methodPrefer.Origin != config.LayerSchema {
		t.Fatalf("MethodPrefer origin = %q, want schema", methodPrefer.Origin)
	}
	if _, ok := methodPrefer.Layers[config.LayerCatalog]; !ok {
		t.Fatalf("MethodPrefer layers = %v, want the catalog's declared value", methodPrefer.Layers)
	}
	if _, ok := methodPrefer.Layers[config.LayerManifest]; !ok {
		t.Fatalf("MethodPrefer layers = %v, want the manifest's declared value", methodPrefer.Layers)
	}
	if strings.Join(methodPrefer.Overridden, ",") != "catalog,manifest" {
		t.Fatalf("MethodPrefer overridden = %v, want [catalog manifest]", methodPrefer.Overridden)
	}

	tags := findFieldProvenance(&report, "Tags")
	if tags == nil || tags.Origin != config.CombinedSource {
		t.Fatalf("Tags provenance = %+v, want a combined origin", tags)
	}
	if report.CatalogUpdate == nil || !report.CatalogUpdate.AffectsTool {
		t.Fatalf("catalog update = %+v, want an affected tool", report.CatalogUpdate)
	}
	if !containsLayerName(report.CatalogUpdate.Overridden, "MethodPrefer") {
		t.Fatalf("catalog update overridden = %v, want MethodPrefer shielded by the schema", report.CatalogUpdate.Overridden)
	}
	if !containsLayerName(report.CatalogUpdate.Fields, "Methods") {
		t.Fatalf("catalog update fields = %v, want Methods affected", report.CatalogUpdate.Fields)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"tool", "recipe", "field_provenance", "catalog_update", "candidates"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("why --json missing documented key %q", key)
		}
	}
}

func TestWhyFieldsTextShowsAuditSections(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	schemaPath, manifestPath, catalogRoot, _ := whyAuditFixture(t)

	whyJSON, whyFields, noManifest := false, true, false
	catalog := config.NewCatalog([]string{catalogRoot})
	out := captureStdout(t, func() {
		if err := runWhy(context.Background(), "nvim", &schemaPath, &manifestPath, &noManifest, &whyJSON, &whyFields, catalog); err != nil {
			t.Fatalf("runWhy: %v", err)
		}
	})

	for _, want := range []string{
		"Recipe: nvim (layer catalog, root #0)",
		"Selected: none",
		"Catalog update: affects",
		"[when_not_matched]",
		"MethodPrefer: origin=schema",
		"overridden=catalog,manifest",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("why --fields output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DEMO_TOKEN") {
		t.Error("why --fields output leaked a secret")
	}
}

// TestRedactedPlanIntentKeepsSecretsOutOfJSON guards the audit contract against
// credential leaks through candidate identity fields.
func TestRedactedPlanIntentKeepsSecretsOutOfJSON(t *testing.T) {
	attempt := exec.MethodAttempt{
		Kind:       "git",
		Status:     "would_install",
		Intent:     map[string]string{"source": run.RedactSensitiveText("https://user:token@example.test/repo")},
		PlanIntent: &plan.ResolvedInstallPlan{Identity: plan.ResolvedIdentity{Source: run.RedactSensitiveText("https://user:token@example.test/repo")}},
	}
	report := buildWhyReport("demo", &config.Tool{Name: "demo"}, nil, []exec.MethodAttempt{attempt})
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "token") {
		t.Fatalf("why JSON leaked a credential: %s", data)
	}
}

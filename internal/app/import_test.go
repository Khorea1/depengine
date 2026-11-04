package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

const importTestChecksum = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func writeImportSource(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write recipe: %v", err)
	}
	return path
}

func runImportForTest(t *testing.T, source, catalog string, dryRun, force bool) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := runImportTo(&stdout, &stderr, source, []string{catalog}, dryRun, force)
	code := 0
	if err != nil {
		code = requireExitCode(t, err)
	}
	return code, stdout.String() + stderr.String()
}

func TestImportWritesDeclarativeRecipe(t *testing.T) {
	catalog := t.TempDir()
	body := "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, false, false)
	if code != 0 {
		t.Fatalf("import exit code = %d, output:\n%s", code, out)
	}
	dest := filepath.Join(catalog, "tool.toml")
	got, err := os.ReadFile(dest) // #nosec G304 -- test reads the file it imported.
	if err != nil {
		t.Fatalf("read imported recipe: %v", err)
	}
	if string(got) != body {
		t.Fatalf("imported recipe changed:\n%s", got)
	}
	if !strings.Contains(out, "digest:     sha256:") {
		t.Fatalf("preview did not show the recipe digest:\n%s", out)
	}
	if !strings.Contains(out, "executable: none") {
		t.Fatalf("preview did not report the declarative check:\n%s", out)
	}
}

func TestImportRejectsLifecycleHook(t *testing.T) {
	catalog := t.TempDir()
	body := `recipe_version = 1

[packages.tool]
native = "tool"
pre_install = { run = ["echo", "hi"] }
`
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, false, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "not declarative") || !strings.Contains(out, "pre_install lifecycle hook") {
		t.Fatalf("missing declarative diagnostic:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(catalog, "tool.toml")); !os.IsNotExist(err) {
		t.Fatalf("rejected recipe was written: %v", err)
	}
}

func TestImportRejectsCommandField(t *testing.T) {
	catalog := t.TempDir()
	body := `recipe_version = 1

[packages.tool]
git = { url = "https://example.test/tool.git", build = "make" }
`
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "config \"build\" is a command field") {
		t.Fatalf("missing command-field diagnostic:\n%s", out)
	}
}

func TestImportRejectsArbitraryCodePlan(t *testing.T) {
	catalog := t.TempDir()
	body := fmt.Sprintf(`recipe_version = 1

[packages.tool]
deb = { url = "https://example.test/tool.deb", checksum = %q }
`, importTestChecksum)
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "plan requires arbitrary code") {
		t.Fatalf("missing arbitrary-code diagnostic:\n%s", out)
	}
}

func TestImportRejectsMissingChecksum(t *testing.T) {
	catalog := t.TempDir()
	body := `recipe_version = 1

[packages.tool]
github = { repo = "owner/tool", asset = "tool.tar.gz", scope = "user" }
`
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "declares no checksum") {
		t.Fatalf("missing integrity-floor diagnostic:\n%s", out)
	}
}

func TestImportFlagsAutoChecksumAsTOFU(t *testing.T) {
	catalog := t.TempDir()
	body := `recipe_version = 1

[packages.tool]
github = { repo = "owner/tool", asset = "tool.tar.gz", scope = "user", checksum = "sha256:auto", checksum_url = "https://example.test/SHA256SUMS" }
`
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 0 {
		t.Fatalf("import exit code = %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "TOFU") {
		t.Fatalf("auto checksum was not flagged as TOFU:\n%s", out)
	}
	if !strings.Contains(out, "github:owner/tool") {
		t.Fatalf("preview did not show the external origin:\n%s", out)
	}
}

func TestImportRequiresForceToReplace(t *testing.T) {
	catalog := t.TempDir()
	dest := filepath.Join(catalog, "tool.toml")
	if err := os.WriteFile(dest, []byte("old content"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, false, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "already exists; pass --force") {
		t.Fatalf("missing conflict diagnostic:\n%s", out)
	}
	if got, _ := os.ReadFile(dest); string(got) != "old content" { // #nosec G304 -- test reads its own fixture.
		t.Fatalf("conflicting import modified the destination: %q", got)
	}

	code, out = runImportForTest(t, source, catalog, false, true)
	if code != 0 {
		t.Fatalf("forced import exit code = %d, output:\n%s", code, out)
	}
	if got, _ := os.ReadFile(dest); string(got) != body { // #nosec G304 -- test reads its own fixture.
		t.Fatalf("forced import did not replace the destination: %q", got)
	}
}

func TestImportRejectsInvalidTOML(t *testing.T) {
	catalog := t.TempDir()
	source := writeImportSource(t, "tool", "recipe_version = 1\n[packages.tool\n")

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "error:") {
		t.Fatalf("missing parse diagnostic:\n%s", out)
	}
}

func TestImportRejectsUnaddressableName(t *testing.T) {
	catalog := t.TempDir()
	body := "recipe_version = 1\n\n[packages.\"bad name\"]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 2 {
		t.Fatalf("import exit code = %d, want 2, output:\n%s", code, out)
	}
	if !strings.Contains(out, "cannot be stored in a catalog") {
		t.Fatalf("missing name diagnostic:\n%s", out)
	}
}

func TestImportWarnsOnCandidateBudget(t *testing.T) {
	tool := &config.Tool{Name: "tool"}
	for i := 0; i < importCandidateBudget+1; i++ {
		tool.Methods = append(tool.Methods, &config.MethodCandidate{
			Kind:   "native",
			Label:  fmt.Sprintf("candidate-%d", i),
			Config: map[string]any{"pkg": "tool"},
		})
	}
	report, err := inspectImportRecipe("tool.toml", "tool", tool, []byte("recipe_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("candidate budget must not block: %v", report.Errors)
	}
	found := false
	for _, warning := range report.Warnings {
		if strings.Contains(warning, "accepted budget is 8") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing candidate-budget warning: %v", report.Warnings)
	}
}

func TestImportAcceptsDistributionChecksums(t *testing.T) {
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{{
		Kind:   "github",
		Config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "scope": "user"},
		Distributions: []config.Distribution{{
			Checksum:    importTestChecksum,
			HasChecksum: true,
		}},
	}}}
	report, err := inspectImportRecipe("tool.toml", "tool", tool, []byte("recipe_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("distribution checksum must satisfy the floor: %v", report.Errors)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].Integrity != importTestChecksum {
		t.Fatalf("integrity = %#v, want %q", report.Candidates, importTestChecksum)
	}
}

func TestImportAcceptsChecksumSecretReference(t *testing.T) {
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{{
		Kind:              "http",
		Config:            map[string]any{"url": "https://example.test/tool.tar.gz", "checksum_url": "https://example.test/SHA256SUMS"},
		ChecksumSecretRef: &config.SecretReference{Provider: "env", Name: "TOOL_CHECKSUM"},
	}}}
	report, err := inspectImportRecipe("tool.toml", "tool", tool, []byte("recipe_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) != 0 {
		t.Fatalf("checksum secret must satisfy the floor: %v", report.Errors)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].Integrity != "checksum from secret reference" {
		t.Fatalf("integrity = %#v", report.Candidates)
	}
}

func TestImportShowsEveryDeclaredCandidate(t *testing.T) {
	catalog := t.TempDir()
	body := fmt.Sprintf(`recipe_version = 1

[packages.tool.github_linux]
kind = "github"
repo = "owner/tool"
asset = "tool-linux.tar.gz"
scope = "user"
checksum = %q
when = { os = ["linux"] }

[packages.tool.github_darwin]
kind = "github"
repo = "owner/tool"
asset = "tool-darwin.tar.gz"
scope = "user"
checksum = %q
when = { os = ["darwin"] }
`, importTestChecksum, importTestChecksum)
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 0 {
		t.Fatalf("import exit code = %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "candidates: 2") {
		t.Fatalf("preview did not show every declared candidate:\n%s", out)
	}
	if !strings.Contains(out, `github "github_linux"`) || !strings.Contains(out, `github "github_darwin"`) {
		t.Fatalf("candidate preview lost host-gated candidates:\n%s", out)
	}
}

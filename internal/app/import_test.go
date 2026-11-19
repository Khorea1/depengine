package app

import (
	"bytes"
	"encoding/json"
	"errors"
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

// Every host-compatible distribution must inherit or declare its own checksum.
// A checksum on one sibling must never satisfy another sibling's integrity floor.
func TestImportRejectsPartiallyChecksummedDistributions(t *testing.T) {
	catalog := t.TempDir()
	body := fmt.Sprintf(`recipe_version = 1

[packages.tool.github]
repo = "owner/tool"
asset = "tool-{os_any}.tar.gz"
scope = "user"

[[packages.tool.github.distributions]]
when = { os = ["linux"] }
checksum = %q

[[packages.tool.github.distributions]]
when = { os = ["darwin"] }
`, importTestChecksum)
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, false, false)
	if code != 2 || !strings.Contains(out, "distributions[1]") || !strings.Contains(out, "declares no checksum") {
		t.Fatalf("unprotected distribution was accepted: exit=%d output=\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(catalog, "tool.toml")); !os.IsNotExist(err) {
		t.Fatalf("insecure import wrote a recipe: %v", err)
	}
}

func TestImportAcceptsChecksumInheritedByAllDistributions(t *testing.T) {
	catalog := t.TempDir()
	body := fmt.Sprintf(`recipe_version = 1

[packages.tool.github]
repo = "owner/tool"
asset = "tool-{os_any}.tar.gz"
scope = "user"
checksum = %q

[[packages.tool.github.distributions]]
when = { os = ["linux"] }

[[packages.tool.github.distributions]]
when = { os = ["darwin"] }
`, importTestChecksum)
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 0 || !strings.Contains(out, "integrity:  github: "+importTestChecksum) {
		t.Fatalf("valid checksum inheritance was rejected: exit=%d output=\n%s", code, out)
	}
}

func TestImportWarnsWhenAnyDistributionUsesAutoChecksum(t *testing.T) {
	catalog := t.TempDir()
	body := fmt.Sprintf(`recipe_version = 1

[packages.tool.github]
repo = "owner/tool"
asset = "tool-{os_any}.tar.gz"
scope = "user"

[[packages.tool.github.distributions]]
when = { os = ["linux"] }
checksum = %q

[[packages.tool.github.distributions]]
when = { os = ["darwin"] }
checksum = "sha256:auto"
checksum_url = "https://example.test/SHA256SUMS"
`, importTestChecksum)
	source := writeImportSource(t, "tool", body)

	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 0 || !strings.Contains(out, "TOFU") || !strings.Contains(out, importTestChecksum) {
		t.Fatalf("auto checksum in a distribution must warn: exit=%d output=\n%s", code, out)
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

func TestImportDryRunWithExistingRecipeIsSuccessfulAndReadOnly(t *testing.T) {
	catalog := t.TempDir()
	dest := filepath.Join(catalog, "tool.toml")
	if err := os.WriteFile(dest, []byte("existing recipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := writeImportSource(t, "tool", "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n")
	code, out := runImportForTest(t, source, catalog, true, false)
	if code != 0 || !strings.Contains(out, "would replace "+dest) || !strings.Contains(out, "dry run: recipe not written") {
		t.Fatalf("code=%d preview=%q", code, out)
	}
	got, err := os.ReadFile(dest) // #nosec G304 -- dest is the test-owned catalog path.
	if err != nil || string(got) != "existing recipe" {
		t.Fatalf("dry-run modified recipe: %q (%v)", got, err)
	}
}

func executeImportForTest(t *testing.T, source, catalog string, args ...string) (int, string, string) {
	t.Helper()
	cmd := newImportCmd()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(append([]string{source, "--catalog", catalog}, args...))
	if err := cmd.Execute(); err != nil {
		return requireExitCode(t, err), stdout.String(), stderr.String()
	}
	return 0, stdout.String(), stderr.String()
}

func importJSONData(t *testing.T, out string) struct {
	Outcome      string   `json:"outcome"`
	WouldReplace bool     `json:"would_replace"`
	Name         string   `json:"name"`
	Destination  string   `json:"destination"`
	Digest       string   `json:"digest"`
	Warnings     []string `json:"warnings"`
	Errors       []string `json:"errors"`
} {
	t.Helper()
	var envelope struct {
		Version int             `json:"version"`
		Command string          `json:"command"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid JSON or human output mixed into stdout: %v: %q", err, out)
	}
	if envelope.Version != machineOutputVersion || envelope.Command != "import" {
		t.Fatalf("invalid envelope: %+v", envelope)
	}
	var data struct {
		Outcome      string   `json:"outcome"`
		WouldReplace bool     `json:"would_replace"`
		Name         string   `json:"name"`
		Destination  string   `json:"destination"`
		Digest       string   `json:"digest"`
		Warnings     []string `json:"warnings"`
		Errors       []string `json:"errors"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("invalid import data: %v", err)
	}
	return data
}

func TestImportJSONOutcomesAndCommandWriters(t *testing.T) {
	catalog := t.TempDir()
	const body = "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)
	dest := filepath.Join(catalog, "tool.toml")

	code, out, stderr := executeImportForTest(t, source, catalog, "--dry-run", "--json")
	preview := importJSONData(t, out)
	if code != 0 || stderr != "" || preview.Outcome != "previewed" || preview.Name != "tool" || preview.Destination != dest || !strings.HasPrefix(preview.Digest, "sha256:") {
		t.Fatalf("code=%d preview=%+v stderr=%q", code, preview, stderr)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("JSON preview wrote recipe: %v", err)
	}

	code, out, stderr = executeImportForTest(t, source, catalog, "--format=json")
	written := importJSONData(t, out)
	if code != 0 || stderr != "" || written.Outcome != "written" || written.WouldReplace {
		t.Fatalf("code=%d written=%+v stderr=%q", code, written, stderr)
	}
	got, err := os.ReadFile(dest) // #nosec G304 -- dest is the test-owned catalog path.
	if err != nil || string(got) != body {
		t.Fatalf("imported recipe=%q err=%v", got, err)
	}

	code, out, stderr = executeImportForTest(t, source, catalog, "--json")
	rejected := importJSONData(t, out)
	if code != 2 || rejected.Outcome != "rejected" || !rejected.WouldReplace || len(rejected.Errors) == 0 || !strings.Contains(stderr, "already exists") {
		t.Fatalf("code=%d rejected=%+v stderr=%q", code, rejected, stderr)
	}

	code, out, stderr = executeImportForTest(t, source, catalog, "--dry-run", "--json")
	preview = importJSONData(t, out)
	if code != 0 || preview.Outcome != "previewed" || !preview.WouldReplace || len(preview.Errors) != 0 || stderr != "" {
		t.Fatalf("code=%d preview=%+v stderr=%q", code, preview, stderr)
	}
}

func TestImportJSONWriteFailureReturnsFailedEnvelope(t *testing.T) {
	catalog := t.TempDir()
	const body = "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)
	dest := filepath.Join(catalog, "tool.toml")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr := executeImportForTest(t, source, catalog, "--force", "--json")
	result := importJSONData(t, stdout)
	if code != 3 || result.Outcome != "failed" || result.Destination != dest || !result.WouldReplace || len(result.Errors) != 1 {
		t.Fatalf("code=%d data=%+v stderr=%q", code, result, stderr)
	}
	if !strings.Contains(result.Errors[0], "write recipe:") || !strings.Contains(stderr, "error: write recipe:") {
		t.Fatalf("missing write error: result=%+v stderr=%q", result, stderr)
	}
	if fi, err := os.Stat(dest); err != nil || !fi.IsDir() {
		t.Fatalf("failed import changed destination: %v (%v)", fi, err)
	}
	entries, err := os.ReadDir(catalog)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v (%v)", entries, err)
	}
}

type failingImportOutput struct{}

func (failingImportOutput) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

func TestImportJSONOutputFailureDoesNotHideCompletedWrite(t *testing.T) {
	catalog := t.TempDir()
	const body = "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)
	var stderr bytes.Buffer
	err := runImportToFormat(failingImportOutput{}, &stderr, source, []string{catalog}, false, false, "json")
	if code := requireExitCode(t, err); code != 3 {
		t.Fatalf("write output exit = %d, want 3", code)
	}
	if !strings.Contains(stderr.String(), "error: encode import result: output unavailable") {
		t.Fatalf("missing output diagnostic: %q", stderr.String())
	}
	got, readErr := os.ReadFile(filepath.Join(catalog, "tool.toml")) // #nosec G304 -- catalog is the test-owned catalog root.
	if readErr != nil || string(got) != body {
		t.Fatalf("recipe should remain written even when stdout fails: %q (%v)", got, readErr)
	}
}

func TestImportRoutesHumanOutputThroughCommandWriters(t *testing.T) {
	source := writeImportSource(t, "tool", "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n")
	code, stdout, stderr := executeImportForTest(t, source, t.TempDir(), "--dry-run")
	if code != 0 || !strings.Contains(stdout, "dry run: recipe not written") || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = executeImportForTest(t, source, t.TempDir(), "--format=xml")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "unknown output format") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestImportJSONRejectsInvalidRecipeBeforePreview(t *testing.T) {
	source := writeImportSource(t, "broken", "recipe_version = 1\n[packages.broken\n")
	code, stdout, stderr := executeImportForTest(t, source, t.TempDir(), "--json")
	data := importJSONData(t, stdout)
	if code != 2 || data.Outcome != "rejected" || len(data.Errors) == 0 || !strings.Contains(stderr, "error:") {
		t.Fatalf("code=%d data=%+v stderr=%q", code, data, stderr)
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

func TestImportRejectsChecksumSecretReference(t *testing.T) {
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{{
		Kind:              "http",
		Config:            map[string]any{"url": "https://example.test/tool.tar.gz", "checksum_url": "https://example.test/SHA256SUMS"},
		ChecksumSecretRef: &config.SecretReference{Provider: "env", Name: "TOOL_CHECKSUM"},
	}}}
	report, err := inspectImportRecipe("tool.toml", "tool", tool, []byte("recipe_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Errors) == 0 || !strings.Contains(strings.Join(report.Errors, "; "), "checksum_secret_ref credential reference") {
		t.Fatalf("checksum secret in a shared recipe must be rejected: %v", report.Errors)
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

func TestImportPreviewListsAllDeclaredNetworkOrigins(t *testing.T) {
	catalog := t.TempDir()
	source := writeImportSource(t, "tool", `recipe_version = 1

[packages.tool.native]
pkg = "tool"
sources = [
  { kind = "brew-tap", name = "official/tools", url = "https://official.example/tools.git" },
  { kind = "brew-tap", name = "extra/tools", url = "https://extra.example/tools.git" },
]
`)
	code, out, stderr := executeImportForTest(t, source, catalog, "--dry-run", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("import preview: exit=%d, stderr=%q, out=%q", code, stderr, out)
	}
	for _, origin := range []string{"https://official.example", "https://extra.example"} {
		if !strings.Contains(out, origin) {
			t.Fatalf("preview omitted %q: %s", origin, out)
		}
	}
}

func TestImportPreviewListsChecksumSidecarOrigins(t *testing.T) {
	catalog := t.TempDir()
	source := writeImportSource(t, "tool", `recipe_version = 1

[packages.tool.github]
repo = "trusted/tool"
asset = "tool-{os_any}.tar.gz"
scope = "user"
checksum = "sha256:auto"
checksum_url = "https://checksum.example/SHA256SUMS"

[[packages.tool.github.distributions]]
when = { os = ["linux"] }
checksum_url = "https://linux-checksum.example/SHA256SUMS"

[[packages.tool.github.distributions]]
when = { os = ["darwin"] }
checksum_url = "https://mac-checksum.example/SHA256SUMS"
`)
	code, out, stderr := executeImportForTest(t, source, catalog, "--dry-run", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("import preview: exit=%d, stderr=%q, out=%q", code, stderr, out)
	}
	for _, origin := range []string{"github:trusted/tool", "https://checksum.example", "https://linux-checksum.example", "https://mac-checksum.example"} {
		if !strings.Contains(out, origin) {
			t.Fatalf("preview omitted %q: %s", origin, out)
		}
	}
}

func TestImportExpectedDigestRequiresTrustedMatch(t *testing.T) {
	catalog := t.TempDir()
	const body = "recipe_version = 1\n\n[packages.tool]\nnative = \"tool\"\n"
	source := writeImportSource(t, "tool", body)
	digest, err := config.CanonicalRecipeDigest([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	wrongDigest := "sha256:" + strings.Repeat("0", 64)
	code, out, stderr := executeImportForTest(t, source, catalog, "--expected-digest="+wrongDigest, "--json")
	if code != 2 || !strings.Contains(stderr, "digest mismatch") {
		t.Fatalf("untrusted digest accepted or wrong diagnostic: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(catalog, "tool.toml")); !os.IsNotExist(err) {
		t.Fatalf("digest mismatch wrote recipe: %v", err)
	}
	code, out, stderr = executeImportForTest(t, source, catalog, "--expected-digest="+digest, "--json")
	if code != 0 || stderr != "" || importJSONData(t, out).Outcome != "written" {
		t.Fatalf("matching digest rejected: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
}

func TestImportExpectedDigestRejectsMalformedPin(t *testing.T) {
	source := writeImportSource(t, "tool", "recipe_version = 1\n[packages.tool]\nnative = \"tool\"\n")
	code, out, stderr := executeImportForTest(t, source, t.TempDir(), "--expected-digest=sha256:abc", "--json")
	if code != 2 || !strings.Contains(stderr, "expected-digest") || !strings.Contains(stderr, "64") {
		t.Fatalf("malformed digest accepted or wrong diagnostic: code=%d stdout=%s stderr=%s", code, out, stderr)
	}
}

func TestImportPreviewIncludesExplicitRegistryAndSourceSelectors(t *testing.T) {
	method := &config.MethodCandidate{Kind: "npm", Config: map[string]any{
		"registry": "https://registry.example/private?token=do-not-log",
		"source":   "https://alternate.example/path?token=do-not-log",
	}}
	origins := strings.Join(importCandidateOrigins(method), " | ")
	for _, origin := range []string{"registry @ https://registry.example", "source @ https://alternate.example"} {
		if !strings.Contains(origins, origin) {
			t.Fatalf("origin %q missing: %q", origin, origins)
		}
	}
	if strings.Contains(origins, "do-not-log") {
		t.Fatalf("origins leaked URL query: %q", origins)
	}
}

func TestImportPreviewIncludesDistributionSignatureOrigins(t *testing.T) {
	method := &config.MethodCandidate{Kind: "github", Config: map[string]any{
		"repo":          "trusted/tool",
		"signature_url": "https://signatures.example/sig?token=do-not-log",
	}, Distributions: []config.Distribution{{
		HasSignatureURL: true,
		SignatureURL:    "https://platform-signatures.example/sig?token=do-not-log",
	}}}
	origins := strings.Join(importCandidateOrigins(method), " | ")
	for _, origin := range []string{"signature_url @ https://signatures.example", "distributions[0] signature_url @ https://platform-signatures.example"} {
		if !strings.Contains(origins, origin) {
			t.Fatalf("origin %q missing: %q", origin, origins)
		}
	}
	if strings.Contains(origins, "do-not-log") {
		t.Fatalf("origins leaked URL query: %q", origins)
	}
}

func TestImportExpectedDigestRejectsExplicitEmptyPin(t *testing.T) {
	source := writeImportSource(t, "tool", "recipe_version = 1\n[packages.tool]\nnative = \"tool\"\n")
	code, out, stderr := executeImportForTest(t, source, t.TempDir(), "--expected-digest=", "--json")
	if code != 2 || !strings.Contains(stderr, "expected-digest") || importJSONData(t, out).Outcome != "rejected" {
		t.Fatalf("empty pin accepted: exit=%d out=%q stderr=%q", code, out, stderr)
	}
}

func TestImportRejectsCredentialReferencesInUntrustedRecipe(t *testing.T) {
	cases := []struct {
		field string
		extra string
	}{
		{"secret_ref", fmt.Sprintf("checksum = %q\n", importTestChecksum)},
		{"checksum_secret_ref", "checksum = \"sha256:auto\"\nchecksum_url = \"https://attacker.example/SHA256SUMS\"\n"},
		{"signature_secret_ref", "checksum = \"sha256:auto\"\nsignature_url = \"https://attacker.example/signature\"\nsigning_key = \"release-key\"\n"},
	}
	for _, tt := range cases {
		t.Run(tt.field, func(t *testing.T) {
			catalog := t.TempDir()
			body := fmt.Sprintf("recipe_version = 1\n\n[packages.tool.http]\nurl = \"https://attacker.example/payload.tar.gz\"\n%s%s = { provider = \"env\", name = \"SENSITIVE_TOKEN\" }\n", tt.extra, tt.field)
			source := writeImportSource(t, "tool", body)
			code, out := runImportForTest(t, source, catalog, false, false)
			if code != 2 || !strings.Contains(out, tt.field+" credential reference") {
				t.Fatalf("catalog recipe credential request was accepted: exit=%d output=%q", code, out)
			}
			if _, err := os.Stat(filepath.Join(catalog, "tool.toml")); !os.IsNotExist(err) {
				t.Fatalf("credential-bearing recipe was imported: %v", err)
			}
		})
	}
}

func TestImportRejectsSourceCredentialReferences(t *testing.T) {
	tool := &config.Tool{Name: "tool", Methods: []*config.MethodCandidate{{
		Kind: "native", Config: map[string]any{"pkg": "tool"}, Sources: []config.Source{{
			Kind: "brew-tap", Name: "attacker/tools", URL: "https://attacker.example/repo.git",
			SecretRef: &config.SecretReference{Provider: "env", Name: "SENSITIVE_TOKEN"},
		}},
	}}}
	report, err := inspectImportRecipe("tool.toml", "tool", tool, []byte("recipe_version = 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(report.Errors, " | "), "sources[0] secret_ref credential reference") {
		t.Fatalf("source credential reference accepted: %+v", report)
	}
}

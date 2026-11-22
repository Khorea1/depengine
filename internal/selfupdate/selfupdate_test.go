package selfupdate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestValidateRepoURL(t *testing.T) {
	for _, tc := range []struct {
		repo    string
		wantErr bool
	}{
		{"https://github.com/Khorea1/depengine.git", false},
		{"ssh://git@github.com/Khorea1/depengine.git", false},
		{"git@github.com:Khorea1/depengine.git", false},
		{"file:///tmp/depengine", false},
		{"/tmp/depengine", false},
		{"http://github.com/Khorea1/depengine.git", true},
		{"https://user:pass@github.com/Khorea1/depengine.git", true},
		{"https://github.com/Khorea1/depengine.git?token=x", true},
		{"--upload-pack=evil", true},
		{"relative/path", true},
	} {
		if err := validateRepoURL(tc.repo); (err != nil) != tc.wantErr {
			t.Errorf("validateRepoURL(%q) = %v, wantErr=%t", tc.repo, err, tc.wantErr)
		}
	}
}

func TestNormalizeOptionsRejectsUnsafeRef(t *testing.T) {
	for _, ref := range []string{"-x", " main", "a\nb"} {
		if _, err := normalizeOptions(Options{Ref: ref}); err == nil {
			t.Errorf("ref %q was accepted", ref)
		}
	}
}

func TestResolveTargetRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, executableName()), 0o755); err != nil { // #nosec G301 -- test-owned temporary directory.
		t.Fatal(err)
	}
	if _, _, _, err := resolveTarget(Options{BinDir: dir}, true); err == nil {
		t.Fatal("directory target was accepted")
	}
}

func TestResolveTargetPreservesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update refuses Windows")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real", "depengine")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil { // #nosec G301 -- test-owned temporary directory.
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("binary"), 0o755); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil { // #nosec G301 -- test-owned temporary directory.
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(binDir, "depengine")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	target, _, _, err := resolveTarget(Options{BinDir: binDir}, true)
	if err != nil {
		t.Fatal(err)
	}
	if target != real {
		t.Fatalf("target = %q, want %q", target, real)
	}
}

func TestEmbeddedRecipeMatchesPublicExample(t *testing.T) {
	public, err := os.ReadFile(filepath.Join("..", "..", "recipes", "depengine.toml.example")) // #nosec G304 -- repository fixture path.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(public, embeddedSelfRecipe) {
		t.Fatal("recipes/depengine.toml.example diverges from the embedded self-update recipe")
	}
}

func TestReadSelfRecipeRejectsUnsafeShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing build", `schema_version = 1
[tools.depengine]
[tools.depengine.git]
url = "https://example.test/depengine.git"
artifact = "depengine"
binary = "depengine"
`},
		{"non-git method", `schema_version = 1
[tools.depengine]
[tools.depengine.git]
url = "https://example.test/depengine.git"
build = { run = ["true"] }
artifact = "depengine"
binary = "depengine"
[tools.depengine.native]
pkg = "depengine"
`},
		{"artifact escapes checkout", `schema_version = 1
[tools.depengine]
[tools.depengine.git]
url = "https://example.test/depengine.git"
build = { run = ["true"] }
artifact = "../depengine"
binary = "depengine"
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "recipe.toml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			selfRecipe, err := readSelfRecipe(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := selfRecipe.Method(nil); err == nil {
				t.Fatal("unsafe recipe was accepted")
			}
		})
	}
}

func TestReadOnlyResolutionDoesNotCreateBinDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "bin")
	rn := &run.FakeRunner{
		Stdout:    "ref: refs/heads/master\tHEAD\n0123456789abcdef0123456789abcdef01234567\tHEAD\n",
		LookPaths: map[string]bool{"git": true},
	}
	if _, err := Run(context.Background(), rn, Options{Check: true, BinDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("--check created %s: %v", dir, err)
	}
}

func TestRunUsesRecipeSourceWhenRepoOmitted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update refuses Windows")
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable: %v", tool, err)
		}
	}
	src := t.TempDir()
	writeFixture(t, src, "recipe-v1")
	gitCommand(t, src, "init", "-q")
	gitCommand(t, src, "add", ".")
	gitCommand(t, src, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial")

	recipe := filepath.Join(t.TempDir(), "self.toml")
	body := "schema_version = 1\n\n[tools.depengine]\nmethod_only = [\"git\"]\n\n[tools.depengine.git]\nurl = " + strconv.Quote(src) + "\nbuild = { run = [\"go\", \"build\", \"-trimpath\", \"-o\", \"depengine\", \".\"] }\nartifact = \"depengine\"\nextract_to = \"~/.local/bin\"\nbinary = \"depengine\"\n"
	if err := os.WriteFile(recipe, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	result, err := Run(context.Background(), run.OSExecRunner{}, Options{Recipe: recipe, BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replaced || result.BuiltVersion != "depengine recipe-v1" {
		t.Fatalf("recipe install = %+v", result)
	}
	if result.Repo != src {
		t.Fatalf("repo = %q, want recipe source %q", result.Repo, src)
	}
}

func TestTargetBackupRestoresAndDiscards(t *testing.T) {
	target := filepath.Join(t.TempDir(), "depengine")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil { // #nosec G306 -- executable fixture.
		t.Fatal(err)
	}
	backup, err := backupTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o755); err != nil { // #nosec G306 -- executable fixture.
		t.Fatal(err)
	}
	if err := backup.restore(); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(target); string(body) != "old" { // #nosec G304 -- test-owned path.
		t.Fatalf("restored target = %q, want old", body)
	}

	backup, err = backupTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := backup.discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target + replacementBackupSuffix); !os.IsNotExist(err) {
		t.Fatalf("backup was not discarded: %v", err)
	}
}

func TestTargetBackupRecoversOrphanedTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "depengine")
	backup := target + replacementBackupSuffix
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil { // #nosec G306 -- executable fixture.
		t.Fatal(err)
	}
	b, err := backupTarget(target)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(target); string(body) != "old" { // #nosec G304 -- test-owned path.
		t.Fatalf("orphan was not recovered: %q", body)
	}
	if !b.active {
		t.Fatal("recovered target should be backed up for the current attempt")
	}
}

func TestCheckReportsUpdateAvailability(t *testing.T) {
	const remote = "0123456789abcdef0123456789abcdef01234567"
	previous := buildRevision
	t.Cleanup(func() { buildRevision = previous })

	for _, tc := range []struct {
		name     string
		current  string
		modified bool
		want     bool
	}{
		{"up to date", remote, false, false},
		{"different revision", strings.Repeat("f", 40), false, true},
		{"dirty build", remote, true, true},
		{"unknown revision", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buildRevision = func() (string, bool) { return tc.current, tc.modified }
			rn := &run.FakeRunner{
				Stdout:    "ref: refs/heads/master\tHEAD\n" + remote + "\tHEAD\n",
				LookPaths: map[string]bool{"git": true},
			}
			result, err := Run(context.Background(), rn, Options{Check: true, BinDir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if result.UpdateAvailable != tc.want {
				t.Fatalf("update available = %t, want %t (%+v)", result.UpdateAvailable, tc.want, result)
			}
			if result.Revision != remote || result.Ref != "master" {
				t.Fatalf("resolved ref = %q/%q", result.Ref, result.Revision)
			}
		})
	}
}

func TestRunLocalRepoInstallsAndUpdates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update refuses Windows")
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable: %v", tool, err)
		}
	}
	src := t.TempDir()
	writeFixture(t, src, "fixture-v1")
	gitCommand(t, src, "init", "-q")
	gitCommand(t, src, "add", ".")
	gitCommand(t, src, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial")

	binDir := filepath.Join(t.TempDir(), "bin")
	result, err := Run(context.Background(), run.OSExecRunner{}, Options{Repo: src, BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replaced || result.BuiltVersion != "depengine fixture-v1" {
		t.Fatalf("first install = %+v", result)
	}
	target := filepath.Join(binDir, "depengine")
	if body, err := os.ReadFile(target); err != nil || len(body) == 0 { // #nosec G304 -- target is a test-owned path.
		t.Fatalf("installed binary unavailable: %v", err)
	}

	writeFixture(t, src, "fixture-v2")
	gitCommand(t, src, "add", ".")
	gitCommand(t, src, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "second")
	result, err = Run(context.Background(), run.OSExecRunner{}, Options{Repo: src, BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if result.BuiltVersion != "depengine fixture-v2" {
		t.Fatalf("second install = %+v", result)
	}

	writeFixture(t, src, "fixture-v2")
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\nfunc broken(\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommand(t, src, "add", ".")
	gitCommand(t, src, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "broken")
	if _, err := Run(context.Background(), run.OSExecRunner{}, Options{Repo: src, BinDir: binDir}); err == nil {
		t.Fatal("broken source build succeeded")
	}
	out, err := exec.Command(target, "version").Output() // #nosec G204 -- target is the fixture binary built by this test.
	if err != nil || !strings.Contains(string(out), "fixture-v2") {
		t.Fatalf("previous executable was not preserved: %q, %v", out, err)
	}
}

func TestRunDryRunDoesNotReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update refuses Windows")
	}
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s unavailable: %v", tool, err)
		}
	}
	src := t.TempDir()
	writeFixture(t, src, "fixture-v1")
	gitCommand(t, src, "init", "-q")
	gitCommand(t, src, "add", ".")
	gitCommand(t, src, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial")
	binDir := t.TempDir()
	target := filepath.Join(binDir, "depengine")
	if err := os.WriteFile(target, []byte("old-binary"), 0o755); err != nil { // #nosec G306 -- executable test fixture requires owner execute permission.
		t.Fatal(err)
	}
	result, err := Run(context.Background(), run.OSExecRunner{}, Options{Repo: src, BinDir: binDir, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Replaced || result.BuiltVersion != "" {
		t.Fatalf("dry run mutated result: %+v", result)
	}
	body, err := os.ReadFile(target) // #nosec G304 -- target is a test-owned path.
	if err != nil || !bytes.Equal(body, []byte("old-binary")) {
		t.Fatalf("dry run changed target: %q, %v", body, err)
	}
}

func writeFixture(t *testing.T, dir, version string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.invalid/selfupdate-fixture\n\ngo 1.27.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tif len(os.Args) > 1 && os.Args[1] == \"version\" {\n\t\tfmt.Println(\"depengine " + version + "\")\n\t\treturn\n\t}\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) // #nosec G204 -- fixed executable with fixture-controlled arguments.
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCLIDocsGolden(t *testing.T) {
	setEnglishLocale(t)
	markdown, man, err := generateCLIDocs(newRootCmd())
	if err != nil {
		t.Fatal(err)
	}

	if os.Getenv("UPDATE_CLI_DOCS") == "1" {
		if err := writeCLIDocs(markdown, man); err != nil {
			t.Fatal(err)
		}
		return
	}

	assertGoldenFile(t, markdownDocsPath, markdown)
	assertGoldenFile(t, manDocsPath, man)
}

func TestCLIDocsCoverCommandTree(t *testing.T) {
	setEnglishLocale(t)
	root := newRootCmd()
	markdown, man, err := generateCLIDocs(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range documentedCommands(root) {
		if !bytes.Contains(markdown, []byte("## `"+cmd.UseLine()+"`")) {
			t.Errorf("Markdown reference does not contain %q", cmd.CommandPath())
		}
		if !bytes.Contains(man, []byte(roffEscape(cmd.UseLine()))) {
			t.Errorf("man page does not contain %q", cmd.CommandPath())
		}
	}

	for _, path := range []string{
		"depengine", "depengine check", "depengine completion",
		"depengine diff", "depengine forget", "depengine graph", "depengine help",
		"depengine init", "depengine install", "depengine remove", "depengine sbom",
		"depengine status", "depengine undo", "depengine update", "depengine upgrade",
		"depengine validate", "depengine version", "depengine why",
	} {
		if commandDocByPath(normalizeCLI(root), path) == nil {
			t.Errorf("documented command set does not contain %q", path)
		}
	}
}

func TestCLIDocsHighRiskFlags(t *testing.T) {
	setEnglishLocale(t)
	docs := normalizeCLI(newRootCmd())
	cases := map[string][]string{
		"depengine":         {"help", "version"},
		"depengine help":    {"help", "man"},
		"depengine install": {"allow-arbitrary-code", "diagnose", "dry-run", "frozen-lockfile", "help", "jobs", "json", "log-level", "manifest", "no-manifest", "only", "profile", "quiet", "schema", "skip", "sort-by", "verbose"},
		"depengine check":   {"format", "help", "json", "live", "manifest", "no-manifest", "schema"},
		"depengine graph":   {"format", "help", "manifest", "no-manifest", "only", "profile", "schema", "skip", "view", "width"},
		"depengine remove":  {"all", "dry-run", "force", "help", "only", "schema"},
		"depengine diff":    {"help", "json", "other"},
		"depengine sbom":    {"format", "help"},
	}
	for path, want := range cases {
		doc := commandDocByPath(docs, path)
		if doc == nil {
			t.Fatalf("missing command %q", path)
		}
		got := make([]string, 0, len(doc.Flags))
		for _, flag := range doc.Flags {
			if !flag.Hidden {
				got = append(got, flag.Name)
			}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s flags = %v, want %v", path, got, want)
		}
	}
	rootVersion := flagDocByName(t, commandDocByPath(docs, "depengine"), "version")
	if rootVersion.Shorthand != "v" || rootVersion.Default != "false" {
		t.Errorf("root version flag = %+v, want shorthand v and default false", rootVersion)
	}
	if flagDocByName(t, commandDocByPath(docs, "depengine help"), "man").Default != "false" {
		t.Error("help --man default is not false")
	}
}

func TestInstallYoloAliasesAllowArbitraryCode(t *testing.T) {
	cmd := newInstallCmd()
	canonical := cmd.Flags().Lookup("allow-arbitrary-code")
	alias := cmd.Flags().Lookup("yolo")
	if canonical == nil || alias == nil {
		t.Fatalf("install flags missing: canonical=%v alias=%v", canonical != nil, alias != nil)
	}
	if !alias.Hidden {
		t.Error("--yolo should remain a hidden convenience alias")
	}

	if err := cmd.Flags().Set("yolo", "true"); err != nil {
		t.Fatal(err)
	}
	if got := canonical.Value.String(); got != "true" {
		t.Fatalf("--yolo did not enable --allow-arbitrary-code storage: got %q", got)
	}

	if err := cmd.Flags().Set("allow-arbitrary-code", "false"); err != nil {
		t.Fatal(err)
	}
	if got := alias.Value.String(); got != "false" {
		t.Fatalf("canonical flag did not update --yolo storage: got %q", got)
	}
}

func commandDocByPath(docs []cliCommandDoc, path string) *cliCommandDoc {
	for i := range docs {
		if docs[i].Path == path {
			return &docs[i]
		}
	}
	return nil
}

func flagDocByName(t *testing.T, command *cliCommandDoc, name string) cliFlagDoc {
	t.Helper()
	if command == nil {
		t.Fatalf("command for flag %q is missing", name)
	}
	for _, flag := range command.Flags {
		if flag.Name == name {
			return flag
		}
	}
	t.Fatalf("%s flag %q is missing", command.Path, name)
	return cliFlagDoc{}
}

func setEnglishLocale(t *testing.T) {
	t.Helper()
	for _, name := range []string{"LANGUAGE", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(name, "C")
	}
}

func assertGoldenFile(t *testing.T, path string, got []byte) {
	t.Helper()
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; run go generate ./...", path)
	}
}

func writeCLIDocs(markdown, man []byte) error {
	type stagedFile struct{ path, temporary string }
	files := []struct {
		path string
		data []byte
	}{{markdownDocsPath, markdown}, {manDocsPath, man}}
	staged := make([]stagedFile, 0, len(files))
	defer func() {
		for _, file := range staged {
			_ = os.Remove(file.temporary)
		}
	}()

	for _, file := range files {
		temporary, err := os.CreateTemp(filepath.Dir(file.path), ".cli-docs-*")
		if err != nil {
			return fmt.Errorf("stage %s: %w", file.path, err)
		}
		staged = append(staged, stagedFile{file.path, temporary.Name()})
		if _, err := temporary.Write(file.data); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("write staged %s: %w", file.path, err)
		}
		if err := temporary.Chmod(0o644); err != nil {
			_ = temporary.Close()
			return fmt.Errorf("set mode on staged %s: %w", file.path, err)
		}
		if err := temporary.Close(); err != nil {
			return fmt.Errorf("close staged %s: %w", file.path, err)
		}
	}
	for _, file := range staged {
		if err := os.Rename(file.temporary, file.path); err != nil {
			return fmt.Errorf("replace %s: %w", file.path, err)
		}
	}
	return nil
}

func TestRoffEscape(t *testing.T) {
	got := roffEscape(".-x\\y")
	if !strings.HasPrefix(got, `\&.`) || !strings.Contains(got, `\-x\ey`) {
		t.Fatalf("roffEscape() = %q", got)
	}
}

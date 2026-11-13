package app

import (
	"bytes"
	"fmt"
	"io/fs"
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
		"depengine", "depengine audit", "depengine check", "depengine completion",
		"depengine diff", "depengine doctor", "depengine forget", "depengine graph", "depengine help",
		"depengine import", "depengine init", "depengine install", "depengine remove", "depengine sbom",
		"depengine status", "depengine undo", "depengine update", "depengine upgrade",
		"depengine validate", "depengine version", "depengine why",
	} {
		if commandDocByPath(normalizeCLI(root), path) == nil {
			t.Errorf("documented command set does not contain %q", path)
		}
	}
}

func TestCLIDocsRenderCommandExamples(t *testing.T) {
	setEnglishLocale(t)
	docs := normalizeCLI(newRootCmd())
	for _, path := range []string{
		"depengine audit", "depengine check", "depengine diff", "depengine doctor", "depengine forget", "depengine graph",
		"depengine import", "depengine init", "depengine install", "depengine remove",
		"depengine sbom", "depengine status", "depengine undo", "depengine update",
		"depengine upgrade", "depengine validate", "depengine why",
	} {
		doc := commandDocByPath(docs, path)
		if doc == nil {
			t.Fatalf("missing command %q", path)
		}
		if doc.Examples == "" {
			t.Errorf("%q has no Example block", path)
		}
	}

	markdown, man, err := generateCLIDocs(newRootCmd())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(markdown, []byte("Examples:\n\n```sh\n  depengine install")) {
		t.Error("Markdown reference does not render the install example")
	}
	if !bytes.Contains(man, []byte("Examples:\n.nf\n  depengine install")) {
		t.Error("man page does not render the install example")
	}
}

func TestCLIDocsHighRiskFlags(t *testing.T) {
	setEnglishLocale(t)
	docs := normalizeCLI(newRootCmd())
	cases := map[string][]string{
		"depengine":         {"help", "version"},
		"depengine help":    {"help", "man"},
		"depengine install": {"allow-arbitrary-code", "bundle", "catalog", "diagnose", "dry-run", "format", "frozen-lockfile", "help", "jobs", "json", "log-level", "manifest", "no-catalog", "no-manifest", "only", "profile", "quiet", "schema", "skip", "sort-by", "verbose"},
		"depengine check":   {"catalog", "format", "help", "json", "live", "manifest", "no-catalog", "no-manifest", "schema"},
		"depengine doctor":  {"catalog", "format", "help", "json", "manifest", "no-catalog", "no-manifest", "schema"},
		"depengine graph":   {"catalog", "depth", "direction", "format", "help", "manifest", "no-catalog", "no-manifest", "only", "profile", "schema", "show-inactive", "skip", "view", "width"},
		"depengine remove":  {"all", "dry-run", "force", "format", "help", "json", "only", "quiet", "schema"},
		"depengine diff":    {"format", "help", "json", "other"},
		"depengine sbom":    {"format", "help"}, "depengine audit": {"catalog", "format", "help", "json", "manifest", "no-catalog", "no-manifest", "schema", "strict"},
		"depengine validate":      {"catalog", "check-env", "format", "help", "json", "manifest", "no-catalog", "no-manifest", "schema", "strict"},
		"depengine update":        {"catalog", "dry-run", "format", "frozen-lockfile", "help", "json", "lock", "manifest", "no-catalog", "no-manifest", "profile", "quiet", "schema", "verbose"},
		"depengine undo":          {"format", "help", "json", "list", "snapshot"},
		"depengine forget":        {"format", "help", "json"},
		"depengine bundle export": {"format", "help", "json", "lock", "output"},
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

func TestCommandAliasesResolveAndAreDocumented(t *testing.T) {
	setEnglishLocale(t)
	root := newRootCmd()
	docs := normalizeCLI(root)
	cases := []struct {
		canonical string
		aliases   []string
	}{
		{canonical: "install", aliases: []string{"i"}},
		{canonical: "remove", aliases: []string{"rm", "uninstall"}},
		{canonical: "status", aliases: []string{"st"}},
		{canonical: "why", aliases: []string{"explain"}},
		{canonical: "undo", aliases: []string{"rollback"}},
		{canonical: "update", aliases: []string{"lock"}},
		{canonical: "validate", aliases: []string{"lint"}},
	}

	for _, tt := range cases {
		t.Run(tt.canonical, func(t *testing.T) {
			canonical, _, err := root.Find([]string{tt.canonical})
			if err != nil {
				t.Fatalf("find canonical command %q: %v", tt.canonical, err)
			}
			if !slices.Equal(canonical.Aliases, tt.aliases) {
				t.Fatalf("%s aliases = %v, want %v", tt.canonical, canonical.Aliases, tt.aliases)
			}

			doc := commandDocByPath(docs, "depengine "+tt.canonical)
			if doc == nil {
				t.Fatalf("missing docs for %q", tt.canonical)
			}
			if !slices.Equal(doc.Aliases, tt.aliases) {
				t.Fatalf("%s documented aliases = %v, want %v", tt.canonical, doc.Aliases, tt.aliases)
			}

			for _, alias := range tt.aliases {
				resolved, _, err := root.Find([]string{alias})
				if err != nil {
					t.Fatalf("find alias %q: %v", alias, err)
				}
				if resolved.Name() != tt.canonical {
					t.Errorf("alias %q resolved to %q, want %q", alias, resolved.Name(), tt.canonical)
				}
			}
		})
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

// TestEnvironmentVariableTablesMatch keeps the hand-written environment
// variable table in README.md and docs/cli-reference.md identical: both are
// published copies of the same claim and must not drift apart.
func TestEnvironmentVariableTablesMatch(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	reference, err := os.ReadFile(markdownDocsPath)
	if err != nil {
		t.Fatalf("read CLI reference: %v", err)
	}

	got := environmentVariableTable(string(readme))
	want := environmentVariableTable(string(reference))
	if got == "" {
		t.Fatal("README is missing the environment-variable table")
	}
	if want == "" {
		t.Fatalf("%s is missing the environment-variable table", markdownDocsPath)
	}
	if got != want {
		t.Fatalf("environment-variable tables differ; keep README and %s in sync\n\nREADME:\n%s\n%s:\n%s",
			markdownDocsPath, got, markdownDocsPath, want)
	}
	if !strings.Contains(got, "| `XDG_DATA_HOME` |") {
		t.Fatal("environment-variable tables must document XDG_DATA_HOME; runtime placement coverage pins its behavior")
	}
}

// environmentVariableTable returns the rows of the "Environment variables"
// table, or "" when the document has none.
func environmentVariableTable(doc string) string {
	const heading = "## Environment variables"
	start := strings.Index(doc, heading)
	if start < 0 {
		return ""
	}

	var table strings.Builder
	inTable := false
	for _, line := range strings.Split(doc[start:], "\n") {
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		table.WriteString(line)
		table.WriteByte('\n')
	}
	return table.String()
}

// TestTroubleshootingMessagesExistInSource keeps the troubleshooting index
// honest: every documented literal must still appear in non-test source, so a
// renamed diagnostic fails CI instead of sending users to a dead entry.
func TestTroubleshootingMessagesExistInSource(t *testing.T) {
	doc, err := os.ReadFile("../../docs/troubleshooting.md")
	if err != nil {
		t.Fatalf("read troubleshooting: %v", err)
	}
	messages := troubleshootingMessageLiterals(string(doc))
	if len(messages) < 10 {
		t.Fatalf("troubleshooting index has %d entries, want at least 10", len(messages))
	}
	source, err := internalSource()
	if err != nil {
		t.Fatalf("read internal source: %v", err)
	}
	for _, message := range messages {
		if !strings.Contains(source, message) {
			t.Errorf("troubleshooting documents %q, but no non-test source file contains it", message)
		}
	}
}

// TestExitCodeTablesMatch keeps the published exit-code contract identical in
// the CLI reference and the troubleshooting index.
func TestExitCodeTablesMatch(t *testing.T) {
	reference, err := os.ReadFile(markdownDocsPath)
	if err != nil {
		t.Fatalf("read CLI reference: %v", err)
	}
	troubleshooting, err := os.ReadFile("../../docs/troubleshooting.md")
	if err != nil {
		t.Fatalf("read troubleshooting: %v", err)
	}
	got := exitCodeTable(string(reference))
	want := exitCodeTable(string(troubleshooting))
	if got == "" {
		t.Fatal("CLI reference is missing the exit-code table")
	}
	if want == "" {
		t.Fatal("troubleshooting is missing the exit-code table")
	}
	if got != want {
		t.Fatalf("exit-code tables differ\n\nCLI reference:\n%s\ntroubleshooting:\n%s", got, want)
	}
}

func troubleshootingMessageLiterals(doc string) []string {
	const heading = "## Find your message"
	start := strings.Index(doc, heading)
	if start < 0 {
		return nil
	}
	var messages []string
	for _, line := range strings.Split(doc[start:], "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		rest := strings.TrimPrefix(line, "| `")
		end := strings.Index(rest, "`")
		if end < 0 {
			continue
		}
		messages = append(messages, rest[:end])
	}
	return messages
}

func exitCodeTable(doc string) string {
	const heading = "## Exit codes"
	start := strings.Index(doc, heading)
	if start < 0 {
		return ""
	}
	var table strings.Builder
	inTable := false
	for _, line := range strings.Split(doc[start:], "\n") {
		if !strings.HasPrefix(line, "|") {
			if inTable {
				break
			}
			continue
		}
		inTable = true
		table.WriteString(line)
		table.WriteByte('\n')
	}
	return table.String()
}

func internalSource() (string, error) {
	var paths []string
	err := filepath.WalkDir("../../internal", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	var source strings.Builder
	for _, path := range paths {
		data, readErr := os.ReadFile(path) // #nosec G304 -- paths come from a fixed repository source walk.
		if readErr != nil {
			return "", readErr
		}
		source.Write(data)
		source.WriteByte('\n')
	}
	return source.String(), nil
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
	want, err := os.ReadFile(path) // #nosec G304 -- fixture-controlled path; no untrusted runtime input crosses this test boundary.
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		wantLines := strings.Split(string(want), "\n")
		gotLines := strings.Split(string(got), "\n")
		limit := min(len(wantLines), len(gotLines))
		for i := 0; i < limit; i++ {
			if wantLines[i] != gotLines[i] {
				t.Fatalf("%s is stale at line %d:\nwant: %q\n got: %q\nrun go generate ./...", path, i+1, wantLines[i], gotLines[i])
			}
		}
		t.Fatalf("%s is stale: line count differs (want %d, got %d); run go generate ./...", path, len(wantLines), len(gotLines))
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

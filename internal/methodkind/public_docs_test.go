package methodkind_test

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/native"
)

// TestReadmeInstallMethodsMatchContracts keeps the README's public install
// method list aligned with the contracts the implementation enforces. The
// README is free to group methods and to mention native manager aliases, but
// every contract kind must appear and every named method must be a real one.
func TestReadmeInstallMethodsMatchContracts(t *testing.T) {
	data, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	section := documentSection(string(data), "## Install methods", "\n## ")
	if section == "" {
		t.Fatal("README is missing the install-methods section")
	}

	var tokens []string
	parts := strings.Split(section, "`")
	for i := 1; i < len(parts); i += 2 {
		tokens = append(tokens, parts[i])
	}
	assertPublicMethodNames(t, "README", tokens)
}

// TestCheatsheetInstallMethodsMatchContracts applies the same contract check
// to the cheatsheet's grouped method block.
func TestCheatsheetInstallMethodsMatchContracts(t *testing.T) {
	data, err := os.ReadFile("../../docs/cheatsheet.md")
	if err != nil {
		t.Fatalf("read cheatsheet: %v", err)
	}
	section := documentSection(string(data), "## Installation methods", "\n## ")
	if section == "" {
		t.Fatal("cheatsheet is missing the installation-methods section")
	}
	block := firstFencedBlock(section)
	if block == "" {
		t.Fatal("cheatsheet installation-methods section has no method block")
	}

	assertPublicMethodNames(t, "cheatsheet", methodBlockFields(block))
}

// assertPublicMethodNames fails when a documented method list omits a contract
// kind or names something no adapter or native manager registers.
func assertPublicMethodNames(t *testing.T, source string, tokens []string) {
	t.Helper()

	documented := make(map[string]bool, len(tokens))
	for _, token := range tokens {
		if token != "" {
			documented[token] = true
		}
	}

	vocabulary := make(map[string]bool, len(methodkind.Contracts))
	for _, contract := range methodkind.Contracts {
		vocabulary[contract.Kind] = true
	}
	for _, name := range native.ManagerNames() {
		vocabulary[name] = true
	}
	for _, name := range native.ManagerBinaryNames() {
		vocabulary[name] = true
	}

	var missing, unknown []string
	for _, contract := range methodkind.Contracts {
		if !documented[contract.Kind] {
			missing = append(missing, contract.Kind)
		}
	}
	for name := range documented {
		if !vocabulary[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(missing)
	slices.Sort(unknown)
	if len(missing) > 0 || len(unknown) > 0 {
		t.Fatalf("%s method list drift: missing=%v unknown=%v", source, missing, unknown)
	}
}

// documentSection returns the text from heading up to the next heading of the
// same level, or "" when heading is absent.
func documentSection(doc, heading, nextHeading string) string {
	start := strings.Index(doc, heading)
	if start < 0 {
		return ""
	}
	rest := doc[start+len(heading):]
	if end := strings.Index(rest, nextHeading); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

// firstFencedBlock returns the content of the first ``` block in section,
// without its language tag line.
func firstFencedBlock(section string) string {
	open := strings.Index(section, "```")
	if open < 0 {
		return ""
	}
	rest := section[open+len("```"):]
	if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
		rest = rest[newline+1:]
	}
	closed := strings.Index(rest, "```")
	if closed < 0 {
		return ""
	}
	return rest[:closed]
}

// methodTokenRE matches only the lowercase identifier vocabulary used by the
// grouped method block. Group labels start uppercase; punctuation and the
// explicit ellipsis are ignored.
var methodTokenRE = regexp.MustCompile(`\b[a-z][a-z0-9-]*\b`)

// methodBlockFields extracts every method identifier named in the grouped
// cheatsheet block. Native-manager examples are listed as ordinary comma-
// separated identifiers, so they are validated by the same vocabulary check
// instead of disappearing inside explanatory prose.
func methodBlockFields(block string) []string {
	return methodTokenRE.FindAllString(block, -1)
}

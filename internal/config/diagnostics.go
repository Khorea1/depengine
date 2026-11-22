package config

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/methodkind"
)

// schemaDiagnosticPath matches the dotted path that every strict validation
// message starts with. Array indexes are part of the path.
var schemaDiagnosticPath = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]+$`)

// enrichSchemaError adds source position, a minimal valid example, a field
// suggestion and a documentation anchor to each strict validation line. It is a
// diagnostic decoration: the original message stays intact and callers that
// only match substrings keep working.
func enrichSchemaError(err error, src []byte) error {
	if err == nil {
		return nil
	}
	lines := strings.Split(err.Error(), "\n")
	index := newTOMLPositionIndex(src)
	for i, line := range lines {
		if i == 0 && !strings.Contains(line, ": ") {
			continue // header such as "invalid schema:"
		}
		if enriched := enrichSchemaDiagnostic(line, index); enriched != "" {
			lines[i] = enriched
		}
	}
	return errors.New(strings.Join(lines, "\n"))
}

type sourcePosition struct {
	Line   int
	Column int
}

type tomlPositionIndex struct {
	exact map[string]sourcePosition
}

func newTOMLPositionIndex(src []byte) *tomlPositionIndex {
	index := &tomlPositionIndex{exact: make(map[string]sourcePosition)}
	table := ""
	for lineNumber, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if path, column, ok := parseTableHeader(line); ok {
			table = path
			index.record(path, lineNumber+1, column)
			continue
		}
		key, column, ok := parseKeyAssignment(line)
		if !ok {
			continue
		}
		path := key
		if table != "" {
			path = table + "." + key
		}
		index.record(path, lineNumber+1, column)
	}
	return index
}

func (i *tomlPositionIndex) record(path string, line, column int) {
	if _, exists := i.exact[path]; !exists {
		i.exact[path] = sourcePosition{Line: line, Column: column}
	}
}

// lookup resolves the closest declared position: the exact key, then its
// containing table, then progressively shorter prefixes.
func (i *tomlPositionIndex) lookup(path string) (sourcePosition, bool) {
	path = stripArrayIndexes(path)
	for {
		if position, ok := i.exact[path]; ok {
			return position, true
		}
		cut := strings.LastIndexByte(path, '.')
		if cut < 0 {
			return sourcePosition{}, false
		}
		path = path[:cut]
	}
}

func stripArrayIndexes(path string) string {
	for {
		open := strings.IndexByte(path, '[')
		if open < 0 {
			return path
		}
		close := strings.IndexByte(path[open:], ']')
		if close < 0 {
			return path
		}
		path = path[:open] + path[open+close+1:]
	}
}

func parseTableHeader(line string) (string, int, bool) {
	if !strings.HasPrefix(line, "[") {
		return "", 0, false
	}
	trimmed := strings.TrimPrefix(line, "[")
	trimmed = strings.TrimPrefix(trimmed, "[")
	close := strings.IndexByte(trimmed, ']')
	if close < 0 {
		return "", 0, false
	}
	path := strings.TrimSpace(trimmed[:close])
	if path == "" {
		return "", 0, false
	}
	segments := strings.Split(path, ".")
	column := strings.LastIndex(line, segments[len(segments)-1]) + 1
	return unquoteSegments(path), column, true
}

func parseKeyAssignment(line string) (string, int, bool) {
	equals := strings.IndexByte(line, '=')
	if equals < 0 {
		return "", 0, false
	}
	key := strings.TrimSpace(line[:equals])
	if key == "" || strings.ContainsAny(key, " \t") && !strings.HasPrefix(key, `"`) {
		return "", 0, false
	}
	return unquoteSegments(key), strings.Index(line, key) + 1, true
}

func unquoteSegments(path string) string {
	segments := strings.Split(path, ".")
	for i, segment := range segments {
		segments[i] = strings.Trim(segment, `"'`)
	}
	return strings.Join(segments, ".")
}

// enrichSchemaDiagnostic decorates one validation line. Lines without a
// recognized path are returned unchanged.
func enrichSchemaDiagnostic(line string, index *tomlPositionIndex) string {
	trimmed := strings.TrimSpace(line)
	path, message, ok := strings.Cut(trimmed, ": ")
	if !ok || !schemaDiagnosticPath.MatchString(path) {
		return ""
	}
	var b strings.Builder
	if position, found := index.lookup(path); found {
		b.WriteString(strconv.Itoa(position.Line))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(position.Column))
		b.WriteString(": ")
	}
	b.WriteString(trimmed)
	suggestion := ""
	if diagnosticWantsSuggestion(message) {
		suggestion = suggestField(path)
	}
	if suggestion != "" {
		b.WriteString(` (did you mean "`)
		b.WriteString(suggestion)
		b.WriteString(`"?)`)
	}
	example := ""
	if suggestion != "" {
		example = exampleForField(suggestion)
	}
	if example == "" {
		example = exampleForDiagnostic(path, message)
	}
	if example != "" && !strings.Contains(example, "\n") {
		b.WriteString("; example: ")
		b.WriteString(example)
	}
	anchor := ""
	if suggestion != "" {
		anchor = schemaDocAnchors[suggestion]
	}
	if anchor == "" {
		anchor = docAnchorFor(path, message)
	}
	if anchor != "" {
		b.WriteString("; docs: docs/schema-reference.md#")
		b.WriteString(anchor)
	}
	if strings.Contains(example, "\n") {
		b.WriteString("\n    example:\n    ")
		b.WriteString(strings.ReplaceAll(example, "\n", "\n    "))
	}
	return b.String()
}

// diagnosticWantsSuggestion limits edit-distance guesses to diagnostics where
// the offending key is unknown to the schema, so a valid field with the wrong
// value never gets a "did you mean" for a different field.
func diagnosticWantsSuggestion(message string) bool {
	return strings.Contains(message, "unknown field") ||
		strings.Contains(message, "unknown root field") ||
		strings.Contains(message, "is not supported by method kind") ||
		strings.Contains(message, "expected string, true, or table")
}

// suggestField returns the closest known field/method for an unknown field
// diagnostic. It returns "" when the diagnostic is not about an unknown field
// or no candidate is close enough.
func suggestField(path string) string {
	unknown := lastPathSegment(path)
	if unknown == "" {
		return ""
	}
	candidates := vocabularyFor(path)
	best, bestDistance := "", 3
	for _, candidate := range candidates {
		if candidate == unknown {
			return ""
		}
		distance := editDistance(strings.ToLower(unknown), strings.ToLower(candidate))
		if distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	if bestDistance <= 2 {
		return best
	}
	return ""
}

func vocabularyFor(path string) []string {
	segments := strings.Split(stripArrayIndexes(path), ".")
	last := segments[len(segments)-1]
	switch {
	case strings.Contains(path, ".when.") || last == "when":
		return sortedKeysOf(conditionFields)
	case strings.Contains(path, ".pre_install.") || strings.Contains(path, ".post_install.") || last == "pre_install" || last == "post_install":
		return []string{"cmd", "run", "when"}
	case last == "defaults" || strings.HasPrefix(path, "defaults."):
		return []string{"manager", "aur_helper", "method_prefer", "method_order", "arch_map", "os_map"}
	case last == "manifest" || strings.HasPrefix(path, "manifest."):
		return []string{"allow_new_tools"}
	case len(segments) == 1:
		return []string{"schema_version", "recipe_version", "tools", "packages", "defaults", "manifest"}
	case len(segments) == 3:
		// [tools.<name>] fields or a method table name.
		candidates := []string{"requires", "requires_when", "dependency_only", "pre_install", "post_install", "tags", "method_prefer", "method_only"}
		candidates = append(candidates, methodkind.KnownKinds()...)
		candidates = append(candidates, sortedKeysOf(methodkind.DefaultBuckets)...)
		return candidates
	case len(segments) >= 4:
		if contract, ok := methodkind.Lookup(segments[2]); ok {
			return sortedKeysOf(contract.Fields)
		}
	}
	return nil
}

func sortedKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func lastPathSegment(path string) string {
	path = stripArrayIndexes(path)
	cut := strings.LastIndexByte(path, '.')
	return path[cut+1:]
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// exampleForDiagnostic returns a minimal, one-line valid TOML shape for the
// failing location. It intentionally stays small: the goal is to show the
// expected shape, not to author the tool for the user.
func exampleForDiagnostic(path, message string) string {
	segments := strings.Split(stripArrayIndexes(path), ".")
	last := segments[len(segments)-1]
	switch {
	case strings.Contains(message, "this method requires a config table"), strings.Contains(message, "expected string, true, or table"):
		return methodExample(segments)
	case strings.Contains(message, "expected table"):
		switch {
		case last == "defaults":
			return "[defaults]\nmethod_prefer = [\"native\"]"
		case last == "manifest":
			return "[manifest]\nallow_new_tools = true"
		case last == "when":
			return `when = { os = ["linux"] }`
		case len(segments) == 2 && (segments[0] == "tools" || segments[0] == "packages"):
			return "[" + segments[0] + "." + last + "]\ntags = [\"cli\"]"
		case len(segments) == 3 && (segments[0] == "tools" || segments[0] == "packages"):
			return "[" + segments[0] + "." + segments[1] + "." + last + "]"
		}
	case strings.Contains(message, "expected boolean"):
		return last + " = true"
	case strings.Contains(message, "expected integer"):
		return last + " = 1"
	case strings.Contains(message, "expected string list"), strings.Contains(message, "expected array"):
		return last + ` = ["value"]`
	case strings.Contains(message, "expected string"):
		return last + ` = "value"`
	}
	if suggestion := suggestField(path); suggestion != "" {
		if example := exampleForField(suggestion); example != "" {
			return example
		}
	}
	return ""
}

func methodExample(segments []string) string {
	if len(segments) < 3 {
		return ""
	}
	tool, kind := segments[1], segments[2]
	switch kind {
	case "native":
		return `native = { pkg = "` + tool + `" }`
	case "github":
		return `github = { repo = "owner/repo", asset = "tool-{version}.tar.gz" }`
	case "http":
		return `http = { url = "https://example.test/tool.tar.gz" }`
	case "git":
		return `git = { url = "https://example.test/tool.git" }`
	case "cargo":
		return `cargo = { crate = "crate-name" }`
	}
	return "[" + strings.Join(segments[:3], ".") + "]"
}

func exampleForField(field string) string {
	switch field {
	case "tags":
		return `tags = ["cli"]`
	case "when":
		return `when = { os = ["linux"] }`
	case "requires":
		return `requires = ["dependency"]`
	case "pre_install", "post_install":
		return field + ` = { run = ["echo", "ready"] }`
	case "method_prefer":
		return `method_prefer = ["native"]`
	case "method_only":
		return `method_only = ["native"]`
	case "allow_new_tools":
		return "allow_new_tools = true"
	case "repo":
		return `repo = "owner/repo"`
	case "asset":
		return `asset = "tool-{version}.tar.gz"`
	case "url":
		return `url = "https://example.test/tool.tar.gz"`
	case "pkg":
		return `pkg = "tool"`
	case "crate":
		return `crate = "crate-name"`
	case "binary":
		return `binary = "tool"`
	}
	return ""
}

func docAnchorFor(path, message string) string {
	last := lastPathSegment(path)
	if anchor, ok := schemaDocAnchors[last]; ok {
		return anchor
	}
	switch {
	case strings.Contains(path, ".when"):
		return schemaDocAnchors["when"]
	case strings.Contains(message, "method"):
		return "method-reference"
	case len(strings.Split(stripArrayIndexes(path), ".")) <= 2:
		return schemaDocAnchors["tools"]
	}
	return "method-reference"
}

var schemaDocAnchors = map[string]string{
	"schema_version":  "minimal-project-schema-complete",
	"recipe_version":  "complete-copy-paste-documents",
	"tools":           "naming-a-tool",
	"packages":        "personal-manifest-admitting-new-tools",
	"defaults":        "per-tool-method-control",
	"manifest":        "personal-manifest-admitting-new-tools",
	"simple":          "simple-tool-name--package-everywhere",
	"when":            "platform-conditions-when-multi-dimension-gating",
	"pre_install":     "hooks--dependencies",
	"post_install":    "hooks--dependencies",
	"requires":        "tool-to-tool-dependency",
	"requires_when":   "tool-to-tool-dependency",
	"tags":            "per-tool-method-control",
	"method_prefer":   "per-tool-method-control",
	"method_only":     "per-tool-method-control",
	"method_order":    "per-tool-method-control",
	"sources":         "custom-sources",
	"allow_new_tools": "personal-manifest-admitting-new-tools",
}

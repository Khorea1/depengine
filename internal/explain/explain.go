// Package explain answers offline vocabulary questions about depengine's
// schema fields, installation methods, and error codes. It reads only the
// generated JSON Schema and the method contracts, so it never probes the host,
// the network, or a project directory.
package explain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/native"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/validate"
)

// Entry groups.
const (
	GroupField      = "field"
	GroupMethod     = "method"
	GroupErrorCode  = "error_code"
	GroupErrorClass = "error_class"
)

const (
	schemaDoc       = "docs/schema-reference.md"
	cliDoc          = "docs/cli-reference.md"
	troubleshootDoc = "docs/troubleshooting.md"
)

// Entry is one answer to a vocabulary question. Example is TOML for fields and
// methods and empty for error codes, where Form already names the surface that
// emits the code.
type Entry struct {
	Term    string `json:"term"`
	Group   string `json:"group"`
	Where   string `json:"where"`
	Summary string `json:"summary"`
	Form    string `json:"form"`
	Example string `json:"example,omitempty"`
	Doc     string `json:"doc"`
}

// Catalog is the immutable vocabulary index.
type Catalog struct {
	entries []Entry
	byTerm  map[string][]Entry
	terms   []string
}

// Load returns the vocabulary catalog, building it once per process.
func Load() (*Catalog, error) { return loadCatalog() }

var loadCatalog = sync.OnceValues(buildCatalog)

// Entries returns every entry in catalog order: schema fields, methods, then
// error vocabulary.
func (c *Catalog) Entries() []Entry {
	if c == nil {
		return nil
	}
	return append([]Entry(nil), c.entries...)
}

// Lookup returns the entries whose term matches term, ignoring case.
func (c *Catalog) Lookup(term string) []Entry {
	if c == nil {
		return nil
	}
	term = strings.TrimSpace(term)
	if matches := c.byTerm[term]; len(matches) > 0 {
		return append([]Entry(nil), matches...)
	}
	return append([]Entry(nil), c.byTerm[strings.ToLower(term)]...)
}

// Terms returns every known term, sorted.
func (c *Catalog) Terms() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.terms...)
}

// Suggest returns up to three known terms close to an unknown query.
func (c *Catalog) Suggest(term string) []string {
	if c == nil {
		return nil
	}
	term = strings.ToLower(strings.TrimSpace(term))
	type candidate struct {
		term     string
		distance int
	}
	var candidates []candidate
	for _, known := range c.terms {
		distance := editDistance(term, known)
		if distance <= 2 {
			candidates = append(candidates, candidate{term: known, distance: distance})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		return candidates[i].term < candidates[j].term
	})
	out := make([]string, 0, 3)
	seen := make(map[string]bool, 3)
	for _, item := range candidates {
		if seen[item.term] {
			continue
		}
		seen[item.term] = true
		out = append(out, item.term)
		if len(out) == 3 {
			break
		}
	}
	return out
}

type schemaDocument struct {
	Definitions map[string]schemaNode `json:"definitions"`
}

// schemaNode keeps only the schema shape needed to describe a field.
type schemaNode struct {
	Ref                  string                `json:"$ref"`
	Type                 string                `json:"type"`
	Description          string                `json:"description"`
	Enum                 []string              `json:"enum"`
	Const                any                   `json:"const"`
	Items                *schemaNode           `json:"items"`
	Properties           map[string]schemaNode `json:"properties"`
	AdditionalProperties json.RawMessage       `json:"additionalProperties"`
	OneOf                []schemaNode          `json:"oneOf"`
	MinLength            *int                  `json:"minLength"`
}

func buildCatalog() (*Catalog, error) {
	raw, err := config.GenerateJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("generate JSON schema: %w", err)
	}
	var doc schemaDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("decode JSON schema: %w", err)
	}
	entries := fieldEntries(doc)
	entries = append(entries, methodEntries()...)
	entries = append(entries, errorEntries...)
	return newCatalog(entries), nil
}

func newCatalog(entries []Entry) *Catalog {
	catalog := &Catalog{entries: entries, byTerm: make(map[string][]Entry, len(entries))}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		catalog.byTerm[entry.Term] = append(catalog.byTerm[entry.Term], entry)
		lower := strings.ToLower(entry.Term)
		if lower != entry.Term {
			catalog.byTerm[lower] = append(catalog.byTerm[lower], entry)
		}
		if !seen[lower] {
			seen[lower] = true
			catalog.terms = append(catalog.terms, lower)
		}
	}
	sort.Strings(catalog.terms)
	return catalog
}

var fieldSections = []struct {
	definition string
	where      string
	doc        string
}{
	{"tool", "[tools.<name>]", schemaDoc + "#naming-a-tool"},
	{"condition", "inside a when/hook condition", schemaDoc + "#platform-conditions-when-multi-dimension-gating"},
	{"defaults", "[defaults]", schemaDoc + "#per-tool-method-control"},
	{"manifestOptions", "[manifest]", schemaDoc + "#manifest-merge-rules"},
	{"secretReference", "secret_ref", schemaDoc + "#http-download-and-extract-an-artifact-archive-or-binary"},
}

// fieldExamples keeps hand-written TOML for fields where a generated
// placeholder would not teach the shape.
var fieldExamples = map[string]string{ //nolint:gosec // G101: schema placeholders printed by explain, not credentials.
	"tool.requires":        `requires = ["git"]`,
	"tool.requires_when":   `requires_when = { fontconfig = { target_family = ["unix"] } }`,
	"tool.tags":            `tags = ["shell"]`,
	"tool.method_prefer":   `method_prefer = ["cargo"]`,
	"tool.method_only":     `method_only = ["github"]`,
	"tool.dependency_only": `dependency_only = true`,

	"condition.distro_family":      `distro_family = ["debian"]`,
	"condition.distro_id":          `distro_id = ["ubuntu"]`,
	"condition.distro_version":     `distro_version = ["24.04"]`,
	"condition.distro_version_min": `distro_version_min = "22.04"`,
	"condition.distro_version_max": `distro_version_max = "24.04"`,
	"condition.arch":               `arch = ["x86_64"]`,
	"condition.os":                 `os = ["linux"]`,
	"condition.target_family":      `target_family = ["unix"]`,
	"condition.kernel":             `kernel = ["6.7.0-arch1-1"]`,
	"condition.kernel_min":         `kernel_min = "6.6"`,
	"condition.kernel_max":         `kernel_max = "6.8"`,
	"condition.libc":               `libc = ["musl"]`,
	"condition.init_system":        `init_system = ["systemd"]`,
	"condition.is_wsl":             `is_wsl = false`,
	"condition.is_container":       `is_container = false`,
	"condition.is_android":         `is_android = true`,

	"defaults.manager":       `manager = "native"`,
	"defaults.aur_helper":    `aur_helper = "yay"`,
	"defaults.method_prefer": `method_prefer = ["cargo"]`,
	"defaults.method_order":  `method_order = ["github"]`,
	"defaults.arch_map":      `arch_map = { x86_64 = "amd64", aarch64 = "arm64" }`,
	"defaults.os_map":        `os_map = { darwin = "macos" }`,

	"manifest.allow_new_tools": `allow_new_tools = true`,

	"secret_ref.provider": `provider = "env"`,
	"secret_ref.name":     `name = "EXAMPLE_ENV_NAME"`,
}

// fieldDocs overrides the section document for one field.
var fieldDocs = map[string]string{
	"tool.requires":        schemaDoc + "#tool-to-tool-dependency",
	"tool.requires_when":   schemaDoc + "#tool-to-tool-dependency",
	"tool.dependency_only": schemaDoc + "#virtual-tools-dependency-groups-with-no-methods",
	"tool.method_prefer":   schemaDoc + "#per-tool-method-control",
	"tool.method_only":     schemaDoc + "#per-tool-method-control",
	"tool.tags":            cliDoc + "#depengine-install-flags",
	"defaults.manager":     schemaDoc + "#naming-a-tool",
	"defaults.aur_helper":  schemaDoc + "#method-reference",
}

func fieldEntries(doc schemaDocument) []Entry {
	var out []Entry
	for _, section := range fieldSections {
		definition, ok := doc.Definitions[section.definition]
		if !ok {
			continue
		}
		names := make([]string, 0, len(definition.Properties))
		for name := range definition.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if section.definition == "tool" && (methodkind.IsKnownKind(name) || isBucket(name)) {
				// Method shorthands answer through the method entries.
				continue
			}
			property := definition.Properties[name]
			if property.Description == "" {
				continue
			}
			qualified := section.definition + "." + name
			entry := Entry{
				Term:    name,
				Group:   GroupField,
				Where:   section.where,
				Summary: property.Description,
				Form:    formOf(property),
				Example: fieldExamples[qualified],
				Doc:     section.doc,
			}
			if doc := fieldDocs[qualified]; doc != "" {
				entry.Doc = doc
			}
			if entry.Example == "" {
				entry.Example = generatedExample(name, property)
			}
			out = append(out, entry)
		}
	}
	return out
}

func isBucket(name string) bool {
	_, ok := methodkind.DefaultBuckets[name]
	return ok
}

func formOf(node schemaNode) string {
	switch {
	case node.Ref != "":
		return strings.TrimPrefix(node.Ref, "#/definitions/")
	case len(node.Enum) > 0:
		return "one of " + strings.Join(quoted(node.Enum), ", ")
	case node.Const != nil:
		return fmt.Sprint(node.Const)
	case len(node.OneOf) > 0:
		options := make([]string, 0, len(node.OneOf))
		for _, option := range node.OneOf {
			options = append(options, formOf(option))
		}
		return "one of " + strings.Join(options, ", ")
	}
	switch node.Type {
	case "array":
		item := "string"
		if node.Items != nil {
			item = formOf(*node.Items)
		}
		return item + "[]"
	case "object":
		value := additionalPropertyForm(node.AdditionalProperties)
		if value != "" {
			return "table<string, " + value + ">"
		}
		return "table"
	case "boolean":
		return "bool"
	case "integer":
		return "integer"
	case "string":
		if node.MinLength != nil && *node.MinLength > 0 {
			return "non-empty string"
		}
		return "string"
	default:
		return node.Type
	}
}

func additionalPropertyForm(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var node schemaNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return ""
	}
	if node.Ref == "" && node.Type == "" {
		return ""
	}
	return formOf(node)
}

// generatedExample renders a best-effort TOML line for fields without a
// hand-written example.
func generatedExample(name string, node schemaNode) string {
	if len(node.Enum) > 0 {
		return name + " = " + quote(node.Enum[0])
	}
	switch node.Type {
	case "boolean":
		return name + " = true"
	case "integer":
		return name + " = 1"
	case "array":
		return name + " = [" + placeholderForSchema(name, schemaNode{}) + "]"
	case "object":
		return name + " = { key = " + placeholderForSchema(name, schemaNodeType(node.AdditionalProperties)) + " }"
	default:
		return name + " = " + placeholderForSchema(name, node)
	}
}

func schemaNodeType(raw json.RawMessage) schemaNode {
	var node schemaNode
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &node)
	}
	return node
}

func methodEntries() []Entry {
	kinds := methodkind.KnownKinds()
	out := make([]Entry, 0, len(kinds))
	for _, kind := range kinds {
		contract, ok := methodkind.Lookup(kind)
		if !ok {
			continue
		}
		where := "candidate method"
		summary := methodkind.Describe(kind)
		switch {
		case kind != contract.Kind && contract.Kind == "native":
			where = "native manager alias"
			summary = "Native package-manager alias; `" + kind + " = true` (or a package name) selects it as a native candidate."
		case kind != contract.Kind:
			where = "alias for the " + contract.Kind + " method"
			summary = "Alias for " + contract.Kind + ": " + methodkind.Describe(contract.Kind)
		}
		out = append(out, Entry{
			Term:    kind,
			Group:   GroupMethod,
			Where:   where,
			Summary: summary,
			Form:    methodForm(kind, contract),
			Example: methodExample(kind, contract),
			Doc:     methodDoc(kind, contract),
		})
	}
	return out
}

func methodForm(kind string, contract *methodkind.Contract) string {
	options := make([]string, 0, 3)
	if contract.AllowTrue {
		options = append(options, "true")
	}
	if contract.AllowString {
		options = append(options, `"<pkg>"`)
	}
	options = append(options, "table")
	form := options[0]
	if len(options) > 1 {
		form = "one of " + strings.Join(options, ", ")
	}
	if required := requiredFields(contract); len(required) > 0 {
		form += "; required: " + strings.Join(required, ", ")
	}
	if len(contract.SourceAlternatives) > 0 {
		alternatives := make([]string, 0, len(contract.SourceAlternatives))
		for _, alternative := range contract.SourceAlternatives {
			alternatives = append(alternatives, strings.Join(alternative, "+"))
		}
		form += "; source: one of " + strings.Join(alternatives, " | ")
	}
	if len(contract.MutuallyExclusive) > 0 {
		groups := make([]string, 0, len(contract.MutuallyExclusive))
		for _, group := range contract.MutuallyExclusive {
			groups = append(groups, strings.Join(group, "/"))
		}
		form += "; not both: " + strings.Join(groups, ", ")
	}
	return form
}

func requiredFields(contract *methodkind.Contract) []string {
	out := make([]string, 0, len(contract.Fields))
	for key, field := range contract.Fields {
		if field.Required {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func methodExample(kind string, contract *methodkind.Contract) string {
	if contract.Kind == "native" || native.IsNativeManagerName(kind) {
		return "myapp = { " + kind + ` = "myapp" }`
	}
	fields := requiredFields(contract)
	if len(fields) == 0 && len(contract.SourceAlternatives) > 0 {
		fields = append(fields, contract.SourceAlternatives[0]...)
	}
	if len(fields) == 0 {
		if contract.AllowTrue {
			return "myapp = { " + kind + " = true }"
		}
		return "myapp = { " + kind + ` = "myapp" }`
	}
	lines := make([]string, 0, len(fields)+1)
	lines = append(lines, "[tools.myapp."+kind+"]")
	for _, key := range fields {
		lines = append(lines, "  "+key+" = "+placeholderForField(key, contract.Fields[key]))
	}
	return strings.Join(lines, "\n")
}

func placeholderForField(key string, field methodkind.Field) string {
	if len(field.Enum) > 0 {
		return quote(field.Enum[0])
	}
	if literal, ok := placeholderLiteral(key); ok {
		return literal
	}
	switch field.Type {
	case methodkind.Boolean:
		return "true"
	case methodkind.Integer:
		return "1"
	case methodkind.StringList:
		return `["value"]`
	case methodkind.StringMap, methodkind.StringStringMap:
		return `{ key = "value" }`
	case methodkind.Command:
		return `["myapp", "--version"]`
	default:
		return `"value"`
	}
}

func placeholderForSchema(key string, node schemaNode) string {
	if len(node.Enum) > 0 {
		return quote(node.Enum[0])
	}
	if literal, ok := placeholderLiteral(key); ok {
		return literal
	}
	switch node.Type {
	case "boolean":
		return "true"
	case "integer":
		return "1"
	default:
		return `"value"`
	}
}

// placeholderLiteral returns a field-specific value for the keys users meet
// most often, so generated examples stay meaningful.
func placeholderLiteral(key string) (string, bool) {
	switch key {
	case "url":
		return `"https://example.com/myapp-1.0.0.tar.gz"`, true
	case "checksum":
		return `"sha256:` + strings.Repeat("a", 64) + `"`, true
	case "repo":
		return `"owner/myapp"`, true
	case "asset":
		return `"myapp_{os_any}_{arch_any}"`, true
	case "pkg":
		return `"myapp"`, true
	case "product_name":
		return `"MyApp"`, true
	case "publisher":
		return `"CN=Example"`, true
	case "package_id":
		return `"com.example.myapp"`, true
	case "app":
		return `"MyApp.app"`, true
	case "source":
		return `"redis"`, true
	case "build":
		return `"make install"`, true
	case "local_path":
		return `"vendor/myapp"`, true
	case "digest":
		return `"sha256:` + strings.Repeat("a", 64) + `"`, true
	}
	return "", false
}

var methodDocs = map[string]string{
	"native":    schemaDoc + "#naming-a-tool",
	"cargo":     schemaDoc + "#language-ecosystem-managers",
	"git":       schemaDoc + "#git-clone--manual-build",
	"deb":       schemaDoc + "#debian-packages-install-and-track-a-deb",
	"http":      schemaDoc + "#http-download-and-extract-an-artifact-archive-or-binary",
	"local":     schemaDoc + "#localoffline-install-a-vendored-artifact",
	"github":    schemaDoc + "#github-release-assets",
	"container": schemaDoc + "#container-pull-an-image-via-dockerpodman",
	"appimage":  schemaDoc + "#appimage-portable-appimage-binaries",
	"android":   schemaDoc + "#android-hand-an-apk-to-termuss-package-installer",
}

func methodDoc(kind string, contract *methodkind.Contract) string {
	if doc := methodDocs[contract.Kind]; doc != "" {
		return doc
	}
	return schemaDoc + "#method-reference"
}

var errorEntries = buildErrorEntries()

func buildErrorEntries() []Entry {
	codes := []struct {
		code    validate.ErrorCode
		summary string
	}{
		{validate.ErrRequiredField, "A method is missing a field its contract requires, such as http without a source or checksum."},
		{validate.ErrDanglingRef, "requires or requires_when names a tool that the schema does not declare."},
		{validate.ErrCycle, "Tool dependencies form a cycle, so no install order exists."},
		{validate.ErrMalformedURL, "A URL field does not parse as an absolute HTTP(S) URL."},
		{validate.ErrInvalidValue, "A value has the wrong type, an unsupported option, or a conflicting combination."},
		{validate.ErrInvalidChecksum, "A checksum is not sha256:<64 hex>; sha256:auto is the explicit opt-in to resolve one later."},
		{validate.ErrUnsafePackageName, "A package name or artifact path could escape its intended directory."},
		{validate.ErrDupeTool, "A tool is declared twice, typically as a list entry plus an inline table."},
		{validate.ErrSchemaNotFound, "A command needed a schema file and could not find one."},
		{validate.WarnUnknownPlaceholder, "A placeholder is not part of the supported vocabulary and stays literal."},
		{validate.WarnUnknownWhenKey, "A condition uses a key that is not a supported dimension."},
		{validate.WarnUnknownDistroFamily, "distro_family names a clan that depengine never resolves."},
		{validate.WarnAutoChecksum, "The declaration relies on sha256:auto, so the artifact identity is resolved at install time."},
		{validate.WarnEnvMissing, "A declared environment variable is unset."},
		{validate.WarnInertTool, "A tool declaration has no method that can ever match the host."},
		{validate.WarnAmbiguousCondition, "A condition can match more than one candidate equally."},
	}
	out := make([]Entry, 0, len(codes)+6)
	for _, item := range codes {
		out = append(out, Entry{
			Term:    string(item.code),
			Group:   GroupErrorCode,
			Where:   "validate, install --dry-run",
			Summary: item.summary,
			Form:    "validate --json findings[].code",
			Doc:     cliDoc + "#depengine-validate-flags",
		})
	}
	classes := []struct {
		class   plan.ErrorClass
		summary string
	}{
		{plan.ErrorInvalidManifest, "The declaration cannot become a resolved plan; fix schema findings first."},
		{plan.ErrorUnsupportedCapability, "The selected method cannot express part of the intent, such as a scope or environment the adapter lacks."},
		{plan.ErrorUnavailableCandidate, "Every candidate was rejected by its condition, a missing tool, or its environment."},
		{plan.ErrorResolutionFailure, "A remote lookup failed or returned no usable answer."},
		{plan.ErrorAuthRequirement, "A candidate needs credentials that were not provided."},
		{plan.ErrorHostIncompatibility, "The host cannot run the selected method, for example a missing manager or unsupported platform."},
	}
	for _, item := range classes {
		out = append(out, Entry{
			Term:    string(item.class),
			Group:   GroupErrorClass,
			Where:   "resolution (why, install, update)",
			Summary: item.summary,
			Form:    "plan error class",
			Doc:     troubleshootDoc,
		})
	}
	return out
}

func quoted(values []string) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = quote(value)
	}
	return out
}

func quote(value string) string { return `"` + value + `"` }

// editDistance is the Levenshtein distance between a and b.
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
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

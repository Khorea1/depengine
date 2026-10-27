package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Catalog environment and layout constants. The directory name is shared with
// docs/design/adr-009-catalog-layout.md.
const (
	CatalogEnv       = "DEPENGINE_CATALOG"
	catalogDirName   = "depengine"
	catalogSubdir    = "catalog"
	catalogExtension = ".toml"
)

var (
	// ErrRecipeNotFound means the name is valid but no catalog root contains a
	// recipe for it.
	ErrRecipeNotFound = errors.New("catalog recipe not found")
	// ErrRecipeNotAddressable means the name cannot be mapped to a portable
	// catalog file name.
	ErrRecipeNotAddressable = errors.New("catalog name is not addressable")
)

// CatalogError marks catalog configuration and lookup failures so the CLI can
// map them to the schema/configuration exit code rather than a generic failure.
type CatalogError struct{ Err error }

func (e *CatalogError) Error() string { return "catalog: " + e.Err.Error() }
func (e *CatalogError) Unwrap() error { return e.Err }

// catalogNamePattern is the portable name grammar from ADR-009. It deliberately
// excludes path separators, colons, spaces, control characters and non-ASCII
// text so a name is one path segment on every supported platform.
var catalogNamePattern = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$")

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateCatalogName reports whether name can be stored as <name>.toml.
func ValidateCatalogName(name string) error {
	if !catalogNamePattern.MatchString(name) {
		return fmt.Errorf("name %q must match %s", name, catalogNamePattern.String())
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("name %q must not end with a dot", name)
	}
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if windowsReservedNames[strings.ToUpper(stem)] {
		return fmt.Errorf("name %q is a reserved Windows device name", name)
	}
	return nil
}

// Catalog is an ordered set of recipe roots. The first root containing a name
// wins; roots are not field-merged.
type Catalog struct {
	roots []string
}

// NewCatalog returns a catalog over roots. A nil or empty catalog is valid and
// contributes no recipes.
func NewCatalog(roots []string) *Catalog {
	return &Catalog{roots: append([]string(nil), roots...)}
}

// Roots returns a copy of the ordered roots.
func (c *Catalog) Roots() []string {
	if c == nil {
		return nil
	}
	return append([]string(nil), c.roots...)
}

// Recipe is one canonical catalog entry. Schema is parsed without host facts,
// so placeholders remain literal and the candidate sequence is host-independent.
// Call Specialize before merging into a host-aware project.
type Recipe struct {
	Name      string
	Path      string
	RootIndex int
	Schema    *Schema
	// Digest is the canonical content digest of the recipe as written, before
	// host placeholder expansion. It verifies; a catalog revision only retrieves.
	Digest string
}

// Specialize parses the recipe again with host facts. The recipe file is
// re-read so lookup stays host-independent while execution sees expanded
// placeholders and arch/os aliases.
func (r *Recipe) Specialize(facts map[string]string) (*Schema, error) {
	if r == nil {
		return nil, &CatalogError{Err: errors.New("nil recipe")}
	}
	s, err := ParseRecipe(r.Path, facts)
	if err != nil {
		return nil, &CatalogError{Err: err}
	}
	if _, ok := s.Tools[r.Name]; !ok {
		return nil, &CatalogError{Err: fmt.Errorf("recipe %s no longer declares %q", r.Path, r.Name)}
	}
	return s, nil
}

// Lookup returns the canonical recipe for name from the first root that
// contains it. It never lists a directory.
func (c *Catalog) Lookup(name string) (*Recipe, error) {
	if err := ValidateCatalogName(name); err != nil {
		return nil, &CatalogError{Err: fmt.Errorf("%w: %w", ErrRecipeNotAddressable, err)}
	}
	if c == nil || len(c.roots) == 0 {
		return nil, &CatalogError{Err: fmt.Errorf("%w: %q", ErrRecipeNotFound, name)}
	}
	for i, root := range c.roots {
		path := filepath.Join(root, name+catalogExtension)
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, &CatalogError{Err: fmt.Errorf("root %d (%s): stat %s: %w", i, root, path, err)}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, &CatalogError{Err: fmt.Errorf("root %d (%s): recipe %s is a symlink; catalog recipes must be regular files", i, root, path)}
		}
		if !info.Mode().IsRegular() {
			return nil, &CatalogError{Err: fmt.Errorf("root %d (%s): recipe %s is not a regular file", i, root, path)}
		}
		raw, err := os.ReadFile(path) // #nosec G304 -- path is <validated root>/<validated name>.toml.
		if err != nil {
			return nil, &CatalogError{Err: fmt.Errorf("root %d (%s): read %s: %w", i, root, path, err)}
		}
		s, err := ParseRecipeBytes(path, raw, nil)
		if err != nil {
			return nil, &CatalogError{Err: fmt.Errorf("root %d (%s): %w", i, root, err)}
		}
		if _, ok := s.Tools[name]; !ok {
			return nil, &CatalogError{Err: fmt.Errorf(
				"root %d (%s): recipe %s declares %q, not %q; file name and recipe name must match",
				i, root, path, singleToolName(s), name,
			)}
		}
		digest, err := canonicalRecipeDigest(raw)
		if err != nil {
			return nil, &CatalogError{Err: fmt.Errorf("root %d (%s): %w", i, root, err)}
		}
		return &Recipe{Name: name, Path: path, RootIndex: i, Schema: s, Digest: digest}, nil
	}
	return nil, &CatalogError{Err: fmt.Errorf("%w: %q (roots: %s)", ErrRecipeNotFound, name, strings.Join(c.roots, string(os.PathListSeparator)))}
}

func singleToolName(s *Schema) string {
	if s == nil || len(s.Tools) != 1 {
		return ""
	}
	for name := range s.Tools {
		return name
	}
	return ""
}

// DefaultCatalogRoots returns the platform default root, or nil when the
// platform config directory cannot be determined.
func DefaultCatalogRoots() []string {
	base := catalogBaseDir(runtime.GOOS, os.Getenv("HOME"), os.Getenv("XDG_CONFIG_HOME"), os.Getenv("AppData"))
	if base == "" {
		return nil
	}
	return []string{filepath.Join(base, catalogDirName, catalogSubdir)}
}

// catalogBaseDir is pure so platform branches can be tested on one host.
func catalogBaseDir(goos, home, xdg, appData string) string {
	switch goos {
	case "windows":
		if filepath.IsAbs(appData) {
			return appData
		}
		return ""
	case "darwin":
		if filepath.IsAbs(home) {
			return filepath.Join(home, "Library", "Application Support")
		}
		return ""
	default:
		if filepath.IsAbs(xdg) {
			return xdg
		}
		if filepath.IsAbs(home) {
			return filepath.Join(home, ".config")
		}
		return ""
	}
}

// ResolveCatalogRoots applies the ADR-009 precedence: --catalog, then
// DEPENGINE_CATALOG, then the platform default. Explicit roots are validated
// eagerly; the default root is allowed to be absent.
func ResolveCatalogRoots(flagRoots []string, noCatalog bool) ([]string, error) {
	if noCatalog {
		if len(flagRoots) > 0 {
			return nil, &CatalogError{Err: errors.New("--catalog and --no-catalog are mutually exclusive")}
		}
		return nil, nil
	}
	if len(flagRoots) > 0 {
		return normalizeCatalogRoots(flagRoots, true)
	}
	if env := os.Getenv(CatalogEnv); env != "" {
		return normalizeCatalogRoots(filepath.SplitList(env), true)
	}
	return normalizeCatalogRoots(DefaultCatalogRoots(), false)
}

func normalizeCatalogRoots(raw []string, explicit bool) ([]string, error) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		if value == "" {
			return nil, &CatalogError{Err: errors.New("catalog root must not be empty")}
		}
		path := ExpandHomeDir(value)
		if !filepath.IsAbs(path) {
			return nil, &CatalogError{Err: fmt.Errorf("catalog root %q must be absolute", value)}
		}
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		if explicit {
			info, err := os.Stat(path) // #nosec G703 -- catalog roots are operator-selected configuration paths.
			if err != nil {
				return nil, &CatalogError{Err: fmt.Errorf("catalog root %s: %w", path, err)}
			}
			if !info.IsDir() {
				return nil, &CatalogError{Err: fmt.Errorf("catalog root %s is not a directory", path)}
			}
		}
		seen[path] = true
		out = append(out, path)
	}
	return out, nil
}

// LayerResolution is the merged project configuration plus the layer counts
// needed by command output.
type LayerResolution struct {
	Schema        *Schema
	ManifestCount int
	CatalogCount  int
	// Warnings reports non-fatal layer findings that callers should surface.
	Warnings []LayerWarning
}

// LayerWarning is one non-fatal resolution finding tied to a tool.
type LayerWarning struct {
	Tool    string
	Message string
}

// ResolveLayers is the single resolution point for project schema, personal
// manifest and catalog layers. It filters manifest-only tools, resolves
// catalog recipes for project-declared names and their requires closure, and
// merges catalog -> manifest -> project schema.
func ResolveLayers(schema, manifest *Schema, cat *Catalog, facts map[string]string, provenance bool) (*LayerResolution, error) {
	if schema == nil {
		return nil, &CatalogError{Err: errors.New("project schema is nil")}
	}

	manifestCount := 0
	if manifest != nil {
		FilterManifestTools(schema, manifest)
		if err := ValidateManifestLayer(manifest); err != nil {
			return nil, err
		}
		if err := ValidateManifestNewTools(schema, manifest); err != nil {
			return nil, err
		}
		manifestCount = len(manifest.Tools)
	}

	catalogSchema, catalogCount, err := resolveCatalogLayer(schema, manifest, cat, facts)
	if err != nil {
		return nil, err
	}

	// Name the real layers so provenance reports where a field came from
	// instead of inferring it from the merge position.
	schema.layer = LayerSchema
	if manifest != nil {
		manifest.layer = LayerManifest
	}
	if catalogSchema != nil {
		catalogSchema.layer = LayerCatalog
	}

	layers := make([]*Schema, 0, 3)
	if catalogSchema != nil {
		layers = append(layers, catalogSchema)
	}
	if manifestCount > 0 {
		layers = append(layers, manifest)
	}
	layers = append(layers, schema)

	var merged *Schema
	if provenance {
		merged = MergeLayersWithProvenance(layers...)
	} else {
		merged = MergeLayers(layers...)
	}
	var catalogRoots []string
	if cat != nil {
		catalogRoots = cat.Roots()
	}
	return &LayerResolution{
		Schema:        merged,
		ManifestCount: manifestCount,
		CatalogCount:  catalogCount,
		Warnings:      inertToolWarnings(schema, merged, catalogRoots),
	}, nil
}

// inertToolWarnings flags project-declared tools that resolve to no methods and
// no dependencies. Such a tool is a no-op: it has neither a local definition
// nor a catalog recipe to supply one, so the project author most likely meant
// to install it.
func inertToolWarnings(declared, merged *Schema, catalogRoots []string) []LayerWarning {
	if declared == nil || merged == nil {
		return nil
	}
	var warnings []LayerWarning
	for _, name := range sortedToolNames(declared.Tools) {
		tool := merged.Tools[name]
		if tool == nil || len(tool.Methods) > 0 {
			continue
		}
		// An explicitly empty requires/requires_when is a deliberate marker
		// for a non-installable tool, not a missing recipe.
		if len(tool.Requires) > 0 || len(tool.RequiresWhen) > 0 ||
			fieldDeclared(tool, "Requires") || fieldDeclared(tool, "RequiresWhen") {
			continue
		}
		message := fmt.Sprintf("tool %q declares no install method and no requires, so it will not be installed", name)
		if len(tool.MethodOnly) > 0 {
			message = fmt.Sprintf("tool %q restricts methods to %s but declares none", name, strings.Join(tool.MethodOnly, ", "))
		}
		if len(catalogRoots) > 0 {
			message += fmt.Sprintf("; no catalog recipe supplied it (roots: %s)", strings.Join(catalogRoots, string(os.PathListSeparator)))
		}
		message += ". Add a method, a requires, or a catalog recipe."
		warnings = append(warnings, LayerWarning{Tool: name, Message: message})
	}
	return warnings
}

func resolveCatalogLayer(schema, manifest *Schema, cat *Catalog, facts map[string]string) (*Schema, int, error) {
	if cat == nil || len(cat.Roots()) == 0 {
		return nil, 0, nil
	}

	local := make(map[string]bool, len(schema.Tools))
	for name := range schema.Tools {
		local[name] = true
	}
	manifestOnly := make(map[string]bool)
	if manifest != nil {
		for name := range manifest.Tools {
			local[name] = true
			if _, ok := schema.Tools[name]; !ok {
				manifestOnly[name] = true
			}
		}
	}

	recipes := make(map[string]*Tool)
	attempted := make(map[string]bool)
	pending := sortedToolNames(schema.Tools)

	for len(pending) > 0 {
		for _, name := range pending {
			if attempted[name] {
				continue
			}
			attempted[name] = true

			// A tool that exists only because allow_new_tools admitted it must
			// be fully defined by the manifest. Its dependencies are still
			// discovered from the effective merged tool below.
			if manifestOnly[name] {
				continue
			}

			recipe, err := cat.Lookup(name)
			if err != nil {
				if errors.Is(err, ErrRecipeNotFound) || errors.Is(err, ErrRecipeNotAddressable) {
					if local[name] {
						continue
					}
					return nil, 0, fmt.Errorf("%w: dependency %q is not defined locally and has no recipe", err, name)
				}
				return nil, 0, err
			}

			specialized, err := recipe.Specialize(facts)
			if err != nil {
				return nil, 0, err
			}
			tool := specialized.Tools[name]
			if tool == nil {
				return nil, 0, &CatalogError{Err: fmt.Errorf("recipe %s does not declare %q", recipe.Path, name)}
			}
			if _, inSchema := schema.Tools[name]; !inSchema {
				tool = cloneTool(tool)
				tool.DependencyOnly = true
			}
			tool.Recipe = &RecipeOrigin{
				Name:      name,
				Root:      catalogRoot(cat, recipe.RootIndex),
				RootIndex: recipe.RootIndex,
				Path:      recipe.Path,
				Digest:    recipe.Digest,
			}
			recipes[name] = tool
		}

		if len(recipes) == 0 {
			break
		}

		// Walk the effective merged tools, not the raw catalog recipes: a
		// schema or manifest value can override the catalog's requires.
		catalogSchema := &Schema{Version: schema.Version, Tools: recipes, ProjectRoot: schema.ProjectRoot, layer: LayerCatalog}
		layers := []*Schema{catalogSchema}
		if manifest != nil && len(manifest.Tools) > 0 {
			layers = append(layers, manifest)
		}
		layers = append(layers, schema)
		merged := MergeLayers(layers...)

		var next []string
		for _, tool := range merged.Tools {
			for _, dependency := range toolRequires(tool) {
				if !local[dependency] && recipes[dependency] == nil && !attempted[dependency] {
					next = append(next, dependency)
				}
			}
		}
		pending = uniqueSorted(next)
	}

	if len(recipes) == 0 {
		return nil, 0, nil
	}
	return &Schema{Version: schema.Version, Tools: recipes, ProjectRoot: schema.ProjectRoot, layer: LayerCatalog}, len(recipes), nil
}

// catalogRoot names the winning root of a recipe lookup. The index is the
// recipe's position in the ordered root list.
func catalogRoot(cat *Catalog, index int) string {
	if cat == nil {
		return ""
	}
	roots := cat.Roots()
	if index < 0 || index >= len(roots) {
		return ""
	}
	return roots[index]
}

// canonicalRecipeDigest hashes a stable JSON projection of the TOML document.
// encoding/json sorts map keys, and the TOML decoder drops comments and
// whitespace, so the same recipe hashes identically on every host. Placeholders
// remain literal because the raw document is hashed, not a host-expanded parse.
func canonicalRecipeDigest(raw []byte) (string, error) {
	var document map[string]any
	if err := toml.Unmarshal(raw, &document); err != nil {
		return "", fmt.Errorf("canonicalize recipe: %w", err)
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("canonicalize recipe: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func uniqueSorted(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	out := names[:0]
	for _, name := range names {
		if len(out) == 0 || out[len(out)-1] != name {
			out = append(out, name)
		}
	}
	return out
}

func toolRequires(tool *Tool) []string {
	if tool == nil {
		return nil
	}
	out := make([]string, 0, len(tool.Requires))
	out = append(out, tool.Requires...)
	for _, method := range tool.Methods {
		if method != nil {
			out = append(out, method.Requires...)
		}
	}
	return out
}

func sortedToolNames(tools map[string]*Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

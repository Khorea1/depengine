// Corpus authoring helpers for the pinned real-recipe sample. The only CLI entrypoint
// lives in recipe-corpus-metrics.go so `go build ./...` sees one command per package.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/Khorea1/depengine/internal/app"
	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/validate"
)

type corpusSources struct {
	Sources []corpusSource `json:"sources"`
}

type corpusSource struct {
	Manager              string            `json:"manager"`
	Repository           string            `json:"repository"`
	Revision             string            `json:"revision"`
	Path                 string            `json:"path"`
	URL                  string            `json:"url"`
	RepositoryLicense    string            `json:"repository_license"`
	RepositoryLicenseURL string            `json:"repository_license_url"`
	LicenseFile          string            `json:"license_file"`
	LicenseSHA256        string            `json:"license_sha256"`
	LocalPath            string            `json:"local_path"`
	SHA256               string            `json:"sha256"`
	Files                map[string]string `json:"files"`
}

type scoopManifest struct {
	Version      string              `json:"version"`
	Architecture map[string]artifact `json:"architecture"`
	Bin          string              `json:"bin"`
}

type artifact struct {
	URL        string `json:"url"`
	Hash       string `json:"hash"`
	ExtractDir string `json:"extract_dir"`
}

type variant struct {
	upstream string
	arch     string
}

var scoopVariants = []variant{{"64bit", "x86_64"}, {"32bit", "x86"}, {"arm64", "aarch64"}}

func readCorpusFile(root, name string) ([]byte, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("corpus path %q must be relative and local", name)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	file, err := dir.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func loadCorpusSources(root string) (corpusSources, error) {
	data, err := readCorpusFile(root, "sources.json")
	if err != nil {
		return corpusSources{}, err
	}
	var sources corpusSources
	if err := json.Unmarshal(data, &sources); err != nil {
		return corpusSources{}, err
	}
	return sources, nil
}

// verifyCorpusSources checks the pinned upstream files and retained repository licenses.
// Counts deliberately keep recipe source files separate from license texts.
func verifyCorpusSources(root string, print bool) (recipeFiles, recipeLines, licenseFiles, licenseLines int, err error) {
	sources, err := loadCorpusSources(root)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	seenLicenses := make(map[string]bool)
	for _, source := range sources.Sources {
		if source.URL == "" || source.Revision == "" || source.Path == "" || source.RepositoryLicense == "" || source.RepositoryLicenseURL == "" {
			return 0, 0, 0, 0, fmt.Errorf("incomplete source/license provenance for %s", source.Manager)
		}
		if !seenLicenses[source.LicenseFile] {
			if source.LicenseFile == "" || source.LicenseSHA256 == "" {
				return 0, 0, 0, 0, fmt.Errorf("missing retained license for %s", source.Repository)
			}
			count, err := verifyCorpusFile(root, source.LicenseFile, source.LicenseSHA256)
			if err != nil {
				return 0, 0, 0, 0, err
			}
			licenseFiles++
			licenseLines += count
			seenLicenses[source.LicenseFile] = true
			if print {
				fmt.Printf("license %-34s lines=%d sha256=%s (%s)\n", source.LicenseFile, count, source.LicenseSHA256, source.RepositoryLicense)
			}
		}
		if len(source.Files) == 0 {
			count, err := verifyCorpusFile(root, source.LocalPath, source.SHA256)
			if err != nil {
				return 0, 0, 0, 0, err
			}
			recipeFiles++
			recipeLines += count
			if print {
				fmt.Printf("source  %-34s lines=%d sha256=%s\n", source.LocalPath, count, source.SHA256)
			}
			continue
		}
		for name, digest := range source.Files {
			local := filepath.Join(source.LocalPath, name)
			count, err := verifyCorpusFile(root, local, digest)
			if err != nil {
				return 0, 0, 0, 0, err
			}
			recipeFiles++
			recipeLines += count
			if print {
				fmt.Printf("source  %-34s lines=%d sha256=%s\n", local, count, digest)
			}
		}
	}
	return recipeFiles, recipeLines, licenseFiles, licenseLines, nil
}

func verifyCorpusFile(root, name, digest string) (int, error) {
	data, err := readCorpusFile(root, name)
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != digest {
		return 0, fmt.Errorf("%s SHA-256=%s, want %s", name, got, digest)
	}
	return strings.Count(string(data), "\n"), nil
}

func generateCorpusRecipes(root string, writeFiles bool) error {
	if _, _, _, _, err := verifyCorpusSources(root, false); err != nil {
		return err
	}
	sources, err := loadCorpusSources(root)
	if err != nil {
		return err
	}
	recipes := make(map[string]scoopManifest)
	provenance := make(map[string]corpusSource)
	for _, source := range sources.Sources {
		if source.Manager != "Scoop" {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(source.Path), filepath.Ext(source.Path))
		raw, err := readCorpusFile(root, source.LocalPath)
		if err != nil {
			return err
		}
		if err := rejectDuplicateArchitectureKeys(raw); err != nil {
			return fmt.Errorf("%s: %w", source.URL, err)
		}
		var recipe scoopManifest
		if err := json.Unmarshal(raw, &recipe); err != nil {
			return fmt.Errorf("%s: %w", source.URL, err)
		}
		if recipe.Version == "" || recipe.Bin == "" {
			return fmt.Errorf("%s: missing version or bin entrypoint", source.URL)
		}
		for key := range recipe.Architecture {
			if !knownScoopVariant(key) {
				return fmt.Errorf("%s: unsupported architecture variant %q", source.URL, key)
			}
		}
		for _, item := range scoopVariants {
			if artifact, ok := recipe.Architecture[item.upstream]; ok && (artifact.URL == "" || artifact.Hash == "" || artifact.ExtractDir == "") {
				return fmt.Errorf("%s: incomplete %s artifact", source.URL, item.upstream)
			}
		}
		recipes[name] = recipe
		provenance[name] = source
	}
	var enumerated strings.Builder
	enumerated.WriteString("schema_version = 1\n")
	catalog := make(map[string][]byte, len(recipes))
	names := make([]string, 0, len(recipes))
	for name := range recipes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		recipe := recipes[name]
		if err := appendRecipeProjection(&enumerated, name, recipe, "tools"); err != nil {
			return err
		}
		var recipeTOML strings.Builder
		recipeTOML.WriteString("recipe_version = 1\n")
		source := provenance[name]
		fmt.Fprintf(&recipeTOML, "# Source: %s\n# Revision: %s\n# SHA-256: %s\n", source.URL, source.Revision, source.SHA256)
		if err := appendRecipeProjection(&recipeTOML, name, recipe, "packages"); err != nil {
			return err
		}
		catalog[name] = []byte(recipeTOML.String())
	}
	if err := putCanonical(root, "enumerated.toml", []byte(enumerated.String()), writeFiles); err != nil {
		return err
	}
	for _, name := range names {
		if err := putCanonical(root, filepath.Join("catalog", name+".toml"), catalog[name], writeFiles); err != nil {
			return err
		}
	}
	return validateProjectionParity(root)
}

func appendRecipeProjection(out *strings.Builder, name string, recipe scoopManifest, section string) error {
	binary := filepath.Base(filepath.ToSlash(recipe.Bin))
	if binary == "." || strings.ContainsAny(binary, "\r\n\"{}") || strings.ContainsAny(recipe.Bin, "\\") {
		return fmt.Errorf("%s: unsupported bin path %q", name, recipe.Bin)
	}
	command := strings.TrimSuffix(binary, filepath.Ext(binary))
	if command == "" || strings.ContainsAny(command, "\r\n\"{}") {
		return fmt.Errorf("%s: invalid command name derived from bin %q", name, recipe.Bin)
	}
	for _, item := range scoopVariants {
		a, ok := recipe.Architecture[item.upstream]
		if !ok {
			continue
		}
		if strings.ContainsAny(a.URL+a.Hash+a.ExtractDir, "\r\n\"") || filepath.IsAbs(a.ExtractDir) || strings.Contains(filepath.ToSlash(a.ExtractDir), "../") {
			return fmt.Errorf("%s: unsafe Scoop %s artifact fields", name, item.upstream)
		}
		components := strings.Split(strings.Trim(filepath.ToSlash(a.ExtractDir), "/"), "/")
		if len(components) == 0 || components[0] == "" {
			return fmt.Errorf("%s: empty Scoop extract_dir for %s", name, item.upstream)
		}
		if len(a.Hash) != 64 {
			return fmt.Errorf("%s: invalid SHA-256 length for %s", name, item.upstream)
		}
		for _, char := range a.Hash {
			if !strings.ContainsRune("0123456789abcdefABCDEF", char) {
				return fmt.Errorf("%s: invalid SHA-256 for %s", name, item.upstream)
			}
		}
		fmt.Fprintf(out, "\n[%s.%s.scoop-%s]\nkind = \"http\"\nurl = %q\nchecksum = %q\nscope = \"user\"\nstrip_components = %d\nentrypoints = { %s = %q }\nwhen = { os = [\"windows\"], arch = [%q] }\n", section, name, item.upstream, a.URL, "sha256:"+a.Hash, len(components), command, binary, item.arch)
	}
	return nil
}

func validateProjectionParity(root string) error {
	enumerated, err := config.ParseProjectSchema(filepath.Join(root, "enumerated.toml"), nil)
	if err != nil {
		return err
	}
	intent, err := config.ParseProjectSchema(filepath.Join(root, "intent.toml"), nil)
	if err != nil {
		return err
	}
	catalogRoot, err := filepath.Abs(filepath.Join(root, "catalog"))
	if err != nil {
		return err
	}
	before, err := config.ResolveLayers(enumerated, nil, nil, nil, false)
	if err != nil {
		return err
	}
	after, err := config.ResolveLayers(intent, nil, config.NewCatalog([]string{catalogRoot}, planner.ValidateDeclarativeRecipe), nil, false)
	if err != nil {
		return err
	}
	if err := validateCorpusSchema(before.Schema); err != nil {
		return fmt.Errorf("enumerated projection: %w", err)
	}
	if err := validateCorpusSchema(after.Schema); err != nil {
		return fmt.Errorf("catalog projection: %w", err)
	}
	if len(before.Schema.Tools) != len(after.Schema.Tools) {
		return fmt.Errorf("resolved tool count differs: enumerated=%d catalog=%d", len(before.Schema.Tools), len(after.Schema.Tools))
	}
	for name, tool := range before.Schema.Tools {
		other := after.Schema.Tools[name]
		if other == nil || len(tool.Methods) != len(other.Methods) {
			return fmt.Errorf("resolved tool %s differs between projections", name)
		}
		for i, method := range tool.Methods {
			candidate := other.Methods[i]
			if method.Kind != candidate.Kind || method.Label != candidate.Label || !reflect.DeepEqual(method.Config, candidate.Config) || method.When.String() != candidate.When.String() {
				return fmt.Errorf("resolved tool %s candidate %d differs between projections", name, i)
			}
			for _, arch := range []string{"x86_64", "x86", "aarch64"} {
				facts := &platform.Facts{OS: "windows", TargetArch: arch}
				if method.When.Match(facts) != (method.Label == "scoop-64bit" && arch == "x86_64" || method.Label == "scoop-32bit" && arch == "x86" || method.Label == "scoop-arm64" && arch == "aarch64") {
					return fmt.Errorf("tool %s candidate %q has incorrect Windows architecture applicability for %s", name, method.Label, arch)
				}
			}
		}
	}
	return nil
}

var corpusAdaptersOnce sync.Once

func validateCorpusSchema(schema *config.Schema) error {
	corpusAdaptersOnce.Do(app.InitAdapters)
	if err := config.ValidateEffectiveIntegrity(schema); err != nil {
		return err
	}
	result := validate.ValidateSchema(schema, exec.RegisteredKinds())
	if result.HasErrors() {
		return result.Errors[0]
	}
	return nil
}

func rejectDuplicateArchitectureKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if _, err := decoder.Token(); err != nil {
		return err
	}
	seenArchitecture := false
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, _ := keyToken.(string)
		if key != "architecture" {
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return err
			}
			continue
		}
		if seenArchitecture {
			return fmt.Errorf("duplicate architecture object")
		}
		seenArchitecture = true
		if _, err := decoder.Token(); err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(scoopVariants))
		for decoder.More() {
			variantToken, err := decoder.Token()
			if err != nil {
				return err
			}
			variant, _ := variantToken.(string)
			if _, duplicate := seen[variant]; duplicate {
				return fmt.Errorf("duplicate architecture variant %q", variant)
			}
			seen[variant] = struct{}{}
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
	}
	return nil
}

func knownScoopVariant(key string) bool {
	for _, item := range scoopVariants {
		if key == item.upstream {
			return true
		}
	}
	return false
}

func putCanonical(root, name string, want []byte, writeFiles bool) error {
	path := filepath.Join(root, name)
	if writeFiles {
		if !filepath.IsLocal(name) {
			return fmt.Errorf("generated corpus path %q must be relative and local", name)
		}
		dir, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer dir.Close()
		file, err := dir.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if _, err := file.Write(want); err != nil {
			_ = file.Close()
			return err
		}
		return file.Close()
	}
	got, err := readCorpusFile(root, name)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("%s differs from canonical generated TOML; run go run ./scripts -write", path)
	}
	return nil
}
